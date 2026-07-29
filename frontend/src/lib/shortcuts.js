// shortcuts.js — one global keymap. Registered once by App.svelte after login.
//
//   Ctrl/Cmd+K        quick switcher
//   Ctrl/Cmd+F        focus message search
//   Ctrl/Cmd+,        user settings
//   Alt+↑ / Alt+↓     previous / next channel in the active guild
//   Alt+Shift+↑/↓     previous / next unread channel (across servers)
//   Ctrl+Alt+↑/↓      previous / next server
//   Ctrl+Shift+M      toggle mic mute (while in a call)
//   Ctrl+Shift+D      toggle deafen (while in a call)
//   (your choice)     hold to talk, when push-to-talk is on — see below
//   Ctrl/Cmd+U        toggle the member panel
//   Escape            close switcher / pins / search / reply — or, if nothing
//                     is open, mark the current channel read
//   Shift+Escape      mark ALL channels read
//   ? or Ctrl+/       keyboard-shortcut cheat sheet
import { S, activeGuild, selectChannel, selectGuild, jumpToChannel, markRead, markAllRead, toggleMemberPanel, isMuted } from "./state.svelte.js";
import { closeSearch } from "./search.js";
import { pressesBind, releasesBind, typesCharacter } from "./keybind.js";

function channelsOfActive() {
  return activeGuild()?.channels ?? [];
}

function stepChannel(dir) {
  const chs = channelsOfActive();
  if (!chs.length) return;
  const i = chs.findIndex((c) => c.id === S.activeChannelId);
  const next = chs[(i + dir + chs.length) % chs.length];
  selectChannel(next.id);
}

function stepGuild(dir) {
  const servers = S.guilds.filter((g) => g.kind !== "dm");
  if (!servers.length) return;
  const i = servers.findIndex((g) => g.id === S.activeGuildId);
  const from = i < 0 ? (dir > 0 ? -1 : 0) : i;
  selectGuild(servers[(from + dir + servers.length) % servers.length].id);
}

// Focus the desktop search box, returning false if it isn't on screen (mobile
// has its own search entry, and there's no box before a channel is open).
function focusSearch() {
  const sb = document.querySelector(".search-box");
  if (!sb) return false;
  sb.focus();
  sb.select?.();
  return true;
}

function stepUnread(dir) {
  const flat = [];
  for (const g of S.guilds) for (const c of g.channels) flat.push(c);
  if (!flat.length) return;
  const unreadIdx = flat
    .map((c, i) => ({ c, i }))
    .filter(({ c }) => S.unread[c.id] && !isMuted(c.id));
  if (!unreadIdx.length) return;
  const cur = flat.findIndex((c) => c.id === S.activeChannelId);
  const target =
    dir > 0
      ? unreadIdx.find(({ i }) => i > cur) || unreadIdx[0]
      : [...unreadIdx].reverse().find(({ i }) => i < cur) || unreadIdx[unreadIdx.length - 1];
  jumpToChannel(target.c.id);
}

// ---- push-to-talk ----
//
// Its own pair of listeners, because it's the only binding that cares about the
// key coming back UP. Two things it has to get right or the mic sticks open:
//
//   • Alt-tab away mid-push and the keyup lands in the other window, never
//     here. So blur and a hidden tab both count as a release.
//   • Holding a key autorepeats keydown; we only act on the transition.
//
// Scope: this is in-window only. A hotkey that works while Concord is behind
// another window needs the OS, and Concord doesn't reach outside its own
// process for anything — so the settings copy says so rather than pretending.

// The binding that STARTED the current hold, kept so the release is matched
// against it — rebinding or leaving the call mid-push must still let go.
let heldBind = null;

function pttBind() {
  if (!S.voice || !S.prefs.pushToTalk) return null;
  return S.prefs.pttBind?.code ? S.prefs.pttBind : null;
}

function release() {
  if (!heldBind) return;
  heldBind = null;
  S.talking = false;
  S.voice?.mesh?.setTalking(false);
}

function pttDown(e) {
  const bind = pttBind();
  if (!bind || e.repeat || heldBind || !pressesBind(e, bind)) return;
  // A binding with no modifier also types: while the composer has focus that
  // key belongs to the message, not to the mic.
  if (typesCharacter(bind) && inputFocused()) return;
  e.preventDefault();
  heldBind = bind;
  S.talking = true;
  S.voice.mesh?.setTalking(true);
}

function pttUp(e) {
  // Matched on the key alone — releasing a modifier first must not strand it.
  if (heldBind && releasesBind(e, heldBind)) release();
}

export function installShortcuts() {
  const handler = (e) => {
    const mod = e.ctrlKey || e.metaKey;
    if (mod && e.key.toLowerCase() === "k") {
      e.preventDefault();
      S.quickSwitcher = !S.quickSwitcher;
      return;
    }
    // Ctrl/Cmd+F — jump into message search (overrides the browser find bar,
    // but only when our search box is actually present, so we don't swallow it).
    if (mod && !e.shiftKey && e.key.toLowerCase() === "f") {
      if (focusSearch()) e.preventDefault();
      return;
    }
    // Ctrl/Cmd+, — user settings.
    if (mod && e.key === ",") {
      e.preventDefault();
      S.modal = S.modal?.kind === "settings" ? null : { kind: "settings" };
      return;
    }
    // Ctrl+Shift+M — toggle mic mute (only meaningful in a call).
    if (mod && e.shiftKey && e.key.toLowerCase() === "m") {
      if (S.voice) {
        e.preventDefault();
        S.muted = !S.muted;
        S.voice.mesh?.setMuted(S.muted);
      }
      return;
    }
    // Ctrl+Shift+D — toggle deafen (only meaningful in a call).
    if (mod && e.shiftKey && e.key.toLowerCase() === "d") {
      if (S.voice) {
        e.preventDefault();
        S.deafened = !S.deafened;
        S.voice.mesh?.setDeafened(S.deafened);
        if (S.deafened) S.muted = true;
      }
      return;
    }
    // Ctrl/Cmd+U — toggle the member panel.
    if (mod && !e.shiftKey && e.key.toLowerCase() === "u") {
      e.preventDefault();
      toggleMemberPanel();
      return;
    }
    // Ctrl+Alt+↑/↓ — previous / next server (distinct from plain Alt = channel).
    if (mod && e.altKey && (e.key === "ArrowUp" || e.key === "ArrowDown")) {
      e.preventDefault();
      stepGuild(e.key === "ArrowDown" ? 1 : -1);
      return;
    }
    if (e.altKey && !mod && (e.key === "ArrowUp" || e.key === "ArrowDown")) {
      e.preventDefault();
      const dir = e.key === "ArrowDown" ? 1 : -1;
      if (e.shiftKey) stepUnread(dir);
      else stepChannel(dir);
      return;
    }
    // Cheat sheet: ? (Shift+/) or Ctrl+/.
    if ((mod && e.key === "/") || (e.key === "?" && !inputFocused())) {
      e.preventDefault();
      S.modal = S.modal?.kind === "shortcuts" ? null : { kind: "shortcuts" };
      return;
    }
    if (e.key === "Escape" && !inputFocused()) {
      if (e.shiftKey) {
        markAllRead();
      } else if (S.contextMenu) S.contextMenu = null;
      else if (S.quickSwitcher) S.quickSwitcher = false;
      else if (S.pickerTarget) S.pickerTarget = null;
      else if (S.showPins) S.showPins = false;
      else if (S.searchResults !== null || S.searchLoading) closeSearch();
      else if (S.replyingTo) S.replyingTo = null;
      else if (S.modal) S.modal = null;
      // Nothing to dismiss → mark the current channel read (Discord-style).
      else if (S.activeChannelId) markRead(S.activeChannelId);
    }
  };
  window.addEventListener("keydown", handler);
  // Push-to-talk rides its own listeners: keydown first (so the mic opens on
  // the same event the keymap sees) plus the two ways a hold can end without a
  // keyup ever arriving.
  window.addEventListener("keydown", pttDown);
  window.addEventListener("keyup", pttUp);
  window.addEventListener("blur", release);
  document.addEventListener("visibilitychange", release);
  return () => {
    window.removeEventListener("keydown", handler);
    window.removeEventListener("keydown", pttDown);
    window.removeEventListener("keyup", pttUp);
    window.removeEventListener("blur", release);
    document.removeEventListener("visibilitychange", release);
    release();
  };
}

function inputFocused() {
  const el = document.activeElement;
  return el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.isContentEditable);
}
