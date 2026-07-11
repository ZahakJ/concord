// deeplink.js — concord:// URL handling. The link-device QR encodes
// `concord://link?c=<code>` so the OS camera can open the app directly; the
// in-app scanner and the paste box accept both that URL form and the raw code.
// Reached via the Capacitor runtime global (like biometric.js) so the
// web/desktop bundle carries no Capacitor dependency.

import { S, flash } from "./state.svelte.js";

// linkCodeFrom extracts a link code from scanned/pasted text: a concord://link
// URL or a raw CL1… code. Returns "" when it's neither.
export function linkCodeFrom(text) {
  const t = (text || "").trim();
  if (!t) return "";
  if (/^concord:\/\/link/i.test(t)) {
    try {
      // URL() mangles custom schemes in some WebViews — parse the query by hand.
      const q = t.split("?")[1] || "";
      const m = /(?:^|&)c=([^&]+)/.exec(q);
      if (m) return decodeURIComponent(m[1]);
    } catch {
      /* fall through */
    }
    return "";
  }
  return t;
}

// linkURLFor wraps a raw link code as the deep-link URL the QR encodes.
export function linkURLFor(code) {
  return `concord://link?c=${encodeURIComponent(code)}`;
}

function handleURL(url) {
  const code = /^concord:\/\/link/i.test(url || "") ? linkCodeFrom(url) : "";
  if (!code) return;
  // Stash for the Login screen; it consumes this via $effect. If the user is
  // already logged in there's nothing safe to do automatically (re-linking
  // requires logout) — surface a hint instead.
  if (S.ready) {
    flash("This device is already set up — to re-link it, sign out first, then scan again.");
    return;
  }
  S.pendingLinkCode = code;
}

// initDeepLinks wires the Capacitor App plugin: the cold-start launch URL and
// warm-start appUrlOpen events. No-op outside the mobile shell.
export async function initDeepLinks() {
  const app = window.Capacitor?.Plugins?.App;
  if (!app) return;
  try {
    const launch = await app.getLaunchUrl();
    if (launch?.url) handleURL(launch.url);
  } catch {
    /* plugin without getLaunchUrl — fine */
  }
  app.addListener("appUrlOpen", (ev) => handleURL(ev?.url));
}
