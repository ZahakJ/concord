// Permission bits — must match internal/app/govstate.go exactly (bit order).
export const PERM = {
  MANAGE_MEMBERS: 1 << 0, // invite, kick, ban
  MANAGE_MESSAGES: 1 << 1, // delete anyone's messages, pin
  MANAGE_CHANNELS: 1 << 2, // channels & categories
  MANAGE_GUILD: 1 << 3, // rename, emoji
  MANAGE_ROLES: 1 << 4, // define & assign roles
  MUTE_MEMBERS: 1 << 5, // mute/time-out members
  SYNC_HOST: 1 << 6, // preferred history-sync/relay host
};

// Ordered list for the role editor, with human labels + short descriptions.
export const PERM_LIST = [
  { bit: PERM.MANAGE_MEMBERS, label: "Manage members", hint: "Invite, kick, and ban people" },
  { bit: PERM.MANAGE_MESSAGES, label: "Manage messages", hint: "Delete anyone's messages and pin" },
  { bit: PERM.MANAGE_CHANNELS, label: "Manage channels", hint: "Create, rename, move, delete channels" },
  { bit: PERM.MANAGE_GUILD, label: "Manage guild", hint: "Rename the guild and manage emoji" },
  { bit: PERM.MANAGE_ROLES, label: "Manage roles", hint: "Define and assign roles" },
  { bit: PERM.MUTE_MEMBERS, label: "Mute members", hint: "Time-out members from posting" },
  { bit: PERM.SYNC_HOST, label: "Sync host", hint: "Preferred always-on sync/relay host" },
];

export const has = (perms, bit) => (perms & bit) === bit;

// Every permission — what the one-click "Make admin" role grants.
export const PERM_ALL = PERM_LIST.reduce((n, p) => n | p.bit, 0);

// adminRoleOf picks the role "Make admin" hands out. A role holding every
// permission is the admin role by definition; failing that, a role the guild
// already calls "Admin" is reused rather than minting a second one beside it
// (two indistinguishable "Admin" groups in the member list, two in the picker).
// null means there is none yet and one has to be made.
export function adminRoleOf(roles) {
  const list = roles || [];
  return (
    list.find((r) => r.perms === PERM_ALL) ||
    list.find((r) => (r.name || "").trim().toLowerCase() === "admin") ||
    null
  );
}

// permLabels lists the names of the permissions in a mask, in editor order.
export function permLabels(perms) {
  return PERM_LIST.filter((p) => has(perms, p.bit)).map((p) => p.label);
}
