// cardgate.js — how long a message may wait for its link card, and what ends
// the wait. Pure: no state outside the gate, no network, no runes, so it runs
// in plain node (cardgate.test.mjs).
//
// A message with a link waits a short while (CARD_WAIT_MS) so the card the
// sender's core is fetching can go out inside it. Two earlier designs failed:
//
//   - Fetching while the draft was typed (the first sender-side version) spent
//     one-time links and fetched whatever was in the box, sent or not.
//   - Queueing every later message behind the waiting one (feat/link-cards)
//     kept the order, but a send the core never answered then held the whole
//     channel, Retry included, and the queued messages were lost if the app
//     closed.
//
// So the wait is never a queue. At most one message per channel waits, and
// anything else sent into that channel ends the wait at once: the waiting
// message goes out as typed, first, and then the new one. Nothing is
// reordered, nothing waits behind a website for longer than it takes to call
// send() twice, and the waiting message is handed to the core before the app
// is hidden (outbox.svelte.js).

// makeCardGate({ waitMs, setTimer, clearTimer }) -> gate
//
// gate.hold(key, fetchBlock, send): send(block) is called exactly once, with
//   the block fetchBlock() resolved to, or with "" when fetchBlock fails,
//   answers something else, misses the deadline, or the wait is released.
//   Any message already waiting under `key` is released first.
// gate.release(key): end the wait under key now; its message goes out bare.
// gate.releaseAll(): the same for every key.
// gate.holding(key): is a message waiting under key?
export function makeCardGate({ waitMs, setTimer = setTimeout, clearTimer = clearTimeout } = {}) {
  const held = new Map();

  function release(key) {
    held.get(key)?.finish("");
  }

  return {
    hold(key, fetchBlock, send) {
      release(key);
      let done = false;
      let timer = null;
      const h = {
        finish(block) {
          if (done) return;
          done = true;
          clearTimer(timer);
          // Out of the map BEFORE send runs: send hands the message to the
          // core, and a send into this channel releases whatever waits here.
          if (held.get(key) === h) held.delete(key);
          send(typeof block === "string" ? block : "");
        },
      };
      held.set(key, h);
      timer = setTimer(() => h.finish(""), waitMs);
      let p;
      try {
        p = Promise.resolve(fetchBlock());
      } catch (err) {
        p = Promise.reject(err);
      }
      p.then(
        (block) => h.finish(block),
        () => h.finish(""),
      );
    },
    release,
    releaseAll() {
      for (const key of [...held.keys()]) release(key);
    },
    holding(key) {
      return held.has(key);
    },
  };
}
