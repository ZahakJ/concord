// Coming back to the page after the OS put it away.
//
// iOS freezes a Safari tab, or a home-screen web app, the moment it leaves the
// screen, and may keep it frozen for hours. Two things went wrong on the way
// back:
//
//  - The event stream was dead and did not know it. The socket under the
//    EventSource goes with the freeze, usually without an `error`, so the page
//    came back saying OPEN and never heard another message: the chat stayed as
//    it was when you left until you reloaded. On return, a page that was away
//    for more than a moment, or that the browser restored from its back/forward
//    cache, always dials a fresh stream, and the fresh stream's `open` fetches
//    what was said meanwhile (onStreamReopen in lib/state.svelte.js).
//
//  - The app was gone. A page whose render died while it was away (a crash in
//    a component, a boot that never finished) is an empty #app on the app's
//    dark background: an all-black screen with nothing to tap. That page is
//    reloaded, which at worst asks for the passphrase.

// Shorter than this away is a flick between tabs, not a freeze: the stream is
// almost certainly fine, and redialling on every glance costs a round trip.
export const AWAY_MS = 3000;

export function appRendered(root = document.getElementById("app")) {
  return !!root && root.childElementCount > 0;
}

// installResume wires the page lifecycle. `reopen` redials the event stream
// (api.js reopenEvents); the rest are seams for tests.
export function installResume({
  reopen,
  doc = document,
  win = window,
  now = () => Date.now(),
  reload = () => win.location.reload(),
  rendered = () => appRendered(doc.getElementById("app")),
} = {}) {
  let hiddenAt = doc.hidden ? now() : 0;
  const back = (froze) => {
    if (!rendered()) {
      reload();
      return;
    }
    if (froze) reopen();
  };
  const onVis = () => {
    if (doc.hidden) {
      hiddenAt = now();
      return;
    }
    const away = hiddenAt ? now() - hiddenAt : 0;
    hiddenAt = 0;
    back(away >= AWAY_MS);
  };
  // persisted: restored from the back/forward cache, where every connection
  // the page had was closed under it.
  const onShow = (e) => e.persisted && back(true);
  doc.addEventListener("visibilitychange", onVis);
  win.addEventListener("pageshow", onShow);
  return () => {
    doc.removeEventListener("visibilitychange", onVis);
    win.removeEventListener("pageshow", onShow);
  };
}
