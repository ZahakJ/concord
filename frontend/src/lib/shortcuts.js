// shortcuts.js — one global keymap. Registered once by App.svelte after login.
//
//   Ctrl/Cmd+K        quick switcher
//   Alt+↑ / Alt+↓     previous / next channel in the active guild
//   Alt+Shift+↑/↓     previous / next unread channel (across servers)
//   Escape            close switcher / pins / search / reply — or, if nothing
//                     is open, mark the current channel read
//   Shift+Escape      mark ALL channels read
//   ? or Ctrl+/       keyboard-shortcut cheat sheet
import { S, activeGuild, selectChannel, jumpToChannel, markRead, markAllRead } from "./state.svelte.js";

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

function stepUnread(dir) {
  const flat = [];
  for (const g of S.guilds) for (const c of g.channels) flat.push(c);
  if (!flat.length) return;
  const unreadIdx = flat
    .map((c, i) => ({ c, i }))
    .filter(({ c }) => S.unread[c.id] && !S.mutes[c.id]);
  if (!unreadIdx.length) return;
  const cur = flat.findIndex((c) => c.id === S.activeChannelId);
  const target =
    dir > 0
      ? unreadIdx.find(({ i }) => i > cur) || unreadIdx[0]
      : [...unreadIdx].reverse().find(({ i }) => i < cur) || unreadIdx[unreadIdx.length - 1];
  jumpToChannel(target.c.id);
}

export function installShortcuts() {
  const handler = (e) => {
    const mod = e.ctrlKey || e.metaKey;
    if (mod && e.key.toLowerCase() === "k") {
      e.preventDefault();
      S.quickSwitcher = !S.quickSwitcher;
      return;
    }
    if (e.altKey && (e.key === "ArrowUp" || e.key === "ArrowDown")) {
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
      else if (S.searchResults !== null) {
        S.searchResults = null;
        S.searchQuery = "";
      } else if (S.replyingTo) S.replyingTo = null;
      else if (S.modal) S.modal = null;
      // Nothing to dismiss → mark the current channel read (Discord-style).
      else if (S.activeChannelId) markRead(S.activeChannelId);
    }
  };
  window.addEventListener("keydown", handler);
  return () => window.removeEventListener("keydown", handler);
}

function inputFocused() {
  const el = document.activeElement;
  return el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.isContentEditable);
}
