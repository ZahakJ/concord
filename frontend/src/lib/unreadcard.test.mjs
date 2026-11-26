// Zero-dependency test for the unread card's sync (`npm test` runs it).
import { summary, createUnreadSync, MAX_CONVERSATIONS, MAX_COUNT, DEBOUNCE_MS } from "./unreadcard.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};
const a = "0123456789abcdef0123456789abcdef";
const b = "fedcba9876543210fedcba9876543210";

// U1: counts only, for conversations with something unread, capped.
{
  const s = summary({ [a]: { count: 3, mentions: 1 }, [b]: { count: 0, mentions: 0 }, "x y": { count: 2 }, z: { count: 1e9 } });
  assert(JSON.stringify(s) === JSON.stringify({ counts: { [a]: 3, z: MAX_COUNT } }), `summary: ${JSON.stringify(s)}`);
  assert(Object.keys(summary(null).counts).length === 0, "nothing for no state");
  const many = {};
  for (let i = 0; i < MAX_CONVERSATIONS + 50; i++) many[`c${i}`] = { count: 1 };
  assert(Object.keys(summary(many).counts).length === MAX_CONVERSATIONS, "conversations capped");
  const long = "a".repeat(129);
  assert(summary({ [long]: { count: 1 } }).counts[long] === undefined, "an over-long id is dropped");
}

function fakeTimers() {
  const q = new Map();
  let next = 1;
  return {
    setTimer: (fn, ms) => {
      assert(ms === DEBOUNCE_MS, "debounced a second");
      q.set(next, fn);
      return next++;
    },
    clearTimer: (t) => q.delete(t),
    run() {
      const fns = [...q.values()];
      q.clear();
      fns.forEach((f) => f());
    },
    pending: () => q.size,
  };
}

// U2: nothing before the page has counted; then one call per burst; identical ones once.
{
  const sent = [];
  let unread = { [a]: { count: 9 } };
  const t = fakeTimers();
  const sync = createUnreadSync({ read: () => unread, send: (s) => sent.push(s), ...t });
  sync.changed();
  assert(t.pending() === 0 && sync.flushNow() === false && sent.length === 0, "a change before the first count sends nothing");
  sync.counted();
  unread = { [a]: { count: 10 } };
  sync.changed();
  sync.changed();
  assert(t.pending() === 1, "a burst is one timer");
  t.run();
  assert(sent.length === 1 && sent[0].counts[a] === 10, "the burst's last state goes out");
  sync.changed();
  t.run();
  assert(sent.length === 1, "the same summary is not sent twice");
  unread = {};
  assert(sync.flushNow() === true && sent.length === 2 && Object.keys(sent[1].counts).length === 0, "going to the background sends at once, and an empty summary clears");
}

if (failures) {
  console.error(`${failures} failure(s)`);
  process.exit(1);
}
console.log("unreadcard: ok");
