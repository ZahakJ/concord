// Run with: node lib/visibility.test.mjs
import { createVisibilityReporter, hideDelay } from "./visibility.js";

let fails = 0;
const ok = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    fails++;
  }
};

// A fake clock, so the debounce is asserted rather than waited out.
function harness({ delay } = {}) {
  const sent = [];
  let seq = 0;
  const timers = new Map();
  const reporter = createVisibilityReporter({
    send: (v) => sent.push(v),
    delay,
    setTimer: (fn, ms) => {
      const id = ++seq;
      timers.set(id, { fn, ms });
      return id;
    },
    clearTimer: (id) => timers.delete(id),
  });
  return {
    reporter,
    sent,
    armed: () => timers.size,
    delayOf: () => [...timers.values()][0]?.ms,
    tick: () => {
      const pending = [...timers.entries()];
      timers.clear();
      for (const [, t] of pending) t.fn();
    },
  };
}

// --- the foreground edge is immediate ---
{
  const h = harness();
  h.reporter.update(true);
  ok(h.sent.length === 1 && h.sent[0] === true, "becoming visible reports at once, with no timer");
  ok(h.armed() === 0, "a visible page arms no timer");
}

// --- the background edge waits ---
{
  const h = harness();
  h.reporter.update(true);
  h.reporter.update(false);
  ok(h.sent.length === 1, "hiding does not report immediately");
  ok(h.armed() === 1, "hiding arms exactly one timer");
  ok(h.delayOf() === hideDelay, `the default delay is ${hideDelay}ms`);
  h.tick();
  ok(h.sent.join() === "true,false", "the timer firing is what reports hidden");
}

// --- the flurry this exists for ---
{
  const h = harness();
  h.reporter.update(true);
  for (let i = 0; i < 5; i++) {
    h.reporter.update(false);
    h.reporter.update(true);
  }
  ok(h.sent.length === 1, "five alt-tabs and back sent one call, not ten");
  ok(h.armed() === 0, "coming back cancels the armed hide");
  h.tick();
  ok(h.sent.length === 1, "a cancelled hide cannot fire later");
}

// --- repeated hides do not stack timers ---
{
  const h = harness();
  h.reporter.update(true);
  h.reporter.update(false);
  h.reporter.update(false);
  h.reporter.update(false);
  ok(h.armed() === 1, "repeated hide events arm one timer, not three");
  h.tick();
  ok(h.sent.join() === "true,false", "and report once");
}

// --- an idle page is silent ---
{
  const h = harness();
  h.reporter.update(true);
  h.reporter.update(true);
  h.reporter.update(true);
  ok(h.sent.length === 1, "re-reporting the state the backend already holds makes no call");
  h.reporter.update(false);
  h.tick();
  h.reporter.update(false);
  h.tick();
  ok(h.sent.join() === "true,false", "nor does re-reporting hidden");
}

// --- pagehide does not wait ---
{
  const h = harness();
  h.reporter.update(true);
  h.reporter.leave();
  ok(h.sent.join() === "true,false", "pagehide reports hidden immediately");
  ok(h.armed() === 0, "and leaves no timer behind to fire into a dead page");
}
{
  const h = harness();
  h.reporter.update(true);
  h.reporter.update(false); // hide armed
  h.reporter.leave();
  ok(h.sent.join() === "true,false", "pagehide during an armed hide reports once, now");
  h.tick();
  ok(h.sent.length === 2, "and the superseded timer cannot report again");
}

// --- teardown ---
{
  const h = harness();
  h.reporter.update(true);
  h.reporter.update(false);
  h.reporter.stop();
  h.tick();
  ok(h.sent.length === 1, "stop() disarms a pending hide");
}

// --- a page that starts hidden (restored session, background tab) ---
{
  const h = harness();
  h.reporter.update(false);
  ok(h.sent.length === 0, "a page that starts hidden says nothing until the delay passes");
  h.tick();
  ok(h.sent.join() === "false", "then reports hidden without ever having claimed to be visible");
}

// --- the reported getter mirrors what the backend holds ---
{
  const h = harness();
  ok(h.reporter.reported === null, "nothing reported before the first update");
  h.reporter.update(true);
  ok(h.reporter.reported === true, "reported tracks the visible report");
  h.reporter.leave();
  ok(h.reporter.reported === false, "reported tracks the hidden report");
}

console.log(fails ? `visibility: ${fails} failure(s)` : "visibility: ok");
process.exit(fails ? 1 : 0);
