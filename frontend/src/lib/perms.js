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
