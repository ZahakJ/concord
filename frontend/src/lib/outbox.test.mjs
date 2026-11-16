// The one piece of the outbox worth testing without a browser: when a pending
// row stops being drawn.
//
// Every rule here was a bug first. The first live drive of the optimistic rows
// showed no pending row at all for a message that had been sent once before —
// because a plain "is this body present?" test retires a row the instant it is
// created if you have ever said those words. The second attempt used a
// timestamp window and failed the same way for two sends a second apart.
import {
  unsettled,
  alreadySaid,
  toFlush,
  unheard,
  toPersist,
  restorable,
  toStore,
  landedSince,
  CLAIM_TTL_MS,
} from "./outbox.js";

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

// What goes out again by itself when the core comes back.
{
  const now = 1000;
  const rows = [
    { id: "a", kind: "text", body: "while down", state: "failed", offline: true },
    { id: "b", kind: "text", body: "refused", state: "failed", offline: false },
    { id: "c", kind: "text", body: "slow", state: "failed", offline: true, retryAt: now + 5000 },
    { id: "d", kind: "att", state: "failed", offline: true },
    { id: "e", kind: "text", body: "in flight", state: "sending", offline: true },
    { id: "f", kind: "text", body: "second while down", state: "failed", offline: true },
  ];
  eq(
    toFlush(rows, now).map((e) => e.id).join(","),
    "a,f",
    "only text the core never heard, in the order written; refusals, waits and attachments stay the user's",
  );
}

{
  const off = new Error("Concord isn't responding — trying to reconnect");
  off.offline = true;
  eq(unheard(off), true, "nothing answered: unheard");
  eq(unheard(new Error("identity is locked; log in first")), true, "a restarted core still locked: unheard");
  eq(unheard(new Error("slow mode: wait 20s")), false, "a refusal was heard");
}

// LC2-03: a link message's row stays written down, as not sent, from the
// moment it starts waiting for its card until its send has finished. A reload
// or a closed tab ends the wait and starts the send at once, and the browser
// may cut that send off; the row being written down is then the only copy.
{
  const base = { kind: "text", channelId: "ch", body: "look https://x.org", at: 5 };
  const rows = [
    { ...base, id: "waiting", state: "sending", held: true, sendId: "s1" },
    { ...base, id: "sending", state: "sending", held: false, handed: true, sendId: "s2" },
    { ...base, id: "plain", state: "sending" },
    { ...base, id: "failed", state: "failed", error: "Couldn't send" },
  ];
  const kept = toPersist(rows);
  eq(kept.map((e) => e.id).join(","), "waiting,sending,failed", "a row being sent after its wait is still written down");
  const sending = kept.find((e) => e.id === "sending");
  eq(sending?.error, "Not sent", "as not sent");
  eq(sending?.offline, true, "and flushed by itself when the core is back");
  eq(sending?.handed, true, "marked to be checked against the channel first");
  eq(toFlush(kept).map((e) => e.id).join(","), "waiting,sending", "both go out by themselves after a reload");
  eq(kept.find((e) => e.id === "failed")?.handed, false, "an ordinary failure is not");
}

// The check a restored `handed` row gets before it is sent again: did the
// message reach the channel after all?
{
  const at = Date.parse("2026-11-15T10:00:00Z");
  const m = (content, sent, sender = ME, extra = {}) => ({ content, sender, sent, ...extra });
  eq(landedSince([m("hi", "2026-11-15T10:00:01Z")], ME, "hi", at), true, "it arrived");
  eq(landedSince([m("hi", "2026-11-15T09:50:00Z")], ME, "hi", at), false, "the same words ten minutes earlier are another message");
  eq(landedSince([m("hi", "2026-11-15T10:00:01Z", "them")], ME, "hi", at), false, "someone else's");
  eq(landedSince([m("hi", "2026-11-15T10:00:01Z", ME, { deleted: true })], ME, "hi", at), false, "deleted");
  eq(landedSince([], ME, "hi", at), false, "nothing there");
}

// LC2-05: a second tab of the same browser reads the same storage. A row the
// first tab is still waiting on or sending is that tab's: the second neither
// takes it on nor erases it when it writes its own rows down.
{
  const now = 100000;
  const row = (id, sendId) => ({ id, kind: "text", channelId: "ch", body: `b ${id}`, state: "failed", sendId });
  const stored = [row("a", "s-a"), row("b", "s-b"), row("c", "s-c"), row("d", "")];
  const claims = {
    "s-a": { tab: "other", at: now - 1000 }, // being sent by an open tab
    "s-b": { tab: "other", at: now - CLAIM_TTL_MS - 1 }, // a tab that died long ago
    "s-c": { tab: "me", at: now - 1000 }, // this very page (cannot happen after a load, harmless)
  };
  eq(restorable(stored, claims, "me", now).map((e) => e.id).join(","), "b,c,d", "another open tab's row is left to it");
  eq(restorable(stored, {}, "me", now).length, 4, "with no claims every row comes back");
  eq(
    toStore([row("mine", "s-m")], stored, claims, "me", now).map((e) => e.id).join(","),
    "mine,a",
    "writing down this tab's rows keeps the one another tab is sending",
  );
}

if (failures) {
  console.error(`outbox: ${failures} failure(s)`);
  process.exit(1);
}
console.log("outbox: all tests passed");
