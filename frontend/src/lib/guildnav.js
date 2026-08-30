// guildnav.js — the one description of what is in the guild hub.
//
// It is settingsnav.js's sibling, and for the same reason. The hub was sixteen
// chevron rows in one scrolling column, drilled into one at a time: the dialog
// swung 380↔460 wide and 168↔810 tall between panels, so the box under the
// cursor moved on every navigation — 40px across and 321px down, measured —
// and every errand was open hub → click row → Escape → Escape. Moderation log,
// Members, Roles and Bans are four things one moderation session touches
// together and no two of them could be on screen in the same minute.
//
// So the hub is a rail: every destination visible at once on the left, one
// constant 1000×660 box, and moving between pages changes only the pane. The
// phone keeps the drill-down list — there is no room for a rail on a 390px
// sheet, and a sheet you can flick through is already the overview — reading
// this same table, so the two cannot drift.
//
// `kind` is the S.modal kind the entry opens (see MODAL_LOADERS in App.svelte).
// `gate(g)` decides whether this reader is offered the entry at all; a row you
// cannot use, you do not see. `sub` is one honest sentence about what is behind
// the door, and it may be a FUNCTION of the guild, because half of these doors
// lead somewhere different depending on what the reader may do. A plain member
// was being told Members would let them "hand out roles & moderate"; the panel
// then told them the truth once they were inside it, which is one screen too
// late.

import { PERM, has } from "./perms.js";
import { plural } from "./plural.js";

const canRoles = (g) => !!g && (g.isOwner || has(g.myPerms || 0, PERM.MANAGE_ROLES));
const canModerate = (g) =>
  !!g &&
  (g.isOwner ||
    has(g.myPerms || 0, PERM.MANAGE_MEMBERS) ||
    has(g.myPerms || 0, PERM.MANAGE_ROLES) ||
    has(g.myPerms || 0, PERM.MUTE_MEMBERS));

export const GUILD_GROUPS = [
  {
    label: "Identity",
    items: [
      {
        kind: "guildSettings",
        title: "Overview",
        icon: "gear",
        sub: (g) =>
          has(g?.myPerms || 0, PERM.MANAGE_GUILD)
            ? "Name, icon, banner & description"
            : "The guild's name, icon and banner",
      },
    ],
  },
  {
    label: "People",
    items: [
      {
        kind: "members",
        title: "Members",
        icon: "members",
        // The row that got you here must not promise authority the panel then
        // takes away. A plain member is coming to look, and that is worth a
        // door of its own — it just is not the same door.
        sub: (g) => (canModerate(g) ? "Search, sort, hand out roles & moderate" : "See who is here"),
      },
      {
        kind: "roles",
        title: "Roles",
        icon: "spark",
        sub: "Permissions & who can do what",
        gate: canRoles,
      },
      {
        kind: "bans",
        title: "Banned members",
        icon: "door",
        sub: "Review & lift bans",
        gate: (g) => !!g?.canManage,
      },
      {
        kind: "invite",
        title: "Invite people",
        icon: "members",
        sub: "Share a code that lets people in",
        gate: (g) => !!g?.canManage,
      },
      {
        // Not gated: the log is already on every member's disk, so hiding the
        // screen would hide the record from the people it is about and from
        // nobody else.
        kind: "modLog",
        title: "Moderation log",
        icon: "list",
        sub: "Every signed decision, checked on your device",
      },
    ],
  },
  {
    label: "Expression",
    items: [
      {
        kind: "emoji",
        title: "Guild emoji",
        icon: "smile",
        sub: "Custom emoji everyone here can use",
      },
      {
        kind: "events",
        title: "Events & calendar",
        icon: "calendar",
        sub: "The crew's shared board — what's coming up",
      },
    ],
  },
  {
    label: "Data",
    items: [
      {
        // "Insights" rather than "Stats", because that is the first word of the
        // panel it opens: a rail entry and the page it lands on disagreeing
        // about their own name is the small kind of lie this table exists to
        // stop.
        kind: "stats",
        title: "Insights",
        icon: "poll",
        sub: "Activity & diagnostics for this guild",
      },
      {
        kind: "chronicle",
        title: "Archive",
        icon: "clock",
        sub: (g, S) =>
          S?.chronicle
            ? `${S.chronicle.source || "Imported history"} — ${plural(S.chronicle.messages, "message")}`
            : "Older history imported into this guild",
      },
      {
        // Offered whether or not one is already attached: a second import
        // supersedes the first, which is how a policy anyone regrets gets
        // corrected.
        kind: "chronicleImport",
        title: "Import a chat archive",
        icon: "folder",
        sub: (g, S) =>
          S?.chronicle
            ? "Import again to replace the archive this guild carries"
            : "Bring a community's exported history in, and see what it costs first",
        gate: (g) => !!g?.isOwner,
      },
      {
        // Not gated, on the same reasoning as the moderation log. A retention
        // policy deletes the reader's own copy of their own conversation off
        // their own device on a timer nobody watches, so the people it acts on
        // are exactly the people who need to be told it exists.
        kind: "retention",
        title: "Message history",
        icon: "clock",
        sub: (g) =>
          g?.canManage
            ? "How long members keep messages before their copy prunes itself"
            : "How long this guild keeps messages before your copy prunes itself",
      },
      {
        // A page rather than a row that downloads on click. What it writes is
        // an unencrypted copy of every word in the guild, and it used to be a
        // chevron identical in shape to the ten rows that merely open a panel,
        // two rows under the retention promise it undoes.
        kind: "exportGuild",
        title: "Export history",
        icon: "download",
        sub: "Every channel, as one plain Markdown file on this disk",
      },
    ],
  },
  {
    label: "Danger zone",
    danger: true,
    items: [
      {
        kind: "ownership",
        title: "Ownership & heir",
        icon: "crown",
        sub: (g) =>
          g?.isOwner
            ? "Hand the guild over, or name who can take it if you vanish."
            : "You are this guild's heir.",
        gate: (g) => !!(g?.isOwner || g?.heir),
      },
      {
        // "Delete guild" is what it used to say, in a section headed DANGER
        // ZONE, to somebody who has run a community — and it is a local
        // unsubscribe. Nothing is deleted for anyone else. The label says what
        // happens now, and the page it opens says the rest.
        kind: "leaveGuild",
        title: (g) => (g?.isOwner ? "Leave and delete my copy" : "Leave guild"),
        icon: (g) => (g?.isOwner ? "trash" : "door"),
        sub: (g) =>
          g?.isOwner
            ? "Nobody can add or remove members after you go."
            : "Take this guild off this device.",
      },
    ],
  },
];

// Resolve a field that may be a plain value or a function of the guild.
export const guildField = (v, g, S) => (typeof v === "function" ? v(g, S) : v);

// The entries this reader is offered, group by group, gates applied.
export function guildGroupsFor(g) {
  return GUILD_GROUPS.map((grp) => ({
    ...grp,
    items: grp.items.filter((it) => !it.gate || it.gate(g)),
  })).filter((grp) => grp.items.length);
}

export const GUILD_ITEMS = GUILD_GROUPS.flatMap((grp) => grp.items);

export function guildItem(kind) {
  return GUILD_ITEMS.find((i) => i.kind === kind) || null;
}

export function inGuildHub(kind) {
  return !!guildItem(kind);
}

// guildRailFor answers "should this dialog wear the guild rail, and which entry
// should be lit".
//
// Unlike the settings rail, most of these panels are reachable from somewhere
// else as well — Events from the header's calendar button, Roles from the
// member panel's notice, Stats from a keyboard shortcut, the moderation log
// from the inbox. Those are ordinary dialogs and must stay ordinary dialogs;
// only a panel reached through the hub wears the hub's furniture. The trail
// says which: `hub` is stamped on the modal when the hub is opened, carried
// sideways by switchPanel, and left on the stack entry when a page drills into
// a sub-panel of its own (Archive → Import). A stamped entry that is not itself
// a rail page lights nothing, exactly as a settings sub-panel does.
export function guildRailFor(modal, stack = []) {
  if (modal?.hub) return inGuildHub(modal.kind) ? modal.kind : "";
  for (let i = stack.length - 1; i >= 0; i--) {
    if (stack[i]?.hub) return inGuildHub(stack[i].kind) ? stack[i].kind : "";
  }
  return "";
}

// Whether this dialog is on the hub trail at all — the rail is drawn for a
// drilled sub-panel too, with the page it came from still lit.
export function onGuildTrail(modal, stack = []) {
  return !!(modal?.hub || stack.some((s) => s?.hub));
}
