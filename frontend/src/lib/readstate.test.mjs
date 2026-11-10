// Zero-dependency test for the read-state rule (`npm test` runs it).
import { isReading, readThrough, attention, hasUnread, badgeText, windowTitle, addressedToYou } from "./readstate.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};
const eq = (a, b, msg) => assert(JSON.stringify(a) === JSON.stringify(b), `${msg}\n  got ${JSON.stringify(a)}\n  exp ${JSON.stringify(b)}`);

// ---- isReading ----
const desk = { open: true, onScreen: true, hidden: false, focused: true, mobile: false, atBottom: true };
assert(isReading(desk), "open, on screen, visible, focused, at the bottom: reading");
assert(!isReading({ ...desk, hidden: true }), "a hidden window is not reading (messages that land while minimised stay unread)");
assert(!isReading({ ...desk, focused: false }), "a desktop window behind another one is not reading");
assert(isReading({ ...desk, focused: false, mobile: true }), "a phone ignores focus: its WebView reports false while you look at it");
assert(!isReading({ ...desk, mobile: true, onScreen: false }), "a phone showing Home over the open channel is not reading it");
assert(!isReading({ ...desk, atBottom: false }), "someone scrolled up in history has not seen what lands below");
assert(!isReading({ ...desk, open: false }), "a channel that is not open is never being read");

// ---- readThrough ----
eq(readThrough([]), "", "an empty channel has nothing to read through");
eq(
  readThrough([
    { sent: "2026-01-01T10:00:00.000Z" },
    { sent: "2026-01-01T10:05:00.000Z" },
    { sent: "2026-01-01T10:01:00.000Z" },
  ]),
  "2026-01-01T10:05:00.000Z",
  "the mark is the newest message by its own timestamp, whatever the row order",
);
eq(readThrough([{ sent: "garbage" }, { sent: "2026-01-01T10:00:00Z" }]), "2026-01-01T10:00:00.000Z", "an unparseable stamp is skipped");
// The bug this replaces: a fast page clock used to become the mark.
{
  const realNow = Date.now;
  Date.now = () => realNow() + 6 * 3600e3;
  const t = readThrough([{ sent: "2026-01-01T10:00:00Z" }]);
  Date.now = realNow;
  eq(t, "2026-01-01T10:00:00.000Z", "the device clock never enters the mark");
}

// ---- badges ----
eq(attention({ count: 123, mentions: 1 }, false), 1, "a guild's number is its mentions, not its chatter");
eq(attention({ count: 3, mentions: 0 }, false), 0, "chatter alone has no number in a guild");
eq(attention({ count: 3, mentions: 0 }, true), 3, "every DM message is aimed at you");
eq(attention(null, true), 0, "nothing unread, nothing to count");
assert(hasUnread({ count: 1, mentions: 0 }) && !hasUnread({ count: 0 }) && !hasUnread(null), "hasUnread");
assert(addressedToYou(true, "all"), "a DM is aimed at you");
assert(!addressedToYou(true, "mentions"), "a DM set to Only @mentions counts only its mentions");
assert(!addressedToYou(false, "all"), "a guild channel is not");
eq(badgeText(100), "99+", "badge cap");

// ---- window title ----
eq(windowTitle({ channelName: "general", guild: { kind: "" } }), "#general — Concord", "a guild channel");
eq(windowTitle({ attention: 2, channelName: "general", guild: { kind: "" } }), "(2) #general — Concord", "attention prefix");
eq(windowTitle({ channelName: "dm", guild: { kind: "dm", name: "Cy" } }), "Cy — Concord", "a DM is named after the person, not its channel");
eq(windowTitle({ attention: 1, channelName: "dm", guild: { kind: "dm", name: "Book club" } }), "(1) Book club — Concord", "a group DM uses the group's name");
eq(windowTitle({ channelName: "notes", guild: { kind: "dm", dmNotes: true, name: "notes" } }), "Notes — Concord", "Notes is Notes");
eq(windowTitle({}), "Concord", "nowhere open");

if (failures) {
  console.error(`readstate: ${failures} failure(s)`);
  process.exit(1);
}
console.log("readstate: ok");
