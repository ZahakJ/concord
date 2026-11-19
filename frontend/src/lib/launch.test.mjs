// The launch-code trade (`npm test` runs it): a live code buys the token; a
// code someone else redeemed first (409) is reported as spent, so the page
// explains itself instead of booting signed out; anything else is neither.
import { tradeLaunchCode, showLaunchSpent, SPENT_TITLE } from "./launch.js";

let failures = 0;
const check = (name, ok) => {
  if (!ok) {
    console.error("  FAIL " + name);
    failures++;
  }
};

const reply = (status, body) => async (url, init) => {
  check("posts to /launch", url === "/launch" && init.method === "POST" && JSON.parse(init.body).code === "c0de");
  return { ok: status === 200, status, json: async () => body };
};

let r = await tradeLaunchCode("c0de", reply(200, { token: "tok" }));
check("a live code buys the token", r.token === "tok" && !r.spent);
r = await tradeLaunchCode("c0de", reply(409));
check("a spent code is reported spent", r.spent === true && !r.token);
r = await tradeLaunchCode("c0de", reply(403));
check("a wrong code is neither", !r.spent && !r.token);

// The notice replaces whatever was in #app, as text.
const made = [];
const el = (tag) => {
  const e = { tag, style: {}, attrs: {}, kids: [], textContent: "" };
  e.setAttribute = (k, v) => (e.attrs[k] = v);
  e.append = (...k) => e.kids.push(...k);
  e.replaceChildren = (...k) => (e.kids = k);
  made.push(e);
  return e;
};
const app = el("div");
app.kids = ["boot"];
const doc = { getElementById: (id) => (id === "app" ? app : null), createElement: el, body: { style: {} } };
showLaunchSpent(doc);
check("the notice is the only thing in #app", app.kids.length === 1 && app.kids[0].attrs.role === "alert");
check("the notice says what happened", app.kids[0].kids[0]?.textContent === SPENT_TITLE);

if (failures) {
  console.error(`launch: ${failures} failure(s)`);
  process.exit(1);
}
console.log("launch: ok");
