// Zero-dependency test for the notification-level model (`npm test` runs it).
import {
  EMPTY,
  normalize,
  migrateMutes,
  resolve,
  setChannel,
  setGuild,
  wantsAlert,
  showsBadge,
  isLevel,
  levelLabel,
  shouldNotify,
  notificationTitle,
} from "./notifs.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};
const eq = (a, b, msg) => assert(JSON.stringify(a) === JSON.stringify(b), `${msg}\n  got ${JSON.stringify(a)}\n  exp ${JSON.stringify(b)}`);

// ---- reading stored state ----
eq(normalize(undefined), EMPTY, "nothing stored reads as empty");
eq(normalize({ channels: { a: "none" } }), { channels: { a: "none" }, guilds: {} }, "a stored level survives");
eq(
  normalize({ channels: { a: "loud", b: "mentions" }, guilds: { g: true } }),
  { channels: { b: "mentions" }, guilds: {} },
  "junk from an older build is dropped, not trusted",
);
assert(isLevel("mentions") && !isLevel("loud"), "level ids are checked");
assert(levelLabel("none") === "Nothing", "levels have labels");

// ---- migrating the old mute list ----
// The whole point: an upgrading user's muted channels must stay exactly as
// muted as they were, with no prompt and no surprise noise.
eq(
  migrateMutes(EMPTY, { a: true, b: true }).channels,
  { a: "none", b: "none" },
  "an old mute becomes 'none'",
);
eq(migrateMutes(EMPTY, { a: false }).channels, {}, "an unmuted channel migrates to nothing");
eq(
  migrateMutes({ channels: { a: "mentions" }, guilds: {} }, { a: true }).channels,
  { a: "mentions" },
  "a level already chosen here wins — migration is idempotent",
);
eq(migrateMutes(EMPTY, null).channels, {}, "no mute list, nothing to migrate");

// ---- resolving down the chain ----
const st = { channels: { c1: "none" }, guilds: { g1: "mentions" } };
eq(resolve(st, "c1", "g1"), "none", "a channel setting wins over its server");
eq(resolve(st, "c2", "g1"), "mentions", "an unset channel follows its server");
eq(resolve(st, "c2", "g2"), "all", "an unset server is 'all'");
eq(resolve(EMPTY, "c", "g"), "all", "nothing set anywhere is 'all'");

// ---- writing ----
eq(setChannel(EMPTY, "c", "mentions").channels, { c: "mentions" }, "a channel level is pinned");
eq(setChannel(st, "c1", null).channels, {}, "clearing a channel puts it back on its server");
eq(setChannel(st, "c1", "bogus").channels, {}, "an unknown level clears rather than corrupts");
assert(setChannel(EMPTY, "c", "none") !== EMPTY, "writes are pure — the input is not mutated");
eq(EMPTY, { channels: {}, guilds: {} }, "EMPTY is still empty after a write");

eq(setGuild(EMPTY, "g", "none").guilds, { g: "none" }, "a server default is stored");
eq(setGuild(st, "g1", "all").guilds, {}, "'all' is the default, so it stores nothing");
// A per-channel override that just repeats the new server default is noise.
eq(
  setGuild({ channels: { c1: "none", c2: "mentions" }, guilds: {} }, "g", "none", ["c1", "c2"]),
  { channels: { c2: "mentions" }, guilds: { g: "none" } },
  "channel overrides that merely echo the new server default are dropped",
);
eq(
  setGuild({ channels: { c1: "all" }, guilds: { g: "none" } }, "g", "all", ["c1"]),
  { channels: {}, guilds: {} },
  "clearing a server back to 'all' also clears channels pinned to 'all'",
);

// ---- what a level does ----
assert(wantsAlert("all", { mention: false }), "'all' pings on an ordinary message");
assert(wantsAlert("all", { mention: true }), "'all' pings on a mention");
assert(!wantsAlert("mentions", { mention: false }), "'mentions' stays quiet for chatter");
assert(wantsAlert("mentions", { mention: true }), "'mentions' pings when it's you");
assert(!wantsAlert("none", { mention: true }), "'none' is silent even for a mention");

// Do Not Disturb used to be decoration. It silences every level, including a
// direct mention — that is what a user setting it is asking for.
assert(!wantsAlert("all", { mention: true, dnd: true }), "DND silences a mention");
assert(!wantsAlert("all", { mention: false, dnd: true }), "DND silences everything");

// …but it must not hide where you'd got to.
assert(showsBadge("all") && showsBadge("mentions"), "unread still shows at 'all' and 'mentions'");
assert(!showsBadge("none"), "'none' hides the badge, exactly as muting used to");

// ---- when an allowed alert becomes an OS notification ----
const desk = { hidden: false, focused: true, mobile: false };
assert(shouldNotify({ ...desk, hidden: true }), "hidden: chatter notifies");
assert(shouldNotify({ ...desk, hidden: true, direct: true }), "hidden: a DM notifies");
assert(!shouldNotify({ ...desk, focused: false }), "unfocused: chatter waits for the badge");
assert(shouldNotify({ ...desk, focused: false, direct: true }), "unfocused: a DM notifies, exactly like a mention");
assert(!shouldNotify({ ...desk, direct: true }), "focused: never — the app is in front of them");
assert(!shouldNotify({ ...desk, focused: false, mobile: true, direct: true }), "a visible phone counts as focused");
assert(shouldNotify({ hidden: true, focused: false, mobile: true }), "a backgrounded phone notifies");

eq(notificationTitle({ sender: "Ada", kind: "dm" }), "Ada (direct message)", "a DM says so");
eq(notificationTitle({ sender: "Ada", kind: "group", place: "Book club" }), "Ada in Book club", "a group DM names the group");
eq(
  notificationTitle({ sender: "Ada", channel: "general", place: "Dee Guild" }),
  "Ada in #general · Dee Guild",
  "a guild message names channel and guild",
);
eq(
  notificationTitle({ sender: "Ada", mention: true, channel: "general", place: "Dee Guild" }),
  "@ Ada in #general · Dee Guild",
  "a mention keeps its lead",
);
assert(
  notificationTitle({ sender: "Ada", kind: "dm" }) !== notificationTitle({ sender: "Ada", channel: "general", place: "G" }),
  "a DM and a guild message are told apart",
);

if (failures) {
  console.error(`\n${failures} notifs test(s) failed`);
  process.exit(1);
}
console.log("notifs.js: all tests passed");
