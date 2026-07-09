// state.svelte.js — Concord's shared UI state (Svelte 5 runes) and the actions
// that mutate it. Components import { S, ... } and read/mutate S's properties;
// api.js stays the only transport layer underneath.
import { api, on } from "./api.js";
import { notify } from "./notify.js";
import { containsMention } from "./markdown.js";
import { playVoiceJoin, playVoiceLeave, playMention, playDM } from "./sounds.js";

export const S = $state({
  ready: false,
  identity: { peerId: "", fingerprint: "", displayName: "" },
  displayName: "",
  guilds: [],
  activeGuildId: "",
  activeChannelId: "",
  messages: [],
  members: [],
  roles: [], // active guild's roles (highest position first)
  contacts: [],

  replyingTo: null, // message being replied to
  editing: null, // message being edited (Message.svelte owns the draft)
  pickerTarget: null, // "composer" | message object (shared emoji picker)
  // Floating profile card: { fingerprint, rect } where rect is the anchor's
  // viewport box. Opened by hovering/clicking a mention or a member row.
  profilePopover: null,
  modal: null, // { kind, ... }
  toasts: [], // [{ id, kind, text }] — kind: "info" | "success" | "error"
  quickSwitcher: false,
  // Raised by the command palette's "Set status" action; ChannelList consumes
  // it and opens the status popover anchored to the self row.
  statusPopRequest: false,

  // unread[channelId] = { count, mentions } — counts survive refresh via the
  // localStorage last-read map (recomputed on load).
  unread: {},
  mutes: loadJSON("concord.mutes", {}), // channelId -> true
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
    theme: "dark",
    accent: "",
    density: "cozy",
    ...loadJSON("concord.prefs", {}),
  },

  typingList: [], // [{ from, label, timer }]

  voice: null, // { mesh, channelId }
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
  sharing: false, // we are screen-sharing
  cameraOn: false, // our camera is on
  // videoTiles: [{ key, peerId, kind, self }] — one per live video source
  // (camera/screen, ours or a peer's). Streams live in the non-reactive
  // videoStreams map below; this array just drives which <video> tiles render.
  videoTiles: [],

  searchQuery: "",
  searchResults: null, // null = closed, [] = no hits
  showPins: false,

  // newBelow: messages arrived while the user was scrolled up reading history
  // (we deliberately do NOT yank the feed to the bottom in that case).
  newBelow: false,

  // update: an available app update {available, current, latest, url, notes},
  // or null. Drives the "Download" banner. Dismissal is per-version.
  update: null,

  // contextMenu: right-click menu {x, y, items} or null.
  contextMenu: null,
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
  return memberByFpr(fpr)?.name || frozenName || (fpr ? fpr.slice(0, 9) : "?");
}

// customEmojiMap: {name -> imageDataURI} for the active guild's custom emoji.
export function customEmojiMap() {
  const list = activeGuild()?.emoji || [];
  const m = {};
  for (const e of list) m[e.name] = e.image;
  return m;
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
}

// markAllRead clears unread across every channel (Shift+Esc).
export function markAllRead() {
  const now = new Date().toISOString();
  for (const g of S.guilds) for (const c of g.channels) lastRead[c.id] = now;
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

// markUnread rewinds a channel's read cursor to just before a message, so it
// (and everything after) shows as unread again, with the NEW divider restored.
export function markUnread(channelId, msg) {
  if (!channelId || !msg) return;
  const before = new Date(new Date(msg.sent).getTime() - 1).toISOString();
  lastRead[channelId] = before;
  saveJSON("concord.lastRead", lastRead);
  recomputeUnread();
  if (channelId === S.activeChannelId) S.readAnchor = before;
}

// ---- shared right-click context menu ----
// S.contextMenu = { x, y, items:[{label, icon?, danger?, onClick}|null] } | null
export function openContextMenu(e, items) {
  e.preventDefault();
  e.stopPropagation();
  S.contextMenu = { x: e.clientX, y: e.clientY, items: items.filter(Boolean) };
}
export function closeContextMenu() {
  S.contextMenu = null;
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

// Recompute unread counts for every channel from persisted last-read marks —
// called once after login so a refresh doesn't wipe the badges.
async function recomputeUnread() {
  const unread = {};
  for (const g of S.guilds) {
    for (const c of g.channels) {
      if (c.id === S.activeChannelId) continue;
      try {
        const msgs = (await api.messages(c.id)) || [];
        const since = lastRead[c.id] ? new Date(lastRead[c.id]) : null;
        let count = 0;
        let mentions = 0;
        for (const m of msgs) {
          // Match the live counter: only normal messages from others count.
          if (m.kind !== "" || m.deleted || m.sender === S.identity.fingerprint) continue;
          if (since && new Date(m.sent) <= since) continue;
          count++;
          if (isMentionOfSelf(m)) mentions++;
        }
        if (count) unread[c.id] = { count, mentions };
      } catch {
        /* channel unreadable right now — skip */
      }
    }
  }
  S.unread = unread;
}

function isMentionOfSelf(m) {
  if (m.kind !== "") return false;
  // @everyone / @here ping every member; don't fire on your own message.
  if (/(^|\s)@(everyone|here)\b/.test(m.content) && m.sender !== S.identity.fingerprint) return true;
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
export function scrollToMessage(id) {
  const el = feedEl?.querySelector(`[data-msg-id="${CSS.escape(id)}"]`);
  if (!el) return false;
  el.scrollIntoView({ block: "center" });
  el.classList.add("flash-highlight");
  setTimeout(() => el.classList.remove("flash-highlight"), 1600);
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

export function applyAccent(color) {
  if (!color) return;
  document.documentElement.style.setProperty("--accent", color);
}

// ---- appearance (theme / accent preset / density) ----
// Device-local prefs, stamped onto <html>: data-theme picks the palette in
// app.css, data-density the message spacing, and a non-empty accent pref
// overrides the profile color (--accent-hover/-soft derive from it in CSS).

const sysDark = window.matchMedia?.("(prefers-color-scheme: dark)");

export function applyAppearance() {
  const el = document.documentElement;
  const t = S.prefs.theme || "dark";
  el.dataset.theme = t === "system" ? (sysDark && !sysDark.matches ? "light" : "dark") : t;
  el.dataset.density = S.prefs.density === "compact" ? "compact" : "cozy";
  applyAccent(S.prefs.accent || S.identity.color);
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
  S.ready = true;
  initEvents();
  recomputeUnread();
}

export async function refreshGuilds() {
  S.guilds = (await api.guilds()) || [];
  if (!S.activeGuildId && S.guilds.length) {
    await selectGuild(S.guilds[0].id);
    return;
  }
  // If the active channel was deleted remotely (guild-updated -> refresh), don't
  // strand the user in a phantom channel that still renders + sends messages.
  const g = S.guilds.find((x) => x.id === S.activeGuildId);
  if (S.activeChannelId && !g?.channels?.some((c) => c.id === S.activeChannelId)) {
    if (g?.channels?.length) await selectChannel(g.channels[0].id);
    else {
      S.activeChannelId = "";
      S.messages = [];
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
    S.messages = [];
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
  let msgs;
  try {
    msgs = (await api.messages(id)) || [];
  } catch (err) {
    if (S.activeChannelId === id) flash(err);
    return;
  }
  // Guard against a stale fetch: if the user switched channels while this was in
  // flight, don't overwrite the now-active channel's messages/read-cursor.
  if (S.activeChannelId !== id) return;
  S.messages = msgs;
  // Advance the read mark past the newest message actually loaded, so a peer's
  // clock-skewed (future-dated) message we've now seen can't keep the badge lit.
  let newest = "";
  for (const m of S.messages) if (m.sent > newest) newest = m.sent;
  if (newest) markRead(id, newest);
  scrollSoon();
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
  } catch (err) {
    flash(err);
  }
}

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
        if (document.hasFocus()) markRead(m.channelId, m.sent);
      }
    } else if (firstSeen && m.channelId && m.kind === "" && !m.deleted && m.sender !== S.identity.fingerprint) {
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

  on("presence", () => scheduleRefresh({ panel: true }));

  on("guild-updated", () => scheduleRefresh({ guilds: true, panel: true }));

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
    updateVoiceRoster(v.channelId, v.from, v.fingerprint, v.action);

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
  }
  S.voiceRosters = rosters;
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
