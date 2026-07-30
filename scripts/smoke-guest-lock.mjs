// Browser end-to-end for the LOCKED guest meeting (office hours): a real member
// in one browser context, real guests (no account, just the link) in others, all
// through the real rendezvous gateway. Run it via scripts/smoke-guest-lock.sh,
// which builds and launches everything and feeds this driver its environment.
//
// The interesting assertions are made from OUTSIDE the page: every line the
// guest's WebSocket receives is recorded by a hook installed before any page
// script runs, so "a knocking guest sees nothing" is checked against the wire,
// not against whatever the page chose to render.
import fs from "node:fs";
import path from "node:path";

const need = (name) => {
  const v = process.env[name];
  if (!v) {
    console.error(`FAIL: missing env ${name} (run this via scripts/smoke-guest-lock.sh)`);
    process.exit(1);
  }
  return v;
};
const MEMBER_URL = need("MEMBER_URL");
const GUEST_LINK = need("GUEST_LINK");
const CHANNEL_ID = need("CHANNEL_ID");
const PASSPHRASE = need("PASSPHRASE");
const OUT_DIR = process.env.OUT_DIR || ".";
const CHROMIUM = process.env.CHROMIUM || "/usr/bin/chromium";

let pwPath =
  process.env.PLAYWRIGHT_CORE ||
  "/tmp/claude-1000/-home-avicenna-Documents-side-concord/8fb499ff-d55a-41c7-9317-4b8b6c3d03ea/scratchpad/node_modules/playwright-core/index.mjs";
if (fs.existsSync(pwPath) && fs.statSync(pwPath).isDirectory()) {
  pwPath = path.join(pwPath, "index.mjs");
}
if (!fs.existsSync(pwPath)) {
  console.error("FAIL: playwright-core not found. Set PLAYWRIGHT_CORE=<dir>/node_modules/playwright-core.");
  process.exit(1);
}
const { chromium } = await import(pwPath);

const log = (...a) => console.log(new Date().toISOString().slice(11, 23), ...a);
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function rpc(method, args = []) {
  const res = await fetch(`${MEMBER_URL}/rpc`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ method, args }),
  });
  const body = await res.json();
  if (body.error) throw new Error(`rpc ${method}: ${body.error}`);
  return body.result;
}

// Guest-side hooks, installed before the page's own script: every frame the
// gateway delivers is recorded, and every RTCPeerConnection is captured so media
// can be measured (and its ABSENCE proven) from outside.
const guestHook = () => {
  window.__rx = [];
  window.__pcs = [];
  const OWS = window.WebSocket;
  window.WebSocket = class extends OWS {
    constructor(...a) {
      super(...a);
      let buf = "";
      this.addEventListener("message", (ev) => {
        buf += String(ev.data);
        const lines = buf.split("\n");
        buf = lines.pop() ?? "";
        for (const l of lines) {
          if (!l.trim()) continue;
          // Record the raw line too: a frame the gateway failed to terminate
          // would show up here as unparseable rather than silently vanish.
          try {
            window.__rx.push(JSON.parse(l));
          } catch {
            window.__rx.push({ type: "__unparseable", raw: l });
          }
        }
      });
    }
  };
  const OP = window.RTCPeerConnection;
  if (OP) {
    window.RTCPeerConnection = class extends OP {
      constructor(...a) {
        super(...a);
        window.__pcs.push(this);
      }
    };
  }
};
// Member-side hook: capture peer connections (same as the guest side, so audio
// can be measured on both ends) and record the RPCs the UI issues, so the test
// can learn the guest fingerprint the Admit button used instead of guessing.
const memberHook = () => {
  window.__pcs = [];
  const OP = window.RTCPeerConnection;
  if (OP) {
    window.RTCPeerConnection = class extends OP {
      constructor(...a) {
        super(...a);
        window.__pcs.push(this);
      }
    };
  }
  window.__rpc = [];
  const of = window.fetch;
  window.fetch = function (...args) {
    try {
      const body = args[1] && args[1].body;
      if (typeof body === "string" && body.includes('"method"')) window.__rpc.push(JSON.parse(body));
    } catch {
      /* not our RPC */
    }
    return of.apply(this, args);
  };
};

const rx = (page) => page.evaluate(() => window.__rx || []);
const types = (frames) => frames.map((f) => f.type);
async function inbound(page) {
  return page.evaluate(async () => {
    const out = [];
    for (const pc of window.__pcs || []) {
      if (pc.connectionState === "closed") continue;
      const st = await pc.getStats();
      st.forEach((r) => {
        if (r.type === "inbound-rtp") out.push({ kind: r.kind, packetsReceived: r.packetsReceived });
      });
    }
    return out;
  });
}
const sum = (rows, kind, field) =>
  rows.filter((r) => r.kind === kind).reduce((a, r) => a + (r[field] || 0), 0);

const browser = await chromium.launch({
  executablePath: CHROMIUM,
  args: [
    "--no-sandbox",
    "--use-fake-device-for-media-stream",
    "--use-fake-ui-for-media-stream",
    "--auto-select-desktop-capture-source=Entire",
    "--autoplay-policy=no-user-gesture-required",
  ],
});
const memberCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
await memberCtx.addInitScript(memberHook);
const m = await memberCtx.newPage();

const guestPages = [];
async function newGuest(name) {
  const ctx = await browser.newContext({ viewport: { width: 1100, height: 800 } });
  await ctx.addInitScript(guestHook);
  const p = await ctx.newPage();
  guestPages.push([name, p]);
  return p;
}

async function shot(page, file) {
  await page.screenshot({ path: path.join(OUT_DIR, file) }).catch(() => {});
  log(`   screenshot ${file}`);
}
async function fail(reason) {
  await shot(m, "fail-member.png");
  for (const [name, p] of guestPages) await shot(p, `fail-guest-${name}.png`);
  await browser.close().catch(() => {});
  console.error(`FAIL: ${reason}`);
  process.exit(1);
}

// join drives the guest page's form and returns once the socket is up.
async function joinAsGuest(page, name) {
  await page.goto(GUEST_LINK);
  await page.fill("#name", name);
  await page.click("#go");
}

try {
  // ---- member: unlock the UI, open the meeting, join its call, LOCK it ----
  log("member: login");
  await m.goto(MEMBER_URL);
  await m.fill('input[placeholder*="assphrase" i]', PASSPHRASE);
  await m.keyboard.press("Enter");
  await m.waitForSelector('[aria-label="Servers"]', { timeout: 30000 });
  await sleep(800);
  await m.keyboard.press("Escape");
  // ---- the mint UI itself: is the lifetime pickable, and the expiry shown? ----
  // A separate throwaway meeting, made the way a user makes one, so the modal
  // that hands out the link is exercised and not just the RPC behind it.
  log("member: start an instant meeting from the rail (the mint modal)");
  await m.click('[aria-label="Start an instant meeting"]');
  const modal = await m.waitForSelector(".chip-btn", { timeout: 20000 }).catch(() => null);
  if (!modal) await fail("the meeting modal has no lifetime chips");
  await shot(m, "lock-0-mint-modal.png");
  const before = (await m.textContent('[aria-live="polite"]')) || "";
  if (!/expire/i.test(before)) await fail(`the modal never states the expiry: ${JSON.stringify(before)}`);
  await m.click('.chip-btn:has-text("7 days")');
  let after = before;
  for (let i = 0; i < 40 && after === before; i++) {
    after = (await m.textContent('[aria-live="polite"]')) || "";
    if (after === before) await sleep(250);
  }
  if (after === before) await fail("picking 7 days did not change the shown expiry");
  log(`PASS mint modal: "${before.replace(/\s+/g, " ").trim().slice(0, 60)}…" → 7 days picked`);
  await shot(m, "lock-0-mint-7days.png");
  await m.keyboard.press("Escape");

  log("member: open meeting, start call");
  await m.click('[aria-label*="Meeting"]');
  await m.waitForSelector('button[title="Start a call"]', { timeout: 15000 });
  await m.click('button[title="Start a call"]');
  await m.waitForSelector('[aria-label="Share screen"]', { timeout: 30000 });

  // The lock button must EXIST in a meeting — it used to be hidden there, which
  // made office hours impossible in the one room guests can reach.
  const lockBtn = await m.waitForSelector('[aria-label="Lock call"]', { timeout: 10000 }).catch(() => null);
  if (!lockBtn) await fail("no Lock call button in a meeting — office hours can't be armed");
  await lockBtn.click();
  await m.waitForSelector('[aria-label="Unlock call"]', { timeout: 10000 });
  log("PASS member locked the meeting");
  await shot(m, "lock-1-member-locked.png");
  await sleep(1000); // let one lock announcement land in the backend

  // ---- guest #1 knocks and must see NOTHING ----
  log("guest1: open the link at a locked meeting");
  const g1 = await newGuest("knocker");
  await joinAsGuest(g1, "Nadia");
  let waited = false;
  for (let i = 0; i < 40 && !waited; i++) {
    waited = types(await rx(g1)).includes("waiting");
    if (!waited) await sleep(250);
  }
  if (!waited) await fail(`guest never got a "waiting" frame: ${JSON.stringify(types(await rx(g1)))}`);
  const seen = types(await rx(g1));
  log(`guest1 frames while knocking: ${JSON.stringify(seen)}`);
  for (const forbidden of ["welcome", "msg", "sys", "signal", "__unparseable"]) {
    if (seen.includes(forbidden))
      await fail(`a knocking guest received a "${forbidden}" frame — they can see inside a locked meeting`);
  }
  if (await g1.evaluate(() => document.getElementById("room").classList.contains("on")))
    await fail("a knocking guest is in the room UI");
  if ((await g1.evaluate(() => (window.__pcs || []).length)) !== 0)
    await fail("a knocking guest already has an RTCPeerConnection — media before admission");
  log("PASS knocking guest saw only 'waiting': no welcome, no chat, no signalling, no peer connection");
  await shot(g1, "lock-2-guest-waiting.png");

  // ---- the host sees who is knocking, by the name they typed ----
  const knockRow = await m.waitForSelector(".knock", { timeout: 20000 }).catch(() => null);
  if (!knockRow) await fail("the host was never shown the knock");
  const knockText = (await m.textContent(".knock-who")) || "";
  if (!knockText.includes("Nadia")) await fail(`knock row says ${JSON.stringify(knockText)} — not the typed name`);
  log(`PASS host sees the knock: "${knockText.trim()}"`);
  await shot(m, "lock-3-member-knock.png");

  // Nothing said in the room may reach the door.
  await rpc("SendMessage", [CHANNEL_ID, "members-only-secret", ""]);
  await sleep(1500);
  if (JSON.stringify(await rx(g1)).includes("members-only-secret"))
    await fail("a knocking guest received a message sent inside the meeting");
  log("PASS a message sent inside the locked meeting did not leak to the door");

  // ---- admit: only now does the guest get in ----
  await m.click(".knock-admit");
  const inRoom = await g1.waitForSelector("#room.on", { timeout: 20000 }).catch(() => null);
  if (!inRoom) await fail("admitted guest never entered the room");
  log("PASS admitted guest entered the room");
  await shot(g1, "lock-4-guest-admitted.png");
  await shot(m, "lock-4-member-admitted.png");

  // The fingerprint the Admit button used — needed to kick them later, and proof
  // the host identifies a guest per SESSION, not just by name.
  const rpcs = await m.evaluate(() => window.__rpc || []);
  const admit = rpcs.filter((r) => r.method === "SignalCall" && r.args?.[1] === "admit").pop();
  if (!admit) await fail("no admit RPC recorded");
  const guestFpr = admit.args[2];
  if (!/^guest:Nadia#[0-9a-f]{6,}$/.test(guestFpr))
    await fail(`admit targeted ${JSON.stringify(guestFpr)}, want guest:<name>#<session>`);
  log(`PASS admit targeted one session: ${guestFpr}`);

  // ---- chat + audio only after admission ----
  const guestSays = `lock-smoke-${Date.now()}`;
  await g1.fill("#msg", guestSays);
  await g1.press("#msg", "Enter");
  let arrived = false;
  for (let i = 0; i < 20 && !arrived; i++) {
    const msgs = await rpc("Messages", [CHANNEL_ID]);
    arrived = msgs.some((x) => x.content === guestSays && x.kind === "guest");
    if (!arrived) await sleep(500);
  }
  if (!arrived) await fail("admitted guest's chat never reached the member app");
  log("PASS chat guest→member after admission");

  log("guest1: join the call");
  await g1.click("#callBtn");
  const states = (p) => p.evaluate(() => (window.__pcs || []).map((x) => x.connectionState));
  let connected = false;
  for (let i = 0; i < 60 && !connected; i++) {
    const [ms, gs] = [await states(m), await states(g1)];
    if (i % 5 === 0) log("pc states member:", JSON.stringify(ms), "guest:", JSON.stringify(gs));
    connected = ms.includes("connected") && gs.includes("connected");
    if (!connected) await sleep(1000);
  }
  if (!connected) await fail("admitted guest's call never connected");
  await sleep(1500);
  const [g0, m0] = [await inbound(g1), await inbound(m)];
  await sleep(3000);
  const [gg1, mm1] = [await inbound(g1), await inbound(m)];
  const gA = [sum(g0, "audio", "packetsReceived"), sum(gg1, "audio", "packetsReceived")];
  const mA = [sum(m0, "audio", "packetsReceived"), sum(mm1, "audio", "packetsReceived")];
  log(`audio member→guest ${gA[0]} → ${gA[1]}; guest→member ${mA[0]} → ${mA[1]}`);
  if (gA[1] <= gA[0] || mA[1] <= mA[0]) await fail("audio did not flow both ways after admission");
  log("PASS audio both ways for an admitted guest");
  await shot(m, "lock-5-member-incall.png");
  await shot(g1, "lock-5-guest-incall.png");

  // ---- guest #2 is refused, and must be told ----
  log("guest2: knock and get refused");
  const g2 = await newGuest("refused");
  await joinAsGuest(g2, "Mallory");
  let knocking2 = false;
  for (let i = 0; i < 40 && !knocking2; i++) {
    knocking2 = types(await rx(g2)).includes("waiting");
    if (!knocking2) await sleep(250);
  }
  if (!knocking2) await fail("second guest never reached the door");
  // The refuse button is the ✕ on the knock row for Mallory.
  const rows = await m.$$(".knock");
  let clicked = false;
  for (const row of rows) {
    const who = (await row.$eval(".knock-who", (e) => e.textContent)) || "";
    if (who.includes("Mallory")) {
      await row.$eval(".knock-deny", (b) => b.click());
      clicked = true;
    }
  }
  if (!clicked) await fail("no knock row for the second guest to refuse");
  let refused = null;
  for (let i = 0; i < 40 && !refused; i++) {
    refused = (await rx(g2)).find((f) => f.type === "end");
    if (!refused) await sleep(250);
  }
  if (!refused) await fail("a refused guest was left hanging with no end frame");
  if (!refused.reason) await fail("the refusal carried no reason — a blank hang for the guest");
  const shown = (await g2.textContent("#err")) || "";
  if (!shown.trim()) await fail("the refusal never reached the guest's screen");
  log(`PASS refused guest was told: "${refused.reason}" (page shows: "${shown.trim()}")`);
  await shot(g2, "lock-6-guest-refused.png");
  if (types(await rx(g2)).includes("welcome")) await fail("a refused guest still got a welcome");

  // ---- kicking an ADMITTED guest also says why ----
  // This is the RPC the roster's "Disconnect" menu item issues
  // (disconnectVoiceMember → SignalCall "disconnect").
  log("member: kick the admitted guest");
  await rpc("SignalCall", [CHANNEL_ID, "disconnect", guestFpr, ""]);
  let kicked = null;
  for (let i = 0; i < 40 && !kicked; i++) {
    kicked = (await rx(g1)).find((f) => f.type === "end");
    if (!kicked) await sleep(250);
  }
  if (!kicked) await fail("a kicked guest's socket just went quiet — no end frame");
  if (!kicked.reason) await fail("the kick carried no reason");
  log(`PASS kicked guest was told: "${kicked.reason}"`);
  await shot(g1, "lock-7-guest-kicked.png");

  // ---- the lock must SURVIVE the room emptying ----
  // A host alone in a locked meeting is office hours, not a stale lock: if the
  // lock evaporated when the last guest left, the next arrival would walk
  // straight in without knocking.
  await sleep(4000);
  const stillLocked = await m.$('[aria-label="Unlock call"]');
  if (!stillLocked) await fail("the lock dropped when the room emptied — the next guest would walk right in");
  log("PASS lock survived the room emptying (host alone, still locked)");
  await shot(m, "lock-8-member-still-locked.png");

  // ---- and with the door open again, a guest walks straight in ----
  log("member: unlock, then a third guest walks in");
  await m.click('[aria-label="Unlock call"]');
  await sleep(1500);
  const g3 = await newGuest("walkin");
  await joinAsGuest(g3, "Priya");
  const walked = await g3.waitForSelector("#room.on", { timeout: 20000 }).catch(() => null);
  if (!walked) await fail("an unlocked meeting still held a guest at the door");
  if (types(await rx(g3)).includes("waiting"))
    await fail("an unlocked meeting made a guest knock anyway");
  log("PASS unlocked meeting admits a guest with no knock");
  await shot(g3, "lock-9-guest-walkin.png");

  await browser.close();
  console.log(
    "PASS: locked guest meeting — lifetime honoured, knock held, nothing leaked, admit/refuse/kick all explained",
  );
} catch (err) {
  await fail(String(err && err.message ? err.message : err));
}
