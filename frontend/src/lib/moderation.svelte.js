// ONE moderation menu, wherever a member is on screen.
//
// There used to be two, on the same object, with different action sets, and
// the complete one was the hidden one: right-clicking a member — the natural
// gesture — offered View profile / Copy user ID / Transfer ownership / Name as
// heir / Remove from guild, while Mute and Ban lived behind an unlabelled 30px
// dots button painted on top of the profile card's decorative art. This module
// is the single list; the card's `⋯`, the member list's context menu and the
// members table's row overflow all render it.
//
// It also settles the naming. "Remove from guild" and "Kick" were the same
// operation under two words in two places. The verb is KICK everywhere a human
// reads it — short, unambiguous, and the word people already use — while the
// governance op stays `remove_member`, because that is a signed record on
// forty devices and renaming it would be a wire change for a synonym.
import {
  S,
  activeGuild,
  flash,
  refreshRightPanel,
  closeProfilePopover,
  isBlocked,
  blockUser,
  unblockUser,
} from "./state.svelte.js";
import { api } from "./api.js";
import { PERM, PERM_ALL, has } from "./perms.js";

// The durations the picker offers. A mute is a cool-off, and the shape of a
// cool-off is minutes-to-days: sixty seconds for "stop typing", a week for
// "come back when the thread is over". Anything longer is a kick, and saying
// so is better than a timeout nobody remembers setting.
export const MUTE_DURATIONS = [
  { minutes: 1, label: "60 seconds" },
  { minutes: 5, label: "5 minutes" },
  { minutes: 10, label: "10 minutes" },
  { minutes: 60, label: "1 hour" },
  { minutes: 1440, label: "1 day" },
  { minutes: 10080, label: "1 week" },
];

export function muteDurationLabel(minutes) {
  const hit = MUTE_DURATIONS.find((d) => d.minutes === minutes);
  if (hit) return hit.label;
  if (minutes >= 1440) return `${Math.round(minutes / 1440)} days`;
  if (minutes >= 60) return `${Math.round(minutes / 60)} hours`;
  return `${minutes} minutes`;
}

export function isMemberMuted(mem) {
  return !!mem && mem.mutedUntil > Date.now() / 1000;
}

// ---- the actions ----------------------------------------------------------

export async function muteMember(mem, minutes, reason) {
  try {
    await api.muteMember(S.activeGuildId, mem.fingerprint, minutes, reason || "");
    await refreshRightPanel();
    flash(`Muted for ${muteDurationLabel(minutes)}`, "success");
  } catch (err) {
    flash(err);
  }
}

export async function unmuteMember(mem) {
  try {
    await api.unmuteMember(S.activeGuildId, mem.fingerprint);
    await refreshRightPanel();
    flash("Unmuted", "success");
  } catch (err) {
    flash(err);
  }
}

export function confirmKick(mem, after) {
  const fpr = mem.fingerprint;
  const name = mem.name || "this member";
  S.modal = {
    kind: "confirm",
    title: `Kick ${name}?`,
    body: "They lose access now but can rejoin with a new invite.",
    confirmLabel: "Kick",
    reasonLabel: "Reason",
    reasonPlaceholder: "For the moderation log",
    onConfirm: async (reason) => {
      try {
        await api.removeMember(S.activeGuildId, fpr, reason);
        await refreshRightPanel();
        flash(`${name} was kicked`, "success");
        after?.();
      } catch (err) {
        flash(err);
      }
      S.modal = null;
    },
  };
}

export function confirmBan(mem, after) {
  const fpr = mem.fingerprint;
  const name = mem.name || "this member";
  S.modal = {
    kind: "confirm",
    title: `Ban ${name}?`,
    body: "They're removed now and can't rejoin, even with a new invite.",
    confirmLabel: "Ban",
    reasonLabel: "Reason",
    reasonPlaceholder: "For the moderation log and the ban list",
    onConfirm: async (reason) => {
      try {
        await api.banMember(S.activeGuildId, fpr, reason);
        await refreshRightPanel();
        flash(`${name} was banned`, "success");
        after?.();
      } catch (err) {
        flash(err);
      }
      S.modal = null;
    },
  };
}

export function openMuteDialog(mem) {
  S.modal = { kind: "mute", member: mem };
}

// One-click admin: find (or mint) a role holding every permission and assign
// it. Only offered to someone whose op would actually stick — governance
// refuses a role granting more than the actor holds, so a plain moderator
// clicking this would watch it vanish.
export async function toggleAdmin(mem) {
  const adminRole = S.roles.find((r) => r.perms === PERM_ALL);
  const grant = !(adminRole && mem?.roleIds?.includes(adminRole.id));
  try {
    let role = adminRole;
    if (!role) {
      await api.upsertRole(S.activeGuildId, "", "Admin", "#e0a63c", PERM_ALL, 100);
      S.roles = (await api.roles(S.activeGuildId)) || [];
      role = S.roles.find((r) => r.perms === PERM_ALL);
    }
    if (!role) throw new Error("couldn't create the Admin role");
    await api.assignRole(S.activeGuildId, mem.fingerprint, role.id, grant);
    await refreshRightPanel();
    flash(grant ? `${mem.name || "Member"} is now an admin 👑` : "Admin removed", "success");
  } catch (err) {
    flash(err);
  }
}

// ---- the menu -------------------------------------------------------------

// moderationItems returns the ContextMenu item list for one member. `close`
// runs before anything that opens a dialog, so a popover holding the menu gets
// out of the way. Every gate is computed here, once, which is what makes the
// two surfaces provably identical rather than identical by inspection.
export function moderationItems(mem, { close } = {}) {
  const g = activeGuild();
  if (!mem || !g || g.kind === "dm") return [];
  const myPerms = g.myPerms || 0;
  const canModerate = !mem.isSelf && !mem.isOwner && (g.isOwner || g.canManage);
  const canMute = canModerate && has(myPerms, PERM.MUTE_MEMBERS);
  const canMakeAdmin = !mem.isSelf && (g.isOwner || has(myPerms, PERM_ALL));
  const adminRole = S.roles.find((r) => r.perms === PERM_ALL);
  const isAdmin = !!adminRole && !!mem.roleIds?.includes(adminRole.id);
  const muted = isMemberMuted(mem);
  const blocked = isBlocked(mem.fingerprint);
  const go = (fn) => () => {
    close?.();
    fn();
  };

  return [
    canMakeAdmin && {
      label: isAdmin ? "Remove admin" : "Make admin",
      icon: "crown",
      onClick: go(() => toggleAdmin(mem)),
    },
    canMute && {
      label: muted ? "Unmute" : "Mute…",
      icon: muted ? "micOff" : "mic",
      onClick: go(() => (muted ? unmuteMember(mem) : openMuteDialog(mem))),
    },
    (canMakeAdmin || canMute) && { sep: true },
    canModerate && { label: "Kick", icon: "door", onClick: go(() => confirmKick(mem)) },
    canModerate && {
      label: "Ban",
      icon: "trash",
      danger: true,
      onClick: go(() => confirmBan(mem)),
    },
    !mem.isSelf && {
      label: blocked ? "Unblock" : "Block",
      icon: blocked ? "eye" : "eyeOff",
      onClick: go(() => (blocked ? unblockUser(mem.fingerprint, mem.name) : blockUser(mem.fingerprint, mem.name))),
    },
  ].filter(Boolean);
}

// closeCard is the `close` a profile card passes: the menu items are shared,
// the dismissal is not.
export const closeCard = () => closeProfilePopover();
