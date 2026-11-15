// Zero-dependency test for the link-card wait (`npm test`): a message waits for
// its card only until the card comes, the deadline passes, or anything else is
// sent into its channel, and in every case it goes out exactly once and first.
import { makeCardGate } from "./cardgate.js";

let failures = 0;
const eq = (got, want, msg) => {
  if (JSON.stringify(got) !== JSON.stringify(want)) {
    console.error(`FAIL: ${msg}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
    failures++;
  }
};

// A clock the test moves by hand.
function fakeClock() {
  let now = 0;
  let timers = [];
  return {
    setTimer(fn, ms) {
      const t = { at: now + ms, fn };
      timers.push(t);
      return t;
    },
    clearTimer(t) {
      timers = timers.filter((x) => x !== t);
    },
    advance(ms) {
      now += ms;
      const due = timers.filter((t) => t.at <= now);
      timers = timers.filter((t) => t.at > now);
      for (const t of due) t.fn();
    },
  };
}
const flush = () => new Promise((r) => setTimeout(r, 0));
const later = () => {
  let resolve, reject;
  const p = new Promise((a, b) => ((resolve = a), (reject = b)));
  return { p, resolve, reject };
};

// The card comes in time: it goes out with the message.
{
  const clock = fakeClock();
  const gate = makeCardGate({ waitMs: 2000, ...clock });
  const log = [];
  const f = later();
  gate.hold("ch", () => f.p, (b) => log.push(`A:${b}`));
  eq(gate.holding("ch"), true, "waiting while the card is fetched");
  f.resolve("[T](https://x.org)");
  await flush();
  eq(log, ["A:[T](https://x.org)"], "the card rides the message");
  eq(gate.holding("ch"), false, "nothing waits after");
}

// The deadline: the message goes out bare, once, and a late card is dropped.
{
  const clock = fakeClock();
  const gate = makeCardGate({ waitMs: 2000, ...clock });
  const log = [];
  const f = later();
  gate.hold("ch", () => f.p, (b) => log.push(`A:${b}`));
  clock.advance(1999);
  eq(log, [], "still waiting just before the deadline");
  clock.advance(1);
  eq(log, ["A:"], "bare at the deadline");
  f.resolve("[T](https://x.org)");
  await flush();
  eq(log, ["A:"], "a card after the deadline is not sent again");
}

// The next send into the same channel ends the wait: the waiting message goes
// out first, as typed, then the new one. This is the order rule; the old queue
// made the second wait instead, and a hung first send held the channel.
{
  const clock = fakeClock();
  const gate = makeCardGate({ waitMs: 2000, ...clock });
  const log = [];
  const f = later();
  gate.hold("ch", () => f.p, (b) => log.push(`A:${b}`));
  // What the outbox and api.js do for any later send into "ch".
  gate.release("ch");
  log.push("B");
  eq(log, ["A:", "B"], "the waiting message first, bare, then the next");
  f.resolve("[T](https://x.org)");
  await flush();
  eq(log, ["A:", "B"], "and never again");
}

// A second link message while the first waits: the first goes bare, the
// second waits for its own card.
{
  const clock = fakeClock();
  const gate = makeCardGate({ waitMs: 2000, ...clock });
  const log = [];
  const f1 = later();
  const f2 = later();
  gate.hold("ch", () => f1.p, (b) => log.push(`A:${b}`));
  gate.hold("ch", () => f2.p, (b) => log.push(`B:${b}`));
  eq(log, ["A:"], "first released by the second");
  f2.resolve("card2");
  f1.resolve("card1");
  await flush();
  eq(log, ["A:", "B:card2"], "second keeps its card");
}

// Channels are independent: a send elsewhere does not end this wait.
{
  const clock = fakeClock();
  const gate = makeCardGate({ waitMs: 2000, ...clock });
  const log = [];
  const f = later();
  gate.hold("ch1", () => f.p, (b) => log.push(`A:${b}`));
  gate.release("ch2");
  eq(log, [], "another channel's send leaves it waiting");
  gate.releaseAll();
  eq(log, ["A:"], "releaseAll (the app going to the background) sends it");
}

// A fetch that fails, throws, or answers nonsense: bare, at once.
{
  const clock = fakeClock();
  const gate = makeCardGate({ waitMs: 2000, ...clock });
  const log = [];
  gate.hold("a", () => Promise.reject(new Error("x")), (b) => log.push(`a:${b}`));
  gate.hold("b", () => {
    throw new Error("sync");
  }, (b) => log.push(`b:${b}`));
  gate.hold("c", () => Promise.resolve({ block: 1 }), (b) => log.push(`c:${b}`));
  await flush();
  eq(log.sort(), ["a:", "b:", "c:"], "failures send the words as typed");
}

if (failures) {
  console.error(`cardgate: ${failures} failure(s)`);
  process.exit(1);
}
console.log("cardgate: all tests passed");
