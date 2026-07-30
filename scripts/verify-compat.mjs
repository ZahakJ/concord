// One mixed-version case: mint a guest link on whichever app is running, open
// it in a real browser through whichever gateway is running, and report what the
// guest actually receives. Driven by scripts/verify-compat.sh.
import fs from "node:fs";
import path from "node:path";

const CASE = process.env.CASE || "?";
const MEMBER_URL = process.env.MEMBER_URL;
const OUT_DIR = process.env.OUT_DIR || ".";
const CHROMIUM = process.env.CHROMIUM || "/usr/bin/chromium";
const PASSPHRASE = "compat-pass";

let pwPath = process.env.PLAYWRIGHT_CORE ||
  "/tmp/claude-1000/-home-avicenna-Documents-side-concord/8fb499ff-d55a-41c7-9317-4b8b6c3d03ea/scratchpad/node_modules/playwright-core/index.mjs";
if (fs.existsSync(pwPath) && fs.statSync(pwPath).isDirectory()) pwPath = path.join(pwPath, "index.mjs");
const { chromium } = await import(pwPath);

const log = (...a) => console.log(`[${CASE}]`, ...a);
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
async function rpc(method, args = []) {
  const res = await fetch(`${MEMBER_URL}/rpc`, {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ method, args }),
  });
  const body = await res.json();
  if (body.error) throw new Error(`rpc ${method}: ${body.error}`);
  return body.result;
}
const wsHook = () => {
  window.__rx = [];
  const OW = window.WebSocket;
  window.WebSocket = class extends OW {
    constructor(...a) { super(...a); window.__ws = this; this.addEventListener("message", (e) => window.__rx.push(String(e.data))); }
  };
};

const browser = await chromium.launch({
  executablePath: CHROMIUM,
  args: ["--no-sandbox", "--use-fake-device-for-media-stream", "--use-fake-ui-for-media-stream", "--autoplay-policy=no-user-gesture-required"],
});
try {
  await rpc("Login", [PASSPHRASE]);
  const meeting = await rpc("StartMeeting", []);
  const GUILD = meeting.guild.id, CHANNEL = meeting.guild.channels[0].id;

  // The new app takes a lifetime; the old one has no such parameter. Extra args
  // must not break the old app, so try two and fall back to one.
  let link, lifetimeAccepted = false;
  try { link = await rpc("CreateGuestLink", [GUILD, 24 * 7]); lifetimeAccepted = true; }
  catch (e) { log("CreateGuestLink with a lifetime failed:", e.message); link = await rpc("CreateGuestLink", [GUILD]); }
  log("guest link:", link, "| lifetime arg accepted:", lifetimeAccepted);
  let expiry = null;
  try { expiry = await rpc("MeetingExpiry", [GUILD]); } catch (e) { log("MeetingExpiry unsupported:", e.message); }
  log("MeetingExpiry:", expiry ? new Date(expiry).toISOString() : "n/a");

  // Which page did this gateway serve? It is embedded in the binary, so the
  // page version is a property of the gateway, not a choice.
  const pageSrc = await (await fetch(link.split("#")[0])).text();
  const pageKind = /"waiting"/.test(pageSrc) ? "NEW (has a waiting branch)" : "OLD (no waiting branch)";
  log(`gateway served the ${pageKind} page — ${pageSrc.split("\n").length} lines`);

  const ctx = await browser.newContext({ viewport: { width: 1100, height: 800 } });
  await ctx.addInitScript(wsHook);
  const g = await ctx.newPage();
  await g.goto(link);
  await g.fill("#name", "CompatGuest");

  // ---- UNLOCKED: does an ordinary guest get in at all? ----
  let joined = false;
  for (let i = 0; i < 3 && !joined; i++) {
    // #go disables itself while a join is in flight, so a stuck join must not be
    // retried by clicking a dead button — that hides the real symptom.
    const clickable = await g.evaluate(() => { const b = document.getElementById("go"); return !!b && !b.disabled; });
    if (!clickable) { log(`join attempt ${i + 1}: #go is disabled (join still in flight)`); await sleep(6000); }
    else {
      await g.click("#go");
      joined = await g.waitForSelector("#room.on", { timeout: 12000 }).then(() => true).catch(() => false);
    }
    if (!joined) log(`join attempt ${i + 1}: #err="${(await g.textContent("#err").catch(() => "")) || ""}"`);
  }
  const raw = await g.evaluate(() => (window.__rx || []));
  const anyNewline = raw.some((s) => s.includes("\n"));
  log(`guest received ${raw.length} websocket message(s); any newline-terminated: ${anyNewline}`);
  log("first message verbatim:", JSON.stringify(raw[0] || null));
  log(joined ? "RESULT: guest JOINED the meeting (unlocked)" : "RESULT: guest COULD NOT JOIN — guest path is DEAD in this combination");
  await g.screenshot({ path: path.join(OUT_DIR, `${CASE}-guest.png`) }).catch(() => {});

  // ---- chat, if it got in ----
  if (joined) {
    const said = `compat-${Date.now()}`;
    await g.fill("#msg", said);
    await g.press("#msg", "Enter");
    let arrived = false;
    for (let i = 0; i < 16 && !arrived; i++) {
      const msgs = await rpc("Messages", [CHANNEL]);
      arrived = (msgs || []).some((x) => x.content === said);
      if (!arrived) await sleep(500);
    }
    log(arrived ? "RESULT: guest chat reached the member app" : "RESULT: guest chat did NOT reach the member app");
  }

  // ---- LOCKED: does the door exist on this app, and what does the page do? ----
  let doorExists = false;
  try {
    await rpc("SignalCall", [CHANNEL, "lock", "", ""]);
    doorExists = true;
  } catch (e) { log("lock verb unsupported on this app:", e.message); }
  if (doorExists) {
    // The door is a 12s lease the front end renews; nothing renews it here, so
    // knock immediately and keep renewing while the guest arrives.
    const renew = setInterval(() => rpc("SignalCall", [CHANNEL, "lock", "", ""]).catch(() => {}), 2500);
    const ctx2 = await browser.newContext({ viewport: { width: 1100, height: 800 } });
    await ctx2.addInitScript(wsHook);
    const g2 = await ctx2.newPage();
    await g2.goto(link);
    await g2.fill("#name", "LockedGuest");
    await g2.click("#go");
    await sleep(9000);
    const raw2 = await g2.evaluate(() => (window.__rx || []));
    const types = raw2.join("").split("\n").filter((s) => s.trim()).map((s) => { try { return JSON.parse(s).type; } catch { return "UNPARSEABLE"; } });
    const inRoom = await g2.evaluate(() => document.getElementById("room")?.classList.contains("on"));
    const knockShown = await g2.evaluate(() => {
      const k = document.getElementById("knock");
      return k ? getComputedStyle(k).display !== "none" : null;
    });
    log(`LOCKED: guest frames=${JSON.stringify(types)} inRoom=${inRoom} knockUIvisible=${knockShown}`);
    log(`LOCKED RESULT: ${inRoom ? "guest WALKED IN despite the lock" : knockShown ? "guest shown a proper knock state" : "guest held at an unexplained screen (degrades to a spinner)"}`);
    await g2.screenshot({ path: path.join(OUT_DIR, `${CASE}-locked-guest.png`) }).catch(() => {});
    clearInterval(renew);
    await ctx2.close();
  } else {
    log("LOCKED RESULT: this app has no guest door at all (pre-door version) — a locked call does not gate guests");
  }
  await browser.close();
} catch (err) {
  console.error(`[${CASE}] DRIVER ERROR:`, err && err.stack ? err.stack : err);
  await browser.close().catch(() => {});
  process.exit(1);
}
