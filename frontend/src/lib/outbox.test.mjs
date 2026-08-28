// The one piece of the outbox worth testing without a browser: when a pending
// row stops being drawn.
//
// Every rule here was a bug first. The first live drive of the optimistic rows
// showed no pending row at all for a message that had been sent once before —
// because a plain "is this body present?" test retires a row the instant it is
// created if you have ever said those words. The second attempt used a
// timestamp window and failed the same way for two sends a second apart.
import { unsettled, alreadySaid } from "./outbox.js";

let failures = 0;
const fail = (msg) => {
  console.error("  FAIL: " + msg);
  failures++;
};
const eq = (got, want, what) => {
  if (got !== want) fail(`${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
};

const ME = "me-fpr";
const msg = (content, sender = ME, extra = {}) => ({ content, sender, ...extra });
// An entry as sendText builds it: the body to match, and the count that was
// already in the channel when it was queued.
const entry = (id, match, seen = 0, state = "sending") => ({ id, match, seen, state });

const ids = (list) => list.map((e) => e.id).join(",");

// Nothing sent, nothing drawn.
eq(unsettled([], [msg("hello")], ME).length, 0, "an empty outbox stays empty");

// The ordinary promotion.
eq(unsettled([entry("a", "hello")], [msg("hello")], ME).length, 0, "the echo retires the row");
eq(unsettled([entry("a", "hello")], [], ME).length, 1, "no echo, the row stands");

// Saying the same thing again. The channel already holds one "ok", the entry
// knows that, and it must still be drawn until a SECOND one arrives.
eq(unsettled([entry("a", "ok", 1)], [msg("ok")], ME).length, 1, "an older identical message is not this echo");
eq(unsettled([entry("a", "ok", 1)], [msg("ok"), msg("ok")], ME).length, 0, "the second arrival is");

// Three deep, because the off-by-one here is the whole mechanism.
eq(unsettled([entry("a", "ok", 2)], [msg("ok"), msg("ok")], ME).length, 1, "two seen, two present: still pending");
eq(unsettled([entry("a", "ok", 2)], [msg("ok"), msg("ok"), msg("ok")], ME).length, 0, "…until the third");

// Two identical sends in flight: one arrival settles exactly one row, and it is
// the older one, because that is the order they were sent in.
{
  const two = [entry("a", "+1", 0), entry("b", "+1", 0)];
  eq(ids(unsettled(two, [msg("+1")], ME)), "b", "one echo retires the older of two identical sends");
  eq(unsettled(two, [msg("+1"), msg("+1")], ME).length, 0, "two echoes retire both");
  eq(unsettled(two, [], ME).length, 2, "no echoes, both stand");
}

// Somebody else saying the same thing is not our echo.
eq(unsettled([entry("a", "ok")], [msg("ok", "someone-else")], ME).length, 1, "another member's copy is not our echo");
// Neither is a deleted one.
eq(unsettled([entry("a", "ok")], [msg("ok", ME, { deleted: true })], ME).length, 1, "a deleted message is not an echo");

// A failed row is the user's to retry or discard; a coincidence must never
// silently delete their text.
eq(unsettled([entry("a", "ok", 0, "failed")], [msg("ok")], ME).length, 1, "a failed row survives an echo");

// An attachment has no body to match on until the core answers, so it is only
// ever retired by its own RPC resolving.
eq(unsettled([{ id: "a", match: "", state: "sending" }], [msg("x")], ME).length, 1,
   "an attachment row is not matched by content");

// alreadySaid is the other half of the pair and counts the same way.
eq(alreadySaid([msg("ok"), msg("ok"), msg("no")], ME, "ok"), 2, "alreadySaid counts our copies");
eq(alreadySaid([msg("ok", "someone-else")], ME, "ok"), 0, "…and only ours");
eq(alreadySaid([msg("ok", ME, { deleted: true })], ME, "ok"), 0, "…and not deleted ones");

if (failures) {
  console.error(`outbox: ${failures} failure(s)`);
  process.exit(1);
}
console.log("outbox: all tests passed");
