// The launch-code trade (`npm test` runs it): a live code buys the token; a
// code someone else redeemed first (409) is reported as spent, so the page
// explains itself instead of booting signed out; anything else is neither.
import { tradeLaunchCode, showLaunchSpent, SPENT_TITLE, RESTART_TEXT } from "./launch.js";

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
r = await tradeLaunchCode("c0de", reply(409, { fresh: "curl --unix-socket /run/x.sock http://launch/code" }));
check("a spent code is reported spent", r.spent === true && !r.token);
check("a spent code passes on the fresh-code command", r.fresh === "curl --unix-socket /run/x.sock http://launch/code");
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
const texts = (n) => n.kids.map((k) => k.textContent);
showLaunchSpent(doc);
check("the notice is the only thing in #app", app.kids.length === 1 && app.kids[0].attrs.role === "alert");
check("the notice says what happened", app.kids[0].kids[0]?.textContent === SPENT_TITLE);
check("the notice says what to check", texts(app.kids[0]).some((t) => t.includes("Your devices") && t.includes("cannot undo")));
check("with no launch socket, the way back in is a restart", texts(app.kids[0]).includes(RESTART_TEXT) && !texts(app.kids[0]).some((t) => t.includes("curl")));
doc.location = { origin: "http://127.0.0.1:8787" };
showLaunchSpent(doc, "curl --unix-socket /run/x.sock http://launch/code");
check("with a launch socket, the notice gives the command", texts(app.kids[0]).includes("curl --unix-socket /run/x.sock http://launch/code"));
check("and the address to open", texts(app.kids[0]).some((t) => t.includes("http://127.0.0.1:8787/?c=")));

if (failures) {
  console.error(`launch: ${failures} failure(s)`);
  process.exit(1);
}
console.log("launch: ok");
