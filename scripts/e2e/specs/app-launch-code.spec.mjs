// The browser build opens with a one-time launch code (?c=), which the page
// trades for the API token over POST /launch. A request with no Origin is
// refused (one that sets a loopback Origin is not, which is why the rest
// matters). A code redeemed a second time rotates the token and cancels any
// device-link offer: whoever redeemed it first can do nothing more, and the
// page says what happened, what to check and how to get back in.
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

async function rpcWith(peer, token, method, ...args) {
  const r = await fetch(`${peer.url}/rpc`, {
    method: "POST",
    headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
    body: JSON.stringify({ method, args }),
  });
  const j = await r.json();
  if (j.error) throw new Error(`${method}: ${j.error}`);
  return j.result;
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
  const pass = app.page.locator('input[aria-label="Passphrase"]');
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

test("a code taken before the page boots locks the taker out and is explained", async ({ world, assert, step, sleep, log }) => {
  const [alice] = await world.peers(1);
  const code = await mintCode(alice);

  step("something other than the page redeems its code first");
  const taken = await redeem(alice, code, alice.url);
  assert.equal(taken.status, 200, taken.body);
  const first = JSON.parse(taken.body).token;
  assert.equal(await tokenWorks(alice, first), true);
  // What a token held for a moment can leave behind: an open device-link
  // offer, which is served over the network, not over this API.
  const offer = await rpcWith(alice, first, "LinkOffer");

  step("the page finds its code spent and says so");
  const { app, notice } = await openWithCode(alice, code);
  await notice.waitFor({ state: "visible" });
  const said = await notice.innerText();
  assert.match(said, /already used/);
  assert.match(said, /Your devices/);
  assert.ok(said.includes(`curl --unix-socket ${alice.launchSocket} http://launch/code`), `notice gives no fresh-code command:\n${said}`);
  assert.equal(await app.page.evaluate(() => localStorage.getItem("concord.apiToken")), null);
  assert.equal(await tokenWorks(alice, first), false, "the token the first redemption bought still works");

  step("the link offer opened with that token is cancelled");
  const [dev] = await world.peers(1, { names: ["Dev"], start: false });
  dev.passphrase = alice.passphrase;
  await dev.start();
  // A failure to reach alice at all ("open link stream") says nothing; retry
  // until the request reaches her and is answered.
  let refused = null;
  for (let i = 0; i < 6 && !refused; i++) {
    try {
      await dev.rpc("RedeemLinkCode", offer, dev.passphrase);
      assert.fail("a device linked with the offer opened under the rotated token");
    } catch (e) {
      if (e.code === "ERR_ASSERTION") throw e;
      if (/open link stream/.test(e.message)) await sleep(2000);
      else refused = e.message;
    }
  }
  assert.ok(refused, "the new device never reached alice to be refused");
  log(`link refused: ${refused}`);

  step("a fresh code opens the app with the new token");
  const again = await openWithCode(alice, await mintCode(alice));
  assert.equal(await again.notice.count(), 0);
  const fresh = await again.app.page.evaluate(() => localStorage.getItem("concord.apiToken"));
  assert.ok(fresh && fresh !== first, `token after a fresh code: ${fresh}`);
  assert.equal(await tokenWorks(alice, fresh), true);
});
