// callflow.js — what a call in a direct message IS, from one side of it.
//
// A DM call used to have no state of its own. "Ringing" was whatever the stage
// derived from being alone in the room, so every way of becoming alone again —
// the other person hanging up, a decline nobody heard about, a ring nobody
// answered — re-entered it: the caller was told "Ringing…" after the call had
// ended, a declined call rang on for half a minute and then sat open forever,
// the callee's ringtone went on for as long as the caller stayed in the room,
// and the sidebar said "Voice connected" in green with a clock counting ring
// time as call time. Each surface was locally reasonable and the sum lied.
//
// So the call is a small state machine, one per DM channel, the same on every
// device, and every surface reads it instead of guessing:
//
//   caller                                    callee
//   ------                                    ------
//   idle --dial--> ringing                    idle --ring--> incoming
//                  unreachable  (nobody of        incoming --accept--> idle (we join;
//                    theirs is online; still         the join makes us "active")
//                    times out, they see a           incoming --decline--> ended:declined
//                    missed call on return)          incoming --tick ≥ RING_MS--> ended:missed
//   ringing|unreachable --arrive--> active         incoming --gone--> ended:missed
//   ringing|unreachable --declined--> ended:declined   (caller hung up / timed out)
//   ringing|unreachable --tick ≥ RING_MS--> ended:missed
//   ringing|unreachable --hangup--> ended:cancelled
//   active --hangup--> ended:ended            (we pressed End call)
//   active --depart(goodbye, nobody left)--> ended:ended   (1:1: they hung up, so
//                                              the call is over for both of us)
//
//   Ringing is entered ONCE per call. Once anybody has answered, being alone in
//   the room again is "everyone else left" (a group) or the end of the call (a
//   1:1) — never "Ringing…".
//
// Both sides time the ring with the same RING_MS. The callee cannot see the
// caller's clock, so it counts from the moment it first heard the call, which
// is never earlier than the caller's start: the caller always gives up first,
// and its leave stops the callee's ring on the same beat. The callee's own
// timer is the backstop for a caller who never gives up — an older client
// does not, and that is exactly the case where a ringtone used to sound
// forever.
//
// A decline is a signalling blob sent straight to the caller's devices (see
// declineBlob). It deliberately carries no `channelId` field, because every
// client before it opened a media connection for any blob naming the room: an
// older caller drops it unread and behaves exactly as it always did, which is
// to ring out and leave a "Missed call".
//
// Records. Exactly one line per call per side, saying what happened on THAT
// side:
//   · nobody answered (cancelled / timed out / unreachable) — the caller posts
//     the shared "call-missed" line, as it always has: "You called — no answer"
//     to the caller, "Missed call from Ada" to the callee (who was perhaps
//     offline, which is why it has to travel).
//   · declined — the caller posts the same shared kind with content "Declined":
//     "Ben declined" / "You declined Ada's call". An older client ignores the
//     content and shows its old missed-call line, which is what it said before.
//   · answered, then ended — each side writes its OWN "Call ended · 2m" locally;
//     it never travels, because an older client would render a new shared kind
//     as a plain message, and the two sides' lengths differ by a second anyway.
//   · group DM, somebody else answered and we never did — a local "Missed call
//     from Ada", because the caller's shared line is only posted when NOBODY
//     answered, and the member who ignored the ring was otherwise left with no
//     trace a call ever happened.

export const RING_MS = 30000;

// Content of the shared "call-missed" line for a decline. Anything else is the
// historical "Missed call".
export const DECLINED = "Declined";

const RINGING = new Set(["ringing", "unreachable"]);

export function isRinging(st) {
  return !!st && RINGING.has(st.phase);
}

export function idle() {
  return { phase: "idle" };
}

// step(state, event) → { st, fx }. Pure: the effects are returned as data and
// carried out by whoever owns the side effects (state.svelte.js), so every
// transition can be checked without a call, a network or a clock.
//
// fx entries:
//   { leave: true }                     hang up our side of the call
//   { ring: true|false }                start / stop the incoming ringtone + card
//   { clock: "start" }                  the call is answered: start counting
//   { sendDecline: true }               tell the caller's devices we declined
//   { wire: content }                   post the shared "call-missed" line
//   { local: content }                  write a local-only call record
//   { say: "declined"|"missed"|"ended"|"unreachable" }   tell the user
export function step(st, ev) {
  st = st || idle();
  const same = { st, fx: [] };
  switch (ev.type) {
    // ---- caller ----
    case "dial":
      if (st.phase !== "idle" && st.phase !== "ended") return same;
      return {
        st: {
          phase: ev.reachable === false ? "unreachable" : "ringing",
          role: "caller",
          group: !!ev.group,
          since: ev.now,
          declinedBy: [],
        },
        fx: [],
      };
    case "reach":
      if (!isRinging(st)) return same;
      return { st: { ...st, phase: ev.reachable ? "ringing" : "unreachable" }, fx: [] };
    case "join-existing":
      // We walked into a room somebody else was already in: we answered a ring,
      // or rejoined a call in progress. Either way it is a call, not a ring.
      return {
        st: { phase: "active", role: st.role || "callee", group: !!ev.group, since: ev.now, answeredAt: ev.now },
        fx: [{ clock: "start" }],
      };
    case "arrive":
      if (!isRinging(st)) return same;
      return { st: { ...st, phase: "active", answeredAt: ev.now }, fx: [{ clock: "start" }] };
    case "declined": {
      if (!isRinging(st)) return same;
      const declinedBy = st.declinedBy.includes(ev.by) ? st.declinedBy : [...st.declinedBy, ev.by];
      // In a group one "no" is not the end of the call: the others may still
      // pick up. It ends when everyone who could answer has said no.
      if (st.group && !ev.everyone) return { st: { ...st, declinedBy }, fx: [{ say: "declined" }] };
      return {
        st: { ...st, phase: "ended", outcome: "declined", declinedBy },
        fx: [{ leave: true }, { wire: DECLINED }, { say: "declined" }],
      };
    }
    case "tick":
      if (isRinging(st) && ev.now - st.since >= RING_MS) {
        return {
          st: { ...st, phase: "ended", outcome: "missed" },
          fx: [{ leave: true }, { wire: "Missed call" }, { say: st.phase === "unreachable" ? "unreachable" : "missed" }],
        };
      }
      if (st.phase === "incoming" && ev.now - st.since >= RING_MS) {
        return { st: { ...st, phase: "ended", outcome: "missed" }, fx: [{ ring: false }, ...calleeMissed(st)] };
      }
      return same;
    case "hangup":
      if (isRinging(st)) {
        return { st: { ...st, phase: "ended", outcome: "cancelled" }, fx: [{ wire: "Missed call" }] };
      }
      if (st.phase === "active") {
        return {
          st: { ...st, phase: "ended", outcome: "ended" },
          fx: [{ local: ended(ev.now - st.answeredAt) }],
        };
      }
      return same;
    case "depart":
      // Only a GOODBYE ends a 1:1. A heartbeat that went quiet is a network
      // problem the mesh is already reconnecting, and hanging up on it would
      // turn every Wi-Fi hiccup into a dropped call.
      if (st.phase !== "active" || st.group || !ev.goodbye || ev.remaining > 0) return same;
      return {
        st: { ...st, phase: "ended", outcome: "ended" },
        fx: [{ leave: true }, { local: ended(ev.now - st.answeredAt) }, { say: "ended" }],
      };

    // ---- callee ----
    case "ring":
      if (st.phase !== "idle") return same;
      return {
        st: {
          phase: "incoming",
          role: "callee",
          group: !!ev.group,
          since: ev.now,
          caller: ev.caller || "",
          answeredElsewhere: !!ev.answeredElsewhere,
        },
        fx: [{ ring: true }],
      };
    case "seen-answer":
      if (st.phase !== "incoming" || st.answeredElsewhere) return same;
      return { st: { ...st, answeredElsewhere: true }, fx: [] };
    case "accept":
      if (st.phase !== "incoming") return same;
      return { st: idle(), fx: [{ ring: false }] };
    case "decline":
      if (st.phase !== "incoming") return same;
      // A group's caller is not hanging up on our account, so there is nothing
      // to tell it beyond the news; a 1:1 caller ends its side on hearing it.
      return {
        st: { ...st, phase: "ended", outcome: "declined" },
        fx: [{ ring: false }, { sendDecline: true }],
      };
    case "gone":
      // The room emptied. A ring that was still going is a missed call; a call
      // that already ended has nothing more to say.
      if (st.phase === "incoming") {
        return { st: idle(), fx: [{ ring: false }, ...calleeMissed(st)] };
      }
      if (st.phase === "ended") return { st: idle(), fx: [] };
      return same;
    case "reset":
      return { st: idle(), fx: st.phase === "incoming" ? [{ ring: false }] : [] };
    default:
      return same;
  }
}

// A callee's own record of a ring it never answered. Only a group needs one:
// in a 1:1, and in a group nobody answered, the caller's shared line already
// says it.
function calleeMissed(st) {
  return st.group && st.answeredElsewhere && st.caller ? [{ local: `missed:${st.caller}` }] : [];
}

function ended(ms) {
  return `ended:${Math.max(0, Math.round((ms || 0) / 1000))}`;
}

// ---- reading the records back --------------------------------------------

// callLength turns seconds into the short form a record line uses: "45s",
// "2m", "1h 5m". A call log reads in minutes; the seconds are noise past one.
export function callLength(secs) {
  const s = Math.max(0, Math.floor(secs || 0));
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  return m % 60 ? `${h}h ${m % 60}m` : `${h}h`;
}

// recordText is what a call line says, for whichever side is reading it.
//   kind     "call-missed" (shared) or "call" (local)
//   content  the line's stored content
//   mine     did this side post it
//   other    the other side's name (the DM peer's name, or the sender's)
//   nameOf   fingerprint → name, for a group's local missed line
export function recordText({ kind, content, mine, other, nameOf = (f) => f }) {
  if (kind === "call") {
    if (content?.startsWith("ended:")) return `Call ended · ${callLength(Number(content.slice(6)))}`;
    if (content?.startsWith("missed:")) return `Missed call from ${nameOf(content.slice(7)) || "someone"}`;
    return "Call";
  }
  if (content === DECLINED) {
    return mine ? `${other || "They"} declined` : `You declined ${other ? `${other}'s` : "a"} call`;
  }
  return mine ? "You called — no answer" : `Missed call from ${other || "someone"}`;
}

// callPreview is the DM list's line for a call record — the same words the
// backend's LastSaid uses (internal/app/snippet.go callPreview), so the row
// reads the same before and after a refresh. "" for anything not a call.
export function callPreview(kind, content, mine) {
  if (kind === "call-missed") {
    if (content === DECLINED) return mine ? "Call declined" : "You declined a call";
    return mine ? "You called — no answer" : "Missed call";
  }
  if (kind === "call") {
    if (content?.startsWith("ended:")) return `Call ended · ${callLength(Number(content.slice(6)))}`;
    return "Missed call";
  }
  return "";
}

// ---- the other things a call needs to get right ----------------------------

// declineBlob is the signalling payload a callee sends to each of the caller's
// devices. No `channelId` on purpose — see the top of this file.
export function declineBlob(channelId) {
  return JSON.stringify({ decline: channelId });
}

// parseDecline returns the channel a decline blob is about, or "".
export function parseDecline(raw) {
  try {
    const m = JSON.parse(raw);
    return m && typeof m.decline === "string" && !("channelId" in m) ? m.decline : "";
  } catch {
    return "";
  }
}

// sharersOf: which remote peers have a SCREEN up. The sidebar's "LIVE — click
// to watch" pill is the promise of something to watch, and it was handed to
// anyone with a camera on, so a room of four cameras was four LIVE badges and
// none of them a share.
export function sharersOf(metas) {
  const out = {};
  for (const m of metas) if (m?.peerId && !m.self && m.kind === "screen") out[m.peerId] = true;
  return out;
}

// GHOST_MS is how long a media connection may exist for a peer the room's
// presence does not list. Presence and signalling race — an offer can land a
// beat before that peer's first heartbeat — so a fresh one gets this long;
// after that it is a connection to somebody who is not in the call, which is
// what a closed second window used to leave on everybody's stage for good.
export const GHOST_MS = 12000;

// ghostsToReap(meshPeers, roster, firstSeen, now) → { reap, firstSeen }.
//   meshPeers  peer ids the mesh holds
//   roster     { peerId: … } presence for the room
//   firstSeen  { peerId: ms } when each unlisted peer was first noticed
export function ghostsToReap(meshPeers, roster, firstSeen, now) {
  const next = {};
  const reap = [];
  for (const pid of meshPeers) {
    if (roster[pid]) continue;
    const at = firstSeen[pid] ?? now;
    if (now - at >= GHOST_MS) reap.push(pid);
    else next[pid] = at;
  }
  return { reap, firstSeen: next };
}
