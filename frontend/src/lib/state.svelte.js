// state.svelte.js — Concord's shared UI state (Svelte 5 runes) and the actions
// that mutate it. Components import { S, ... } and read/mutate S's properties;
// api.js stays the only transport layer underneath.
import { api, on } from "./api.js";
import { notify } from "./notify.js";
import { containsMention } from "./markdown.js";
import { playVoiceJoin, playVoiceLeave, playMention, playDM } from "./sounds.js";
import { PERM, has } from "./perms.js";

export const S = $state({
  ready: false,
  identity: { peerId: "", fingerprint: "", displayName: "" },
  displayName: "",
  guilds: [],
  activeGuildId: "",
  activeChannelId: "",
  messages: [], // the active channel's messages, oldest first
  members: [],
  roles: [], // active guild's roles (highest position first)
  contacts: [],
  blocked: [], // account fingerprints you've blocked

  replyingTo: null, // message being replied to
  editing: null, // message being edited (Message.svelte owns the draft)
  pickerTarget: null, // "composer" | message object (shared emoji picker)
  // Floating profile card: { fingerprint, rect } where rect is the anchor's
  // viewport box. Opened by hovering/clicking a mention or a member row.
  profilePopover: null,
  modal: null,
  // Panels we drilled through to reach S.modal, so Back can walk out the way
  // it came in (see openPanel/backPanel).
  modalStack: [], // { kind, ... }
  toasts: [], // [{ id, kind, text }] — kind: "info" | "success" | "error"
  quickSwitcher: false,
  // Raised by the command palette's "Set status" action; ChannelList consumes
  // it and opens the status popover anchored to the self row.
  statusPopRequest: false,

  // Mobile shell: a coarse pointer or narrow viewport gets the drawer-based
  // MobileShell instead of the desktop 4-column grid. drawerOpen = the left
  // nav drawer (guild rail + channels); membersOpen = the right member sheet.
  isMobile: detectMobile(),
  drawerOpen: false,
  membersOpen: false,

  // Connectivity for the connection pill: { peers, bootstrapReached,
  // hasBootstrap, outOfSyncGuilds }, refreshed on presence events + a slow poll.
  // null until the first fetch (login).
  netStatus: null,

  // feedLoading: a channel switch is fetching history (drives the skeleton —
  // without it the OLD channel's rows linger under the new header).
  feedLoading: false,
  // loadingOlder / feedReachedStart: scroll-up pagination state. The initial
  // load is only the recent window; older rows are paged in on demand.
  loadingOlder: false,
  feedReachedStart: false,
  // restarting: a self-update restart is in flight; the app shows a full-bleed
  // "right back" curtain so the outgoing version is never visible mid-swap.
  restarting: false,
  // unread[channelId] = { count, mentions } — counts survive refresh via the
  // localStorage last-read map (recomputed on load).
  unread: {},
  mutes: loadJSON("concord.mutes", {}), // channelId -> true
  // Guild-rail layout: device-local ordering + Discord-style folders (see
  // lib/rail.js). An array of { t:"g", id } / { t:"f", id, name, color, open,
  // ids }. Reconciled against the live guild list on render.
  rail: loadJSON("concord.rail", []),
  readAnchor: "", // ISO time we'd last read the active channel (for the "new" line)

  // Privacy + appearance prefs. linkPreviews defaults OFF: fetching a preview
  // for a link in a message reveals your IP + online time to that link's host,
  // so a message with a link to an attacker-controlled server is a zero-click
  // deanonymization. Opt in only if you trust who you talk to.
  // theme: "dark" | "light" | "system"; density: "cozy" | "compact";
  // accent: a preset hex, or "" to follow the profile color (the old behavior).
  // Spread over defaults so prefs saved before a key existed still get it.
  prefs: {
    linkPreviews: false,
    showDeleted: false, // off = deleted messages vanish; on = leave a faint marker
    hideCallIp: false, // on = always relay calls through the rendezvous (hide IP)
    theme: "dark",
    accent: "",
    themePack: "", // curated full-palette skin ("" = default palette)
    // Shape/typeface overrides. "" = follow the active theme pack, which now
    // carries a corner radius and a UI face of its own, not just colors.
    shape: "",
    font: "",
    density: "cozy",
    clock: "system", // "system" | "12" | "24" — timestamp hour format
    memberPanel: true, // show the right-hand member panel (toggle with Ctrl+U)
    // Chosen call hardware (see lib/devices.js). "" = whatever the OS picked,
    // which is also what a stored id falls back to when that device is gone.
    micId: "",
    speakerId: "",
    cameraId: "",
    // Call audio knobs. The defaults are exactly "what the browser does on its
    // own", so an untouched install sounds the same as it always has.
    outputVolume: 1, // master playback level for a call, 0..1
    micGain: 1, // mic boost/trim, 0.25..4
    micGate: 0, // noise gate threshold, 0 = off
    // Opus target, bits/s. 64k is a real step up from the browser's ~32k
    // default and still modest on a mesh where you send one copy per peer.
    voiceBitrate: 64000,
    echoCancel: true,
    noiseSuppress: true,
    autoGain: true,
    ...loadJSON("concord.prefs", {}),
  },

  typingList: [], // [{ from, label, timer }]

  voice: null, // { mesh, channelId }
  joiningVoice: "", // channelId we're mid-join on (before S.voice is set)
  // Soft call lock (see voice.go): channelId -> true when a call is locked;
  // channelId -> [fingerprints] of people knocking to be let in.
  callLocks: {},
  callKnocks: {},
  knocking: "", // a locked channelId we're waiting to be admitted to
  // A moderator moved or disconnected us; App.svelte acts on it and clears it.
  // { action: "move"|"disconnect", by, channelId?, name? }
  moderatedVoice: null,
  admittedJoin: "", // set when admitted → App.svelte joins for real
  voiceParticipants: [],
  voiceSpeaking: [],
  voicePeerFpr: {},
  // voiceRosters: guild-wide "who is in each voice channel", built from gossip
  // presence heartbeats so the sidebar shows call participants without joining.
  // Shape: { channelId: { peerId: { fingerprint, ts } } }.
  voiceRosters: {},
  // peers currently screen-sharing, keyed by peerId (surfaced as a share icon).
  voiceSharing: {},
  // DM channels whose incoming call the user dismissed (declined) — suppressed
  // until that call ends and the roster clears.
  dismissedCalls: [],
  muted: false,
  deafened: false, // we've silenced all incoming call audio (implies mic muted)
  peerVolumes: {}, // peerId -> 0..1 local playback gain (absent = full)
  sharing: false, // we are screen-sharing
  cameraOn: false, // our camera is on
  // videoTiles: [{ key, peerId, kind, self }] — one per live video source
  // (camera/screen, ours or a peer's). Streams live in the non-reactive
  // videoStreams map below; this array just drives which <video> tiles render.
  videoTiles: [],

  searchQuery: "",
  searchResults: null, // null = closed, [] = no hits
  searchChips: [], // parsed operator chips [{key, raw, label}] shown above results
  searchTerms: [], // free-text terms, for match highlighting in results
  searchLoading: false, // a search round-trip is in flight
  showPins: false,

  // Local image-text (OCR) search status: { available, engine, counts }. Null
  // until first fetched; drives the settings readout. No engine ⇒ available:false.
  ocr: null,

  // newBelow: messages arrived while the user was scrolled up reading history
  // (we deliberately do NOT yank the feed to the bottom in that case).
  newBelow: false,

  // update: an available app update {available, current, latest, url, notes},
  // or null. Drives the "Download" banner. Dismissal is per-version.
  update: null,

  // contextMenu: right-click menu {x, y, items} or null.
  contextMenu: null,

  // pendingLinkCode: a device-link code that arrived via a concord:// deep
  // link (OS camera scanned the QR). The Login screen consumes it.
  pendingLinkCode: "",
});

export const activeGuild = () => S.guilds.find((g) => g.id === S.activeGuildId) || null;
export const activeChannel = () =>
  activeGuild()?.channels.find((c) => c.id === S.activeChannelId) || null;
export const memberByFpr = (fpr) => S.members.find((m) => m.fingerprint === fpr);

// ---- voice video streams (camera + screenshare) ----
// MediaStreams are held outside the reactive store (a $state proxy corrupts
// them); S.videoTiles mirrors the keys+meta to drive rendering. Each video
// source (a peer's camera, a peer's screen, our own preview) is one tile keyed
// uniquely, so a peer can show camera AND screen at once.
const videoStreams = new Map(); // key -> MediaStream
const videoMetaMap = new Map(); // key -> { peerId, kind, self }

export function getVideoStream(key) {
  return videoStreams.get(key) || null;
}

export function setVideoStream(key, stream, meta = {}) {
  if (stream) {
    videoStreams.set(key, stream);
    videoMetaMap.set(key, meta);
  } else {
    videoStreams.delete(key);
    videoMetaMap.delete(key);
  }
  S.videoTiles = [...videoStreams.keys()].map((k) => ({ key: k, ...videoMetaMap.get(k) }));
  // Sidebar "sharing" icon: any live video from a remote peer marks them.
  const sh = {};
  for (const m of videoMetaMap.values()) if (m.peerId && !m.self) sh[m.peerId] = true;
  S.voiceSharing = sh;
}

export function clearVideoStreams() {
  videoStreams.clear();
  videoMetaMap.clear();
  S.videoTiles = [];
}

// nameFor: the single source of truth for a peer's display name — the current
// learned member name (same as the member list), falling back to a message's
// self-asserted name, then a short fingerprint. Keeps chat and roster in sync.
export function nameFor(fpr, frozenName = "") {
  // A browser guest has no account to look up — their name rides in the
  // fingerprint slot ("guest:Alice"). One branch here labels them everywhere:
  // sidebar, call roster, video tiles.
  if (isGuestFpr(fpr)) return `${guestName(fpr)} (guest)`;
  // Fall back beyond the active guild's roster to the contact's learned profile
  // name, so people show their display name in contact/add/block lists (and any
  // guild they're not a current member of) instead of a cryptic fingerprint.
  return (
    memberByFpr(fpr)?.name ||
    (fpr && S.contacts.find((c) => c.fingerprint === fpr)?.name) ||
    frozenName ||
    (fpr ? fpr.slice(0, 9) : "?")
  );
}

// customEmojiMap: {name -> imageDataURI} for the active guild's custom emoji.
export function customEmojiMap() {
  const list = activeGuild()?.emoji || [];
  const m = {};
  for (const e of list) m[e.name] = e.image;
  return m;
}

// detectMobile reports whether to use the touch/drawer layout: a coarse pointer
// (phone/tablet) or a narrow viewport. Re-evaluated on resize/orientation via
// the listener below so a desktop window narrowed past the breakpoint adapts too.
function detectMobile() {
  if (typeof window === "undefined") return false;
  const coarse = window.matchMedia?.("(pointer: coarse)")?.matches;
  const narrow = window.matchMedia?.("(max-width: 768px)")?.matches;
  return !!(coarse || narrow);
}

if (typeof window !== "undefined") {
  const sync = () => {
    const now = detectMobile();
    if (now !== S.isMobile) S.isMobile = now;
  };
  window.addEventListener("resize", sync);
  window.matchMedia?.("(orientation: portrait)")?.addEventListener?.("change", sync);
}

// modalNav carries the direction of the last modal navigation to the panel
// that's about to mount: 1 = opened a sub-panel, -1 = went back to its parent,
// 0 = a fresh dialog. A plain object rather than reactive state because
// Modal.svelte reads it exactly once, at mount, to pick an entrance animation.
export const modalNav = { dir: 0 };

// openPanel navigates INTO a sub-panel of the modal that's open now, so the
// back arrow returns there and the entrance slides the right way. The panel we
// were on is pushed onto S.modalStack: without a stack, a three-deep path
// (Settings → Privacy → Blocked) forgets its middle step and the second Back
// has nowhere to go.
export function openPanel(kind, from) {
  modalNav.dir = 1;
  if (S.modal) S.modalStack = [...S.modalStack, S.modal];
  S.modal = { kind, from };
}

// backPanel walks one step out, the way you came in.
export function backPanel() {
  modalNav.dir = -1;
  if (S.modalStack.length) {
    S.modal = S.modalStack[S.modalStack.length - 1];
    S.modalStack = S.modalStack.slice(0, -1);
  } else {
    // Opened with a plain `from` and no stack behind it (a panel reached
    // directly): fall back to that parent.
    S.modal = S.modal?.from ? { kind: S.modal.from } : null;
  }
}

// ---- persistence helpers (device-local UI state) ----

function loadJSON(key, fallback) {
  try {
    return JSON.parse(localStorage.getItem(key)) ?? fallback;
  } catch {
    return fallback;
  }
}
function saveJSON(key, value) {
  try {
    localStorage.setItem(key, JSON.stringify(value));
  } catch {
    /* storage full/blocked: unread state just won't survive refresh */
  }
}

const lastRead = loadJSON("concord.lastRead", {}); // channelId -> ISO time
// Message ids already accounted for (unread bump + chime/notify), so the
// backend re-emitting a message (edit/reaction/pin/sync) doesn't double-count.
const countedMsgIds = new Set();

// markRead marks a channel read as of now. throughTime (the newest message's
// `sent`, when known) guards against peer clock skew: if a message we've
// actually seen is timestamped ahead of our clock, "now" alone would leave it
// counting as unread forever, so we advance lastRead past it.
//
// The cursor is also pushed to the backend, which fans it out to every other
// session and linked device (they get a "read-state" event), so reading here
// clears the badge everywhere.
export function markRead(channelId, throughTime = "") {
  if (!channelId) return;
  let t = new Date().toISOString();
  if (throughTime && new Date(throughTime) > new Date(t)) t = new Date(throughTime).toISOString();
  lastRead[channelId] = t;
  saveJSON("concord.lastRead", lastRead);
  if (S.unread[channelId]) {
    const u = { ...S.unread };
    delete u[channelId];
    S.unread = u;
  }
  api.markRead(channelId, Date.parse(t)).catch(() => {});
}

// markAllRead clears unread across every channel (Shift+Esc).
export function markAllRead() {
  const now = new Date().toISOString();
  const at = Date.parse(now);
  for (const g of S.guilds)
    for (const c of g.channels) {
      lastRead[c.id] = now;
      api.markRead(c.id, at).catch(() => {});
    }
  saveJSON("concord.lastRead", lastRead);
  S.unread = {};
}

export function toggleMute(channelId) {
  const m = { ...S.mutes };
  if (m[channelId]) delete m[channelId];
  else m[channelId] = true;
  S.mutes = m;
  saveJSON("concord.mutes", m);
}

// clockOpts turns the clock pref into Intl options ({} = follow the locale).
// Reading S.prefs.clock makes any $derived/template that calls it reactive.
export function clockOpts() {
  const c = S.prefs.clock;
  if (c === "12") return { hour12: true };
  if (c === "24") return { hour12: false };
  return {};
}

// fmtClock formats an ISO timestamp as HH:MM honoring the clock pref. Shared by
// the feed, search, and scheduled views so the 12/24h choice is global.
export function fmtClock(iso) {
  try {
    return new Date(iso).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", ...clockOpts() });
  } catch {
    return "";
  }
}

// toggleMemberPanel flips the right-hand member panel (Ctrl/Cmd+U).
export function toggleMemberPanel() {
  setPref("memberPanel", !S.prefs.memberPanel);
}

// setPeerVolume sets one call participant's LOCAL playback gain (0..1) — silence
// or quiet just them, for you only. Nothing is sent to anyone; it's your own
// speakers. Volume 1 is the default, so it's dropped from the map.
export function setPeerVolume(peerId, vol) {
  const v = Math.max(0, Math.min(1, vol));
  const pv = { ...S.peerVolumes };
  if (v === 1) delete pv[peerId];
  else pv[peerId] = v;
  S.peerVolumes = pv;
  S.voice?.mesh.setPeerVolume(peerId, v);
}

// togglePeerMute flips a participant between silenced (0) and full (1) for you.
export function togglePeerMute(peerId) {
  setPeerVolume(peerId, S.peerVolumes[peerId] === 0 ? 1 : 0);
}

// markUnread rewinds a channel's read cursor to just before a message, so it
// (and everything after) shows as unread again, with the NEW divider restored.
export async function markUnread(channelId, msg) {
  if (!channelId || !msg) return;
  const before = new Date(new Date(msg.sent).getTime() - 1).toISOString();
  lastRead[channelId] = before;
  saveJSON("concord.lastRead", lastRead);
  // Only THIS channel's cursor moved — recount just it, not every channel.
  try {
    const u = await countChannelUnread(channelId);
    const next = { ...S.unread };
    if (u) next[channelId] = u;
    else delete next[channelId];
    S.unread = next;
  } catch {
    /* leave the existing badge as-is if the channel isn't readable now */
  }
  if (channelId === S.activeChannelId) S.readAnchor = before;
}

// ---- shared right-click context menu ----
// S.contextMenu = { x, y, items:[{label, icon?, danger?, onClick}|null] } | null
// opts (used by the mobile action-sheet presentation): `title` labels the
// sheet; `quick` = {emojis:[…], onPick(emoji)} renders a quick-reaction row.
export function openContextMenu(e, items, opts = {}) {
  e.preventDefault();
  e.stopPropagation();
  S.contextMenu = { x: e.clientX, y: e.clientY, items: items.filter(Boolean), ...opts };
}
export function closeContextMenu() {
  S.contextMenu = null;
}

// Overlay-closer stack: component-local overlays that AREN'T S.modal (the QR
// scanner, the Ring/Banner studios, …) register their onClose here on mount, so
// the mobile hardware-back button can dismiss the topmost one before falling
// through to closing a modal or exiting the app.
const overlayClosers = [];
export function registerOverlay(close) {
  overlayClosers.push(close);
  return () => {
    const i = overlayClosers.indexOf(close);
    if (i >= 0) overlayClosers.splice(i, 1);
  };
}
// closeTopOverlay dismisses the highest-priority open overlay and reports
// whether it closed anything — the mobile back handler consults this first.
export function closeTopOverlay() {
  if (S.contextMenu) {
    closeContextMenu();
    return true;
  }
  if (overlayClosers.length) {
    overlayClosers[overlayClosers.length - 1]();
    return true;
  }
  if (S.profilePopover) {
    closeProfilePopover();
    return true;
  }
  return false;
}

// guildMenuItems: everything you can do to a guild, in one list — so the rail's
// right-click, the header's "More" menu and the mobile sheet can never drift
// apart. Each entry is permission-gated the same way the backend gates the op:
// what you can't do, you don't see.
export function guildMenuItems(g) {
  if (!g || g.kind === "dm") return [];
  const canRoles = g.isOwner || has(g.myPerms, PERM.MANAGE_ROLES);
  return [
    g.canManage && {
      label: "Invite / add people",
      icon: "members",
      onClick: async () => (S.modal = { kind: "invite", code: await api.inviteCode(g.id) }),
    },
    {
      label: "Mark as read",
      icon: "check",
      onClick: () => g.channels.forEach((c) => markRead(c.id)),
    },
    { label: "Stats & diagnostics", icon: "poll", onClick: () => (S.modal = { kind: "stats", guildId: g.id }) },
    { sep: true },
    { label: "Guild emoji", icon: "smile", onClick: () => (S.modal = { kind: "emoji" }) },
    g.isOwner && { label: "Rename guild", icon: "edit", onClick: () => (S.modal = { kind: "rename" }) },
    canRoles && { label: "Roles & permissions", icon: "spark", onClick: () => (S.modal = { kind: "roles" }) },
    g.canManage && { label: "Banned members", icon: "door", onClick: () => (S.modal = { kind: "bans" }) },
    { sep: true },
    {
      label: g.isOwner ? "Delete guild" : "Leave guild",
      icon: g.isOwner ? "trash" : "door",
      danger: true,
      onClick: () => confirmLeaveGuild(g),
    },
  ].filter(Boolean);
}

// confirmLeaveGuild: leaving is destructive and irreversible for the owner, so
// it always goes through a confirm — wherever it's triggered from.
export function confirmLeaveGuild(g) {
  if (!g) return;
  const verb = g.isOwner ? "Delete" : "Leave";
  S.modal = {
    kind: "confirm",
    title: `${verb} "${g.name}"?`,
    body: "Its messages will be removed from this device.",
    confirmLabel: verb,
    onConfirm: async () => {
      S.modal = null;
      await api.leaveGuild(g.id);
      S.activeGuildId = "";
      S.activeChannelId = "";
      clearFeed();
      await refreshGuilds();
      if (S.guilds.length) selectGuild(S.guilds[0].id);
      flash(g.isOwner ? "Guild deleted" : "Left guild");
    },
  };
}

// commitRail persists the guild-rail layout (ordering + folders). Device-local.
export function commitRail(items) {
  S.rail = items;
  saveJSON("concord.rail", items);
}

// setPref updates a persisted privacy preference.
export function setPref(key, value) {
  S.prefs = { ...S.prefs, [key]: value };
  saveJSON("concord.prefs", S.prefs);
}

// moveChannelToCategory reassigns a channel's category (preserving type/order/topic).
export async function moveChannelToCategory(channel, categoryId) {
  try {
    await api.setChannelMeta(
      S.activeGuildId,
      channel.id,
      channel.type || "",
      categoryId,
      channel.position || 0,
      channel.topic || "",
    );
    await refreshGuilds();
  } catch (err) {
    flash(err);
  }
}

// reorderChannel places `channel` at display index `index` inside `categoryId`
// (an empty string = uncategorized), then renumbers that category's channels to
// sequential positions so the sidebar's position-sort renders exactly the
// dropped order. Positions/categories are applied to local state first (S is
// deeply reactive, so the list snaps into place immediately), then persisted —
// only rows whose category or position actually changed hit the API. On
// failure, refreshGuilds() restores the backend's truth.
export async function reorderChannel(channel, categoryId, index) {
  const g = S.guilds.find((x) => x.id === S.activeGuildId);
  if (!g) return;
  const inCat = (c) => (c.category || "") === categoryId;
  const byPos = (a, b) => (a.position || 0) - (b.position || 0);
  const before = g.channels.filter(inCat).sort(byPos).map((c) => c.id);
  const list = g.channels.filter((c) => inCat(c) && c.id !== channel.id).sort(byPos);
  list.splice(Math.max(0, Math.min(index, list.length)), 0, channel);
  // Dropped back into its own slot? The visible order is unchanged — skip the
  // renumbering writes entirely.
  if (before.join("\n") === list.map((c) => c.id).join("\n")) return;
  const moves = list
    .map((c, i) => ({ c, pos: i }))
    .filter(({ c, pos }) => (c.category || "") !== categoryId || (c.position || 0) !== pos);
  for (const { c, pos } of moves) {
    c.category = categoryId;
    c.position = pos;
  }
  try {
    for (const { c, pos } of moves) {
      await api.setChannelMeta(S.activeGuildId, c.id, c.type || "", categoryId, pos, c.topic || "");
    }
    await refreshGuilds();
  } catch (err) {
    flash(err);
    await refreshGuilds();
  }
}

// setChannelTopic updates a channel's topic (preserving type/category/order).
export async function setChannelTopic(channel, topic) {
  try {
    await api.setChannelMeta(
      S.activeGuildId,
      channel.id,
      channel.type || "",
      channel.category || "",
      channel.position || 0,
      topic,
    );
    await refreshGuilds();
  } catch (err) {
    flash(err);
  }
}

function bumpUnread(channelId, mention) {
  const cur = S.unread[channelId] || { count: 0, mentions: 0 };
  S.unread = {
    ...S.unread,
    [channelId]: { count: cur.count + 1, mentions: cur.mentions + (mention ? 1 : 0) },
  };
}

// countChannelUnread tallies one channel's unread from its history and the
// last-read cursor. Returns null for "nothing unread".
async function countChannelUnread(channelId) {
  const msgs = (await api.messages(channelId)) || [];
  const since = lastRead[channelId] ? new Date(lastRead[channelId]) : null;
  let count = 0;
  let mentions = 0;
  for (const m of msgs) {
    // Match the live counter: normal messages from others count — and so do
    // relayed GUEST messages, which are real chat even though our own key signed
    // them (the host relays them; someone else said them).
    if (!countsAsChat(m) || m.deleted) continue;
    if (m.kind !== "guest" && m.sender === S.identity.fingerprint) continue;
    if (since && new Date(m.sent) <= since) continue;
    count++;
    if (isMentionOfSelf(m)) mentions++;
  }
  return count ? { count, mentions } : null;
}

// Recompute unread counts for every channel from persisted last-read marks —
// called once after login so a refresh doesn't wipe the badges. Applied
// per-channel as each count lands (never a wholesale S.unread swap at the
// end): live bumps arriving during the awaits must not be clobbered.
//
// Fast path: a single backend call counts unread per channel in SQL with NO
// decryption, so the common case (a read channel) costs nothing. Only channels
// the cheap count flags as non-empty are decrypted — and only to get the exact
// "from others" count and the mention tally, which need message content.
async function recomputeUnread() {
  const since = {};
  for (const g of S.guilds) {
    for (const c of g.channels) {
      if (c.id === S.activeChannelId) continue;
      since[c.id] = lastRead[c.id] || "";
    }
  }
  let counts = null;
  try {
    counts = await api.unreadCounts(since);
  } catch {
    /* fall through to the per-channel path below */
  }
  for (const g of S.guilds) {
    for (const c of g.channels) {
      if (c.id === S.activeChannelId) continue;
      // With a working cheap count, skip channels that have nothing past the
      // cursor without ever touching the DB's ciphertext.
      //
      // But `counts` is a snapshot taken before this loop, and every await in
      // it yields to the live message path: a badge bumpUnread lit AFTER the
      // snapshot must not be deleted on the snapshot's stale say-so. A channel
      // that currently shows a badge falls through to the fresh per-channel
      // count instead — authoritative either way, and the fast path still
      // covers the common case (read channel, no badge).
      if (counts && !counts[c.id] && !S.unread[c.id]) continue;
      try {
        const u = await countChannelUnread(c.id);
        const next = { ...S.unread };
        if (u) next[c.id] = u;
        else delete next[c.id];
        S.unread = next;
      } catch {
        /* channel unreadable right now — skip */
      }
    }
  }
}

// syncReadState pulls the backend's account-wide read cursors (which include
// reads from other sessions and linked devices) and merges them into the
// local ones — newest wins per channel.
async function syncReadState() {
  try {
    const remote = (await api.readState()) || {};
    let changed = false;
    for (const [chId, at] of Object.entries(remote)) {
      const local = lastRead[chId] ? Date.parse(lastRead[chId]) : 0;
      if (at > local) {
        lastRead[chId] = new Date(at).toISOString();
        changed = true;
      }
    }
    if (changed) saveJSON("concord.lastRead", lastRead);
  } catch {
    /* backend without read-state support — local cursors still work */
  }
}

// countsAsChat: a message a human said in the room — a normal message, or a
// browser guest's relayed one. System notices, call notices, reactions etc. are
// bookkeeping and never count.
export const countsAsChat = (m) => m.kind === "" || m.kind === "guest";

// The oldest `sent` we have actually fetched for the active channel — the
// scroll-up pagination cursor (kept separate from S.messages so live inserts
// can't move it).
let feedOldest = "";

// clearFeed empties everything scoped to "the channel currently on screen".
export function clearFeed() {
  S.messages = [];
  feedOldest = "";
}

function isMentionOfSelf(m) {
  if (m.kind !== "" && m.kind !== "guest") return false;
  // @everyone / @here ping every member — but ONLY a real member may wield them.
  // A guest is chat-scoped; their message is relayed under the host's key, so
  // without this guard a guest typing "@everyone" would ping the whole guild.
  if (
    m.kind === "" &&
    /(^|\s)@(everyone|here)\b/.test(m.content) &&
    m.sender !== S.identity.fingerprint
  )
    return true;
  // A direct @mention of you still notifies (a guest greeting you by name is
  // fine — it's one ping to one person, not a broadcast).
  return containsMention(m.content, [S.displayName]);
}

export function guildUnread(g) {
  let count = 0;
  let mentions = 0;
  for (const c of g.channels) {
    const u = S.unread[c.id];
    if (!u || S.mutes[c.id]) continue;
    count += u.count;
    mentions += u.mentions;
  }
  return { count, mentions };
}

// ---- toasts ----
// A stack of { id, kind, text }, rendered by Toasts.svelte. Each toast owns
// its expiry timer (the old single-slot toast raced: an earlier flash's
// timeout would clear a later one). Errors linger a bit longer than info.

let toastSeq = 0;
const toastTimers = new Map(); // id -> timeout handle

export function flash(msg, kind = "info") {
  // Back-compat: flash(err) with an Error (or anything error-shaped) reads as
  // a failure without every existing call site having to opt in.
  if (kind === "info" && (msg instanceof Error || (typeof msg === "object" && msg?.message)))
    kind = "error";
  const id = ++toastSeq;
  S.toasts.push({ id, kind, text: String(msg?.message || msg) });
  // Bound the backlog even if something flashes in a tight loop.
  while (S.toasts.length > 8) dismissToast(S.toasts[0].id);
  toastTimers.set(
    id,
    setTimeout(() => dismissToast(id), kind === "error" ? 5000 : 3000),
  );
  return id;
}

export function dismissToast(id) {
  clearTimeout(toastTimers.get(id));
  toastTimers.delete(id);
  const i = S.toasts.findIndex((t) => t.id === id);
  if (i !== -1) S.toasts.splice(i, 1);
}

// Intent-named sugar over flash(); prefer these at new call sites.
export const toastOk = (msg) => flash(msg, "success");
export const toastError = (msg) => flash(msg, "error");

// checkForUpdate asks the backend (once, at startup) whether a newer release is
// out, and surfaces the "Download" banner — unless the user already dismissed
// this exact version. Silent on any error (offline / rate-limited / dev build).
export async function checkForUpdate() {
  try {
    const u = await api.checkForUpdate();
    if (!u?.available) return;
    if (loadJSON("concord.updateDismissed", "") === u.latest) return;
    S.update = u;
  } catch {
    /* offline / rate-limited — no nag */
  }
}

// dismissUpdate hides the banner and remembers this version so it won't reappear
// until a newer one ships.
export function dismissUpdate() {
  if (S.update) saveJSON("concord.updateDismissed", S.update.latest);
  S.update = null;
}

// ---- feed scroll (MessageList registers its element) ----

let feedEl = null;
export function registerFeed(el) {
  feedEl = el;
}
export function scrollSoon() {
  S.newBelow = false;
  requestAnimationFrame(() => {
    if (feedEl) feedEl.scrollTop = feedEl.scrollHeight;
  });
}
// feedNearBottom: is the user effectively at the end of the thread? Used to
// decide between following new messages and leaving the reader alone.
export function feedNearBottom() {
  if (!feedEl) return true;
  return feedEl.scrollHeight - feedEl.scrollTop - feedEl.clientHeight < 120;
}
// scrollToMessage: center the target row and flash-highlight it so the eye
// lands on it (shared by reply-ref clicks, pin jumps, and search results).
// The flash is restartable — jumping again (same row or another) clears any
// in-flight highlight and replays the animation from the start.
let flashTimer = null;
let flashEl = null;
export function scrollToMessage(id) {
  const el = feedEl?.querySelector(`[data-msg-id="${CSS.escape(id)}"]`);
  if (!el) return false;
  const reduceMotion = window.matchMedia?.("(prefers-reduced-motion: reduce)")?.matches;
  el.scrollIntoView({ block: "center", behavior: reduceMotion ? "auto" : "smooth" });
  clearTimeout(flashTimer);
  flashEl?.classList.remove("flash-highlight");
  void el.offsetWidth; // reflow so the CSS animation replays on re-add
  el.classList.add("flash-highlight");
  flashEl = el;
  flashTimer = setTimeout(() => {
    el.classList.remove("flash-highlight");
    if (flashEl === el) flashEl = null;
  }, 1200);
  return true;
}

// ---- floating profile popover ----
// Single shared timer gives hover-intent: a small open delay, and a close
// grace period so the pointer can travel from the mention into the card.

let popTimer;
let popOpenedAt = 0;
export function openProfilePopover(fingerprint, anchorEl, { delay = 0 } = {}) {
  clearTimeout(popTimer);
  const r = anchorEl.getBoundingClientRect();
  const rect = { x: r.left, y: r.top, w: r.width, h: r.height };
  const show = () => {
    S.profilePopover = { fingerprint, rect };
    popOpenedAt = Date.now();
  };
  if (delay) popTimer = setTimeout(show, delay);
  else show();
}
export function holdProfilePopover() {
  clearTimeout(popTimer);
}
export function scheduleCloseProfilePopover() {
  clearTimeout(popTimer);
  popTimer = setTimeout(() => (S.profilePopover = null), 240);
}
export function closeProfilePopover() {
  clearTimeout(popTimer);
  S.profilePopover = null;
}
// True briefly after opening, so the same click that opened the popover (which
// also bubbles to the window) doesn't immediately dismiss it.
export function popoverJustOpened() {
  return Date.now() - popOpenedAt < 250;
}

// ---- profile accent ----

// relLuminance parses a #hex or rgb() color and returns its sRGB relative
// luminance (0 dark … 1 light), or null if it can't be parsed.
function relLuminance(color) {
  if (!color) return null;
  let r, g, b;
  const hex = color.trim().replace(/^#/, "");
  if (/^[0-9a-fA-F]{3}$/.test(hex)) {
    r = parseInt(hex[0] + hex[0], 16);
    g = parseInt(hex[1] + hex[1], 16);
    b = parseInt(hex[2] + hex[2], 16);
  } else if (/^[0-9a-fA-F]{6}$/.test(hex)) {
    r = parseInt(hex.slice(0, 2), 16);
    g = parseInt(hex.slice(2, 4), 16);
    b = parseInt(hex.slice(4, 6), 16);
  } else {
    const m = color.match(/rgba?\(\s*(\d+)[,\s]+(\d+)[,\s]+(\d+)/i);
    if (!m) return null;
    [r, g, b] = [+m[1], +m[2], +m[3]];
  }
  const lin = (v) => {
    v /= 255;
    return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}

// accentForeground picks black or white text for a given accent fill so it stays
// legible — white on the pale shipped accents (gruvbox gold, rose, nord) fails
// contrast badly, which is exactly what the hardcoded #fff did before.
export function accentForeground(color) {
  const l = relLuminance(color);
  return l != null && l > 0.55 ? "#141419" : "#ffffff";
}

// syncAccentFg resolves whatever --accent currently is (an explicit color OR a
// theme pack's CSS value) and stamps a contrast-safe --accent-fg to match.
export function syncAccentFg() {
  const el = document.documentElement;
  const accent = getComputedStyle(el).getPropertyValue("--accent").trim();
  el.style.setProperty("--accent-fg", accentForeground(accent));
}

export function applyAccent(color) {
  if (!color) return;
  document.documentElement.style.setProperty("--accent", color);
  document.documentElement.style.setProperty("--accent-fg", accentForeground(color));
}

// ---- appearance (theme / accent preset / density) ----
// Device-local prefs, stamped onto <html>: data-theme picks the palette in
// app.css, data-density the message spacing, and a non-empty accent pref
// overrides the profile color (--accent-hover/-soft derive from it in CSS).

const sysDark = window.matchMedia?.("(prefers-color-scheme: dark)");

// Packs whose backdrop animates (see app.css .theme-bg + [data-anim-bg]).
export const ANIMATED_PACKS = new Set(["aurora", "synthwave", "cosmos", "molten"]);
// Packs with a STATIC coloured mesh behind translucent surfaces ([data-textured]).
export const TEXTURED_PACKS = new Set(["frost", "dusk", "grape"]);

export function applyAppearance() {
  const el = document.documentElement;
  const t = S.prefs.theme || "dark";
  el.dataset.theme = t === "system" ? (sysDark && !sysDark.matches ? "light" : "dark") : t;
  el.dataset.density = S.prefs.density === "compact" ? "compact" : "cozy";
  if (S.prefs.themePack) el.dataset.themePack = S.prefs.themePack;
  else delete el.dataset.themePack;
  // Animated packs render a live backdrop (.theme-bg) and need translucent
  // surfaces so it shows through; a single flag drives both regardless of pack.
  if (ANIMATED_PACKS.has(S.prefs.themePack)) el.dataset.animBg = "1";
  else delete el.dataset.animBg;
  // Textured packs use the same translucent-surface trick over a STATIC mesh.
  if (TEXTURED_PACKS.has(S.prefs.themePack)) el.dataset.textured = "1";
  else delete el.dataset.textured;
  // Shape and typeface: stamped ONLY when explicitly chosen, so the empty
  // value means "whatever this pack asked for" rather than "override it with
  // the base look" (app.css puts these last so they win when present).
  if (S.prefs.shape) el.dataset.shape = S.prefs.shape;
  else delete el.dataset.shape;
  if (S.prefs.font) el.dataset.font = S.prefs.font;
  else delete el.dataset.font;
  // Accent precedence: explicit preset > theme pack's own accent (CSS) >
  // profile color. An inline --accent would defeat the pack's palette, so
  // clear it when the pack should win.
  if (S.prefs.accent) applyAccent(S.prefs.accent);
  else if (S.prefs.themePack) {
    document.documentElement.style.removeProperty("--accent");
    document.documentElement.style.removeProperty("--accent-fg");
    // Let the pack's --accent apply, then derive a legible foreground from it.
    syncAccentFg();
  } else applyAccent(S.identity.color);
}

// setAppearance persists one appearance pref and applies it under a brief
// cross-fade (app.css .theme-fade; zeroed for prefers-reduced-motion there).
let fadeTimer;
export function setAppearance(key, value) {
  setPref(key, value);
  const el = document.documentElement;
  el.classList.add("theme-fade");
  applyAppearance();
  clearTimeout(fadeTimer);
  fadeTimer = setTimeout(() => el.classList.remove("theme-fade"), 300);
}

// Track the OS while in System mode (fires when the OS theme flips live).
sysDark?.addEventListener?.("change", () => {
  if (S.prefs.theme === "system") setAppearance("theme", "system");
});

// Stamp saved theme/density at import time, so even the login screen (before
// onLogin runs) honors them instead of flashing dark.
applyAppearance();

// ---- session / navigation ----

export async function onLogin() {
  S.identity = await api.identity();
  S.displayName = S.identity.displayName || "";
  applyAppearance(); // profile color, unless an accent preset overrides it
  await refreshGuilds();
  await refreshBlocked();
  S.ready = true;
  initEvents();
  // Adopt reads that happened in other sessions/devices BEFORE counting, so
  // badges cleared elsewhere stay cleared here.
  await syncReadState();
  recomputeUnread();
  refreshNetStatus();
  refreshOcr();
  // Slow poll backstops the presence-event refresh (covers bootstrap dials that
  // don't produce a peer-presence event, e.g. a relay reservation forming).
  setInterval(refreshNetStatus, 15000);
}

// refreshOcr pulls the local image-text search status into S.ocr. Never throws
// — a backend without the method (or no engine) just leaves the readout hidden.
export async function refreshOcr() {
  try {
    S.ocr = await api.ocrStatus();
  } catch {
    S.ocr = null;
  }
}

// refreshNetStatus pulls the current connectivity snapshot into S.netStatus.
export async function refreshNetStatus() {
  try {
    S.netStatus = await api.networkStatus();
  } catch {
    /* locked or transport down — leave the last known status */
  }
}

// ---- blocking ----
export async function refreshBlocked() {
  try {
    S.blocked = (await api.blockedUsers()) || [];
  } catch {
    /* ignore */
  }
}
export function isBlocked(fingerprint) {
  return !!fingerprint && S.blocked.includes(fingerprint);
}
export async function blockUser(fingerprint, name = "") {
  try {
    await api.blockUser(fingerprint);
    await refreshBlocked();
    // Drop any 1:1 DM with them from view — they can't reopen it while blocked.
    const dm = S.guilds.find(
      (g) => g.kind === "dm" && g.dmPeer === fingerprint,
    );
    if (dm) {
      try {
        await api.leaveGuild(dm.id);
      } catch {
        /* best effort */
      }
      await refreshGuilds();
    }
    flash(`Blocked ${name || "user"} — they can't add you to DMs or servers`, "success");
  } catch (err) {
    flash(err);
  }
}
export async function unblockUser(fingerprint, name = "") {
  try {
    await api.unblockUser(fingerprint);
    await refreshBlocked();
    flash(`Unblocked ${name || "user"}`, "success");
  } catch (err) {
    flash(err);
  }
}

// nudge asks the core to reconnect + resync now (called on app resume / by the
// "reconnect" affordance), then refreshes the pill.
export async function nudge() {
  try {
    await api.nudge();
  } catch {
    /* ignore */
  }
  refreshNetStatus();
}

export async function refreshGuilds() {
  S.guilds = (await api.guilds()) || [];
  if (!S.activeGuildId && S.guilds.length) {
    // Land on the top server in the rail, not Notes/DMs (those sort first in
    // the raw list because they're usually the oldest guilds).
    const first = S.guilds.find((g) => g.kind !== "dm") || S.guilds[0];
    await selectGuild(first.id);
    return;
  }
  // If the active channel was deleted remotely (guild-updated -> refresh), don't
  // strand the user in a phantom channel that still renders + sends messages.
  const g = S.guilds.find((x) => x.id === S.activeGuildId);
  if (S.activeChannelId && !g?.channels?.some((c) => c.id === S.activeChannelId)) {
    if (g?.channels?.length) await selectChannel(g.channels[0].id);
    else {
      S.activeChannelId = "";
      clearFeed();
    }
  }
}

export async function selectGuild(id) {
  S.activeGuildId = id;
  const g = S.guilds.find((x) => x.id === id);
  if (g && g.channels.length) await selectChannel(g.channels[0].id);
  else {
    // A guild with no channels (or an unknown id) must not keep the previous
    // guild's channel active — otherwise the old feed renders and, worse,
    // messages get sent to the previous guild's channel.
    S.activeChannelId = "";
    clearFeed();
  }
  await refreshRightPanel();
}

// selectNotes ensures the personal self-DM exists, then opens it.
export async function selectNotes() {
  try {
    const notes = await api.notesDM();
    if (!S.guilds.some((g) => g.id === notes.id)) await refreshGuilds();
    await selectGuild(notes.id);
  } catch (err) {
    flash(err);
  }
}

// openDMs enters the direct-messages area — selects the most recent DM, or
// creates/opens Notes if there are none yet.
export async function openDMs() {
  const dm = S.guilds.find((g) => g.kind === "dm");
  if (dm) await selectGuild(dm.id);
  else await selectNotes();
}

// startDM opens (creating if needed) a DM with a member, optionally sending a
// first message, then navigates to it. Powers the profile-card "Message" box.
export async function startDM(fingerprint, text = "") {
  const dm = await api.startDM(fingerprint);
  await refreshGuilds();
  await selectGuild(dm.id);
  const t = text.trim();
  if (t && dm.channels?.[0]) await api.sendMessage(dm.channels[0].id, t, "");
  return dm;
}

// startMeeting creates a disposable meeting room (voice + chat, 24h TTL),
// opens it, and pops the shareable invitation — the "send them a Concord
// meeting instead of a Zoom link" move.
export async function startMeeting() {
  try {
    const m = await api.startMeeting();
    await refreshGuilds();
    await selectGuild(m.guild.id);
    // A browser-guest link needs a rendezvous server; without one the modal
    // just offers the invite code (the app-to-app path).
    let guestLink = "";
    try {
      guestLink = await api.createGuestLink(m.guild.id);
    } catch {
      /* no rendezvous configured — code-only invitation */
    }
    S.modal = { kind: "meeting", code: m.code, guestLink };
  } catch (err) {
    flash(err);
  }
}

// createGroupDM opens a group DM with the given verified contacts, then
// navigates to it. Powers the "New group DM" modal.
export async function createGroupDM(fingerprints) {
  const dm = await api.createGroupDM(fingerprints);
  await refreshGuilds();
  await selectGuild(dm.id);
  return dm;
}

export async function selectChannel(id) {
  S.activeChannelId = id;
  // Snapshot where we left off BEFORE marking read, to place the "new messages"
  // divider for this viewing session.
  S.readAnchor = lastRead[id] || "";
  markRead(id);
  S.typingList.forEach((t) => clearTimeout(t.timer));
  S.typingList = [];
  S.replyingTo = null;
  S.editing = null;
  S.showPins = false;
  // Clear the outgoing channel's rows right away — they must not linger under
  // the new channel's header while history loads.
  clearFeed();
  S.feedLoading = true;
  S.loadingOlder = false;
  S.feedReachedStart = false;
  let msgs;
  try {
    msgs = (await api.messages(id)) || [];
  } catch (err) {
    if (S.activeChannelId === id) {
      S.feedLoading = false;
      flash(err);
    }
    return;
  }
  // Guard against a stale fetch: if the user switched channels while this was in
  // flight, don't overwrite the now-active channel's messages/read-cursor.
  if (S.activeChannelId !== id) return;
  S.messages = msgs;
  feedOldest = msgs.length ? msgs[0].sent : "";
  S.feedLoading = false;
  // A short first page means there's nothing older to page back to.
  S.feedReachedStart = msgs.length < 200;
  // Advance the read mark past the newest message actually loaded, so a peer's
  // clock-skewed (future-dated) message we've now seen can't keep the badge lit.
  let newest = "";
  for (const m of S.messages) if (m.sent > newest) newest = m.sent;
  if (newest) markRead(id, newest);
  // Land on the "NEW" divider when there's unread history (Discord behavior),
  // instead of dumping the reader at the bottom past everything they missed.
  const hasUnread =
    S.readAnchor &&
    S.messages.some(
      (m) => m.kind === "" && m.sender !== S.identity.fingerprint && m.sent > S.readAnchor,
    );
  if (hasUnread) scrollToNewDivider();
  else scrollSoon();
}

// loadOlder pages in the messages just before the oldest row currently loaded.
// Returns the number of rows prepended (0 = nothing more / no-op), so the caller
// can hold the reader's scroll position steady across the insert.
//
// It keeps fetching until a page yields rows we don't already have (or history
// runs out), bounded so overlapping pages can't spin it forever.
const OLDER_PAGE_TRIES = 5;

export async function loadOlder() {
  const id = S.activeChannelId;
  if (!id || S.loadingOlder || S.feedReachedStart) return 0;
  if (!feedOldest) return 0;
  S.loadingOlder = true;
  let prepended = 0;
  for (let attempt = 0; attempt < OLDER_PAGE_TRIES && !prepended; attempt++) {
    let older;
    try {
      older = (await api.messagesBefore(id, feedOldest, 200)) || [];
    } catch {
      break;
    }
    // Bail if the channel changed while the fetch was in flight.
    if (S.activeChannelId !== id) break;
    if (older.length === 0) {
      S.feedReachedStart = true;
      break;
    }
    feedOldest = older[0].sent;
    const have = new Set(S.messages.map((m) => m.id));
    const fresh = older.filter((m) => !have.has(m.id));
    if (fresh.length) S.messages = [...fresh, ...S.messages];
    prepended = fresh.length;
    if (older.length < 200) {
      S.feedReachedStart = true;
      break;
    }
  }
  S.loadingOlder = false;
  return prepended;
}

// scrollToNewDivider places the unread divider comfortably in view (falling
// back to the bottom if it isn't rendered for any reason).
function scrollToNewDivider() {
  requestAnimationFrame(() => {
    requestAnimationFrame(() => {
      const el = feedEl?.querySelector(".new-divider");
      if (el) el.scrollIntoView({ block: "center" });
      else if (feedEl) feedEl.scrollTop = feedEl.scrollHeight;
    });
  });
}

export async function refreshRightPanel() {
  if (S.activeGuildId) {
    S.members = (await api.members(S.activeGuildId)) || [];
    const g = activeGuild();
    S.roles = g && g.kind !== "dm" ? (await api.roles(S.activeGuildId)) || [] : [];
  }
  S.contacts = (await api.contacts()) || [];
}

// roleColorFor: the color of a member's highest-ranked colored role (roles come
// back highest-position first), or "" for none.
export function roleColorFor(fpr) {
  const m = memberByFpr(fpr);
  if (!m?.roleIds?.length) return "";
  for (const r of S.roles) {
    if (m.roleIds.includes(r.id) && r.color) return r.color;
  }
  return "";
}

// nameColorFor: the color a member's name is painted in, everywhere (chat,
// typing line). A colored role wins (Discord-style); otherwise the member's own
// chosen accent color. "" means fall back to the default name color.
export function nameColorFor(fpr) {
  return roleColorFor(fpr) || memberByFpr(fpr)?.color || "";
}

// Coalesce refreshes: a member join, history sync, or presence flap can emit a
// burst of guild-updated/presence events in quick succession, each otherwise
// triggering full guild + member + contact refetches and a whole-list re-render.
// scheduleRefresh batches them into a single pass ~120ms later, keeping the UI
// smooth during exactly the multi-peer activity where events cluster.
let _refreshTimer = null;
let _pendingGuilds = false;
let _pendingPanel = false;
export function scheduleRefresh({ guilds = false, panel = false } = {}) {
  _pendingGuilds = _pendingGuilds || guilds;
  _pendingPanel = _pendingPanel || panel;
  if (_refreshTimer) return;
  _refreshTimer = setTimeout(async () => {
    _refreshTimer = null;
    const g = _pendingGuilds;
    const p = _pendingPanel;
    _pendingGuilds = false;
    _pendingPanel = false;
    if (g) await refreshGuilds();
    if (p) await refreshRightPanel();
    // Own profile can change server-side without user input (rich-presence
    // overlay) — keep S.identity live so your own card/status match reality.
    if (p && S.ready) {
      try {
        S.identity = await api.identity();
      } catch {
        /* locked or transport down */
      }
    }
  }, 120);
}

// jumpToChannel finds the guild owning channelId and navigates there.
export async function jumpToChannel(channelId) {
  for (const g of S.guilds) {
    if (g.channels.some((c) => c.id === channelId)) {
      if (S.activeGuildId !== g.id) {
        S.activeGuildId = g.id;
        await refreshRightPanel();
      }
      await selectChannel(channelId);
      return true;
    }
  }
  return false;
}

// isDMChannel reports whether a channel belongs to a kind=dm guild (a direct
// message), so incoming DMs can ping even without an @mention.
export function isDMChannel(channelId) {
  return S.guilds.some((g) => g.kind === "dm" && g.channels.some((c) => c.id === channelId));
}

export function channelName(chId) {
  for (const g of S.guilds) {
    const c = g.channels.find((x) => x.id === chId);
    if (c) return `${g.name} #${c.name}`;
  }
  return "unknown channel";
}

// channelShort: just the channel's own name (no guild prefix).
export function channelShort(chId) {
  for (const g of S.guilds) {
    const c = g.channels.find((x) => x.id === chId);
    if (c) return c.name;
  }
  return "voice";
}

// ---- messaging actions ----

export async function sendMessage(text, replyToId) {
  await api.sendMessage(S.activeChannelId, text, replyToId || "");
}

export async function react(m, emoji) {
  try {
    await api.toggleReaction(m.channelId, m.id, emoji);
  } catch (err) {
    flash(err);
  }
}

// sourceLabelFor: a human label for where a message lives (for forward
// attribution), e.g. "axioms #general" or "a direct message".
export function sourceLabelFor(channelId) {
  for (const g of S.guilds) {
    const c = g.channels.find((x) => x.id === channelId);
    if (c) return g.kind === "dm" ? "a direct message" : `${g.name} #${c.name}`;
  }
  return "another channel";
}

// forwardMessage sends a copy of a message's content to another channel, with
// a quoted attribution line. Attachment tokens forward too (the blob is served
// P2P by anyone who holds it, including the forwarder).
export async function forwardMessage(m, destChannelId) {
  const attribution = `↪ Forwarded from ${sourceLabelFor(m.channelId)}`;
  const body = `> ${attribution}\n${m.content}`;
  await api.sendMessage(destChannelId, body, "");
}

export async function deleteMsg(m) {
  try {
    await api.deleteMessage(m.channelId, m.id);
    // Honesty: in a guild, deleting hides a message from members but moderators
    // can still reveal it. In a DM the delete is real (content erased on both
    // sides). Tell the user which they got — once per session, not every time.
    const g = activeGuild();
    if (g && g.kind !== "dm" && m.sender === S.identity.fingerprint && !deleteHintShown) {
      deleteHintShown = true;
      flash("Deleted for members — moderators can still view it in this server.", "info");
    }
  } catch (err) {
    flash(err);
  }
}
let deleteHintShown = false;

export async function saveEdit(m, text) {
  S.editing = null;
  text = text.trim();
  if (!m || !text || text === m.content) return;
  try {
    await api.editMessage(m.channelId, m.id, text);
  } catch (err) {
    flash(err);
  }
}

// ---- event wiring (once, after login) ----

let eventsWired = false;
function initEvents() {
  if (eventsWired) return;
  eventsWired = true;

  on("message", (m) => {
    // Is this the FIRST time we've seen this message id? The backend re-emits the
    // whole message on every edit/reaction/pin and on sync backfill, all reusing
    // the original id + `sent`. Deduping by id is what keeps those from inflating
    // unread counts and re-firing chimes/notifications.
    const firstSeen = !!m.id && !countedMsgIds.has(m.id);
    if (m.id) {
      countedMsgIds.add(m.id);
      if (countedMsgIds.size > 12000) countedMsgIds.clear(); // bound memory (rare)
    }
    // Live (not a sync backfill of old history): within the last minute.
    const isLive = Date.now() - new Date(m.sent).getTime() < 60000;

    if (m.channelId === S.activeChannelId) {
      const i = S.messages.findIndex((x) => x.id === m.id);
      if (i >= 0) {
        S.messages = S.messages.map((x) => (x.id === m.id ? m : x)); // update (edit/delete/react)
      } else {
        S.messages = [...S.messages, m];
        // Follow the conversation only if the reader is already at the end
        // (or it's their own message) — never yank them out of history.
        if (m.sender === S.identity.fingerprint || feedNearBottom()) {
          scrollSoon();
        } else {
          S.newBelow = true;
        }
        // Mark read when the channel is on-screen. Use visibility, not
        // document.hasFocus(): the latter tracks keyboard focus and is unreliable
        // in a mobile WebView (often false while you're actively reading), which
        // left the unread badge stuck on the channel you were looking at.
        if (!document.hidden) markRead(m.channelId, m.sent);
      }
    } else if (
      firstSeen &&
      m.channelId &&
      countsAsChat(m) &&
      !m.deleted &&
      // Our own key signs relayed guest messages, but a guest is not us.
      (m.sender !== S.identity.fingerprint || m.kind === "guest")
    ) {
      // Genuinely-new (first-seen) message in an unread channel bumps the badge.
      const since = lastRead[m.channelId];
      if (!since || new Date(m.sent) > new Date(since)) bumpUnread(m.channelId, isMentionOfSelf(m));
    }

    // Chimes + desktop notifications: only for a genuinely-new, live message from
    // someone else (not edits/reactions/pins, not a sync backfill of old msgs).
    const isMention = isMentionOfSelf(m);
    const genuinelyNew = firstSeen && isLive && m.sender !== S.identity.fingerprint && !m.deleted && m.kind === "";
    if (genuinelyNew && !S.mutes[m.channelId]) {
      // A direct message gets its own chime (unless you're already looking at
      // it); an @mention elsewhere gets the mention ping.
      if (isDMChannel(m.channelId) && (m.channelId !== S.activeChannelId || !document.hasFocus())) {
        playDM();
      } else if (isMention) {
        playMention();
      }
    }
    if (genuinelyNew) {
      notify(m, {
        selfFpr: S.identity.fingerprint,
        mention: isMention,
        muted: !!S.mutes[m.channelId],
        activeChannel: S.activeChannelId,
        onClick: () => jumpToChannel(m.channelId),
      });
    }
  });

  on("presence", () => {
    // Refresh BOTH the member panel and the guild list. A DM peer's online/idle
    // dot is computed as part of the GuildView (dmPeerOnline/dmPeerPresence), so
    // without `guilds` a presence change updated the guild member list but left
    // DM/rail dots stale — "right in the guild, wrong in the DM".
    scheduleRefresh({ panel: true, guilds: true });
    refreshNetStatus();
  });

  on("guild-updated", () => scheduleRefresh({ guilds: true, panel: true }));

  // Another session (a second window) or another linked device advanced a
  // channel's read cursor: adopt it and re-count that channel's badge, so a
  // message read anywhere clears (or trims) the badge here immediately.
  on("read-state", async (r) => {
    if (!r?.channelId || !r?.at) return;
    const local = lastRead[r.channelId] ? Date.parse(lastRead[r.channelId]) : 0;
    if (r.at <= local) return; // we already know a newer read
    lastRead[r.channelId] = new Date(r.at).toISOString();
    saveJSON("concord.lastRead", lastRead);
    if (r.channelId === S.activeChannelId) return; // on-screen: nothing to badge
    try {
      const u = await countChannelUnread(r.channelId);
      const next = { ...S.unread };
      if (u) next[r.channelId] = u;
      else delete next[r.channelId];
      S.unread = next;
    } catch {
      /* history unreadable right now — badge stays until the next recount */
    }
  });

  on("typing", (t) => {
    if (t.channelId !== S.activeChannelId) return;
    const label = t.name || (t.from || "").slice(0, 9);
    // Clear the previous timer for this person, else its stale 4s timeout fires
    // and removes the FRESH entry — making a continuously-typing peer flicker off.
    const prev = S.typingList.find((x) => x.from === t.from);
    if (prev) clearTimeout(prev.timer);
    S.typingList = S.typingList.filter((x) => x.from !== t.from);
    const timer = setTimeout(() => {
      S.typingList = S.typingList.filter((x) => x.from !== t.from);
    }, 4000);
    S.typingList = [...S.typingList, { from: t.from, label, timer }];
  });

  // Voice presence is now guild-wide: every peer hears join/leave heartbeats for
  // every voice channel, so the sidebar can show who's in each call.
  on("voice-presence", (v) => {
    // Soft-lock control actions ride the same presence topic (see voice.go).
    if (v.action === "lock" || v.action === "unlock") {
      const locks = { ...S.callLocks };
      if (v.action === "lock") locks[v.channelId] = true;
      else delete locks[v.channelId];
      S.callLocks = locks;
      return;
    }
    if (v.action === "knock") {
      // Someone wants into a call — surface it to people IN that call.
      if (S.voice?.channelId === v.channelId && v.fingerprint !== S.identity.fingerprint) {
        const list = S.callKnocks[v.channelId] || [];
        if (!list.includes(v.fingerprint)) {
          S.callKnocks = { ...S.callKnocks, [v.channelId]: [...list, v.fingerprint] };
        }
      }
      return;
    }
    if (v.action === "admit") {
      // We were let in → join for real (App.svelte watches admittedJoin).
      if (v.target === S.identity.fingerprint && S.knocking === v.channelId) {
        S.knocking = "";
        S.admittedJoin = v.channelId;
      }
      return;
    }
    if (v.action === "move" || v.action === "disconnect") {
      handleVoiceModeration(v);
      return;
    }

    updateVoiceRoster(v.channelId, v.from, v.fingerprint, v.action);

    // A browser guest joined the call from their invite link. If we're not in
    // the call yet they'd be sitting there alone, so say so — one nudge per
    // guest, not once per heartbeat.
    if (isGuestPeer(v.from)) {
      const inThisCall = S.voice && S.voice.channelId === v.channelId;
      if (v.action === "join" && !inThisCall && !announcedGuests.has(v.from)) {
        announcedGuests.add(v.from);
        playDM();
        flash(`${guestName(v.fingerprint)} is waiting in the call — hit Call to join them`, "info");
      } else if (v.action === "leave") {
        announcedGuests.delete(v.from);
      }
    }

    // Additionally drive the WebRTC mesh + sounds for the room we're actually in.
    if (S.voice && v.channelId === S.voice.channelId) {
      if (v.action === "join") {
        if (!S.voicePeerFpr[v.from]) playVoiceJoin();
        S.voicePeerFpr = { ...S.voicePeerFpr, [v.from]: v.fingerprint };
      } else {
        const c = { ...S.voicePeerFpr };
        delete c[v.from];
        S.voicePeerFpr = c;
        playVoiceLeave();
      }
      S.voice.mesh.handlePresence(v.from, v.action);
    }
  });
  // A verified contact is offering to add us to their server. We show it; we
  // never join on their say-so.
  on("guild-invite", (inv) => {
    if (!inv?.code) return;
    playDM();
    S.modal = { kind: "guildInvite", invite: inv };
  });
  on("voice-signal", (v) => {
    if (S.voice) S.voice.mesh.handleSignal(v.from, v.data);
  });

  // Expire voice-roster entries whose heartbeat stopped (missed ~3 beats = 9s),
  // covering peers that crashed or dropped without a clean "leave".
  setInterval(() => {
    const now = Date.now();
    let changed = false;
    const next = {};
    for (const [ch, peers] of Object.entries(S.voiceRosters)) {
      const kept = {};
      for (const [pid, info] of Object.entries(peers)) {
        if (now - info.ts < 9000) kept[pid] = info;
        else changed = true;
      }
      if (Object.keys(kept).length) next[ch] = kept;
      else if (Object.keys(peers).length) changed = true;
    }
    if (changed) S.voiceRosters = next;
  }, 3000);
}

// A browser guest in a meeting call is the voice peer "guest:<session>", and
// carries its display name where a member's fingerprint would be — a guest has
// no identity to look up, which is the entire point of being a guest.
export const isGuestPeer = (peerId = "") => peerId.startsWith("guest:");
export const isGuestFpr = (fpr = "") => fpr.startsWith("guest:");
export const guestName = (fpr = "") => fpr.slice(6) || "Guest";
const announcedGuests = new Set();

// updateVoiceRoster folds one presence heartbeat into the guild-wide roster.
function updateVoiceRoster(channelId, peerId, fingerprint, action) {
  if (!channelId || !peerId) return;
  const rosters = { ...S.voiceRosters };
  const room = { ...(rosters[channelId] || {}) };
  if (action === "leave") {
    delete room[peerId];
  } else {
    room[peerId] = { fingerprint, ts: Date.now() };
  }
  if (Object.keys(room).length) rosters[channelId] = room;
  else {
    delete rosters[channelId];
    // The call ended — a future call to this DM should ring again.
    if (S.dismissedCalls.includes(channelId)) {
      S.dismissedCalls = S.dismissedCalls.filter((c) => c !== channelId);
    }
    forgetLock(channelId);
  }
  S.voiceRosters = rosters;
}

// forgetLock drops a soft lock once its call is empty.
//
// A lock only means "ask the people inside to let you in". With nobody inside
// there is no one to ask, so a lock that outlives its call is a door that can
// never open — including for whoever set it. That happens easily: "unlock" is
// sent once and gossip is missable (which is why "lock" is re-announced every
// few seconds), and a client that quits or crashes never sends one at all.
// Tying the lock's lifetime to the roster makes every one of those self-heal.
export function forgetLock(channelId) {
  if (S.callLocks[channelId]) {
    const locks = { ...S.callLocks };
    delete locks[channelId];
    S.callLocks = locks;
  }
  if (S.callKnocks[channelId]) {
    const k = { ...S.callKnocks };
    delete k[channelId];
    S.callKnocks = k;
  }
  if (S.knocking === channelId) S.knocking = "";
}

// ---- voice moderation (move / disconnect) ----
//
// There is no server to enforce anything here, so the enforcement lives where
// it can't be forged: on the RECEIVING side. A move or disconnect is a request
// carried on the voice topic, and each client obeys it only after checking, in
// its own copy of the guild's governance state, that the sender really holds
// the authority. The sender's claim about itself is never consulted — the
// fingerprint comes from the authenticated libp2p sender (voice.go), not the
// message body.

// canModerateVoice: does this fingerprint have the standing to move or kick
// people in this guild? Owner, or a role granting member management or mutes —
// the same authority that can already remove someone from the guild entirely.
//
// `members` must be THAT guild's roster. S.members only holds the guild
// currently on screen, and permissions don't travel between guilds: an admin of
// the server you happen to be looking at has no authority over a call in a
// different one. Callers acting on a remote guild pass its roster explicitly
// (see modAuthority).
export function canModerateVoice(fingerprint, guild = activeGuild(), members = null) {
  if (!fingerprint || !guild) return false;
  if (guild.ownerFingerprint === fingerprint) return true;
  const list = members ?? (guild.id === S.activeGuildId ? S.members : null);
  if (!list) return false; // unknown roster: refuse rather than guess
  const mem = list.find((m) => m.fingerprint === fingerprint);
  if (!mem) return false;
  return mem.isOwner || has(mem.perms || 0, PERM.MANAGE_MEMBERS) || has(mem.perms || 0, PERM.MUTE_MEMBERS);
}

// modAuthority resolves the sender's standing in the guild that owns the call,
// fetching that guild's roster when it isn't the one on screen.
async function modAuthority(fingerprint, guild) {
  if (!guild) return false;
  if (guild.ownerFingerprint === fingerprint) return true;
  if (guild.id === S.activeGuildId) return canModerateVoice(fingerprint, guild);
  try {
    const members = await api.members(guild.id);
    return canModerateVoice(fingerprint, guild, members || []);
  } catch {
    return false; // can't verify → don't obey
  }
}

async function handleVoiceModeration(v) {
  if (v.target !== S.identity.fingerprint) return; // not about us
  if (!S.voice || S.voice.channelId !== v.channelId) return; // we're not there
  if (v.fingerprint === S.identity.fingerprint) return; // our own echo
  // The guild that owns the call, not whichever one happens to be on screen.
  const guild = S.guilds.find((g) => g.channels?.some((c) => c.id === v.channelId));
  if (!(await modAuthority(v.fingerprint, guild))) return;
  // Re-check after the await: we may have left the call in the meantime.
  if (!S.voice || S.voice.channelId !== v.channelId) return;
  const by = nameFor(v.fingerprint);
  if (v.action === "disconnect") {
    S.moderatedVoice = { action: "disconnect", by };
    return;
  }
  const dest = guild?.channels?.find((c) => c.id === v.dest && c.type === "voice");
  if (!dest) return;
  S.moderatedVoice = { action: "move", by, channelId: v.dest, name: dest.name };
}

// moveVoiceMember / disconnectVoiceMember are the moderator side. Both are
// requests: a client that ignores them simply stays put, which is the honest
// limit of moderation without a server in the middle.
export function moveVoiceMember(fingerprint, fromChannelId, toChannelId) {
  if (!fingerprint || !toChannelId || fromChannelId === toChannelId) return;
  api.signalCall(fromChannelId, "move", fingerprint, toChannelId).catch(() => {});
  const name = activeGuild()?.channels?.find((c) => c.id === toChannelId)?.name || "the other channel";
  flash(`Moving ${nameFor(fingerprint)} to ${name}…`, "info");
}
export function disconnectVoiceMember(fingerprint, channelId) {
  if (!fingerprint || !channelId) return;
  api.signalCall(channelId, "disconnect", fingerprint).catch(() => {});
  flash(`Disconnecting ${nameFor(fingerprint)}…`, "info");
}

// ---- soft call lock (see voice.go PublishCallControl) ----
export function isCallLocked(channelId) {
  return !!S.callLocks[channelId];
}
let lockRebroadcast = null;
export function toggleCallLock() {
  const ch = S.voice?.channelId;
  if (!ch) return;
  const locking = !S.callLocks[ch];
  api.signalCall(ch, locking ? "lock" : "unlock").catch(() => {});
  const locks = { ...S.callLocks };
  if (locking) locks[ch] = true;
  else delete locks[ch];
  S.callLocks = locks;
  clearInterval(lockRebroadcast);
  if (locking) {
    // One-off gossip is missable, so re-announce the lock every few seconds
    // while it's on, so late watchers (and knockers) learn the call is locked.
    lockRebroadcast = setInterval(() => {
      if (S.voice?.channelId === ch && S.callLocks[ch]) api.signalCall(ch, "lock").catch(() => {});
      else clearInterval(lockRebroadcast);
    }, 3000);
  }
}
export function admitKnocker(channelId, fpr) {
  api.signalCall(channelId, "admit", fpr).catch(() => {});
  dropKnock(channelId, fpr);
}
export function denyKnocker(channelId, fpr) {
  dropKnock(channelId, fpr); // silently ignore; their knock times out
}
function dropKnock(channelId, fpr) {
  const list = (S.callKnocks[channelId] || []).filter((f) => f !== fpr);
  const k = { ...S.callKnocks };
  if (list.length) k[channelId] = list;
  else delete k[channelId];
  S.callKnocks = k;
}
// clearCallState wipes lock/knock bookkeeping for a channel we've left.
export function clearCallState(channelId) {
  clearInterval(lockRebroadcast);
  // Leaving ends our view of the call, lock included. Keeping it would lock US
  // out: isCallLocked() would still be true next time we clicked the channel,
  // so we'd knock at a room we just left — with nobody left inside to admit us.
  forgetLock(channelId);
}

// incomingCall reports a DM whose other member is in the voice channel while we
// are not — i.e. someone is ringing us. Returns { guildId, channelId, name } or
// null. Dismissed (declined) calls stay suppressed until the roster clears.
export function incomingCall() {
  for (const g of S.guilds) {
    if (g.kind !== "dm" || g.name === "Notes") continue;
    const ch = g.channels?.[0];
    if (!ch) continue;
    if (S.voice?.channelId === ch.id) continue; // already in this call
    if (S.joiningVoice === ch.id) continue; // mid-join — stop ringing immediately
    if (S.dismissedCalls.includes(ch.id)) continue;
    const roster = S.voiceRosters[ch.id];
    if (roster && Object.keys(roster).length > 0) {
      return { guildId: g.id, channelId: ch.id, name: g.name };
    }
  }
  return null;
}

// voiceMembersFor returns the display list for a voice channel: {fingerprint,
// self, speaking, sharing} per participant, including ourselves when we're in it.
export function voiceMembersFor(channelId) {
  const out = [];
  const seen = new Set();
  // Ourselves, if we're in this room (we don't hear our own gossip heartbeat).
  if (S.voice && S.voice.channelId === channelId) {
    out.push({
      fingerprint: S.identity.fingerprint,
      self: true,
      speaking: S.voiceSpeaking.includes("self"),
      sharing: S.sharing,
    });
    seen.add(S.identity.fingerprint);
  }
  for (const [pid, info] of Object.entries(S.voiceRosters[channelId] || {})) {
    if (seen.has(info.fingerprint)) continue;
    seen.add(info.fingerprint);
    out.push({
      fingerprint: info.fingerprint,
      self: false,
      // Speaking is only known for the room we're in (from the local mesh).
      speaking: S.voiceSpeaking.includes(pid),
      sharing: !!S.voiceSharing[pid],
    });
  }
  return out;
}
