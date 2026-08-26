// The moderation log's wording, held in place (`npm test`).
//
// The sentences are the feature: the log's whole claim is that a person can read
// what happened and check it, so a row that says the wrong thing is worse than a
// row that says nothing. These assertions pin the wording for every op type the
// governance log can carry — including the one it cannot, which must report
// itself rather than be guessed at.
import {
  govSentenceText,
  humanDuration,
  permNames,
  verdictLabel,
  matchesFilter,
  GOV_FILTERS,
  shortFingerprint,
  actorLabel,
} from "./govlog.js";
import { PERM } from "./perms.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};
const eq = (got, want, what) => assert(got === want, `${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);

const base = {
  hash: "h",
  seq: 1,
  signer: "AAAA BBBB CCCC DDDD EEEE",
  signerName: "Ada",
  target: "FFFF GGGG HHHH IIII JJJJ",
  targetName: "Ben",
  verified: true,
  applied: true,
  at: 0,
};
const e = (o) => ({ ...base, ...o });

// ---- one sentence per op type ---------------------------------------------

eq(govSentenceText(e({ type: "ban" })), "Ada banned Ben", "ban");
eq(govSentenceText(e({ type: "unban" })), "Ada lifted the ban on Ben", "unban");
eq(govSentenceText(e({ type: "unmute" })), "Ada unmuted Ben", "unmute");
eq(govSentenceText(e({ type: "mute" })), "Ada muted Ben", "mute with no expiry");
eq(
  govSentenceText(e({ type: "mute", until: 1000000 })),
  "Ada muted Ben until " + new Date(1000000000).toISOString(),
  "mute until",
);
eq(
  govSentenceText(e({ type: "role_upsert", roleName: "Moderator", created: true })),
  "Ada created the role Moderator",
  "role created",
);
eq(
  govSentenceText(e({ type: "role_upsert", roleName: "Moderator" })),
  "Ada changed the role Moderator",
  "role changed",
);
eq(
  govSentenceText(e({ type: "role_delete", roleName: "Moderator" })),
  "Ada deleted the role Moderator",
  "role deleted",
);
eq(
  govSentenceText(e({ type: "role_assign", roleName: "Moderator", add: true })),
  "Ada gave Ben the Moderator role",
  "role granted",
);
eq(
  govSentenceText(e({ type: "role_assign", roleName: "Moderator", add: false })),
  "Ada took the Moderator role from Ben",
  "role revoked",
);
eq(
  govSentenceText(e({ type: "slow_mode", channelName: "general", seconds: 30 })),
  "Ada set slow mode in #general to one message every 30 seconds",
  "slow mode on",
);
eq(
  govSentenceText(e({ type: "slow_mode", channelName: "general", seconds: 0 })),
  "Ada turned slow mode off in #general",
  "slow mode off",
);
eq(
  govSentenceText(e({ type: "retention", seconds: 604800 })),
  "Ada set this guild to keep messages for 7 days",
  "guild retention",
);
eq(
  govSentenceText(e({ type: "retention", channelId: "c1", channelName: "general", seconds: 3600 })),
  "Ada set #general to keep messages for 1 hour",
  "channel retention",
);
eq(
  govSentenceText(e({ type: "retention", seconds: 0 })),
  "Ada turned message expiry off for this guild",
  "retention off",
);
eq(govSentenceText(e({ type: "transfer_owner" })), "Ada handed ownership to Ben", "transfer");
eq(govSentenceText(e({ type: "set_heir" })), "Ada named Ben as heir", "set heir");
eq(
  govSentenceText(e({ type: "set_heir", target: "", targetName: "" })),
  "Ada revoked the heir designation",
  "revoke heir",
);
eq(
  govSentenceText(e({ type: "claim_heir" })),
  "Ada claimed ownership as the named heir",
  "claim heir",
);
// The fail-closed case: a build that has never heard of this op type says so.
eq(
  govSentenceText(e({ type: "confiscate_everything" })),
  "Ada recorded an operation this version does not recognise",
  "unknown op type",
);
// And it must not throw on a malformed entry.
assert(typeof govSentenceText({}) === "string", "an empty entry still yields a sentence");
assert(typeof govSentenceText(null) === "string", "a null entry still yields a sentence");

// ---- names -----------------------------------------------------------------
//
// A banned member is out of the roster and often out of the profile table too,
// so the fingerprint has to carry the row rather than a blank or an invention.
eq(
  govSentenceText(e({ type: "ban", targetName: "" })),
  "Ada banned FFFF GGGG HHHH",
  "no profile falls back to the fingerprint",
);
eq(actorLabel("", ""), "Someone", "nothing at all still reads as a person");
eq(shortFingerprint("AAAA BBBB CCCC DDDD EEEE"), "AAAA BBBB CCCC", "short fingerprint trims");

// ---- durations -------------------------------------------------------------
eq(humanDuration(1), "1 second", "one second");
eq(humanDuration(60), "1 minute", "one minute");
eq(humanDuration(90), "90 seconds", "not a whole minute stays in seconds");
eq(humanDuration(3600), "1 hour", "one hour");
eq(humanDuration(21600), "6 hours", "the slow-mode ceiling");
eq(humanDuration(31536000), "365 days", "the retention ceiling");
eq(humanDuration(0), "0 seconds", "zero");
eq(humanDuration(-5), "0 seconds", "a negative duration is not a duration");

// ---- permission labels -----------------------------------------------------
eq(permNames(0).length, 0, "no bits, no labels");
eq(permNames(PERM.MANAGE_MEMBERS).join(""), "Manage members", "one bit");
eq(permNames(PERM.MANAGE_MEMBERS | PERM.MUTE_MEMBERS).length, 2, "two bits");
assert(permNames(127).length === 7, "every bit is nameable");

// ---- the verdict is two questions, not one ---------------------------------
eq(verdictLabel({ verified: true, applied: true }).id, "ok", "verified and applied");
eq(verdictLabel({ verified: true, applied: false }).id, "refused", "signed but not permitted");
eq(verdictLabel({ verified: false, applied: false }).id, "bad", "bad signature");
// A row can never claim to have taken effect on a signature that does not check
// out — the replay skips those before it looks at anything else.
eq(verdictLabel({ verified: false, applied: true }).id, "bad", "a bad signature outranks anything else");

// ---- filters ---------------------------------------------------------------
assert(matchesFilter({ type: "ban" }, "all"), "everything matches all");
assert(matchesFilter({ type: "ban" }, "members"), "ban is a member action");
assert(!matchesFilter({ type: "ban" }, "roles"), "ban is not a role action");
assert(matchesFilter({ type: "claim_heir" }, "ownership"), "claim is an ownership action");
assert(matchesFilter({ type: "whatever" }, "all"), "an unknown type still shows under all");
// Every op type the backend can emit belongs to exactly one group, or it is
// unreachable behind every filter but "Everything".
const KNOWN = [
  "role_upsert", "role_delete", "role_assign", "ban", "unban", "mute", "unmute",
  "slow_mode", "retention", "transfer_owner", "set_heir", "claim_heir",
];
for (const t of KNOWN) {
  const groups = GOV_FILTERS.filter((f) => f.types && f.types.includes(t));
  eq(groups.length, 1, `${t} belongs to exactly one filter group`);
}

if (failures) {
  console.error(`\n${failures} govlog test(s) failed`);
  process.exit(1);
}
console.log("govlog.test.mjs: all checks passed");
