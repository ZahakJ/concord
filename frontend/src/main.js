import { mount } from "svelte";
import "./app.css";
import App from "./App.svelte";
import { configureTransport } from "./lib/api.js";
import { loadAnimatedEmoji } from "./lib/markdown.js";

// Which emoji have an animated version is needed by the FIRST message that
// renders, and the answer must not arrive later: markup that changes once the
// manifest lands re-renders every message body. Kick it off before mount and
// let it lose the race harmlessly — an emoji that misses it simply stays still.
loadAnimatedEmoji();

// The browser build's bearer token (main_web.go). It arrives once on the URL
// the binary opens, then lives in localStorage so a reload — or a second tab
// the user opens by hand — still authenticates. localStorage is the right
// place precisely because the threat this token exists for is ANOTHER LOCAL
// PROCESS curling 127.0.0.1, which cannot read this origin's storage.
const TOKEN_KEY = "concord.apiToken";
function browserToken() {
  let token = "";
  try {
    const url = new URL(window.location.href);
    token = url.searchParams.get("t") || "";
    if (token) {
      localStorage.setItem(TOKEN_KEY, token);
      // Strip it from the address bar so it stays out of history, bookmarks,
      // and any screenshot of the window.
      url.searchParams.delete("t");
      history.replaceState(null, "", url.pathname + url.search + url.hash);
    } else {
      token = localStorage.getItem(TOKEN_KEY) || "";
    }
  } catch {
    /* file:// or a locked-down storage policy: fall through unauthenticated
       and let the API surface the 401 rather than failing silently here. */
  }
  return token;
}

// On mobile (Capacitor), the Go core runs in-process behind a loopback port
// with a bearer token; the native ConcordCore plugin boots it and hands both
// over. Desktop/browser mounts immediately — the page origin IS the API.
async function boot() {
  const cap = typeof window !== "undefined" ? window.Capacitor : null;
  // Stamp the platform on <html> the instant the bundle runs — BEFORE the native
  // inset bridge pushes anything, and independently of whether it ever does. On
  // Android the app draws edge-to-edge (insetsHandling:"disable") and env(safe-
  // area-inset-*) reports 0, so if the native --sa-* push fails to land — a plugin
  // that didn't register, a document swap the re-push missed, any OEM timing — the
  // top bar renders under the status bar with nothing to catch it. This attribute
  // lets app.css floor the top a fixed amount on Android with no native help at
  // all; the bridge, when it works, still refines the exact value via max(). This
  // is the belt that does not depend on the suspenders.
  if (cap?.getPlatform) {
    try {
      document.documentElement.dataset.platform = cap.getPlatform();
    } catch {
      /* older Capacitor without getPlatform — the native push is the only path */
    }
  }
  if (cap?.Plugins?.ConcordCore) {
    const { port, token } = await cap.Plugins.ConcordCore.start();
    configureTransport({ baseURL: `http://127.0.0.1:${port}`, authToken: token });
  } else {
    // Browser build: same origin, but the RPC surface now wants the token the
    // binary handed us. Empty is tolerated so a dev server still mounts.
    configureTransport({ baseURL: "", authToken: browserToken() });
  }
  return mount(App, { target: document.getElementById("app") });
}

const app = boot();

export default app;
