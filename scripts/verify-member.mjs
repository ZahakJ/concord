// Member-side regression driver (scripts/verify-member.sh). Two real apps, two
// browser contexts, one guild: a plain member call with no guest anywhere near
// it, because the guest work edited voice.go, VoicePanel.svelte and
// state.svelte.js — the same code an ordinary call runs on.
import fs from "node:fs";
import path from "node:path";

const A_URL = process.env.A_URL;
const B_URL = process.env.B_URL;
const OUT_DIR = process.env.OUT_DIR || ".";
const CHROMIUM = process.env.CHROMIUM || "/usr/bin/chromium";
const PASS_A = "alice-pass-verify";
const PASS_B = "bob-pass-verify";

let pwPath = process.env.PLAYWRIGHT_CORE ||
  "/tmp/claude-1000/-home-avicenna-Documents-side-concord/8fb499ff-d55a-41c7-9317-4b8b6c3d03ea/scratchpad/node_modules/playwright-core/index.mjs";
if (fs.existsSync(pwPath) && fs.statSync(pwPath).isDirectory()) pwPath = path.join(pwPath, "index.mjs");
const { chromium } = await import(pwPath);

const log = (...a) => console.log(new Date().toISOString().slice(11, 23), ...a);
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const results = [];
const pass = (m) => { results.push(["PASS", m]); log("PASS", m); };
const bad = (m) => { results.push(["FAIL", m]); log("FAIL", m); };
const note = (m) => { results.push(["NOTE", m]); log("NOTE", m); };

async function rpc(base, method, args = []) {
  const res = await fetch(`${base}/rpc`, {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ method, args }),
  });
  const body = await res.json();
  if (body.error) throw new Error(`rpc ${method}@${base}: ${body.error}`);
  return body.result;
}
const pcHook = () => {
  const OP = window.RTCPeerConnection;
  if (!OP) return;
  window.__pcs = [];
  window.RTCPeerConnection = class extends OP {
    constructor(...a) { super(...a); window.__pcs.push(this); }
  };
};
const inbound = (page) => page.evaluate(async () => {
  const out = [];
  for (const pc of window.__pcs || []) {
    if (pc.connectionState === "closed") continue;
    const st = await pc.getStats();
    st.forEach((r) => { if (r.type === "inbound-rtp") out.push({ kind: r.kind, packetsReceived: r.packetsReceived, framesDecoded: r.framesDecoded }); });
  }
  return out;
});
const sum = (rows, kind, f) => rows.filter((r) => r.kind === kind).reduce((a, r) => a + (r[f] || 0), 0);

const browser = await chromium.launch({
  executablePath: CHROMIUM,
  args: ["--no-sandbox", "--use-fake-device-for-media-stream", "--use-fake-ui-for-media-stream",
    "--auto-select-desktop-capture-source=Entire", "--autoplay-policy=no-user-gesture-required"],
});
const shot = (page, name) => page.screenshot({ path: path.join(OUT_DIR, name) }).catch(() => {});

async function openApp(url, passphrase, w = 1280, h = 860) {
  // Unlock over RPC first, as scripts/smoke-guest.sh does: onboarding a brand
  // new identity through the first-run screen is a different flow from logging
  // in, and this sweep is about the CALL, not onboarding.
  await rpc(url, "Login", [passphrase]);
  const ctx = await browser.newContext({ viewport: { width: w, height: h } });
  await ctx.addInitScript(pcHook);
  const page = await ctx.newPage();
  await page.goto(url);
  // Already unlocked over RPC, so the passphrase screen may vanish underneath
  // us mid-fill; that race is not a failure.
  try {
    const needsPass = await page.$('input[placeholder*="assphrase" i]');
    if (needsPass) { await needsPass.fill(passphrase); await page.keyboard.press("Enter"); }
  } catch { /* the app unlocked itself first */ }
  const ok = await page.waitForSelector('[aria-label="Servers"]', { timeout: 30000 }).catch(() => null);
  if (!ok) {
    await shot(page, `RZ-openApp-${w}-${Date.now()}.png`);
    log("page text:", (await page.evaluate(() => document.body.innerText.slice(0, 500))));
    throw new Error(`app at ${url} never showed the servers rail`);
  }
  await sleep(800);
  await page.keyboard.press("Escape");
  return { ctx, page };
}

let A, B;
try {
  A = await openApp(A_URL, PASS_A);
  B = await openApp(B_URL, PASS_B);
  pass("both member apps logged in");

  // ---- a real guild, joined over an invite code ----
  const guild = await rpc(A_URL, "CreateGuild", ["RegressionRoom"]);
  const code = await rpc(A_URL, "InviteCode", [guild.id]);
  await rpc(B_URL, "JoinViaInvite", [code]);
  let bGuild = null;
  for (let i = 0; i < 60 && !bGuild; i++) {
    const gs = await rpc(B_URL, "Guilds", []);
    bGuild = (gs || []).find((g) => g.name === "RegressionRoom");
    if (!bGuild) await sleep(500);
  }
  if (!bGuild) { bad("second member never received the guild"); throw new Error("join failed"); }
  pass(`second member joined the guild over an invite code (${code.length} chars)`);

  // A voice channel, so this is the ordinary member call path and not a meeting.
  const vc = await rpc(A_URL, "CreateChannel", [guild.id, "talk", "voice"]).catch(async () => {
    const chans = (await rpc(A_URL, "Guilds", [])).find((g) => g.id === guild.id)?.channels || [];
    return chans.find((c) => c.type === "voice");
  });
  let bVc = null;
  for (let i = 0; i < 40 && !bVc; i++) {
    const gs = await rpc(B_URL, "Guilds", []);
    bVc = (gs.find((g) => g.name === "RegressionRoom")?.channels || []).find((c) => c.type === "voice");
    if (!bVc) await sleep(500);
  }
  log("voice channel:", JSON.stringify(vc?.id || vc), "seen by B:", JSON.stringify(bVc?.id));
  if (bVc) pass(`voice channel "${bVc.name}" replicated to the second member`);
  else bad("voice channel never reached the second member");

  // ---- text both ways (the plain member wire) ----
  const aChan = (await rpc(A_URL, "Guilds", [])).find((g) => g.id === guild.id).channels.find((c) => (c.type || "text") === "text");
  const bChan = (await rpc(B_URL, "Guilds", [])).find((g) => g.name === "RegressionRoom").channels.find((c) => (c.type || "text") === "text");
  const said = `member-regress-${Date.now()}`;
  await rpc(A_URL, "SendMessage", [aChan.id, said, ""]);
  let got = false;
  for (let i = 0; i < 40 && !got; i++) {
    const msgs = await rpc(B_URL, "Messages", [bChan.id]);
    got = (msgs || []).some((x) => x.content === said);
    if (!got) await sleep(500);
  }
  if (got) pass("member->member text still delivers");
  else bad("member->member text did NOT arrive");

  // ---- both join the voice channel through the real UI ----
  await A.page.click(`[aria-label*="RegressionRoom"]`).catch(() => {});
  await B.page.click(`[aria-label*="RegressionRoom"]`).catch(() => {});
  await sleep(1500);
  const joinVoice = async (p, who) => {
    const row = await p.waitForSelector('text="talk"', { timeout: 15000 }).catch(() => null);
    if (!row) { bad(`${who}: could not find the voice channel row in the UI`); return false; }
    await row.click();
    // Clicking a voice channel selects it; joining is the "Voice" action in the
    // header when the app doesn't auto-connect.
    if (!(await p.$('[aria-label="Share screen"]'))) {
      await p.click('text="Voice"').catch(() => {});
    }
    const ok = await p.waitForSelector('[aria-label="Share screen"]', { timeout: 25000 }).then(() => true).catch(() => false);
    if (!ok) bad(`${who}: never entered the voice channel`);
    return ok;
  };
  const aIn = await joinVoice(A.page, "alice");
  const bIn = await joinVoice(B.page, "bob");
  if (aIn && bIn) pass("both members joined a voice channel from the UI");
  await shot(A.page, "R1-alice-in-call.png");
  await shot(B.page, "R2-bob-in-call.png");

  let connected = false;
  for (let i = 0; i < 60 && !connected; i++) {
    const as = await A.page.evaluate(() => (window.__pcs || []).map((p) => p.connectionState));
    const bs = await B.page.evaluate(() => (window.__pcs || []).map((p) => p.connectionState));
    if (i % 6 === 0) log("pc states A:", JSON.stringify(as), "B:", JSON.stringify(bs));
    connected = as.includes("connected") && bs.includes("connected");
    if (!connected) await sleep(1000);
  }
  if (connected) pass("member<->member RTCPeerConnection connected");
  else { bad("member<->member call never connected"); }

  await sleep(2500);
  const [a0, b0] = [await inbound(A.page), await inbound(B.page)];
  await sleep(3000);
  const [a1, b1] = [await inbound(A.page), await inbound(B.page)];
  const aA = [sum(a0, "audio", "packetsReceived"), sum(a1, "audio", "packetsReceived")];
  const bA = [sum(b0, "audio", "packetsReceived"), sum(b1, "audio", "packetsReceived")];
  log(`audio B->A ${aA[0]}->${aA[1]}   A->B ${bA[0]}->${bA[1]}`);
  if (aA[1] > aA[0] && bA[1] > bA[0]) pass(`member<->member audio flows both ways (+${aA[1] - aA[0]} / +${bA[1] - bA[0]} packets)`);
  else bad(`member audio not flowing both ways: ${JSON.stringify({ aA, bA })}`);

  // ---- member screen share ----
  await A.page.click('[aria-label="Share screen"]');
  await sleep(2000);
  const bfd0 = sum(await inbound(B.page), "video", "framesDecoded");
  await sleep(4000);
  const bfd1 = sum(await inbound(B.page), "video", "framesDecoded");
  log(`alice share -> bob framesDecoded ${bfd0} -> ${bfd1}`);
  if (bfd1 > bfd0) pass(`member->member screen share decodes (+${bfd1 - bfd0} frames)`);
  else bad(`member screen share produced no decoded frames on the other member (${bfd0} -> ${bfd1})`);
  await shot(B.page, "R3-bob-sees-share.png");
  await A.page.click('[aria-label="Stop sharing"]').catch(() => {});

  // ---- the LOCK still works between members (no guest involved) ----
  const lock = await A.page.$('[aria-label="Lock call"]');
  if (lock) {
    await lock.click();
    const unlocked = await A.page.waitForSelector('[aria-label="Unlock call"]', { timeout: 5000 }).then(() => true).catch(() => false);
    if (unlocked) pass("lock/unlock still works in an ordinary member voice call");
    else bad("lock did not toggle in a member voice call");
    await A.page.click('[aria-label="Unlock call"]').catch(() => {});
  } else note("no lock button visible in the member voice call (permission-gated?)");

  // ---- the meeting creation UI (where the lifetime chips landed) ----
  await A.page.click('[aria-label="Leave call"]').catch(() => {});
  await sleep(800);
  // The real entry point, not the RPC: the mint modal is what the rail button
  // opens, and the chips only exist inside it.
  const meetBtn = await A.page.$('[aria-label="Start an instant meeting"]');
  if (!meetBtn) bad("no 'Start an instant meeting' button in the rail");
  else {
    await meetBtn.click();
    await sleep(3500);
    await shot(A.page, "R4-meeting-created.png");
    const chips = await A.page.evaluate(() => {
      const txt = document.body.innerText;
      return {
        hasWorksFor: /works for/i.test(txt),
        chips: [...document.querySelectorAll("button")].map((b) => b.textContent.trim())
          .filter((t) => /^(1 hour|24 hours|7 days|30 days)$/i.test(t)),
        expiryLine: (txt.match(/expires?[^\n]*/i) || [])[0] || "",
      };
    });
    log("mint modal:", JSON.stringify(chips));
    if (chips.chips.length === 4) pass(`meeting invite modal offers all four lifetimes: ${chips.chips.join(" / ")}`);
    else bad(`lifetime chips missing or incomplete: ${JSON.stringify(chips.chips)}`);
    if (chips.expiryLine) pass(`modal states the expiry in words: "${chips.expiryLine.slice(0, 90)}"`);
    else bad("modal shows no expiry line");
    await shot(A.page, "R5-meeting-mint-modal.png");

    // Clicking a chip must move the stated expiry, not just look selected.
    const before = await A.page.evaluate(() => (document.body.innerText.match(/expires?[^\n]*/i) || [])[0] || "");
    const sevenDays = await A.page.$('text="7 days"');
    if (sevenDays) {
      await sevenDays.click();
      await sleep(2500);
      const after = await A.page.evaluate(() => (document.body.innerText.match(/expires?[^\n]*/i) || [])[0] || "");
      log("expiry line before:", JSON.stringify(before), "after 7 days:", JSON.stringify(after));
      if (after && after !== before) pass(`picking "7 days" moved the stated expiry: "${before.trim()}" -> "${after.trim()}"`);
      else bad(`picking "7 days" did not change the stated expiry (still "${after}")`);
      await shot(A.page, "R6-meeting-7day.png");
    } else bad("no '7 days' chip to click");
  }

  console.log("\n================ MEMBER REGRESSION ================");
  for (const [k, v] of results) console.log(k.padEnd(5), v);
  const fails = results.filter(([k]) => k === "FAIL");
  console.log(`\n${results.filter(([k]) => k === "PASS").length} pass, ${fails.length} fail, ${results.filter(([k]) => k === "NOTE").length} notes`);
  await browser.close();
  process.exit(fails.length ? 2 : 0);
} catch (err) {
  if (A) await shot(A.page, "RZ-fail-alice.png");
  if (B) await shot(B.page, "RZ-fail-bob.png");
  console.error("DRIVER ERROR:", err && err.stack ? err.stack : err);
  console.log("\n================ MEMBER REGRESSION (incomplete) ================");
  for (const [k, v] of results) console.log(k.padEnd(5), v);
  await browser.close().catch(() => {});
  process.exit(3);
}
