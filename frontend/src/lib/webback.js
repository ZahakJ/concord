// webback.js — the browser's Back button, in the web build.
//
// The app is one page with no history entries of its own, so Back (the
// toolbar arrow, a mouse's back button, Alt+←, a phone browser's back
// gesture) left it entirely: the next page was whatever came before, and
// coming back reloaded the app and asked for the passphrase again. One habitual
// swipe on a phone dropped the session.
//
// The Android shell already answers its hardware back by walking the app's
// own stack (App.svelte handleBack): the top layer, then out of a forum post,
// then out of a conversation to the channel list. The web build now does the
// same, by keeping ONE spare history entry above the page: Back consumes it,
// popstate fires, the app takes its step, and if it took one it puts the spare
// entry back. When there is nothing left to step out of, it does not, and the
// next Back leaves the page as a browser user expects.
//
// Not installed inside the Android shell (it has its own back, and an extra
// history entry would make the WebView claim the press) or the desktop shell
// (no browser chrome, and its webview's history is not ours to change).

// The places Back walks out of once no layer is open, in order. Pure, so the
// order can be tested: `s` describes the app right now.
//   s.inPost      a forum post is open over its board
//   s.phoneChat   a phone showing a conversation (not the channel list)
//   s.prevChannel the conversation visited before this one, if any
export function backStep(s) {
  if (s.inPost) return "closePost";
  if (s.phoneChat) return "toList";
  if (s.prevChannel) return "prevChannel";
  return "";
}

// trail — the conversations visited in this session, newest last, for "back
// to the previous channel". `visit` ignores a repeat of the current one;
// `pop` drops the current and returns the one before it.
export function makeTrail(cap = 50) {
  const ids = [];
  return {
    visit(id) {
      if (!id || ids[ids.length - 1] === id) return;
      ids.push(id);
      if (ids.length > cap) ids.shift();
    },
    previous() {
      return ids.length > 1 ? ids[ids.length - 2] : "";
    },
    pop() {
      ids.pop();
      return ids[ids.length - 1] || "";
    },
  };
}

const GUARD = { concordBack: true };

// installWebBack — `step()` performs one step back and reports whether it did.
export function installWebBack(step) {
  if (typeof window === "undefined" || !window.history?.pushState) return () => {};
  if (!history.state?.concordBack) history.pushState(GUARD, "");
  const onPop = () => {
    let took = false;
    try {
      took = !!step();
    } catch {
      took = false;
    }
    if (took) history.pushState(GUARD, "");
  };
  window.addEventListener("popstate", onPop);
  return () => window.removeEventListener("popstate", onPop);
}
