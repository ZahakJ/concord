// The browser build's launch-code trade (main.js, internal/httpapi/launch.go).
// The page is opened with ?c=<code>, a single-use code it trades once over
// POST /launch for the API token.
//
// When the page finds its code already redeemed, the server answers 409,
// changes the token and cancels any device-link offer, which cuts whatever
// redeemed it first off from further use. It cannot undo what was done
// before that. The page cannot get in either, and says so plainly rather
// than coming up signed out with no reason given, with what to check and how
// to get back in.

// tradeLaunchCode returns { token } for a live code; { spent: true, fresh }
// when someone redeemed this code before the page did, where fresh is the
// command that prints a new code ("" when the binary has no launch socket);
// and {} otherwise (a wrong or expired code: the caller falls back to the
// token it stored last time).
export async function tradeLaunchCode(code, fetchFn = fetch) {
  const r = await fetchFn("/launch", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ code }),
  });
  if (r.ok) return { token: (await r.json()).token || "" };
  if (r.status === 409) {
    let fresh = "";
    try {
      fresh = (await r.json()).fresh || "";
    } catch {
      /* an older binary answered in plain text */
    }
    return { spent: true, fresh };
  }
  return {};
}

export const SPENT_TITLE = "This window's one-time link was already used";
export const SPENT_TEXT =
  "Another tab or another program on this computer used this window's link " +
  "before the window could. In case that was not you, Concord has changed its " +
  "access key and cancelled any device-link offer that was open, so whatever " +
  "used the link can do nothing more. Every Concord tab that was open is " +
  "signed out too. This cannot undo anything done before the change: check " +
  "Settings → Your devices and remove any device you do not recognise.";
export const RESTART_TEXT = "To get back in, quit Concord and start it again.";
export const freshText = (origin) =>
  `To get back in, run the command below and open ${origin}/?c= followed by ` +
  "the code it prints (or quit Concord and start it again):";

// showLaunchSpent puts the explanation where the app would have mounted.
// Plain DOM and text only: nothing here is worth a framework, and nothing in
// it comes from the network.
export function showLaunchSpent(doc = document, fresh = "") {
  const root = doc.getElementById("app");
  if (!root) return;
  const box = doc.createElement("main");
  box.setAttribute("role", "alert");
  box.style.cssText =
    "max-width:34rem;margin:15vh auto 0;padding:0 16px;font:16px/1.5 system-ui,sans-serif;color:#eceeef";
  const h = doc.createElement("h1");
  h.style.cssText = "font-size:1.25rem;margin:0 0 0.75rem";
  h.textContent = SPENT_TITLE;
  const para = (text) => {
    const p = doc.createElement("p");
    p.style.margin = "0 0 0.75rem";
    p.textContent = text;
    return p;
  };
  const origin = doc.location?.origin || "";
  box.append(h, para(SPENT_TEXT), para(fresh ? freshText(origin) : RESTART_TEXT));
  if (fresh) {
    const pre = doc.createElement("pre");
    pre.style.cssText = "white-space:pre-wrap;word-break:break-all;font:14px/1.4 ui-monospace,monospace;margin:0";
    pre.textContent = fresh;
    box.append(pre);
  }
  doc.body.style.background = "#171a1c";
  root.replaceChildren(box);
}
