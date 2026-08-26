// state.svelte.js — Concord's shared UI state (Svelte 5 runes) and the actions
// that mutate it. Components import { S, ... } and read/mutate S's properties;
// api.js stays the only transport layer underneath.
import { api, on, onTransportHealth } from "./api.js";
import { notify, wantsPermissionPrompt } from "./notify.js";
import { containsMention } from "./markdown.js";
import { playVoiceJoin, playVoiceLeave, playMention, playDM, playSfx } from "./sounds.js";

// Per-sender soundboard rate limit (last accepted press, ms). Module-local:
// nothing else needs to react to it.
const sfxLast = {};
import { PERM, has } from "./perms.js";
import { plur } from "./plural.js";
import { fmtCount } from "./chronicle.js";
import {
  LEVELS as NOTIF_LEVELS,
  normalize as normalizeNotifs,
  migrateMutes,
  resolve as resolveNotif,
  setChannel as setChannelNotif,
  setGuild as setGuildNotif,
  wantsAlert,
  showsBadge,
} from "./notifs.js";

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
  // Set when a message has arrived that the user would have been told about if
  // the OS grant existed. Drives the one-line rationale bar in App.svelte; see
  // offerNotifications below for why it is not simply an OS prompt.
  notifyAsk: false,
  // Message IDs this device has been asked to stop drawing (Report -> Hide).
  // Device-local and reversible: the rows stay in the store and in every other
  // copy of the guild, exactly as with a block. Loaded below from localStorage.
  hiddenMessages: [],
  // Message requests: DM invites from strangers the backend deliberately has
  // NOT redeemed ([{ from, fromName, code, at }]). Until one is accepted the
  // sender has learned nothing about us — see internal/app/request.go.
  requests: [],

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
  // The join threshold: { title, leaving? } while an event's meeting room is
  // being entered — JoinVeil.svelte renders it above everything, and the
  // joiner lands in the room mid-fade instead of context-jumping.
  joinVeil: null,
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
  // narrow: still the desktop grid, but the chat column has been squeezed by
  // the two side panels until its own toolbars no longer fit. Between the
  // mobile breakpoint and this one the chat column is only ~350–550px wide,
  // which is where the composer's icon row and the header's action row have to
  // fold away instead of eating the content they sit beside.
  narrow: detectNarrow(),

  // Connectivity for the connection pill: { peers, bootstrapReached,
  // hasBootstrap, outOfSyncGuilds }, refreshed on presence events + a slow poll.
  // null until the first fetch (login).
  netStatus: null,
  // offline: the local core stopped answering. Distinct from having no peers —
  // this is the process behind the window being gone, which every status dot in
  // the app would otherwise keep reporting as fine, because they all render the
  // last thing it said before it died.
  offline: false,

  // feedLoading: a channel switch is fetching history (drives the skeleton —
  // without it the OLD channel's rows linger under the new header).
  feedLoading: false,
  // loadingOlder / feedReachedStart: scroll-up pagination state. The initial
  // load is only the recent window; older rows are paged in on demand.
  loadingOlder: false,
  feedReachedStart: false,
  // The imported archive that sits ABOVE live history. `chronicle` is the
  // active guild's index (null when it has none); the rest is the same
  // scroll-up pagination as loadingOlder/feedReachedStart, one channel at a
  // time, and is reset by every channel switch.
  //
  // archiveMetered is not an error state: it means a page was NOT fetched
  // because the connection is billed by the byte, and the reader is offered
  // the choice rather than having it made for them.
  chronicle: null,
  // A running chat-export import, and the outcome of the last one. Both live
  // out here rather than inside the wizard because the job is asynchronous and
  // the wizard is closable while it runs: whoever started an import gets the
  // completion toast whether or not the dialog is still on screen.
  chronImport: null,
  chronImportDone: null,
  archiveRows: [],
  archiveLoading: false,
  archiveReachedStart: false,
  archiveMetered: false,
  // restarting: a self-update restart is in flight; the app shows a full-bleed
  // "right back" curtain so the outgoing version is never visible mid-swap.
  restarting: false,
  // unread[channelId] = { count, mentions } — counts survive refresh via the
  // localStorage last-read map (recomputed on load).
  unread: {},
  // How loud each channel and guild is allowed to be (see lib/notifs.js).
  // Seeded from the old per-channel mute list so an upgrade is silent about
  // itself — a channel you'd muted comes back as "Nothing", which is the same
  // thing under a name that leaves room for "only @mentions".
  notifs: migrateMutes(normalizeNotifs(loadJSON("concord.notifs", null)), loadJSON("concord.mutes", {})),
  // Guild-rail layout: device-local ordering + Discord-style folders (see
  // lib/rail.js). An array of { t:"g", id } / { t:"f", id, name, color, open,
  // ids }. Reconciled against the live guild list on render.
  rail: loadJSON("concord.rail", []),
  // MRU trail of channel ids, newest first (cap 15). The command palette's
  // empty-query "Jump to" is this list: the Ctrl+K muscle memory is "bounce
  // back to where I just was", not "the first six channels of whichever guild
  // sorts first".
  recentChannels: loadJSON("concord.mru", []),
  readAnchor: "", // ISO time we'd last read the active channel (for the "new" line)

  // The two off-device search switches. Unlike everything in `prefs` below,
  // these live in the BACKEND (internal/app/offdevice.go) — the request they
  // gate is made by the Go process, so a value kept only in localStorage would
  // guard nothing. Mirrored here so the Privacy panel and the two search UIs
  // read the same answer and a flip in one is visible in the other at once.
  // The values here are only what the UI assumes before login; onLogin replaces
  // them with what the backend actually holds.
  offDevice: { gameSearch: false, gifSearch: true },

  // Privacy + appearance prefs. linkPreviews defaults OFF: fetching a preview
  // for a link in a message reveals your IP + online time to that link's host,
  // so a message with a link to an attacker-controlled server is a zero-click
  // deanonymization. Opt in only if you trust who you talk to.
  // theme: "dark" | "light" | "system"; density: "cozy" | "compact";
  // accent: a preset hex, or "" to follow the profile color (the old behavior).
  // Spread over defaults so prefs saved before a key existed still get it.
  prefs: {
    linkPreviews: false,
    // Game box art defaults OFF for the same reason. A profile card carrying a
    // game collection would otherwise load images straight from Valve's CDN the
    // moment you opened it — no click, no prompt — telling Valve your IP and
    // when you were online. The generated gradient covers are the fallback and
    // were designed to stand on their own, so off costs nothing but the art.
    gameCovers: false,
    showDeleted: false, // off = deleted messages vanish; on = leave a faint marker
    // "Not now" on the notification rationale bar. Sticky, because the whole
    // point of asking late is to ask once; Settings -> Notifications is where
    // someone who changes their mind turns it on.
    notifyAskDeclined: false,
    // Set when an account is CREATED on this device and cleared the moment its
    // recovery phrase has been verified — or simply looked at again in
    // Settings. Its only job is to keep the nudge banner up until then: the
    // hold-the-door step at signup can be walked past with a page reload, and
    // an account whose only key exists on one device and nowhere else is one
    // hard drive away from gone. Device-local on purpose; a device that was
    // LINKED rather than created never sets it, because the phrase for that
    // account is already written down somewhere else.
    backupPending: false,
    hideCallIp: false, // on = always relay calls through the rendezvous (hide IP)
    theme: "dark",
    accent: "",
    themePack: "", // curated full-palette skin ("" = default palette)
    // Stackable visual effect, drawn over the app on top of whatever pack is
    // active (lib/themefx.js for the catalogue, FxOverlay.svelte for the
    // layer). A separate axis from themePack on purpose: any effect composes
    // with any pack. "" = none, which is also what it stays as on a phone
    // until someone turns it on THERE — prefs are device-local, so a laptop
    // running snow never starts a phone animating on battery.
    themeFx: "",
    // The specular pass on an animated pack — the band of light that crosses
    // the window on top of everything else (aurora's curtain, prism's bar, the
    // CRT roll, a heat wave, ice glinting). It is the part of a live backdrop
    // that travels OVER your text rather than sitting behind it, and it is the
    // one part some people want gone while keeping the pack's colour and its
    // scenery moving. On by default: it is what the packs were drawn and
    // contrast-measured with, and turning it off silently would change the
    // appearance of every install that never asked. See applyAppearance for
    // the stamp and the "shine off" block in app.css for what each pack drops.
    themeShine: true,
    // Shape/typeface overrides. "" = follow the active theme pack, which now
    // carries a corner radius and a UI face of its own, not just colors.
    shape: "",
    font: "",
    density: "cozy",
    // Whole-UI scale, 0.8..1.5. The Wails webview has no browser chrome to
    // zoom with, so this is the only recourse when 13px UI text is too small.
    // Ctrl+= / Ctrl+- / Ctrl+0 step it; a slider lives in Appearance.
    uiScale: 1,
    clock: "system", // "system" | "12" | "24" — timestamp hour format
    memberPanel: true, // show the right-hand member panel (toggle with Ctrl+U)
    // Chosen call hardware (see lib/devices.js). "" = whatever the OS picked,
    // which is also what a stored id falls back to when that device is gone.
    micId: "",
    speakerId: "",
    cameraId: "",
    // Where a screen share's sound comes from when the platform won't supply
    // it (on Linux, a PulseAudio/PipeWire "Monitor of …" input).
    shareAudioId: "",
    // Call audio knobs. The defaults are exactly "what the browser does on its
    // own", so an untouched install sounds the same as it always has.
    outputVolume: 1, // master playback level for a call, 0..1
    micGain: 1, // mic boost/trim, 0.25..4
    micGate: 0, // noise gate threshold, 0 = off
    // Spectral noise reduction level id ("" = off; see lib/denoise.js). Off by
    // default: it reprocesses your voice, which should be a choice you make.
    micNr: "",
    // Push-to-talk: the mic opens only while pttBind is held (see lib/keybind.js
    // for the binding shape, and lib/shortcuts.js for the hold). Off by default
    // — open mic is what people expect until they say otherwise.
    pushToTalk: false,
    pttBind: null,
    // Opus target, bits/s. 64k is a real step up from the browser's ~32k
    // default and still modest on a mesh where you send one copy per peer.
    voiceBitrate: 64000,
    echoCancel: true,
    noiseSuppress: true,
    autoGain: true,
    // Desktop side-column widths, px (drag the column edges in App.svelte).
    // At the default value the CSS var stays unset, so the stylesheet's
    // responsive defaults (narrower under 900px) keep deciding.
    colChannels: 220,
    colMembers: 260,
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
  // Someone asked us to join their call: { channelId, from, where }.
  callInvite: null,
  admittedJoin: "", // set when admitted → App.svelte joins for real
  // fingerprint -> { muted, deafened } for people in calls. Mute is local to
  // each client (a disabled track), so the only way anyone else can know is if
  // we say so — see publishVoiceState.
  voiceStates: {},
  voiceParticipants: [],
  voiceSpeaking: [],
  talking: false, // push-to-talk key is down right now (drives the mic button)
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
// Your own member row, synthesized from the identity when the roster doesn't
// have it. S.members only exists inside a guild, so without this your own
// profile card comes up empty in a DM or in Notes — and yours is the one card
// that's reachable from anywhere, via the footer.
export function selfMember() {
  const i = S.identity || {};
  return {
    fingerprint: i.fingerprint,
    name: S.displayName || i.name || "",
    username: "",
    status: i.status || "",
    emoji: i.emoji || "",
    color: i.color || "",
    color2: i.color2 || "",
    avatar: i.avatar || "",
    banner: i.banner || "",
    presence: i.presence || "",
    bio: i.bio || "",
    frame: i.frame || "",
    effect: i.effect || "",
    style: i.style || "",
    activity: i.activity || null,
    games: i.games || [],
    isSelf: true,
    online: true,
    verified: true,
    roleIds: [],
    perms: 0,
  };
}
export const memberByFpr = (fpr) =>
  S.members.find((m) => m.fingerprint === fpr) ||
  (fpr && fpr === S.identity?.fingerprint ? selfMember() : undefined);

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
  // YOU. Your own linked devices resolve to your own account fingerprint, and
  // every other source here is about OTHER people: the member roster only has a
  // row for you inside a guild you're currently viewing, and the contact list
  // and profile cache never hold one at all (learnProfile refuses a self row so
  // a peer echoing a stale copy can't rename you). So your phone in a voice room
  // fell all the way through to a fingerprint stub. Answer it first instead.
  if (fpr && fpr === S.identity.fingerprint) return S.displayName || memberByFpr(fpr)?.name || "You";
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

// detectNarrow reports the squeezed-desktop band. The number is measured, not
// picked: with both side columns at their defaults the chat column only clears
// ~600px — enough for the composer's nine icons and the header's seven controls
// — once the window passes 1150px.
function detectNarrow() {
  if (typeof window === "undefined") return false;
  return !!window.matchMedia?.("(max-width: 1150px)")?.matches;
}

if (typeof window !== "undefined") {
  const sync = () => {
    const now = detectMobile();
    if (now !== S.isMobile) S.isMobile = now;
    const tight = detectNarrow();
    if (tight !== S.narrow) S.narrow = tight;
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

// Restore the per-message hides (Report -> Hide). Assigned here rather than in
// the S literal because loadJSON is declared below it.
S.hiddenMessages = loadJSON("concord.hiddenMessages", []);

// Where you were: the guild open when the app last closed, and the channel you
// were reading in each guild. Device-local — this is a view preference, not
// account state, and the phone shouldn't jump because the desktop moved.
const lastPlace = loadJSON("concord.lastPlace", { guildId: "", channels: {} });
if (!lastPlace.channels) lastPlace.channels = {}; // tolerate an older/partial value

function rememberPlace(guildId, channelId) {
  if (!guildId || !channelId) return;
  if (lastPlace.guildId === guildId && lastPlace.channels[guildId] === channelId) return;
  lastPlace.guildId = guildId;
  lastPlace.channels[guildId] = channelId;
  saveJSON("concord.lastPlace", lastPlace);
}

// channelToResume: the channel to open when entering a guild — the one you were
// last reading there, falling back to its first channel. Voice channels are
// skipped: reopening one would look like an invitation to rejoin a call you
// aren't in, and its chat is reachable without that ambiguity.
function channelToResume(guild) {
  const saved = lastPlace.channels[guild.id];
  const ch = guild.channels.find((c) => c.id === saved);
  if (ch && ch.type !== "voice") return ch.id;
  return (guild.channels.find((c) => c.type !== "voice") || guild.channels[0]).id;
}
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

// ---- notification levels ----
//
// The pure model lives in lib/notifs.js; this is the reactive skin over it.

// guildIdOf: which guild (or DM pseudo-guild) a channel belongs to, so a
// channel with no setting of its own can fall through to its guild's.
export function guildIdOf(channelId) {
  return S.guilds.find((g) => g.channels.some((c) => c.id === channelId))?.id || "";
}

// guildId is optional and purely an optimisation: the sidebar resolves a level
// for every channel on every render, and callers that already know the guild
// shouldn't pay for guildIdOf's scan to rediscover it.
export function notifLevel(channelId, guildId) {
  return resolveNotif(S.notifs, channelId, guildId ?? guildIdOf(channelId));
}

export function guildNotifLevel(guildId) {
  return S.notifs.guilds[guildId] || "all";
}

// isMuted keeps the old vocabulary for the places that only ever asked the
// yes/no question: greying a row, hiding a badge, skipping a chime.
export function isMuted(channelId, guildId) {
  return !showsBadge(notifLevel(channelId, guildId));
}

function saveNotifs(next) {
  S.notifs = next;
  saveJSON("concord.notifs", next);
}

// level = null puts the channel back to following its guild.
export function setChannelNotifs(channelId, level) {
  saveNotifs(setChannelNotif(S.notifs, channelId, level));
}

export function setGuildNotifs(guildId, level) {
  const ids = (S.guilds.find((g) => g.id === guildId)?.channels || []).map((c) => c.id);
  saveNotifs(setGuildNotif(S.notifs, guildId, level, ids));
}

// The one-click version, still on the bell icon in the channel list: silence,
// or hand the channel back to whatever its guild says. It clears rather than
// pinning "all", so unmuting a channel in a guild you'd set to @mentions-only
// returns it to @mentions-only rather than quietly making it the loud one.
export function toggleMute(channelId) {
  setChannelNotifs(channelId, isMuted(channelId) ? null : "none");
}

// clockOpts turns the clock pref into Intl options ({} = follow the locale).
// Reading S.prefs.clock makes any $derived/template that calls it reactive.
export function clockOpts() {
  const c = S.prefs.clock;
  if (c === "12") return { hour12: true };
  if (c === "24") return { hour12: false };
  return {};
}

// fmtClock formats an ISO timestamp as H:MM honoring the clock pref. Shared by
// the feed, search, and scheduled views so the 12/24h choice is global.
//
// hour:"numeric" rather than "2-digit": a 24-hour clock pads itself anyway
// ("19:28"), while a 12-hour one loses a leading zero nobody writes — "7:28 PM",
// not "07:28 PM". That one character is also the difference between fitting the
// feed's hover gutter and wrapping the meridiem onto a second line, which made
// grouped rows change height under the cursor.
export function fmtClock(iso) {
  try {
    return new Date(iso).toLocaleTimeString([], { hour: "numeric", minute: "2-digit", ...clockOpts() });
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
  S.contextMenu = { x: e.clientX, y: e.clientY, items: tidySeps(items.filter(Boolean)), ...opts };
}

// Callers build menus as flat lists with permission-gated entries dropped by
// `&&`, so a separator can easily end up leading, trailing, or doubled once
// everything around it has been filtered out — a stray rule with nothing on one
// side of it. Collapse those here, once, rather than in every menu.
function tidySeps(items) {
  const out = [];
  for (const it of items) {
    if (!it.sep) {
      out.push(it);
      continue;
    }
    if (out.length && !out[out.length - 1].sep) out.push(it);
  }
  // A header with nothing under it is the same kind of orphan.
  while (out.length && (out[out.length - 1].sep || out[out.length - 1].header)) out.pop();
  return out;
}
export function closeContextMenu() {
  S.contextMenu = null;
}

// Dismissible layers — modals, sheets, popovers, menus, panels — all live on
// one stack in lib/navstack.svelte.js, which back and Escape pop from. This
// file used to keep a flat closer list of its own with the context menu and the
// profile card wired in front of it as fixed rungs, so those two closed before
// anything registered no matter which had been opened last.

// patchProfile writes one or two profile fields and carries every other one
// through untouched. SetProfile is all-or-nothing — it takes the whole profile
// — so anything a caller forgets to pass is not "left alone", it's erased.
// Everywhere that changes a single field goes through here for that reason.
export async function patchProfile(patch) {
  const id = S.identity;
  const f = (k) => patch[k] ?? id[k] ?? "";
  await api.setProfile(
    f("displayName"),
    f("status"),
    f("emoji"),
    f("color"),
    f("avatar"),
    f("banner"),
    f("presence"),
    f("bio"),
    f("color2"),
    f("frame"),
    f("effect"),
    id.style ? JSON.stringify(id.style) : "",
  );
  S.identity = await api.identity();
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
    // The guild-wide default every channel falls back to. Flat, with a tick on
    // the one in force — this menu has no submenus.
    { label: "Notifications", header: true },
    ...NOTIF_LEVELS.map((l) => ({
      label: l.label,
      icon: l.id === "none" ? "bellOff" : "bell",
      active: guildNotifLevel(g.id) === l.id,
      onClick: () => setGuildNotifs(g.id, l.id),
    })),
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
    // Honest about what actually happens: removal is from THIS side and it
    // sticks — linked devices and old invites won't quietly re-add it. For an
    // owner, other members keep their copy (deleting is local, not a dissolve).
    body: g.isOwner
      ? "Its messages will be removed from this device, and it won't come back on its own. Other members keep their copy."
      : "Its messages will be removed from this device, and you won't be re-added automatically. Rejoining takes a new invite.",
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

// refreshOffDeviceSearch pulls both backend-held search switches. Failures are
// swallowed on purpose: an older backend has neither method, and the panel is
// better off showing the shipped defaults than an error nobody can act on.
export async function refreshOffDeviceSearch() {
  const [game, gif] = await Promise.all([
    api.gameSearchEnabled().catch(() => S.offDevice.gameSearch),
    api.gifSearchEnabled().catch(() => S.offDevice.gifSearch),
  ]);
  S.offDevice = { gameSearch: !!game, gifSearch: !!gif };
}

// setOffDeviceSearch flips one switch, optimistically, and puts it back if the
// backend refuses — the same shape the other backend-held privacy switches use.
// key is "gameSearch" | "gifSearch".
export async function setOffDeviceSearch(key, on) {
  const was = S.offDevice[key];
  S.offDevice = { ...S.offDevice, [key]: on };
  try {
    if (key === "gameSearch") await api.setGameSearchEnabled(on);
    else await api.setGifSearchEnabled(on);
  } catch (err) {
    S.offDevice = { ...S.offDevice, [key]: was };
    flash(err);
  }
}

// moveChannelToCategory reassigns a channel's category (preserving type/order/topic).
// The icon for a channel kind. Shared, because the sidebar and the chat header
// each used to carry their own copy of this and the header's had no case for
// voice — so opening a voice channel's chat showed it as a # channel.
export const channelTypeIcon = (t) =>
  t === "voice" ? "speaker" : t === "announcement" ? "megaphone" : t === "forum" ? "forum" : "hash";

// The channel kinds a channel can be turned into. Threads aren't here: a thread
// belongs to its forum and only makes sense inside it.
export const CHANNEL_TYPES = [
  { id: "text", label: "Text", icon: "hash" },
  { id: "voice", label: "Voice", icon: "speaker" },
  { id: "announcement", label: "Announcement", icon: "megaphone" },
  { id: "forum", label: "Forum", icon: "forum" },
];

// setChannelType converts a channel in place. Nothing is destroyed by this —
// messages stay where they are and a voice channel's chat is still reachable —
// so the two guards below are about not surprising anyone, and both are
// reversible by converting back.
export async function setChannelType(channel, type) {
  const current = channel.type || "text";
  if (type === current) return;

  // Pulling the floor out from under a live call: everyone in it would be
  // talking in a channel that no longer has a call.
  const inCall = Object.keys(S.voiceRosters[channel.id] || {}).length > 0 || S.voice?.channelId === channel.id;
  if (current === "voice" && inCall) {
    flash("There's a call in this channel right now — end it first", "error");
    return;
  }

  // A forum's threads are channels parented to it, and the sidebar only shows
  // threads under a forum. Convert the forum away and they're still there but
  // nowhere to be seen, which looks exactly like data loss unless we say so.
  const threads = (activeGuild()?.channels || []).filter((c) => c.parent === channel.id).length;
  const apply = async () => {
    try {
      await api.setChannelMeta(
        S.activeGuildId,
        channel.id,
        type,
        channel.category || "",
        channel.position || 0,
        channel.topic || "",
      );
      await refreshGuilds();
      flash(`#${channel.name} is now a ${type} channel`, "success");
    } catch (err) {
      flash(err);
    }
  };
  if (current === "forum" && threads) {
    S.modal = {
      kind: "confirm",
      title: `Turn #${channel.name} into a ${type} channel?`,
      body: `Its ${threads} post${threads === 1 ? "" : "s"} won't be deleted, but they'll be hidden until you turn it back into a forum.`,
      confirmLabel: "Convert",
      onConfirm: () => {
        S.modal = null;
        apply();
      },
    };
    return;
  }
  await apply();
}

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
  clearArchive();
}

// ---- the imported archive above live history ----------------------------
//
// A guild's chronicle is a signed index every member holds; the pages behind it
// are fetched from whoever has them, only when somebody scrolls that far back.
// From the feed's point of view it is simply more history: the same cursor, the
// same "hold the reader's position" discipline as loadOlder, reached once live
// history has run out.

function clearArchive() {
  S.archiveRows = [];
  S.archiveLoading = false;
  S.archiveReachedStart = false;
  S.archiveMetered = false;
}

// refreshChronicle loads the active guild's archive index. Absence is the
// ordinary answer — most guilds have never imported anything — so a failure
// here is silent rather than a toast on every channel switch.
export async function refreshChronicle(guildID = S.activeGuildId) {
  if (!guildID) {
    S.chronicle = null;
    return;
  }
  let view = null;
  try {
    view = await api.chronicleInfo(guildID);
  } catch {
    view = null;
  }
  // Another guild was opened while this was in flight.
  if (S.activeGuildId !== guildID) return;
  S.chronicle = view && view.id ? view : null;
}

// archiveChannel is the archived channel whose history sits above the one on
// screen, matched on the mapping the import recorded. "" mapped means the
// import put it nowhere, and nothing should render.
export function archiveChannel() {
  const ch = S.activeChannelId;
  if (!ch || !S.chronicle) return null;
  return S.chronicle.channels?.find((c) => c.mapped === ch && c.messages > 0) || null;
}

// loadArchiveOlder pages the archive backwards from the oldest row on screen.
// Returns how many rows were prepended, so the feed can restore scroll by the
// exact height the content grew — the same contract loadOlder has.
//
// allowMetered is the reader's explicit "yes, on cellular" and is never assumed:
// the default call reports the refusal instead of spending the data.
export async function loadArchiveOlder(allowMetered = false) {
  const guildID = S.activeGuildId;
  const chan = archiveChannel();
  if (!guildID || !chan || S.archiveLoading || S.archiveReachedStart) return 0;
  if (S.archiveMetered && !allowMetered) return 0;
  S.archiveLoading = true;
  let prepended = 0;
  try {
    // The cursor is the oldest row we hold; nothing yet means "start at the
    // newest", which the backend spells 0.
    const before = S.archiveRows.length ? S.archiveRows[0].nano : 0;
    const page = await api.chronicleMessages(guildID, chan.id, before, 100, allowMetered);
    if (S.activeGuildId !== guildID || archiveChannel()?.id !== chan.id) return 0;
    if (page?.metered) {
      S.archiveMetered = true;
      return 0;
    }
    S.archiveMetered = false;
    const older = page?.messages || [];
    if (older.length === 0) {
      S.archiveReachedStart = true;
      return 0;
    }
    // Deduplicate on the way in. The cursor is a nanosecond that has been
    // through a JSON number, which loses a couple of hundred nanoseconds of
    // precision at present-day epochs — enough to hand back the row it was
    // taken from, never enough to skip one.
    const have = new Set(S.archiveRows.map((r) => r.id).filter(Boolean));
    const fresh = older.filter((r) => !r.id || !have.has(r.id));
    if (fresh.length) S.archiveRows = [...fresh, ...S.archiveRows];
    prepended = fresh.length;
    // A short page is the end of this channel's archive. So is a full page that
    // was entirely a repeat, which is what a cursor that failed to advance
    // looks like.
    if (older.length < 100 || prepended === 0) S.archiveReachedStart = true;
  } catch (err) {
    // One unreachable page must not look like the end of history: say so, and
    // let the reader try again by scrolling.
    flash(err);
  } finally {
    S.archiveLoading = false;
  }
  return prepended;
}

// isMentionOfSelf answers "does this message address me?" — the same question
// the unread counter, the alert sound and the row highlight all ask, so they had
// better agree. Exported for the last of those: the renderer's own
// `.mention-self` class is close but not the same thing, since it also fires on
// an @everyone you typed yourself.
export function isMentionOfSelf(m) {
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
  // fine — it's one ping to one person, not a broadcast). A role you hold
  // counts too — that's what a role is for — but only from a real member, for
  // the same reason @everyone is guarded: a guest must not be able to reach a
  // whole role by naming it.
  const names = [S.displayName];
  if (m.kind === "" && m.sender !== S.identity.fingerprint) names.push(...myRoleNames());
  return containsMention(m.content, names);
}

// myRoleNames: the roles this account holds in the active guild. Roles are
// per-guild and S.roles only ever holds the guild you're looking at, so a
// mention of a role you hold elsewhere isn't seen here — the badge lands when
// you next open that guild, which is the same place its members list comes from.
function myRoleNames() {
  const mine = S.members.find((mm) => mm.isSelf)?.roleIds || [];
  return S.roles.filter((r) => mine.includes(r.id) && r.name).map((r) => r.name);
}

// mentionRefs: the role and #channel tables the renderer needs, for the guild
// currently on screen. Built here rather than in each component so the feed,
// embeds and announcements all resolve the same names to the same things —
// and derived ONCE for the whole app, because every message row asks for it and
// rebuilding two arrays per row per keystroke adds up on a long feed.
const refTables = $derived.by(() => {
  const g = activeGuild();
  const mine = S.members.find((mm) => mm.isSelf)?.roleIds || [];
  return {
    roles: S.roles
      .filter((r) => r.name)
      .map((r) => ({ name: r.name, color: r.color, self: mine.includes(r.id) })),
    // Voice channels are left out on purpose: clicking one would either do
    // nothing or drop you into a live call, and neither is what "#name" in a
    // sentence promises. They render as ordinary text.
    channels: (g?.channels || [])
      .filter((c) => c.name && c.type !== "voice")
      .map((c) => ({ id: c.id, name: c.name })),
  };
});
export const mentionRefs = () => refTables;

export function guildUnread(g) {
  let count = 0;
  let mentions = 0;
  for (const c of g.channels) {
    const u = S.unread[c.id];
    if (!u || isMuted(c.id, g.id)) continue;
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

// Errors arrive here still wearing the Go package that raised them — "app:",
// "net:", "store:", or an "rpc Foo:" from the HTTP transport. That prefix is
// for a log, not for a person, and it reached users on 119 call sites: three
// screens stripped it inline and the fourth forgot, which is how you end up
// with "app: they're already in this guild" in a toast. Strip it once, here.
//
// Only the leading package token: NOT everything up to the last colon, which
// would throw away a helpful multi-clause message and leave just the innermost
// transport error.
const GO_PREFIX = /^(?:(?:app|net|store|mls|bridge|rpc\s+\w+):\s*)+/;
// The browser's own words for "nothing answered". They are true and useless.
const RAW_NETWORK = /^(failed to fetch|networkerror when attempting to fetch resource\.?|load failed|the network connection was lost\.?)$/i;

export function humanError(msg) {
  const text = String(msg?.message ?? msg ?? "").replace(GO_PREFIX, "");
  if (msg?.offline || RAW_NETWORK.test(text.trim()))
    return "Concord isn't responding — trying to reconnect";
  return text;
}

export function flash(msg, kind = "info") {
  // Back-compat: flash(err) with an Error (or anything error-shaped) reads as
  // a failure without every existing call site having to opt in.
  if (kind === "info" && (msg instanceof Error || (typeof msg === "object" && msg?.message)))
    kind = "error";
  const id = ++toastSeq;
  S.toasts.push({ id, kind, text: humanError(msg) });
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

// The feed renders a WINDOW of its rows, so "find the row and scroll to it" can
// no longer be a querySelector: the row a search result or a reply-ref points at
// is very often a thousand rows outside the window and has no element at all.
// MessageList registers a way to bring one back — reveal(key) widens the window
// to include it and answers whether it could, and the callers below then look
// the element up on the next tick. Nothing else about their contract changes,
// including the synchronous true/false they hand back.
let feedReveal = null;
export function registerFeedReveal(fn) {
  feedReveal = fn;
  return () => {
    if (feedReveal === fn) feedReveal = null;
  };
}
// Wait for the row to actually be on screen. The window it belongs to has to
// render and lay out first, and how long that takes depends on how many rows the
// jump had to mount — a fixed two frames was enough when the jump was short and
// silently did nothing when it was not, leaving the reader parked wherever they
// were with no sign that anything had been asked for. fn returns false while it
// is still waiting.
function afterRender(fn, tries = 15) {
  requestAnimationFrame(() => {
    if (fn() === false && tries > 0) afterRender(fn, tries - 1);
  });
}
export function scrollSoon() {
  S.newBelow = false;
  // Tell the feed this is a deliberate return to the newest message, so it
  // renders the end of the thread rather than working it out from a scroll
  // position that is about to change.
  feedReveal?.("bottom");
  // Twice, a frame apart. The feed renders a window of its rows and measures
  // them as it goes, so the height it reports on the first frame after a jump is
  // still an estimate — pinning to it once left the reader a screenful short of
  // the newest message about one time in three.
  requestAnimationFrame(() => {
    if (feedEl) feedEl.scrollTop = feedEl.scrollHeight;
    requestAnimationFrame(() => {
      if (feedEl) feedEl.scrollTop = feedEl.scrollHeight;
    });
  });
}
// feedNearBottom: is the user effectively at the end of the thread? Used to
// decide between following new messages and leaving the reader alone.
export function feedNearBottom() {
  if (!feedEl) return true;
  return feedEl.scrollHeight - feedEl.scrollTop - feedEl.clientHeight < 120;
}

// ---- reading-position stash ----
// channelId -> the first visible message id when the reader left mid-scroll.
// Session-only on purpose: after a restart the NEW-divider/bottom landing is
// the right default again, and stale anchors would fight history growth.
const scrollStash = {};

function firstVisibleMsgId() {
  if (!feedEl) return "";
  const top = feedEl.getBoundingClientRect().top;
  for (const el of feedEl.querySelectorAll("[data-msg-id]")) {
    if (el.getBoundingClientRect().bottom > top + 8) return el.dataset.msgId || "";
  }
  return "";
}

// Instant, flash-free — unlike scrollToMessage this is not a "look here" jump,
// it's the reader's own place quietly coming back.
function restoreReadingPosition(id) {
  const anchor = scrollStash[id];
  if (!anchor) return false;
  delete scrollStash[id];
  if (!S.messages.some((m) => m.id === anchor)) return false;
  feedReveal?.(anchor);
  // Same settling problem as scrollToMessage, same answer: keep putting the row
  // back at the top until the window stops moving it.
  let prev = null;
  let steady = 0;
  afterRender(() => {
    const el = feedEl?.querySelector(`[data-msg-id="${CSS.escape(anchor)}"]`);
    if (!el) return false;
    const top = Math.round(el.getBoundingClientRect().top);
    steady = prev !== null && Math.abs(top - prev) <= 1 ? steady + 1 : 0;
    if (steady >= 2) return true;
    el.scrollIntoView({ block: "start" });
    prev = Math.round(el.getBoundingClientRect().top);
    return false;
  }, 24);
  return true;
}
// scrollToMessage: center the target row and flash-highlight it so the eye
// lands on it (shared by reply-ref clicks, pin jumps, and search results).
// The flash is restartable — jumping again (same row or another) clears any
// in-flight highlight and replays the animation from the start.
let flashTimer = null;
let flashEl = null;
function landOn(el, instant = false) {
  const reduceMotion = instant || window.matchMedia?.("(prefers-reduced-motion: reduce)")?.matches;
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
}
// flashChannel: the same land-and-flash treatment, aimed at a row in the
// channel list. Creating a channel now says so with a toast, but a toast in
// the corner does not answer "where did it go?" — the new row appears
// somewhere in a list of thirty, as often as not below the fold. This walks
// the eye from the toast to the row.
//
// Its own querySelector rather than landOn's, because landOn only ever looks
// inside the feed; the channel list is a different scroller entirely.
export function flashChannel(id) {
  if (!id) return false;
  const sel = `[data-ch-id="${CSS.escape(id)}"]`;
  afterRender(() => {
    const el = document.querySelector(sel);
    if (!el) return false;
    landOn(el);
    return true;
  });
  return true;
}

export function scrollToMessage(id) {
  const sel = `[data-msg-id="${CSS.escape(id)}"]`;
  const el = feedEl?.querySelector(sel);
  if (el) {
    landOn(el);
    return true;
  }
  // Not rendered: it may still be loaded and simply outside the feed's window.
  // Asking the feed to bring it back answers both questions at once — false here
  // still means "that message isn't loaded yet", which is what callers report.
  if (!feedReveal?.(id)) return false;
  // A jump into a part of the thread that was not rendered is not a smooth
  // scroll from where we are: the distance is a spacer, there is nothing in it
  // to scroll past, and gliding across it drags the window along behind and
  // loses the row again. It lands.
  //
  // And it lands more than once. The window mounts the rows, measures them, and
  // corrects the spacer above them from that measurement — which moves every row
  // it has just placed, by a screenful or more when the rows turned out shorter
  // than the estimate. Centring on the first frame the row exists therefore aims
  // at a layout that is about to change. So re-centre until the row stops
  // moving, and only then flash it.
  let prev = null;
  let steady = 0;
  afterRender(() => {
    const found = feedEl?.querySelector(sel);
    if (!found) return false;
    const top = Math.round(found.getBoundingClientRect().top);
    steady = prev !== null && Math.abs(top - prev) <= 1 ? steady + 1 : 0;
    if (steady >= 2) {
      landOn(found, true);
      return true;
    }
    found.scrollIntoView({ block: "center", behavior: "auto" });
    prev = Math.round(found.getBoundingClientRect().top);
    return false;
  }, 24);
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

// The two candidate inks. Near-black rather than pure black, so a dark label on
// an accent pill belongs to the same palette as the app's deepest surface.
const FG_DARK = "#141419";
const FG_LIGHT = "#ffffff";
const FG_DARK_L = relLuminance(FG_DARK);

// accentForeground picks black or white text for a given accent fill — whichever
// actually measures better, rather than a guessed luminance cutoff. The cutoff
// used to be 0.55, which is nowhere near the real crossover for this pair
// (0.196): everything from the default teal to nord and dracula was getting
// white at 1.8–3.5:1 when black would have cleared 5:1.
export function accentForeground(color) {
  const l = relLuminance(color);
  if (l == null) return FG_LIGHT;
  const onDark = (l + 0.05) / (FG_DARK_L + 0.05);
  const onLight = 1.05 / (l + 0.05);
  return onDark >= onLight ? FG_DARK : FG_LIGHT;
}

// syncAccentFg resolves whatever --accent currently is (an explicit color OR a
// theme pack's CSS value) and stamps a contrast-safe --accent-fg to match.
export function syncAccentFg() {
  const el = document.documentElement;
  const accent = getComputedStyle(el).getPropertyValue("--accent").trim();
  el.style.setProperty("--accent-fg", accentForeground(accent));
}

export function applyAccent(color) {
  // No profile colour and no preset is the commonest case of all, and it used to
  // leave --accent-fg wherever it happened to be — including whatever the last
  // accent needed. Fall through to the CSS accent and derive from that.
  if (!color) return syncAccentFg();
  document.documentElement.style.setProperty("--accent", color);
  document.documentElement.style.setProperty("--accent-fg", accentForeground(color));
}

// ---- appearance (theme / accent preset / density) ----
// Device-local prefs, stamped onto <html>: data-theme picks the palette in
// app.css, data-density the message spacing, and a non-empty accent pref
// overrides the profile color (--accent-hover/-soft derive from it in CSS).

const sysDark = window.matchMedia?.("(prefers-color-scheme: dark)");

// Packs whose backdrop animates (see app.css .theme-bg + [data-anim-bg]).
export const ANIMATED_PACKS = new Set([
  "aurora",
  "synthwave",
  "cosmos",
  "molten",
  "prism",
  "monsoon",
  "fathom",
  "skyline",
  "eclipse",
  "daybreak",
  "dunes",
  "canopy",
  "datastream",
  "sonar",
  "lantern",
  "glacier",
  "vinyl",
  "storm",
  "bloom",
  "blossom",
  "meridian",
  "orbit",
  "radiant",
  "silicon",
  "uptime",
  "zellige",
  "atrium",
]);
// Packs with a STATIC coloured mesh behind translucent surfaces ([data-textured]).
export const TEXTURED_PACKS = new Set(["frost", "dusk", "grape"]);

// The pack palettes are a fifth of the stylesheet and match nothing at all
// unless somebody chose one, so they travel in their own file. The pack is not
// stamped on the document until that file has arrived: half of them make the
// app's surfaces translucent so a backdrop can show through, and stamping that
// before the backdrop exists would show a frame of transparent panels over
// nothing.
let packsCSS = null;
let packsLoaded = false;
export function themePacksReady() {
  if (!S.prefs.themePack) return Promise.resolve();
  if (!packsCSS) packsCSS = import("../themepacks.css").catch(() => {});
  return packsCSS;
}

export function applyAppearance() {
  const el = document.documentElement;
  const t = S.prefs.theme || "dark";
  el.dataset.theme = t === "system" ? (sysDark && !sysDark.matches ? "light" : "dark") : t;
  el.dataset.density = S.prefs.density === "compact" ? "compact" : "cozy";
  const pack = S.prefs.themePack;
  if (pack && !packsLoaded) {
    // Not yet: keep the default palette on screen and come back when the pack
    // stylesheet lands. main.js waits for this before the first mount, so the
    // only time anybody sees this branch is a live change from the picker.
    themePacksReady().then(() => {
      packsLoaded = true;
      applyAppearance();
    });
  }
  if (pack && packsLoaded) el.dataset.themePack = pack;
  else delete el.dataset.themePack;
  // Animated packs render a live backdrop (.theme-bg) and need translucent
  // surfaces so it shows through; a single flag drives both regardless of pack.
  if (packsLoaded && ANIMATED_PACKS.has(pack)) el.dataset.animBg = "1";
  else delete el.dataset.animBg;
  // Textured packs use the same translucent-surface trick over a STATIC mesh.
  if (packsLoaded && TEXTURED_PACKS.has(pack)) el.dataset.textured = "1";
  else delete el.dataset.textured;
  // The shine pref, stamped as ONE attribute the pack CSS keys off, so a pack
  // that has a specular pass needs a single extra rule and every pack that
  // doesn't needs none. Only the literal `false` turns it off — a value from a
  // newer build or a hand-edited prefs file can never stamp an attribute no
  // stylesheet matches, so an unrecognised pref reads as the default look
  // rather than as some half-painted third state (same contract as validFx).
  if (S.prefs.themeShine === false) el.dataset.themeShine = "off";
  else delete el.dataset.themeShine;
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
  // UI scale rides CSS zoom: it scales the fixed-px type/spacing tokens in one
  // move and both engines this app ships on (Chromium, WebKitGTK) honor it.
  const scale = Number(S.prefs.uiScale) || 1;
  if (scale !== 1) el.style.zoom = scale;
  else el.style.removeProperty("zoom");
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
  await refreshRequests();
  await refreshOffDeviceSearch();
  S.ready = true;
  initEvents();
  // Adopt reads that happened in other sessions/devices BEFORE counting, so
  // badges cleared elsewhere stay cleared here.
  await syncReadState();
  recomputeUnread();
  refreshNetStatus();
  // Slow poll backstops the presence-event refresh (covers bootstrap dials that
  // don't produce a peer-presence event, e.g. a relay reservation forming).
  // Skipped while hidden — nobody can see the indicator, and on a phone every
  // needless timer keeps the CPU awake — and refreshed the moment we're back.
  setInterval(() => {
    if (!document.hidden) refreshNetStatus();
  }, 15000);
  document.addEventListener("visibilitychange", () => {
    if (!document.hidden) refreshNetStatus();
  });
}


// refreshNetStatus pulls the current connectivity snapshot into S.netStatus.
export async function refreshNetStatus() {
  try {
    S.netStatus = await api.networkStatus();
  } catch {
    /* locked or transport down — leave the last known status */
  }
}

// The transport's own verdict on whether anything is listening, raised into the
// reconnecting bar. Coming back is worth a refresh: whatever happened while we
// were blind (messages, members, presence) never reached this page.
onTransportHealth((up) => {
  const wasOffline = S.offline;
  S.offline = !up;
  if (up && wasOffline) {
    refreshGuilds().catch(() => {});
    refreshNetStatus();
  }
});

// ---- notification permission, asked late ----

// offerNotifications raises the in-app rationale bar the first time a message
// arrives that the user would have been notified about, had the OS grant
// existed. It does NOT open the system dialog — that is what the bar's Enable
// button does, so the system dialog only ever appears with the user's finger
// already moving towards "allow".
//
// Once per session at most, and never again after "Not now": a bar that comes
// back on every message is the same nagging as the cold prompt, just slower.
// The declined pref is device-local, and Settings -> Notifications still offers
// the switch, so saying no here is not a dead end.
let notifyAsked = false;
export async function offerNotifications() {
  if (notifyAsked || S.notifyAsk || S.prefs.notifyAskDeclined) return;
  notifyAsked = true;
  try {
    if (await wantsPermissionPrompt()) S.notifyAsk = true;
  } catch {
    /* no notification support here at all — nothing to offer */
  }
}

export function dismissNotifyAsk(remember) {
  S.notifyAsk = false;
  if (remember) setPref("notifyAskDeclined", true);
}

// ---- per-message hiding ----

// Hiding one message, as opposed to blocking its author. The case is a single
// thing somebody does not want on their screen again — a picture, a slur —
// where blocking the whole person would be an overreaction or, in a guild they
// have to keep reading, not an option.
//
// Local, persisted, reversible, and NOT a delete: the message stays in the
// store and in everyone else's copy. Concord cannot remove something from other
// people's devices and must never imply that it has.
export function hideMessage(id) {
  if (!id || S.hiddenMessages.includes(id)) return;
  S.hiddenMessages = [...S.hiddenMessages, id];
  saveJSON("concord.hiddenMessages", S.hiddenMessages);
}

export function unhideMessage(id) {
  S.hiddenMessages = S.hiddenMessages.filter((x) => x !== id);
  saveJSON("concord.hiddenMessages", S.hiddenMessages);
}

export function isHidden(id) {
  return !!id && S.hiddenMessages.includes(id);
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
    flash(`Blocked ${name || "user"} — they can't add you to DMs or guilds`, "success");
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

// ---- message requests ----
// A stranger's DM waits here until you say yes. Nothing in this list has been
// joined: the invite code is held, not redeemed, so declining costs the sender
// nothing they can observe (no read receipt for harassment).
export async function refreshRequests() {
  try {
    S.requests = (await api.messageRequests()) || [];
  } catch {
    /* older backend or locked — the tray just stays empty */
  }
}
export async function acceptRequest(fingerprint) {
  const dm = await api.acceptMessageRequest(fingerprint);
  await refreshRequests();
  await refreshGuilds();
  if (dm?.id) await selectGuild(dm.id);
  return dm;
}
export async function declineRequest(fingerprint, block = false) {
  await api.declineMessageRequest(fingerprint, block);
  await refreshRequests();
  if (block) await refreshBlocked();
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
    // Pick up where you left off — the guild you had open when you closed the
    // app, and inside it the channel you were reading. Only when that place
    // still exists; a guild you've since left falls through to the default.
    const resume = S.guilds.find((g) => g.id === lastPlace.guildId);
    // No memory yet: land on the top GUILD in the rail rather than Notes/DMs,
    // which sort first in the raw list because they're usually the oldest.
    const first = resume || S.guilds.find((g) => g.kind !== "dm") || S.guilds[0];
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
  // Drop the previous guild's archive index before anything renders: the feed
  // matches archived channels by mapped id, and two guilds' ids do not collide
  // but "no archive yet" briefly reading as "the last guild's archive" would.
  S.chronicle = null;
  const g = S.guilds.find((x) => x.id === id);
  if (g && g.channels.length) await selectChannel(channelToResume(g));
  else {
    // A guild with no channels (or an unknown id) must not keep the previous
    // guild's channel active — otherwise the old feed renders and, worse,
    // messages get sent to the previous guild's channel.
    S.activeChannelId = "";
    clearFeed();
  }
  // After the channel, not before: the index is only consulted once live
  // history runs out, so it must never delay the first screen.
  refreshChronicle(id);
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
// dmList is the direct-message column, in the order it is shown: Notes pinned
// on top, then conversations by most recent activity. Exported because the DM
// button used to pick S.guilds.find(kind === "dm") — the first DM in raw guild
// order, which is creation order and so effectively arbitrary. Two definitions
// of "your DMs" that disagree is how a button lands somewhere the list never
// suggested.
export function dmList() {
  // Hide empty pending DMs — a freshly-created invite nobody has joined yet
  // (just you) is noise until a peer redeems it and it gets a name/avatar.
  const list = S.guilds.filter((x) => x.kind === "dm" && (x.dmNotes || (x.dmMembers ?? 2) >= 2));
  list.sort(
    (a, b) => (a.dmNotes ? -1 : b.dmNotes ? 1 : 0) || (b.lastActivity || 0) - (a.lastActivity || 0),
  );
  return list;
}

// openDMs lands where you actually were: the DM you last had open, else the one
// with the newest message, else Notes. Anything else means the button takes you
// to a conversation you did not ask for and were not looking at.
export async function openDMs() {
  const list = dmList();
  const resume = list.find((d) => d.id === lastPlace.guildId);
  const target = resume || list.find((d) => !d.dmNotes) || list[0];
  if (!target || target.id === "__notes__") {
    await selectNotes();
    return;
  }
  await selectGuild(target.id);
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

// startMeeting creates a disposable meeting room (voice + chat, expiring), opens
// it, and pops the shareable invitation — the "send them a Concord meeting
// instead of a Zoom link" move. The modal owns the link's lifetime from there.
export async function startMeeting() {
  try {
    const m = await api.startMeeting();
    await refreshGuilds();
    await selectGuild(m.guild.id);
    // A browser-guest link needs a rendezvous server; without one the modal
    // just offers the invite code (the app-to-app path).
    let guestLink = "";
    let expires = 0;
    try {
      guestLink = await api.createGuestLink(m.guild.id);
      expires = await api.meetingExpiry(m.guild.id);
    } catch {
      /* no rendezvous configured — code-only invitation */
    }
    S.modal = { kind: "meeting", code: m.code, guestLink, guildId: m.guild.id, expires };
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
  const mru = [id, ...S.recentChannels.filter((c) => c !== id)].slice(0, 15);
  S.recentChannels = mru;
  saveJSON("concord.mru", mru);
  // If the reader left the outgoing channel mid-scroll, remember the first
  // visible row so coming back doesn't dump them at the bottom to scroll-hunt.
  // At-bottom clears any stale anchor: bottom is where they chose to be.
  const prev = S.activeChannelId;
  if (prev && prev !== id) {
    if (!feedNearBottom()) scrollStash[prev] = firstVisibleMsgId();
    else delete scrollStash[prev];
  }
  S.activeChannelId = id;
  rememberPlace(S.activeGuildId, id);
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
  // Unread beats the stash: catching up on what you missed outranks resuming
  // an old scrollback session.
  if (hasUnread) scrollToNewDivider();
  else if (!restoreReadingPosition(id)) scrollSoon();
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

// MAX_LOADED_ROWS caps how much history one channel may hold in memory. Every
// page-back added 200 rows and nothing ever took any away, so a long afternoon
// in a busy channel grew the array — and, before the feed was windowed, the DOM
// — without limit, and the only thing that ever released it was switching
// channels.
//
// Trimming happens at exactly one moment: when the reader is back at the bottom.
// Everything dropped is then far above the viewport, the scroller clamps to the
// same place it was already resting, and nothing on screen moves. It also
// undoes the two pieces of state that say "there is nothing older": there is
// again, and scrolling up re-fetches it from the same sqlite pages it came from
// the first time. Editing a message that is about to be dropped defers the trim
// rather than throwing the draft away.
const MAX_LOADED_ROWS = 600;

export function trimLoadedHistory() {
  if (S.messages.length <= MAX_LOADED_ROWS) return false;
  if (S.loadingOlder || S.feedLoading || S.archiveLoading) return false;
  const keep = S.messages.slice(-MAX_LOADED_ROWS);
  if (S.editing && !keep.some((m) => m.id === S.editing.id)) return false;
  S.messages = keep;
  feedOldest = keep[0].sent;
  S.feedReachedStart = false;
  // The archive hangs off the START of live history. With that start no longer
  // loaded there is nothing for it to hang from, and its rows are the largest
  // single thing this channel is holding.
  clearArchive();
  return true;
}

// scrollToNewDivider places the unread divider comfortably in view (falling
// back to the bottom if it isn't rendered for any reason).
function scrollToNewDivider() {
  feedReveal?.("new");
  let waited = 0;
  afterRender(() => {
    const el = feedEl?.querySelector(".new-divider");
    if (el) {
      el.scrollIntoView({ block: "center" });
      return true;
    }
    // There may simply be no divider (nothing unread, or it is not in this
    // channel's loaded history). Give the window a few frames to prove it, then
    // fall back to the newest message.
    if (++waited < 8) return false;
    if (feedEl) feedEl.scrollTop = feedEl.scrollHeight;
    return true;
  });
}

export async function refreshRightPanel() {
  // Which guild this pass is FOR, captured before the first await. Two of these
  // overlap routinely — selectGuild runs one while an event-driven
  // scheduleRefresh runs another — and without the guard the slower fetch wins
  // by finishing last, painting the guild you just left into the member panel of
  // the one you just opened. It corrects itself on the next refresh, which is
  // exactly what makes it read as the panel repainting on its own.
  const forGuild = S.activeGuildId;
  if (forGuild) {
    const members = (await api.members(forGuild)) || [];
    if (S.activeGuildId !== forGuild) return; // moved on: this answer is stale
    S.members = members;
    const g = activeGuild();
    const roles = g && g.kind !== "dm" ? (await api.roles(forGuild)) || [] : [];
    if (S.activeGuildId !== forGuild) return;
    S.roles = roles;
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
    if (g) {
      await refreshGuilds();
      // The tray rides the guild refresh rather than an event of its own: a
      // request arriving IS a guild-list change as far as the sidebar is
      // concerned, and it inherits the same coalescing.
      await refreshRequests();
    }
    if (p) await refreshRightPanel();
    // An archive arrives the way any other guild fact does — gossiped as guild
    // metadata — so a member who was online when the owner finished an import
    // gets the divider without reopening the guild.
    if (g && S.activeGuildId) refreshChronicle(S.activeGuildId);
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

export async function sendMessage(text, replyToId, dir = "") {
  await api.sendMessage(S.activeChannelId, text, replyToId || "", dir);
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
      flash("Deleted for members — moderators can still view it in this guild.", "info");
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
    // Keep the conversation's recency current. lastActivity is computed by the
    // backend when guilds are fetched, so without this the DM list stays in
    // whatever order the last refresh produced: a DM that just received a
    // message did not move to the top until something else happened to
    // re-fetch. Bumping it here is what makes "most recent first" true while
    // you are sitting there watching it. Stored in UnixNano to match what the
    // backend sends.
    const arrivedAt = m.sent ? new Date(m.sent).getTime() * 1e6 : Date.now() * 1e6;
    const owner = S.guilds.find((g) => g.channels.some((c) => c.id === m.channelId));
    if (owner && arrivedAt > (owner.lastActivity || 0)) {
      owner.lastActivity = arrivedAt;
      S.guilds = [...S.guilds]; // reassign so dmList()'s sort re-runs
    }

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
    // One decision, both outputs: the chime and the OS notification agree by
    // construction rather than by two similar-looking conditions drifting apart.
    // A DM counts as a mention here — someone talking only to you is the case
    // "only @mentions" is meant to still let through.
    const alert =
      genuinelyNew &&
      wantsAlert(notifLevel(m.channelId), {
        mention: isMention || isDMChannel(m.channelId),
        dnd: S.identity.presence === "dnd",
      });
    if (alert) {
      // A direct message gets its own chime (unless you're already looking at
      // it); an @mention elsewhere gets the mention ping.
      if (isDMChannel(m.channelId) && (m.channelId !== S.activeChannelId || !document.hasFocus())) {
        playDM();
      } else if (isMention) {
        playMention();
      }
      notify(m, {
        selfFpr: S.identity.fingerprint,
        mention: isMention,
        onClick: () => jumpToChannel(m.channelId),
      });
      // ...and if that notification went nowhere because the OS grant was never
      // asked for, this is the moment to ask — a message the user wanted and
      // did not get is the only honest argument for the permission.
      offerNotifications();
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

  // A chat-export import reports itself every few hundred messages. The wizard
  // draws the bar from S.chronImport; this handler exists so that the run
  // survives the wizard being closed, and so the person who started it is told
  // when it lands even if they went back to reading.
  on("chronicle-import", async (p) => {
    if (!p) return;
    S.chronImport = p;
    if (p.phase !== "done" && p.phase !== "failed") return;
    let st = null;
    try {
      st = await api.chronicleImportStatus(p.jobId);
    } catch {
      /* the job is over either way; the toast below just gets less specific */
    }
    S.chronImportDone = st;
    if (st?.error) flash(st.error);
    else if (st?.result)
      // fmtCount, not plural(): a seven-digit import reads as gibberish
      // without the thousands separators.
      flash(
        `Archive imported — ${fmtCount(st.result.imported)} message${plur(st.result.imported)}`,
        "success",
      );
    // An import creates channels and attaches the archive, so both the sidebar
    // and the index above the feed are stale the moment it finishes.
    await refreshGuilds();
    refreshChronicle(p.guildId || S.activeGuildId);
  });

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
    // Your own typing, relayed from another of your devices, IS shown — typing
    // on your phone lighting up your name on your desktop is the proof the
    // devices are talking (suppressing it read as breakage; the user overruled
    // v0.49 here). It carries your account name, with a quiet "(you)".
    const self = t.from && t.from === S.identity?.fingerprint;
    // Resolve a human name: yours from the identity/roster, everyone else via
    // the backend-resolved name, the member roster, then the contact list.
    let label = self
      ? S.displayName || memberByFpr(t.from)?.name || t.name
      : t.name || memberByFpr(t.from)?.name || S.contacts.find((c) => c.fingerprint === t.from)?.name;
    // No resolvable name means NO entry: a raw key or truncated fingerprint is
    // never rendered in the typing strip. The backend only emits attributable
    // signals, so this drop is a startup-order edge, not a normal path.
    if (!label) return;
    if (self) label += " (you)";
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
    // Soundboard: a ~30-byte trigger; every client synthesizes locally
    // (lib/sounds.js SOUNDBOARD). Gates: only while WE are in that room, never
    // our own echo (we played on press), a per-sender rate limit so a held
    // button can't become a siren, and a zeroed per-peer volume slider mutes
    // their sound effects along with their voice.
    if (v.action === "sfx") {
      if (
        S.voice?.channelId === v.channelId &&
        v.fingerprint !== S.identity.fingerprint &&
        S.peerVolumes[v.fingerprint] !== 0
      ) {
        const now = Date.now();
        if (now - (sfxLast[v.fingerprint] || 0) >= 1000) {
          sfxLast[v.fingerprint] = now;
          playSfx(v.target);
        }
      }
      return;
    }
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
      } else if (isGuestFpr(v.fingerprint) && !announcedKnocks.has(v.fingerprint)) {
        // A guest is knocking at a room we're not sitting in. A member's knock
        // can wait (they have the app, they'll knock again); a guest is holding
        // an open socket at the door with a five-minute budget, so say so once.
        announcedKnocks.add(v.fingerprint);
        playDM();
        flash(`${guestName(v.fingerprint)} is knocking to join — hit Call to let them in`, "info");
      }
      return;
    }
    if (v.action === "unknock") {
      // The knocker gave up (or was dealt with elsewhere). Local-only, emitted
      // for browser guests — see voice.go on why it is never gossiped.
      announcedKnocks.delete(v.fingerprint);
      dropKnock(v.channelId, v.fingerprint);
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
    if (v.action === "invite") {
      // Someone in a call is asking us to come. Only for us, only from someone
      // we share this guild with (they're on its voice topic, which is proof
      // enough for an invitation — it opens a prompt, it doesn't move anyone).
      if (v.target === S.identity.fingerprint && !S.voice && v.fingerprint !== S.identity.fingerprint) {
        const guild = S.guilds.find((g) => g.channels?.some((c) => c.id === v.channelId));
        const ch = guild?.channels?.find((c) => c.id === v.channelId);
        if (ch) {
          playDM();
          S.callInvite = {
            channelId: v.channelId,
            from: nameFor(v.fingerprint),
            where: guild.kind === "dm" ? guild.name : `${guild.name} · ${ch.name}`,
          };
        }
      }
      return;
    }
    if (v.action === "state") {
      // "<muted><deafened>" as two flags, e.g. "10". Compact because it rides
      // the presence topic and gets re-sent whenever anyone new arrives.
      if (v.fingerprint && v.fingerprint !== S.identity.fingerprint) {
        S.voiceStates = {
          ...S.voiceStates,
          [v.fingerprint]: { muted: v.target?.[0] === "1", deafened: v.target?.[1] === "1" },
        };
      }
      return;
    }

    // Someone new in our call hasn't heard our mute state yet — say it again.
    if (v.action === "join" && S.voice?.channelId === v.channelId && v.fingerprint !== S.identity.fingerprint) {
      publishVoiceState();
    }
    if (v.action === "leave" && S.voiceStates[v.fingerprint]) {
      const next = { ...S.voiceStates };
      delete next[v.fingerprint];
      S.voiceStates = next;
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
  // A verified contact is offering to add us to their guild. We show it; we
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
  // covering peers that crashed or dropped without a clean "leave". Skipped
  // while hidden UNLESS we're in a call ourselves (then the roster drives real
  // audio state); a hidden idle app repainting rosters is wasted battery.
  setInterval(() => {
    if (document.hidden && !S.voice) return;
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
// "guest:Alice#3f9c1d" → "Alice". The session id is in there because knocks and
// moderation are keyed by fingerprint, and two strangers who both type "Alice"
// have to be two separate decisions. A guest can't smuggle a '#' into their name
// (the host strips it), so the last one is always the separator — and a
// fingerprint from before the id existed still reads back as the whole name.
export const guestName = (fpr = "") => {
  const s = fpr.slice(6);
  const cut = s.lastIndexOf("#");
  return (cut > 0 ? s.slice(0, cut) : s) || "Guest";
};
const announcedGuests = new Set();
// Knocking guests we have already nudged about, so a knock re-announced every
// few seconds rings once rather than every tick.
const announcedKnocks = new Set();

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
    // …but the lock survives if WE are the ones in the room. The roster counts
    // only other peers, so "empty" here also describes a host sitting alone in a
    // locked meeting waiting for people to knock — which is exactly office
    // hours, and dropping the lock there let the next guest walk straight in the
    // moment the previous one left. The stale-lock worry (see forgetLock) is
    // about a room with nobody inside to admit anyone.
    if (S.voice?.channelId !== channelId) forgetLock(channelId);
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
// the guild you happen to be looking at has no authority over a call in a
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

// inviteToCall pings someone to come and join the call you're in. It's a
// request that opens a prompt on their side — never anything that moves them,
// which is what separates "come hang out" from the moderator's move.
export function inviteToCall(fingerprint) {
  const ch = S.voice?.channelId;
  if (!ch || !fingerprint || fingerprint === S.identity.fingerprint) return;
  api.signalCall(ch, "invite", fingerprint).catch(() => {});
  flash(`Asked ${nameFor(fingerprint)} to join`, "success");
}

// publishVoiceState tells the room whether we're muted or deafened. Nobody can
// observe it otherwise — muting disables a track locally, which looks exactly
// like not speaking — so a badge on someone's tile only means anything if the
// client behind it volunteers the fact.
export function publishVoiceState() {
  const ch = S.voice?.channelId;
  if (!ch) return;
  api.signalCall(ch, "state", `${S.muted ? 1 : 0}${S.deafened ? 1 : 0}`).catch(() => {});
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
  // A member's knock is simply ignored — it times out on their side and they
  // still have the app. A GUEST is holding a socket open at the door, so silence
  // would be an eternal spinner: tell the backend to close it with a reason.
  if (isGuestFpr(fpr)) api.signalCall(channelId, "refuse", fpr).catch(() => {});
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
  // Ourselves, if we're in this room (we don't hear our own gossip heartbeat).
  if (S.voice && S.voice.channelId === channelId) {
    out.push({
      peerId: S.identity.peerId,
      fingerprint: S.identity.fingerprint,
      self: true,
      speaking: S.voiceSpeaking.includes("self"),
      sharing: S.sharing,
      muted: S.muted,
      deafened: S.deafened,
    });
  }
  // Keyed by PEER, not by account. Linked devices share one fingerprint, so
  // deduping on that quietly hid your own phone from your desktop (and vice
  // versa) whenever you joined the same call from both — the audio was flowing
  // the whole time, the roster just refused to admit the second device existed.
  // Only our own peer id is skipped, because we added it above.
  for (const [pid, info] of Object.entries(S.voiceRosters[channelId] || {})) {
    if (pid === S.identity.peerId) continue;
    out.push({
      peerId: pid,
      fingerprint: info.fingerprint,
      self: false,
      // Same account, different device — worth labelling, since otherwise it
      // looks like you're in the call twice for no reason.
      otherDevice: info.fingerprint === S.identity.fingerprint,
      muted: !!S.voiceStates[info.fingerprint]?.muted,
      deafened: !!S.voiceStates[info.fingerprint]?.deafened,
      // Speaking is only known for the room we're in (from the local mesh).
      speaking: S.voiceSpeaking.includes(pid),
      sharing: !!S.voiceSharing[pid],
    });
  }
  return out;
}
