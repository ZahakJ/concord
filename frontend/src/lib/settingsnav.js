// settingsnav.js — the one description of what is in Settings.
//
// It used to be a flat card of eight chevron rows: your profile, the theme,
// your microphone, your chimes, what leaves the device, your office hours, the
// rendezvous address and device linking, all the same size, all one rung down,
// none of them visible at the same time as any other. Eight unrelated
// destinations under a single heading is not a hierarchy; it is a list of
// everything, and the only way to learn it was to open all eight.
//
// So they are grouped by what you came to do. The groups are short on purpose —
// two or three entries — because a group you can read at a glance is worth more
// than a group you have to scan. The desktop rail draws them as
// sections; the phone sheet draws the same table as headed cards; and because
// both read from here, they cannot drift apart.
//
// `kind` is the S.modal kind the entry opens (see MODAL_LOADERS in App.svelte).
// `sub` is the phone list's second line and the rail's tooltip: one honest
// sentence about what is behind the door, never a restatement of the title.

export const SETTINGS_GROUPS = [
  {
    label: "You",
    items: [
      {
        // The landing page, and the reason the desktop dialog opens on
        // something useful rather than on a menu of eight doors.
        kind: "settings",
        title: "Account",
        icon: "shield",
        sub: "Recovery phrase, backup, updates & signing out",
      },
      {
        kind: "profile",
        title: "Your profile",
        icon: "user",
        sub: "Name, avatar, colour, status & bio",
      },
      {
        kind: "appearance",
        title: "Appearance",
        icon: "palette",
        sub: "Theme, colour, shape & type",
      },
    ],
  },
  {
    label: "Alerts",
    items: [
      {
        kind: "notifications",
        title: "Notifications & sounds",
        icon: "bell",
        sub: "What pings you, and how loud",
      },
      {
        kind: "devices",
        title: "Voice & video",
        icon: "mic",
        sub: "Microphone, speaker & camera",
      },
    ],
  },
  {
    label: "Privacy",
    items: [
      {
        kind: "privacy",
        title: "Privacy & safety",
        icon: "lock",
        sub: "What leaves this device, and who can reach you",
      },
      {
        kind: "bookings",
        title: "Bookings",
        icon: "clock",
        sub: "Office hours & your public booking page",
      },
    ],
  },
  {
    label: "This device",
    items: [
      {
        kind: "connection",
        title: "Connection",
        icon: "link",
        sub: "Rendezvous address & diagnostics",
      },
      {
        kind: "linkDevice",
        title: "Link a device",
        icon: "devices",
        sub: "Add your phone or another computer",
      },
    ],
  },
];

// Flat, in rail order. Used to tell "did I move up or down the rail" and by the
// test that keeps this table honest.
export const SETTINGS_ITEMS = SETTINGS_GROUPS.flatMap((g) => g.items);

export function settingsItem(kind) {
  return SETTINGS_ITEMS.find((i) => i.kind === kind) || null;
}

// Whether a modal kind belongs to the settings surface. `settings` itself is
// the Account entry, so it is in the table rather than a special case.
export function inSettings(kind) {
  return !!settingsItem(kind);
}
