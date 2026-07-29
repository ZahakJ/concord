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

if (failures) {
  console.error(`\n${failures} notifs test(s) failed`);
  process.exit(1);
}
console.log("notifs.js: all tests passed");
