// govlog.js — turning one signed governance operation into a sentence.
//
// Pure, so the wording is testable and so the panel stays markup. Every function
// here takes an entry as the backend hands it over and returns data; nothing
// reads the app state and nothing formats HTML.
//
// The sentences are written in the past tense and in the guild's own vocabulary
// ("guild", never "server"), and they say what HAPPENED rather than naming the
// op type: "Ada banned Ben" reads; "ban op, target Ben" does not. Where the log
// cannot support a claim, the sentence does not make one — an op whose type this
// build has never heard of is reported as exactly that instead of being guessed
// at, which is the same fail-closed reading every other peer-supplied value in
// the app gets.

import { PERM_LIST, has } from "./perms.js";

// The filter groups. Order is the order the chips appear in.
export const GOV_FILTERS = [
  { id: "all", label: "Everything", types: null },
  {
    id: "members",
    label: "Members",
    types: ["ban", "unban", "mute", "unmute", "remove_member", "readmit"],
  },
  { id: "roles", label: "Roles", types: ["role_upsert", "role_delete", "role_assign"] },
  { id: "channels", label: "Channels", types: ["slow_mode", "retention"] },
  { id: "ownership", label: "Ownership", types: ["transfer_owner", "set_heir", "claim_heir"] },
];

export function matchesFilter(entry, filterId) {
  const f = GOV_FILTERS.find((x) => x.id === filterId);
  if (!f || !f.types) return true;
  return f.types.includes(entry?.type);
}

// A fingerprint is a 40-character grouped base32 string. When there is no name
// to show — a member who was banned before this device ever saw their profile —
// the first group or two is the honest stand-in, and it is what the bans panel
// already shows.
export function shortFingerprint(fpr) {
  return fpr ? fpr.slice(0, 14).trim() : "";
}

export function actorLabel(name, fpr) {
  return name || shortFingerprint(fpr) || "Someone";
}

// humanDuration turns a span of seconds into the shortest phrase that is still
// exact. Governance durations are always chosen from menus, so they are whole
// minutes, hours and days — a value that is not is a hand-crafted op, and it
// falls back to seconds rather than lying by rounding.
export function humanDuration(seconds) {
  const s = Math.max(0, Math.floor(Number(seconds) || 0));
  const units = [
    [86400, "day"],
    [3600, "hour"],
    [60, "minute"],
  ];
  for (const [size, name] of units) {
    if (s >= size && s % size === 0) {
      const n = s / size;
      return `${n} ${name}${n === 1 ? "" : "s"}`;
    }
  }
  return `${s} second${s === 1 ? "" : "s"}`;
}

// permNames lists the permissions a bitmask carries, using the same labels the
// Roles panel prints so the two screens never disagree about what a bit means.
export function permNames(perms) {
  return PERM_LIST.filter((p) => has(perms, p.bit)).map((p) => p.label);
}

const text = (v) => ({ k: "t", v });
const person = (e, which) =>
  which === "signer"
    ? { k: "person", v: actorLabel(e.signerName, e.signer), fpr: e.signer }
    : { k: "person", v: actorLabel(e.targetName, e.target), fpr: e.target };
const role = (e) => ({ k: "role", v: e.roleName || "a role", color: e.color || "" });
// The hash rides in the value rather than in the markup, so the sentence the
// screen shows and the sentence the accessible name reads are the same string.
const channel = (e) => ({ k: "channel", v: e.channelName ? `#${e.channelName}` : "a channel" });

// govSentence returns the sentence as parts, so the panel can style a name, a
// role and a channel differently without this file emitting markup.
export function govSentence(e) {
  if (!e) return [text("An unreadable entry")];
  const who = person(e, "signer");
  const whom = () => person(e, "target");
  switch (e?.type) {
    case "role_upsert":
      return [who, text(e.created ? " created the role " : " changed the role "), role(e)];
    case "role_delete":
      return [who, text(" deleted the role "), role(e)];
    case "role_assign":
      return e.add
        ? [who, text(" gave "), whom(), text(" the "), role(e), text(" role")]
        : [who, text(" took the "), role(e), text(" role from "), whom()];
    case "ban":
      return [who, text(" banned "), whom()];
    case "unban":
      return [who, text(" lifted the ban on "), whom()];
    case "remove_member":
      return [who, text(" removed "), whom(), text(" from the guild")];
    case "readmit":
      return [who, text(" allowed "), whom(), text(" back in")];
    case "mute":
      return e.until > 0
        ? [who, text(" muted "), whom(), text(" until "), { k: "time", v: e.until * 1000 }]
        : [who, text(" muted "), whom()];
    case "unmute":
      return [who, text(" unmuted "), whom()];
    case "slow_mode":
      return e.seconds > 0
        ? [who, text(" set slow mode in "), channel(e), text(` to one message every ${humanDuration(e.seconds)}`)]
        : [who, text(" turned slow mode off in "), channel(e)];
    case "retention":
      if (e.channelId) {
        return e.seconds > 0
          ? [who, text(" set "), channel(e), text(` to keep messages for ${humanDuration(e.seconds)}`)]
          : [who, text(" turned message expiry off in "), channel(e)];
      }
      return e.seconds > 0
        ? [who, text(` set this guild to keep messages for ${humanDuration(e.seconds)}`)]
        : [who, text(" turned message expiry off for this guild")];
    case "transfer_owner":
      return [who, text(" handed ownership to "), whom()];
    case "set_heir":
      return e.target
        ? [who, text(" named "), whom(), text(" as heir")]
        : [who, text(" revoked the heir designation")];
    case "claim_heir":
      return [who, text(" claimed ownership as the named heir")];
    default:
      // Not an error and not a blank row: a member on a newer build did
      // something this one has no words for, and saying so is more honest than
      // hiding it.
      return [who, text(" recorded an operation this version does not recognise")];
  }
}

// govSentenceText is the same sentence flattened, for an accessible name and for
// the tests. The time part renders as a locale string in the panel; here it is
// left as an ISO date so the assertion does not depend on the machine's locale.
export function govSentenceText(e) {
  return govSentence(e)
    .map((p) => (p.k === "time" ? new Date(p.v).toISOString() : p.v))
    .join("");
}

// verdictLabel says what the signature check found, in the two words the row has
// room for. The distinction matters: "signed" and "took effect" are different
// questions, and a log that showed only the first would print bans that never
// happened.
export function verdictLabel(e) {
  if (!e?.verified) return { id: "bad", label: "Signature does not verify", icon: "alert" };
  if (!e.applied) return { id: "refused", label: "Signed, but not permitted", icon: "eyeOff" };
  return { id: "ok", label: "Signature verified", icon: "check" };
}
