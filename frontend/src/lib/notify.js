// notify.js — the single gate for desktop notifications. One place decides
// whether a message deserves attention; everything else just forwards events.
//
// Rules: never notify for your own/system/deleted messages or muted channels;
// notify when the window is hidden, or (for @mentions) merely unfocused; use
// the web Notification API when available (browser build; the Wails WebView
// may lack it), otherwise stay silent — the unread badges still light up.

import { previewText } from "./attachments.js";

export function requestPermission() {
  if (typeof Notification !== "undefined" && Notification.permission === "default") {
    Notification.requestPermission().catch(() => {});
  }
}

export function notify(m, { selfFpr, mention, muted, activeChannel, onClick }) {
  if (m.kind !== "" || m.deleted || m.sender === selfFpr || muted) return;
  if (typeof Notification === "undefined" || Notification.permission !== "granted") return;

  const hidden = document.hidden;
  const unfocused = !document.hasFocus();
  // Plain messages: only when the window is hidden. Mentions: any time the
  // user isn't actively looking (unfocused), even for the current channel.
  if (!(mention ? hidden || unfocused : hidden)) return;

  const title = (mention ? "@ " : "") + (m.senderName || m.sender.slice(0, 9));
  try {
    const n = new Notification(title, { body: previewText(m.content).slice(0, 120), tag: m.channelId });
    n.onclick = () => {
      window.focus();
      onClick?.();
      n.close();
    };
  } catch {
    /* WebView without Notification support */
  }
}
