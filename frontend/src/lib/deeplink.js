// deeplink.js — concord:// URL handling. Two shapes:
//   concord://link?c=<code>     device linking; the QR encodes this so the OS
//                               camera can open the app directly, and the
//                               in-app scanner/paste box accept it or the raw code
//   concord://channel?id=<id>   open a conversation; this is what a message
//                               notification's tap carries
// Reached via the Capacitor runtime global (like biometric.js) so the
// web/desktop bundle carries no Capacitor dependency.

import { S, jumpToChannel } from "./state.svelte.js";
import { api } from "./api.js";

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

// A concord://channel link that arrived before the session was up. App.svelte
// drains this once start() finishes.
let pendingChannel = "";
export function consumePendingChannel() {
  const c = pendingChannel;
  pendingChannel = "";
  return c;
}

// queryParam pulls one key out of a custom-scheme URL's query string. URL()
// mangles non-http schemes in some WebViews, so parse by hand.
function queryParam(url, key) {
  const q = (url || "").split("?")[1] || "";
  const m = new RegExp(`(?:^|&)${key}=([^&]*)`).exec(q);
  try {
    return m ? decodeURIComponent(m[1]) : "";
  } catch {
    return "";
  }
}

function handleURL(url) {
  const u = url || "";

  // A tapped message notification. Landing the user in the conversation they
  // were just told about is the whole point of the notification; without this
  // the tap dropped them wherever they last were.
  if (/^concord:\/\/channel/i.test(u)) {
    const id = queryParam(u, "id");
    // Arrives during cold start too, before the node has loaded the guild list —
    // jumpToChannel can only work once we're ready, so hold it for App.svelte.
    if (id) {
      if (S.ready) jumpToChannel(id);
      else pendingChannel = id;
    }
    return;
  }

  const code = /^concord:\/\/link/i.test(u) ? linkCodeFrom(u) : "";
  if (!code) return;
  // Stash for the Login screen; it consumes this via $effect. Already signed in
  // means re-linking would have to sign out first — which is a decision, so ask
  // instead of firing a toast that vanishes and throwing the code away (the
  // user would have to go back to the other device and generate a fresh one).
  if (S.ready) {
    S.modal = {
      kind: "confirm",
      title: "Link this device to another account?",
      body: "This device is already set up. Linking signs it out first — messages stay on the devices that already have them.",
      confirmLabel: "Sign out and link",
      onConfirm: async () => {
        S.modal = null;
        await api.logout();
        // The code survives the reload in sessionStorage; Login picks it up.
        try {
          sessionStorage.setItem("concord.pendingLink", code);
        } catch {
          /* private mode — the user can still paste the code by hand */
        }
        location.reload();
      },
    };
    return;
  }
  S.pendingLinkCode = code;
}

// Restore a link code stashed across the sign-out reload above.
function resumeStashedLink() {
  try {
    const code = sessionStorage.getItem("concord.pendingLink");
    if (!code) return;
    sessionStorage.removeItem("concord.pendingLink");
    if (!S.ready) S.pendingLinkCode = code;
  } catch {
    /* no sessionStorage — nothing to resume */
  }
}

// initDeepLinks wires the Capacitor App plugin: the cold-start launch URL and
// warm-start appUrlOpen events. The stashed-link resume runs everywhere,
// including the desktop shell, since the sign-out reload is the same there.
export async function initDeepLinks() {
  resumeStashedLink();
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
