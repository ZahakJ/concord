// The catch-up card's boundary logic (`npm test`).
//
// The gate is the part that has to be right: a card that appears when you switch
// guilds mid-conversation is an interruption, and one that never appears after a
// weekend is a feature nobody discovers. Everything else on the card is a sum.
import {
  awayLongEnough,
  humanAway,
  guildLastSeen,
  buildDigest,
  worthShowing,
  CATCH_UP_HOURS,
  MAX_HIGHLIGHTS,
} from "./digest.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};
const eq = (got, want, what) =>
  assert(
    JSON.stringify(got) === JSON.stringify(want),
    `${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`,
  );

const H = 3600_000;
const now = 1_700_000_000_000;

// ---- the gate --------------------------------------------------------------

assert(!awayLongEnough(now - H, now), "an hour away is not away");
assert(!awayLongEnough(now - CATCH_UP_HOURS * H + 1000, now), "just under the threshold");
assert(awayLongEnough(now - CATCH_UP_HOURS * H, now), "exactly the threshold counts");
assert(awayLongEnough(now - 30 * H, now), "overnight counts");
// A guild this device has never read is not an absence. Treating the zero as a
// date is how a first visit gets greeted with "you were away for 56 years".
assert(!awayLongEnough(0, now), "never read is not away");
assert(!awayLongEnough(null, now), "no mark is not away");
// A clock that went backwards (a device that resynced, a restored backup) must
// not produce a negative absence dressed up as a very long one.
assert(!awayLongEnough(now + 10 * H, now), "a future mark is not an absence");

// ---- the phrase ------------------------------------------------------------

eq(humanAway(0), "0 minutes", "zero");
eq(humanAway(60_000), "1 minute", "one minute");
eq(humanAway(90 * 60_000), "1 hour", "rounds down to the unit above");
eq(humanAway(5 * H), "5 hours", "hours");
eq(humanAway(47 * H), "47 hours", "just under two days stays in hours");
eq(humanAway(48 * H), "2 days", "two days");
eq(humanAway(13 * 24 * H), "13 days", "under a fortnight stays in days");
eq(humanAway(21 * 24 * H), "3 weeks", "weeks");
eq(humanAway(200 * 24 * H), "6 months", "months");
eq(humanAway(900 * 24 * H), "2 years", "years");
eq(humanAway(-5), "0 minutes", "a negative span is not a span");

// ---- last seen -------------------------------------------------------------

const guild = {
  id: "g1",
  channels: [{ id: "c1", name: "general" }, { id: "c2", name: "random" }, { id: "c3", name: "quiet" }],
};
eq(guildLastSeen(guild, {}), 0, "no marks at all");
eq(
  guildLastSeen(guild, { c1: new Date(now - 10 * H).toISOString(), c2: new Date(now - 2 * H).toISOString() }),
  now - 2 * H,
  "the newest mark in the guild wins",
);
eq(guildLastSeen(guild, { c1: "not a date" }), 0, "junk in the map is not a date");
eq(guildLastSeen(null, {}), 0, "no guild");

// ---- the summary -----------------------------------------------------------

const since = now - 20 * H;
const unread = {
  c1: { count: 12, mentions: 0 },
  c2: { count: 3, mentions: 2 },
  c3: { count: 0, mentions: 0 },
};
const entries = [
  { guildId: "g1", channelId: "c2", at: since + H, reason: "mention", snippet: "a" },
  { guildId: "g1", channelId: "c1", at: since + 2 * H, reason: "keyword", snippet: "b" },
  { guildId: "g2", channelId: "x", at: since + H, reason: "mention", snippet: "elsewhere" },
  { guildId: "g1", channelId: "c1", at: since - H, reason: "mention", snippet: "before you left" },
];

{
  const d = buildDigest({ guild, unread, entries, sinceMs: since, nowMs: now });
  eq(d.total, 15, "counts every unread channel");
  eq(d.mentions, 2, "and the mentions among them");
  eq(d.channels.map((c) => c.name), ["random", "general"], "a channel with mentions sorts first");
  assert(!d.channels.some((c) => c.id === "c3"), "a channel with nothing in it is not listed");
  eq(d.highlights.length, 2, "only this guild's entries, only since you left");
  assert(
    !d.highlights.some((e) => e.snippet === "elsewhere"),
    "another guild's mention does not belong on this card",
  );
  assert(
    !d.highlights.some((e) => e.snippet === "before you left"),
    "something you had already seen does not belong on this card",
  );
  eq(d.awayMs, 20 * H, "the absence is the span");
  assert(worthShowing(d), "a card with content is worth showing");
}

// A silenced channel is excluded from BOTH halves. The person turned it off, and
// a card that reports it anyway is the app arguing with a setting.
{
  const d = buildDigest({
    guild,
    unread,
    entries,
    sinceMs: since,
    nowMs: now,
    muted: (id) => id === "c2",
  });
  eq(d.total, 12, "a silenced channel's count is not counted");
  eq(d.mentions, 0, "nor its mentions");
  eq(d.channels.map((c) => c.name), ["general"], "nor is it listed");
  eq(d.highlights.length, 1, "and its highlights are dropped too");
}

// Nothing to say, nothing shown.
{
  const d = buildDigest({ guild, unread: {}, entries: [], sinceMs: since, nowMs: now });
  eq(d.total, 0, "no unread");
  assert(!worthShowing(d), "an empty card is worse than no card");
}

// The highlight list is bounded, and says what it could not fit.
{
  const many = [];
  for (let i = 0; i < 12; i++) {
    // (i + 1), because the mark itself is not "since you left" — an entry
    // exactly on the boundary is one you had already seen.
    many.push({ guildId: "g1", channelId: "c1", at: since + (i + 1) * 1000, reason: "mention", snippet: `m${i}` });
  }
  const d = buildDigest({ guild, unread, entries: many, sinceMs: since, nowMs: now });
  eq(d.highlights.length, MAX_HIGHLIGHTS, "bounded");
  eq(d.moreHighlights, 12 - MAX_HIGHLIGHTS, "and honest about the rest");
}

// It must not throw on the shapes a half-loaded app can hand it.
assert(buildDigest({}).total === 0, "an empty call still yields a digest");
assert(!worthShowing(null), "no digest is not worth showing");

if (failures) {
  console.error(`\n${failures} digest test(s) failed`);
  process.exit(1);
}
console.log("digest.test.mjs: all checks passed");
