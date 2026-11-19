// The browser build's launch-code trade (main.js, internal/httpapi/launch.go).
// The page is opened with ?c=<code>, a single-use code it trades once over
// POST /launch for the API token.
//
// When the page finds its code already redeemed, the server answers 409 and
// changes the token, so whatever redeemed it first is locked out. The page
// cannot get in either, and says so plainly rather than coming up signed out
// with no reason given.

// tradeLaunchCode returns { token } for a live code, { spent: true } when
// someone redeemed this code before the page did, and {} otherwise (a wrong
// or expired code: the caller falls back to the token it stored last time).
export async function tradeLaunchCode(code, fetchFn = fetch) {
  const r = await fetchFn("/launch", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ code }),
  });
  if (r.ok) return { token: (await r.json()).token || "" };
  if (r.status === 409) return { spent: true };
  return {};
}

export const SPENT_TITLE = "This window's one-time link was already used";
export const SPENT_TEXT =
  "Another tab or another program on this computer opened Concord with this " +
  "window's link before the window could. In case that was not you, Concord " +
  "has changed its access key: whatever used the link is locked out, and so " +
  "is every Concord tab that was open. Quit Concord and start it again to " +
  "open a fresh window.";

// showLaunchSpent puts the explanation where the app would have mounted.
// Plain DOM and text only: nothing here is worth a framework, and nothing in
// it comes from the network.
export function showLaunchSpent(doc = document) {
  const root = doc.getElementById("app");
  if (!root) return;
  const box = doc.createElement("main");
  box.setAttribute("role", "alert");
  box.style.cssText =
    "max-width:34rem;margin:15vh auto 0;padding:0 16px;font:16px/1.5 system-ui,sans-serif;color:#eceeef";
  const h = doc.createElement("h1");
  h.style.cssText = "font-size:1.25rem;margin:0 0 0.75rem";
  h.textContent = SPENT_TITLE;
  const p = doc.createElement("p");
  p.style.margin = "0";
  p.textContent = SPENT_TEXT;
  box.append(h, p);
  doc.body.style.background = "#171a1c";
  root.replaceChildren(box);
}
