// notify.js — the last gate before an OS notification: is the user actually
// looking? WHETHER this message deserves attention at all is decided upstream
// by the channel's notification level (lib/notifs.js); by the time it gets here
// that question is settled and only "would they see it anyway" is left.
//
// Rules: never notify for your own/system/deleted messages;
// notify when the window is hidden, or (for @mentions) merely unfocused. On
// desktop/browser we use the web Notification API; on mobile (Capacitor) the
// Android WebView doesn't surface that to the tray, so we hand off to our native
// ConcordCorePlugin.postNotification instead. Either way, if nothing's available
// we stay silent — the unread badges still light up.

import { plainSnippet } from "./snippet.js";

// The native notification bridge (Android/iOS), or null on web/desktop.
function nativeNotifier() {
  const cap = typeof window !== "undefined" ? window.Capacitor : null;
  const platform = cap?.getPlatform?.();
  if (platform !== "android" && platform !== "ios") return null;
  return cap.Plugins?.ConcordCore || null;
}

// asksLazily reports whether this platform must EARN the permission dialog
// rather than simply opening it.
//
// Android 13 gives an app two chances. Two dismissals and POST_NOTIFICATIONS is
// hard-denied forever, with no route back except the user finding this app in
// system Settings — which nobody does, because nobody knows anything is wrong.
// So on a phone the dialog is a resource with exactly two uses, and spending
// one on a person who has been in the app for four seconds and has never
// received a message is spending it on a reflex "no".
//
// Desktop and the browser have no such trap: a dismissed prompt there can be
// re-opened from the site controls, and the request has always been made at
// login. Nothing changes for them.
export function asksLazily() {
  return !!nativeNotifier();
}

// wantsPermissionPrompt: is there a grant to be had, and would asking now be
// the first time? Used to decide whether to show the in-app rationale — never
// to decide whether to notify.
export async function wantsPermissionPrompt() {
  if (!asksLazily()) return false;
  const { enabled, canRequest } = await notificationStatus();
  return !enabled && canRequest;
}

// requestPermission asks for the OS notification grant. On Android this used to
// happen in MainActivity.onCreate — a system dialog on top of the splash, before
// the user had seen what Concord is; then from start(), which is barely later.
// Both spent one of the two chances above on a moment where the app has nothing
// to point at. It is now called only from a place where the user has just been
// told what the permission is for: the rationale bar's Enable button, or the
// notification settings panel they opened themselves.
//
// Resolves with the status AFTER the user has answered, which on Android means
// after the system dialog closes — the native plugin holds the call open across
// the permission callback. Callers that show the answer must await it: the
// alternative is a timer racing a human reading a dialog, and the human wins
// often enough that the settings row sat there saying "Off" over a permission
// that had just been granted.
export async function requestPermission() {
  const native = nativeNotifier();
  if (native?.requestNotifications) {
    try {
      const r = await native.requestNotifications();
      return { enabled: !!r?.enabled, canRequest: !!r?.canRequest };
    } catch {
      return notificationStatus();
    }
  }
  if (typeof Notification !== "undefined" && Notification.permission === "default") {
    try {
      await Notification.requestPermission();
    } catch {
      /* a browser that refuses to be asked */
    }
  }
  return notificationStatus();
}

// notificationStatus reports what the OS currently allows, so settings can say
// "blocked — open system settings" instead of showing a toggle that does
// nothing. {enabled, canRequest}: canRequest false with enabled false means the
// dialog will never appear again and Settings is the only route.
export async function notificationStatus() {
  const native = nativeNotifier();
  if (native?.notificationStatus) {
    try {
      const r = await native.notificationStatus();
      return { enabled: !!r?.enabled, canRequest: !!r?.canRequest };
    } catch {
      return { enabled: false, canRequest: false };
    }
  }
  if (typeof Notification === "undefined") return { enabled: false, canRequest: false };
  return {
    enabled: Notification.permission === "granted",
    canRequest: Notification.permission === "default",
  };
}

// openSystemSettings takes the user to this app's OS settings page — the only
// recovery from a hard deny. Resolves false where there's nowhere to go.
export async function openSystemSettings() {
  const native = nativeNotifier();
  if (!native?.openAppSettings) return false;
  try {
    await native.openAppSettings();
    return true;
  } catch {
    return false;
  }
}

export function notify(m, { selfFpr, mention, onClick }) {
  if (m.kind !== "" || m.deleted || m.sender === selfFpr) return;

  const hidden = document.hidden;
  const unfocused = !document.hasFocus();
  // Plain messages: only when the window is hidden. Mentions: any time the user
  // isn't actively looking (unfocused), even for the current channel. (When the
  // mobile app is backgrounded the WebView reports document.hidden, so the same
  // gate covers "the phone's screen is elsewhere".)
  if (!(mention ? hidden || unfocused : hidden)) return;

  const title = (mention ? "@ " : "") + (m.senderName || m.sender.slice(0, 9));
  // plainSnippet, not previewText: this string is read out by the OS and
  // there is no second chance to render it. previewText only knows about
  // attachments, so a disappearing timer, a send effect, a sealed timestamp, a
  // game, a doodle or a fenced code block all arrived as their source.
  const body = plainSnippet(m.content, 120);

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
