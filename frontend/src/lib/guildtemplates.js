// Starter layouts offered on the create-a-guild dialog.
//
// A template is nothing but a list of categories and channels, played back
// through the ORDINARY creation paths — api.createCategory and
// api.createChannel, the same two calls the sidebar's "+" makes. There is no
// template record, nothing on the wire, and nothing to migrate: five minutes
// after creation a templated guild is indistinguishable from one built by
// hand, which is the point. The owner is meant to rename and delete freely.
//
// They are deliberately SMALL. Every channel is a signed governance op plus a
// guild-meta frame, and a starter layout that ships fourteen rooms hands a new
// community a sidebar it has to prune before it can use. Three to six.

// EMPTY_TEMPLATE is the default: the guild gets whatever CreateGuild gives it
// (one #general) and nothing else. Offering "start empty" as a first-class
// tile rather than as the absence of a choice is what keeps the templates from
// reading as compulsory.
export const EMPTY_TEMPLATE = "empty";

export const GUILD_TEMPLATES = [
  {
    id: EMPTY_TEMPLATE,
    name: "Start empty",
    blurb: "Just #general. Build it yourself.",
    icon: "hash",
    plan: [],
  },
  {
    id: "community",
    name: "Community",
    blurb: "A front door, a lounge, and a place to talk.",
    icon: "members",
    plan: [
      {
        category: "Welcome",
        channels: [
          { name: "welcome", type: "text" },
          { name: "announcements", type: "announcement" },
        ],
      },
      {
        category: "Community",
        channels: [
          { name: "introductions", type: "text" },
          { name: "off-topic", type: "text" },
          { name: "Lounge", type: "voice" },
        ],
      },
    ],
  },
  {
    id: "study",
    name: "Study group",
    blurb: "Somewhere to ask, somewhere to share, somewhere to sit together.",
    icon: "list",
    plan: [
      {
        category: "Study",
        channels: [
          { name: "resources", type: "text" },
          { name: "questions", type: "forum" },
          { name: "Study Hall", type: "voice" },
        ],
      },
    ],
  },
  {
    id: "project",
    name: "Project",
    blurb: "Standups, decisions, and the work itself.",
    icon: "spark",
    plan: [
      {
        category: "Project",
        channels: [
          { name: "standup", type: "text" },
          { name: "decisions", type: "announcement" },
        ],
      },
      {
        category: "Work",
        channels: [
          { name: "code-review", type: "text" },
          { name: "Huddle", type: "voice" },
        ],
      },
    ],
  },
];

export function templateById(id) {
  return GUILD_TEMPLATES.find((t) => t.id === id) || GUILD_TEMPLATES[0];
}

// channelCount is what the tile prints under its name, so the choice can be
// made without reading a plan.
export function templateChannelCount(t) {
  return (t.plan || []).reduce((n, c) => n + c.channels.length, 0);
}

// slugChannelName is the naming rule a text channel's name is held to: a
// guild whose sidebar mixes `#general` with `#Welcome & Rules` reads as one
// nobody is in charge of. Lowercased, runs of anything that is not a letter,
// digit or Unicode mark folded to a single dash, edges trimmed.
//
// Unicode-aware on purpose: an Arabic or Cyrillic channel name must survive,
// and \w would eat it. `\p{L}\p{N}\p{M}` keeps letters, numbers and combining
// marks in every script; everything else — spaces, `&`, emoji, punctuation —
// becomes structure.
//
// VOICE ROOMS ARE NOT SLUGGED. A voice channel is not addressed with a `#`
// and is not named like an address ("Study Hall", not "study-hall"), which is
// why the empty-channel hero already branches on it. Slugging one would be
// enforcing a convention the rest of the app does not follow.
export function slugChannelName(name) {
  const s = String(name || "")
    .toLowerCase()
    .replace(/[^\p{L}\p{N}\p{M}]+/gu, "-")
    .replace(/^-+|-+$/g, "");
  return s;
}
