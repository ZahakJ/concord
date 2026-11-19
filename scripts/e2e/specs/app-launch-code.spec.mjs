// The browser build opens with a one-time launch code (?c=), which the page
// trades for the API token over POST /launch. A request without the page's
// Origin is refused, and a code redeemed a second time rotates the token:
// whoever redeemed it first is locked out, and the page says what happened
// instead of coming up signed out with no reason given.
import http from "node:http";
import { test } from "../harness.mjs";

// A fresh code from the peer's launch socket, as a launcher would ask for it.
function mintCode(peer) {
  return new Promise((resolve, reject) => {
    http
      .get({ socketPath: peer.launchSocket, path: "/code", headers: { Host: "launch" } }, (res) => {
        let body = "";
        res.on("data", (d) => (body += d));
        res.on("end", () => (res.statusCode === 200 && body.trim() ? resolve(body.trim()) : reject(new Error(`launch socket: ${res.statusCode} ${body}`))));
      })
      .on("error", reject);
  });
}

// POST /launch from outside the browser, with or without an Origin header.
function redeem(peer, code, origin) {
  return new Promise((resolve, reject) => {
    const headers = { "Content-Type": "application/json" };
    if (origin) headers.Origin = origin;
    const req = http.request({ host: "127.0.0.1", port: peer.port, path: "/launch", method: "POST", headers }, (res) => {
      let body = "";
      res.on("data", (d) => (body += d));
      res.on("end", () => resolve({ status: res.statusCode, body }));
    });
    req.on("error", reject);
    req.end(JSON.stringify({ code }));
  });
}

async function tokenWorks(peer, token) {
  const r = await fetch(`${peer.url}/rpc`, {
    method: "POST",
    headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
    body: JSON.stringify({ method: "Identity", args: [] }),
  });
  return r.status !== 401;
}

// Opens the page with a code and waits until it is either the app (the
// passphrase box or the shell) or the notice that the code was spent.
async function openWithCode(peer, code) {
  const app = await peer.open({ unlock: false });
  await app.page.goto(`${peer.url}/?c=${code}`, { waitUntil: "domcontentloaded" });
  const pass = app.page.locator('input[placeholder="Passphrase"]');
  const shell = app.page.locator('nav[aria-label="Guilds"], nav[aria-label="Sections"]').first();
  const notice = app.page.locator('main[role="alert"]');
  await pass.or(shell).or(notice).first().waitFor({ state: "attached" });
  return { app, notice };
}

test("the page trades its launch code once; a replay rotates the token", async ({ world, assert, step }) => {
  const [alice] = await world.peers(1);
  const code = await mintCode(alice);

  step("a request with no Origin is refused and spends nothing");
  const bare = await redeem(alice, code, "");
  assert.equal(bare.status, 403, bare.body);
  assert.ok(!bare.body.includes(alice.token));

  step("the page redeems the code and keeps the token");
  const { app, notice } = await openWithCode(alice, code);
  assert.equal(await notice.count(), 0, "the page was told its code was spent");
  assert.equal(await app.page.evaluate(() => localStorage.getItem("concord.apiToken")), alice.token);
  assert.ok(!app.page.url().includes("c="), `code left in the address bar: ${app.page.url()}`);

  step("a replay of the spent code is told apart and changes the token");
  const replay = await redeem(alice, code, alice.url);
  assert.equal(replay.status, 409, replay.body);
  assert.ok(!replay.body.includes(alice.token));
  assert.equal(await tokenWorks(alice, alice.token), false, "the old token still works after the replay");
});

test("a code taken before the page boots locks the taker out and is explained", async ({ world, assert, step }) => {
  const [alice] = await world.peers(1);
  const code = await mintCode(alice);

  step("something other than the page redeems its code first");
  const taken = await redeem(alice, code, alice.url);
  assert.equal(taken.status, 200, taken.body);
  const first = JSON.parse(taken.body).token;
  assert.equal(await tokenWorks(alice, first), true);

  step("the page finds its code spent and says so");
  const { app, notice } = await openWithCode(alice, code);
  await notice.waitFor({ state: "visible" });
  assert.match(await notice.innerText(), /already used/);
  assert.equal(await app.page.evaluate(() => localStorage.getItem("concord.apiToken")), null);
  assert.equal(await tokenWorks(alice, first), false, "the token the first redemption bought still works");

  step("a fresh code opens the app with the new token");
  const again = await openWithCode(alice, await mintCode(alice));
  assert.equal(await again.notice.count(), 0);
  const fresh = await again.app.page.evaluate(() => localStorage.getItem("concord.apiToken"));
  assert.ok(fresh && fresh !== first, `token after a fresh code: ${fresh}`);
  assert.equal(await tokenWorks(alice, fresh), true);
});
