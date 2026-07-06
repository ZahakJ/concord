// state.svelte.js — Concord's shared UI state (Svelte 5 runes) and the actions
// that mutate it. Components import { S, ... } and read/mutate S's properties;
// api.js stays the only transport layer underneath.
import { api, on } from "./api.js";
import { notify } from "./notify.js";
import { containsMention } from "./markdown.js";

export const S = $state({
  ready: false,
  identity: { peerId: "", fingerprint: "", displayName: "" },
  displayName: "",
  guilds: [],
  activeGuildId: "",
  activeChannelId: "",
  messages: [],
  members: [],
  contacts: [],

  replyingTo: null, // message being replied to
  editing: null, // message being edited (Message.svelte owns the draft)
  pickerTarget: null, // "composer" | message object (shared emoji picker)
  memberPopover: null, // fingerprint of the member card being inspected
  modal: null, // { kind, ... }
  toast: "",
  quickSwitcher: false,

  // unread[channelId] = { count, mentions } — counts survive refresh via the
  // localStorage last-read map (recomputed on load).
  unread: {},
  mutes: loadJSON("concord.mutes", {}), // channelId -> true

  typingList: [], // [{ from, label, timer }]

  voice: null, // { mesh, channelId }
  voiceParticipants: [],
  voiceSpeaking: [],
  voicePeerFpr: {},
  muted: false,

  searchQuery: "",
  searchResults: null, // null = closed, [] = no hits
  showPins: false,
});

export const activeGuild = () => S.guilds.find((g) => g.id === S.activeGuildId) || null;
export const activeChannel = () =>
  activeGuild()?.channels.find((c) => c.id === S.activeChannelId) || null;
export const memberByFpr = (fpr) => S.members.find((m) => m.fingerprint === fpr);

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

export function markRead(channelId) {
  if (!channelId) return;
  lastRead[channelId] = new Date().toISOString();
  saveJSON("concord.lastRead", lastRead);
  if (S.unread[channelId]) {
    const u = { ...S.unread };
    delete u[channelId];
    S.unread = u;
  }
}

export function toggleMute(channelId) {
  const m = { ...S.mutes };
  if (m[channelId]) delete m[channelId];
  else m[channelId] = true;
  S.mutes = m;
  saveJSON("concord.mutes", m);
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
          if (m.deleted || m.sender === S.identity.fingerprint) continue;
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
  return m.kind === "" && containsMention(m.content, [S.displayName]);
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

export function flash(msg) {
  S.toast = String(msg?.message || msg);
  setTimeout(() => (S.toast = ""), 2500);
}

// ---- feed scroll (MessageList registers its element) ----

let feedEl = null;
export function registerFeed(el) {
  feedEl = el;
}
export function scrollSoon() {
  requestAnimationFrame(() => {
    if (feedEl) feedEl.scrollTop = feedEl.scrollHeight;
  });
}
export function scrollToMessage(id) {
  const el = feedEl?.querySelector(`[data-msg-id="${CSS.escape(id)}"]`);
  if (!el) return false;
  el.scrollIntoView({ block: "center" });
  el.classList.add("flash-highlight");
  setTimeout(() => el.classList.remove("flash-highlight"), 1600);
  return true;
}

// ---- profile accent ----

export function applyAccent(color) {
  if (!color) return;
  document.documentElement.style.setProperty("--accent", color);
}

// ---- session / navigation ----

export async function onLogin() {
  S.identity = await api.identity();
  S.displayName = S.identity.displayName || "";
  applyAccent(S.identity.color);
  await refreshGuilds();
  S.ready = true;
  initEvents();
  recomputeUnread();
}

export async function refreshGuilds() {
  S.guilds = (await api.guilds()) || [];
  if (!S.activeGuildId && S.guilds.length) await selectGuild(S.guilds[0].id);
}

export async function selectGuild(id) {
  S.activeGuildId = id;
  const g = S.guilds.find((x) => x.id === id);
  if (g && g.channels.length) await selectChannel(g.channels[0].id);
  await refreshRightPanel();
}

export async function selectChannel(id) {
  S.activeChannelId = id;
  markRead(id);
  S.typingList.forEach((t) => clearTimeout(t.timer));
  S.typingList = [];
  S.replyingTo = null;
  S.editing = null;
  S.showPins = false;
  S.messages = (await api.messages(id)) || [];
  scrollSoon();
}

export async function refreshRightPanel() {
  if (S.activeGuildId) S.members = (await api.members(S.activeGuildId)) || [];
  S.contacts = (await api.contacts()) || [];
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

export function channelName(chId) {
  for (const g of S.guilds) {
    const c = g.channels.find((x) => x.id === chId);
    if (c) return `${g.name} #${c.name}`;
  }
  return "unknown channel";
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
    if (m.channelId === S.activeChannelId) {
      const i = S.messages.findIndex((x) => x.id === m.id);
      if (i >= 0) {
        S.messages = S.messages.map((x) => (x.id === m.id ? m : x)); // update (edit/delete/react)
      } else {
        S.messages = [...S.messages, m];
        scrollSoon();
        if (document.hasFocus()) markRead(m.channelId);
      }
    } else if (m.channelId && m.kind === "" && !m.deleted && m.sender !== S.identity.fingerprint) {
      bumpUnread(m.channelId, isMentionOfSelf(m));
    }
    notify(m, {
      selfFpr: S.identity.fingerprint,
      mention: isMentionOfSelf(m),
      muted: !!S.mutes[m.channelId],
      activeChannel: S.activeChannelId,
      onClick: () => jumpToChannel(m.channelId),
    });
  });

  on("presence", () => refreshRightPanel());

  on("guild-updated", async () => {
    await refreshGuilds();
    await refreshRightPanel();
  });

  on("typing", (t) => {
    if (t.channelId !== S.activeChannelId) return;
    const label = t.name || (t.from || "").slice(0, 9);
    S.typingList = S.typingList.filter((x) => x.from !== t.from);
    const timer = setTimeout(() => {
      S.typingList = S.typingList.filter((x) => x.from !== t.from);
    }, 4000);
    S.typingList = [...S.typingList, { from: t.from, label, timer }];
  });

  // Voice signaling routed to the active mesh.
  on("voice-presence", (v) => {
    if (S.voice && v.channelId === S.voice.channelId) {
      if (v.action === "join") {
        S.voicePeerFpr = { ...S.voicePeerFpr, [v.from]: v.fingerprint };
      } else {
        const c = { ...S.voicePeerFpr };
        delete c[v.from];
        S.voicePeerFpr = c;
      }
      S.voice.mesh.handlePresence(v.from, v.action);
    }
  });
  on("voice-signal", (v) => {
    if (S.voice) S.voice.mesh.handleSignal(v.from, v.data);
  });
}
