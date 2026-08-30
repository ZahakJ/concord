// The new-guild setup checklist's memory: one localStorage key per guild,
// written when THIS device creates the guild and removed when the card is
// dismissed or finished.
//
// Deliberately local and deliberately per device. It is a nudge about a first
// session, not a fact about the guild, so it has no business on the wire —
// and a member who joins a guild someone else made must never be handed the
// owner's checklist.
//
// The record also latches the one step that cannot be re-derived. "Have you
// written a welcome message" reads as YES the moment anything is in the
// guild, and a templated guild arrives holding five "created this channel"
// system rows. Counting rows was the first attempt and it ticked the box
// before the owner had typed a word. So the card watches the open channel for
// a message THIS account actually wrote, and latches it here — a fact about
// what you did, not an inference from a total.

const KEY = (guildID) => `setup:${guildID}`;

// localStorage throws in a private window with site data blocked, and the app
// is expected to keep working there — a missing nudge is not a failure state.
function safe(fn, fallback) {
  try {
    return fn();
  } catch {
    return fallback;
  }
}

function read(guildID) {
  return safe(() => {
    const raw = localStorage.getItem(KEY(guildID));
    if (raw === null) return null;
    // "1" is the shape the first version wrote; treat it as an armed record
    // with nothing latched rather than as corruption.
    if (raw === "1") return { welcome: false, invite: false };
    const v = JSON.parse(raw);
    return v && typeof v === "object" ? v : { welcome: false, invite: false };
  }, null);
}

function write(guildID, rec) {
  safe(() => localStorage.setItem(KEY(guildID), JSON.stringify(rec)));
}

export function armGuildSetup(guildID) {
  if (!guildID) return;
  write(guildID, { welcome: false, invite: false });
}

export function isGuildSetupArmed(guildID) {
  if (!guildID) return false;
  return read(guildID) !== null;
}

export function setupWelcomeDone(guildID) {
  return !!read(guildID)?.welcome;
}

export function markSetupWelcome(guildID) {
  const rec = read(guildID);
  if (!rec || rec.welcome) return false;
  write(guildID, { ...rec, welcome: true });
  return true;
}

export function setupInviteDone(guildID) {
  return !!read(guildID)?.invite;
}

export function markSetupInvite(guildID) {
  const rec = read(guildID);
  if (!rec || rec.invite) return false;
  write(guildID, { ...rec, invite: true });
  return true;
}

export function dismissGuildSetup(guildID) {
  if (!guildID) return;
  safe(() => localStorage.removeItem(KEY(guildID)));
}
