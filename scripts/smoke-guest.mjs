// Browser end-to-end for the guest-meeting path: a real member in one browser
// context, a real guest (no account, just the link) in another, driven through
// the REAL rendezvous gateway. The `go test` suite already pins the WebSocket
// framing contract; this script exists for the parts only a browser can prove
// — WebRTC media actually flowing both ways, screen shares decoding on the far
// side. Run it via scripts/smoke-guest.sh, which builds and launches everything
// and feeds this driver its environment.
import path from "node:path";
import { loadChromium } from "./playwright.mjs";

const need = (name) => {
  const v = process.env[name];
  if (!v) {
    console.error(`FAIL: missing env ${name} (run this via scripts/smoke-guest.sh)`);
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

const chromium = await loadChromium();

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

// Every RTCPeerConnection the pages create, captured before the app code runs,
// so media can be measured from outside without touching app internals.
const pcHook = () => {
  const OP = window.RTCPeerConnection;
  if (!OP) return;
  window.__pcs = [];
  window.RTCPeerConnection = class extends OP {
    constructor(...a) {
      super(...a);
      window.__pcs.push(this);
    }
  };
};
async function inbound(page) {
  return page.evaluate(async () => {
    const out = [];
    for (const pc of window.__pcs || []) {
      if (pc.connectionState === "closed") continue;
      const st = await pc.getStats();
      st.forEach((r) => {
        if (r.type === "inbound-rtp")
          out.push({ kind: r.kind, packetsReceived: r.packetsReceived, framesDecoded: r.framesDecoded });
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
    // Fake mic/camera and auto-granted permissions: media must flow without a
    // human clicking browser chrome.
    "--use-fake-device-for-media-stream",
    "--use-fake-ui-for-media-stream",
    "--auto-select-desktop-capture-source=Entire",
    "--autoplay-policy=no-user-gesture-required",
  ],
});
const memberCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
const guestCtx = await browser.newContext({ viewport: { width: 1100, height: 800 } });
await memberCtx.addInitScript(pcHook);
await guestCtx.addInitScript(pcHook);
const m = await memberCtx.newPage();
const g = await guestCtx.newPage();

async function fail(reason) {
  await m.screenshot({ path: path.join(OUT_DIR, "fail-member.png") }).catch(() => {});
  await g.screenshot({ path: path.join(OUT_DIR, "fail-guest.png") }).catch(() => {});
  await browser.close().catch(() => {});
  console.error(`FAIL: ${reason}`);
  process.exit(1);
}

try {
  // ---- member: unlock the UI, open the meeting, join its call ----
  log("member: login");
  await m.goto(MEMBER_URL);
  await m.fill('input[placeholder*="assphrase" i]', PASSPHRASE);
  await m.keyboard.press("Enter");
  await m.waitForSelector('[aria-label="Servers"]', { timeout: 30000 });
  await sleep(800);
  await m.keyboard.press("Escape"); // clear any first-run modal
  log("member: open meeting, start call");
  await m.click('[aria-label*="Meeting"]');
  await m.waitForSelector('button[title="Start a call"]', { timeout: 15000 });
  await m.click('button[title="Start a call"]');
  await m.waitForSelector('[aria-label="Share screen"]', { timeout: 30000 });

  // ---- guest: open the link, join the room ----
  // The app dials the rendezvous asynchronously after login; if the guest
  // races in before that connection exists the gateway answers "host isn't
  // reachable", so retry the join rather than flaking.
  log("guest: open link");
  await g.goto(GUEST_LINK);
  await g.fill("#name", "Guesty");
  let welcomed = false;
  for (let attempt = 1; attempt <= 3 && !welcomed; attempt++) {
    await g.click("#go");
    welcomed = await g
      .waitForSelector("#room.on", { timeout: 12000 })
      .then(() => true)
      .catch(() => false);
    if (!welcomed) log(`guest: join attempt ${attempt} failed:`, await g.textContent("#err").catch(() => "?"));
  }
  if (!welcomed) await fail("guest never reached the room — welcome frame missing or unparseable");
  const roomName = (await g.textContent("#roomName")) || "";
  log(`PASS guest welcomed — room "${roomName.trim()}"`);

  // ---- chat, both directions, through the real relay ----
  const guestSays = `smoke-guest-${Date.now()}`;
  await g.fill("#msg", guestSays);
  await g.press("#msg", "Enter");
  let arrived = false;
  for (let i = 0; i < 20 && !arrived; i++) {
    const msgs = await rpc("Messages", [CHANNEL_ID]);
    arrived = msgs.some((x) => x.content === guestSays && x.kind === "guest");
    if (!arrived) await sleep(500);
  }
  if (!arrived) await fail("guest chat message never reached the member app");
  log(`PASS chat guest→member — "${guestSays}" arrived with kind=guest`);

  const memberSays = `smoke-member-${Date.now()}`;
  await rpc("SendMessage", [CHANNEL_ID, memberSays, ""]);
  const seen = await g
    .waitForSelector(`#feed :text("${memberSays}")`, { timeout: 10000 })
    .then(() => true)
    .catch(() => false);
  if (!seen) await fail("member chat message never rendered in the guest feed");
  log(`PASS chat member→guest — "${memberSays}" rendered in guest feed`);

  // ---- the call: guest joins, both peers must reach connected ----
  log("guest: join call");
  await g.click("#callBtn");
  const states = (p) => p.evaluate(() => (window.__pcs || []).map((x) => x.connectionState));
  let connected = false;
  for (let i = 0; i < 60 && !connected; i++) {
    const [ms, gs] = [await states(m), await states(g)];
    if (i % 5 === 0) log("pc states member:", JSON.stringify(ms), "guest:", JSON.stringify(gs));
    connected = ms.includes("connected") && gs.includes("connected");
    if (!connected) await sleep(1000);
  }
  if (!connected) await fail("RTCPeerConnection never reached connected on both sides");
  log("PASS call connected on both sides");
  await sleep(1500); // let RTP settle before taking a baseline

  // ---- audio flows BOTH directions (inbound-rtp packet growth over 3s) ----
  const [g0, m0] = [await inbound(g), await inbound(m)];
  await sleep(3000);
  const [g1, m1] = [await inbound(g), await inbound(m)];
  const gAudio = [sum(g0, "audio", "packetsReceived"), sum(g1, "audio", "packetsReceived")];
  const mAudio = [sum(m0, "audio", "packetsReceived"), sum(m1, "audio", "packetsReceived")];
  log(`audio member→guest packetsReceived ${gAudio[0]} → ${gAudio[1]} (+${gAudio[1] - gAudio[0]})`);
  log(`audio guest→member packetsReceived ${mAudio[0]} → ${mAudio[1]} (+${mAudio[1] - mAudio[0]})`);
  if (gAudio[1] <= gAudio[0]) await fail("no audio packets flowing member→guest");
  if (mAudio[1] <= mAudio[0]) await fail("no audio packets flowing guest→member");
  log("PASS audio both directions");

  // ---- member shares screen → guest decodes frames ----
  log("member: share screen");
  await m.click('[aria-label="Share screen"]');
  const tile = await g
    .waitForSelector("#tiles .tile.has-video:not(.self) video", { timeout: 20000 })
    .catch(() => null);
  if (!tile) await fail("member screen share produced no video tile on the guest");
  const gfd0 = sum(await inbound(g), "video", "framesDecoded");
  await sleep(3000);
  const gfd1 = sum(await inbound(g), "video", "framesDecoded");
  log(`member share: guest framesDecoded ${gfd0} → ${gfd1} (+${gfd1 - gfd0})`);
  if (gfd1 <= gfd0) await fail("guest is not decoding frames from the member's screen share");
  log("PASS screen share member→guest");
  await m.click('[aria-label="Stop sharing"]').catch(() => {});
  await sleep(1500);

  // ---- guest shares screen → member decodes frames ----
  log("guest: share screen");
  await g.click("#shareBtn");
  const mtile = await m
    .waitForSelector(".focus-main video, .screen-tile video", { timeout: 20000 })
    .catch(() => null);
  if (!mtile) await fail("guest screen share produced no video surface on the member");
  const mfd0 = sum(await inbound(m), "video", "framesDecoded");
  await sleep(3000);
  const mfd1 = sum(await inbound(m), "video", "framesDecoded");
  log(`guest share: member framesDecoded ${mfd0} → ${mfd1} (+${mfd1 - mfd0})`);
  if (mfd1 <= mfd0) await fail("member is not decoding frames from the guest's screen share");
  log("PASS screen share guest→member");

  await browser.close();
  console.log("PASS: guest meeting smoke test — welcome, chat both ways, audio both ways, screen share both ways");
} catch (err) {
  await fail(String(err && err.message ? err.message : err));
}
