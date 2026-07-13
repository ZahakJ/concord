// notify.js — the single gate for OS notifications. One place decides whether a
// message deserves attention; everything else just forwards events.
//
// Rules: never notify for your own/system/deleted messages or muted channels;
// notify when the window is hidden, or (for @mentions) merely unfocused. On
// desktop/browser we use the web Notification API; on mobile (Capacitor) the
// Android WebView doesn't surface that to the tray, so we hand off to our native
// ConcordCorePlugin.postNotification instead. Either way, if nothing's available
// we stay silent — the unread badges still light up.

import { previewText } from "./attachments.js";

// The native notification bridge (Android/iOS), or null on web/desktop.
function nativeNotifier() {
  const cap = typeof window !== "undefined" ? window.Capacitor : null;
  const platform = cap?.getPlatform?.();
  if (platform !== "android" && platform !== "ios") return null;
  return cap.Plugins?.ConcordCore || null;
}

export function requestPermission() {
  // Native: MainActivity requests POST_NOTIFICATIONS at launch — nothing to do.
  if (nativeNotifier()) return;
  if (typeof Notification !== "undefined" && Notification.permission === "default") {
    Notification.requestPermission().catch(() => {});
  }
}

export function notify(m, { selfFpr, mention, muted, activeChannel, onClick }) {
  if (m.kind !== "" || m.deleted || m.sender === selfFpr || muted) return;

  const hidden = document.hidden;
  const unfocused = !document.hasFocus();
  // Plain messages: only when the window is hidden. Mentions: any time the user
  // isn't actively looking (unfocused), even for the current channel. (When the
  // mobile app is backgrounded the WebView reports document.hidden, so the same
  // gate covers "the phone's screen is elsewhere".)
  if (!(mention ? hidden || unfocused : hidden)) return;

  const title = (mention ? "@ " : "") + (m.senderName || m.sender.slice(0, 9));
  const body = previewText(m.content).slice(0, 120);

  // Mobile: post to the OS tray through the native plugin. Tapping opens the
  // app (deep-link to the channel is a later refinement).
  const native = nativeNotifier();
  if (native?.postNotification) {
    native.postNotification({ title, body, tag: m.channelId }).catch(() => {});
    return;
  }

  // Desktop / browser: the web Notification API.
  if (typeof Notification === "undefined" || Notification.permission !== "granted") return;
  try {
    const n = new Notification(title, { body, tag: m.channelId });
    n.onclick = () => {
      window.focus();
      onClick?.();
      n.close();
    };
  } catch {
    /* WebView without Notification support */
  }
}
