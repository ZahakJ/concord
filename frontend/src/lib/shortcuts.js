// shortcuts.js — one global keymap, and the list of what is in it.
//
// The list used to be the comment block that stood here, and the cheat sheet
// (modals/ModalShortcuts.svelte) was a third, separately maintained copy. It
// had drifted: nine bindings the app really answers were missing from it,
// including `?`, so the sheet never taught how to reopen itself. SHORTCUTS
// below is what the sheet renders, it lives against the handler it describes,
// and shortcutlist.test.mjs fails the build when the handler grows a key the
// list has not heard of.
import { S, activeGuild, selectChannel, selectGuild, jumpToChannel, markRead, markAllRead, toggleMemberPanel, isMuted, setAppearance, flash, toggleMicMute, toggleDeafen } from "./state.svelte.js";
import { popLayer } from "./navstack.svelte.js";
import { pressesBind, releasesBind, typesCharacter } from "./keybind.js";

// ---- the registry ----
//
// One entry per thing a person can press or type. `chords` is a list of
// ALTERNATIVES; the parts inside one chord are pressed together. Keeping those
// two apart is not pedantry — the old sheet joined everything with "+", so
// "/shrug + /me + /spoiler" and "Alt + ↑/↓" were printed the same way while
// meaning opposite things. `typed` marks the entries you write rather than
// press: markdown and slash commands, the only groups a phone can use, which
// is why they are the only ones it shows.
export const SHORTCUTS = [
  { group: "Navigation", chords: [["Ctrl/⌘", "K"]], label: "Command palette (jump anywhere, run actions)" },
  { group: "Navigation", chords: [["Ctrl/⌘", "F"]], label: "Search messages" },
  { group: "Navigation", chords: [["Ctrl/⌘", ","]], label: "User settings" },
  { group: "Navigation", chords: [["Ctrl/⌘", "Shift", ","]], label: "Network stats" },
  { group: "Navigation", chords: [["Alt", "↑"], ["Alt", "↓"]], label: "Previous / next channel" },
  { group: "Navigation", chords: [["Alt", "Shift", "↑"], ["Alt", "Shift", "↓"]], label: "Previous / next unread channel" },
  { group: "Navigation", chords: [["Ctrl", "Alt", "↑"], ["Ctrl", "Alt", "↓"]], label: "Previous / next guild" },
  { group: "Navigation", chords: [["Ctrl/⌘", "U"]], label: "Show or hide the member panel" },
  { group: "Navigation", chords: [["Ctrl/⌘", "="], ["Ctrl/⌘", "-"], ["Ctrl/⌘", "0"]], label: "Zoom the whole UI in / out / back to 100%" },
  { group: "Navigation", chords: [["?"], ["Ctrl/⌘", "/"]], label: "This list" },

  { group: "Reading", chords: [["Esc"]], label: "Close what's open — or mark this channel read" },
  { group: "Reading", chords: [["Shift", "Esc"]], label: "Mark all channels read" },

  { group: "Voice", chords: [["Ctrl", "Shift", "M"]], label: "Toggle mute (while in a call)" },
  { group: "Voice", chords: [["Ctrl", "Shift", "D"]], label: "Toggle deafen (while in a call)" },
  { group: "Voice", chords: [["your own key"]], label: "Hold to talk, when push-to-talk is on (choose it in Voice settings)" },

  { group: "Composer", chords: [["Enter"]], label: "Send message" },
  { group: "Composer", chords: [["Shift", "Enter"]], label: "New line" },
  { group: "Composer", chords: [["↑"]], label: "Edit your last message (empty composer)" },
  { group: "Composer", chords: [["Ctrl/⌘", "B"]], label: "Bold the selection" },
  { group: "Composer", chords: [["Ctrl/⌘", "I"]], label: "Italicise the selection" },
  { group: "Composer", chords: [["Ctrl/⌘", "E"]], label: "Code the selection" },
  { group: "Composer", chords: [["Ctrl/⌘", "Shift", "K"]], label: "Link the selection" },
  { group: "Composer", chords: [["Ctrl/⌘", "Shift", "X"]], label: "Spoiler the selection" },
  { group: "Composer", chords: [["Ctrl/⌘", "Shift", "."]], label: "Quote the selection" },
  { group: "Composer", chords: [["Ctrl/⌘", "Shift", "L"]], label: "Cycle the draft's direction: per-line, right-to-left, left-to-right" },

  { group: "Slash commands", typed: true, chords: [["/shrug"], ["/me"], ["/spoiler"], ["…"]], label: "Type one at the start of a message" },

  { group: "Formatting", typed: true, chords: [["**text**"]], label: "Bold" },
  { group: "Formatting", typed: true, chords: [["*text*"]], label: "Italic" },
  { group: "Formatting", typed: true, chords: [["__text__"]], label: "Underline" },
  { group: "Formatting", typed: true, chords: [["~~text~~"]], label: "Strikethrough" },
  { group: "Formatting", typed: true, chords: [["||text||"]], label: "Spoiler" },
  { group: "Formatting", typed: true, chords: [["`code`"], ["```block```"]], label: "Code" },
  { group: "Formatting", typed: true, chords: [["> "], [">>> "]], label: "Quote (line / rest of message)" },
  { group: "Formatting", typed: true, chords: [["# "], ["## "], ["### "]], label: "Headers" },
  { group: "Formatting", typed: true, chords: [["[text](url)"]], label: "Masked link" },
];

// Group order for the sheet, taken from the registry so a new group appears
// without anybody editing a second list.
export const SHORTCUT_GROUPS = [...new Set(SHORTCUTS.map((s) => s.group))];

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
  const guilds = S.guilds.filter((g) => g.kind !== "dm");
  if (!guilds.length) return;
  const i = guilds.findIndex((g) => g.id === S.activeGuildId);
  const from = i < 0 ? (dir > 0 ? -1 : 0) : i;
  selectGuild(guilds[(from + dir + guilds.length) % guilds.length].id);
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

// The chords that keep working while a text field has focus: the ones a caret,
// a selection or a character has no claim on. Everything else in the keymap
// belongs to the field for as long as it holds focus.
function globalWhileTyping(e, mod) {
  // Escape's own branch already decides what it means with focus in a field.
  if (e.key === "Escape") return true;
  // Ctrl+Shift+M / D — mic and deafen, mid-call, mid-sentence: exactly when
  // you need them.
  if (mod && e.shiftKey && (e.key === "m" || e.key === "M" || e.key === "d" || e.key === "D")) return true;
  // Ctrl+, and Ctrl+Shift+, — settings and stats.
  if (mod && (e.key === "," || e.key === "<")) return true;
  // Ctrl+= / - / 0 — UI zoom.
  if (mod && !e.altKey && (e.key === "=" || e.key === "+" || e.key === "-" || e.key === "0")) return true;
  // Ctrl+/ — the cheat sheet. (Bare "?" is a character and stays in the field.)
  if (mod && e.key === "/") return true;
  return false;
}

export function installShortcuts() {
  const handler = (e) => {
    const mod = e.ctrlKey || e.metaKey;
    // A focused text field owns its keys first.
    //
    // Every binding here listens on `window`, which sees a keystroke whether or
    // not a field has focus — and nothing below used to check. So Ctrl+K in the
    // composer inserted a markdown link AND opened the quick switcher on one
    // press; Ctrl+U, which a Unix text field reads as "delete to the start of
    // the line", also toggled the member panel; and Alt+↑/↓, the caret's
    // paragraph jump, switched channels out from under a half-written message
    // and took the draft's focus with it.
    if (inputFocused() && !globalWhileTyping(e, mod)) return;
    // Ctrl/Cmd+K — the quick switcher. Refused while a dialog is up: the
    // switcher and the modal overlay were peers in the stacking order, so it
    // opened UNDER the dialog — an invisible thing holding the keyboard, and an
    // Escape that closed whichever of the two the browser reached first.
    if (mod && e.key.toLowerCase() === "k") {
      if (S.modal) return;
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
    // Ctrl/Cmd+, — user settings. !e.shiftKey because a layout that keeps
    // reporting "," with Shift held (anything but a US keyboard, in practice)
    // sent Ctrl+Shift+, here first and opened Settings instead of the stats
    // panel it is bound to.
    if (mod && !e.shiftKey && e.key === ",") {
      e.preventDefault();
      S.modal = S.modal?.kind === "settings" ? null : { kind: "settings" };
      return;
    }
    // Ctrl+Shift+, — network stats. Same base key as settings with Shift, so
    // the two live next to each other in muscle memory: settings is the door,
    // stats is the diagnostics behind it.
    if (mod && e.shiftKey && (e.key === "," || e.key === "<")) {
      e.preventDefault();
      S.modal = S.modal?.kind === "stats" ? null : { kind: "stats" };
      return;
    }
    // Ctrl+Shift+M / Ctrl+Shift+D — mute and deafen (only meaningful in a
    // call). These call the SAME functions the buttons call. They used to carry
    // their own copy, which had quietly lost publishVoiceState (so the room
    // never learned you had gone deaf) and the mute-restore (so two presses of
    // a toggle left you muted). See lib/state.svelte.js.
    if (mod && e.shiftKey && e.key.toLowerCase() === "m") {
      if (S.voice) {
        e.preventDefault();
        toggleMicMute();
      }
      return;
    }
    if (mod && e.shiftKey && e.key.toLowerCase() === "d") {
      if (S.voice) {
        e.preventDefault();
        toggleDeafen();
      }
      return;
    }
    // Ctrl/Cmd+U — toggle the member panel.
    if (mod && !e.shiftKey && e.key.toLowerCase() === "u") {
      e.preventDefault();
      toggleMemberPanel();
      return;
    }
    // Ctrl+Alt+↑/↓ — previous / next guild (distinct from plain Alt = channel).
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
    // Ctrl+= / Ctrl+- / Ctrl+0 — UI zoom. The desktop webview has no browser
    // chrome, so the app must supply the zoom keys everyone's hands know.
    if (mod && !e.altKey && (e.key === "=" || e.key === "+" || e.key === "-" || e.key === "0")) {
      e.preventDefault();
      const cur = Number(S.prefs.uiScale) || 1;
      const next =
        e.key === "0"
          ? 1
          : Math.min(1.5, Math.max(0.8, Math.round((cur + (e.key === "-" ? -0.1 : 0.1)) * 10) / 10));
      if (next !== cur) {
        setAppearance("uiScale", next);
        flash(`Zoom ${Math.round(next * 100)}%`, "info");
      }
      return;
    }
    // Cheat sheet: ? (Shift+/) or Ctrl+/.
    if ((mod && e.key === "/") || (e.key === "?" && !inputFocused())) {
      e.preventDefault();
      S.modal = S.modal?.kind === "shortcuts" ? null : { kind: "shortcuts" };
      return;
    }
    // Escape: take away the top layer, whatever it is.
    //
    // This used to be a second ladder, listing the same panels the Android back
    // handler listed, in the same fixed order — and Modal.svelte kept a third
    // listener of its own on top of that. Three places had to agree about what
    // was open and about which of two things opened at once should go first,
    // and they drifted: a modal short-circuited the whole ladder, so a picker
    // raised from inside one could not be closed without closing the dialog
    // under it. One stack, popped from the top, cannot drift.
    if (e.key === "Escape") {
      // A dismissal has to work with the caret in a text field — that is where
      // it is needed most (a sheet is up, the keyboard is up, the search box
      // has focus). Only the fallbacks below defer to the field.
      if (popLayer()) {
        e.preventDefault();
        return;
      }
      if (inputFocused()) return;
      if (e.shiftKey) markAllRead();
      // Nothing to dismiss → mark the current channel read.
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
