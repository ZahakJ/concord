// Browser end-to-end for the guest page's CALL EXPERIENCE. Run it via
// scripts/smoke-guest-ui.sh, which builds and launches everything and feeds this
// driver its environment.
//
// scripts/smoke-guest.mjs proves media flows. This one proves the guest is a
// real client: it measures the things a screenshot cannot — per-stream audio
// gains while two screens are shared, a self-preview that is actually decoding
// frames (videoWidth > 0 and currentTime advancing), device lists that
// enumerate, and a mic swap mid-call that doesn't drop the audio. The page
// exposes window.__guest() for exactly this: a read-only view of its tiles and
// its per-stream audio state.
import fs from "node:fs";
import path from "node:path";

const need = (name) => {
  const v = process.env[name];
  if (!v) {
    console.error(`FAIL: missing env ${name} (run this via scripts/smoke-guest-ui.sh)`);
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
const near = (got, want, tol = 0.02) => Math.abs(got - want) <= tol;

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

const pcHook = () => {
  window.__pcs = [];
  const OP = window.RTCPeerConnection;
  if (!OP) return;
  window.RTCPeerConnection = class extends OP {
    constructor(...a) {
      super(...a);
      window.__pcs.push(this);
    }
  };
};
// The member must actually SEND a screen share with sound, or there is no share
// audio to duck. Chromium on Linux never hands getDisplayMedia an audio track,
// so use the app's own fallback: prefs.shareAudioId nominates an audio INPUT to
// carry the share's sound (on a real Linux box, a "Monitor of …" device; here,
// the fake mic). Written before any app script runs.
// Record the RPCs the member UI issues, so the test learns the guest fingerprint
// the Admit button used instead of guessing at its shape.
const rpcHook = () => {
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
const memberPrefs = () => {
  try {
    const p = JSON.parse(localStorage.getItem("concord.prefs") || "{}");
    p.shareAudioId = "default";
    localStorage.setItem("concord.prefs", JSON.stringify(p));
  } catch {
    /* first run: the app writes its own defaults and we lose the share audio */
  }
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
const gstate = (page) => page.evaluate(() => window.__guest());

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
await memberCtx.addInitScript(pcHook);
await memberCtx.addInitScript(rpcHook);
await memberCtx.addInitScript(memberPrefs);
const m = await memberCtx.newPage();

const shots = [];
const guests = [];
async function newGuest(tag, viewport) {
  const ctx = await browser.newContext({ viewport });
  await ctx.addInitScript(pcHook);
  const p = await ctx.newPage();
  p.on("pageerror", (e) => log(`   !! guest(${tag}) page error: ${e.message}`));
  guests.push([tag, p]);
  return p;
}
async function shot(page, file) {
  await page.screenshot({ path: path.join(OUT_DIR, file) }).catch(() => {});
  shots.push(path.join(OUT_DIR, file));
  log(`   screenshot ${file}`);
}
async function fail(reason) {
  await shot(m, "uifail-member.png");
  for (const [tag, p] of guests) await shot(p, `uifail-guest-${tag}.png`);
  await browser.close().catch(() => {});
  console.error(`FAIL: ${reason}`);
  process.exit(1);
}

// joinRoom drives the name form. The app dials the rendezvous asynchronously
// after login, so a guest that races in gets "host isn't reachable" — retry
// rather than flake.
async function joinRoom(page, name) {
  await page.goto(GUEST_LINK);
  await page.fill("#name", name);
  for (let attempt = 1; attempt <= 3; attempt++) {
    await page.click("#go");
    const ok = await page
      .waitForSelector("#room.on", { timeout: 12000 })
      .then(() => true)
      .catch(() => false);
    if (ok) return true;
    log(`   join attempt ${attempt}: ${await page.textContent("#err").catch(() => "?")}`);
  }
  return false;
}

async function bothConnected(g) {
  const states = (p) => p.evaluate(() => (window.__pcs || []).map((x) => x.connectionState));
  for (let i = 0; i < 60; i++) {
    const [ms, gs] = [await states(m), await states(g)];
    if (ms.includes("connected") && gs.includes("connected")) return true;
    await sleep(1000);
  }
  return false;
}

try {
  // ---- member: unlock the UI, open the meeting, join its call ----
  log("member: login");
  await m.goto(MEMBER_URL);
  await m.fill('input[placeholder*="assphrase" i]', PASSPHRASE);
  await m.keyboard.press("Enter");
  await m.waitForSelector('[aria-label="Servers"]', { timeout: 30000 });
  await sleep(800);
  await m.keyboard.press("Escape");
  await m.click('[aria-label*="Meeting"]');
  await m.waitForSelector('button[title="Start a call"]', { timeout: 15000 });
  await m.click('button[title="Start a call"]');
  await m.waitForSelector('[aria-label="Share screen"]', { timeout: 30000 });
  log("member: in the call");

  // =====================================================================
  // Desktop guest
  // =====================================================================
  const g = await newGuest("desktop", { width: 1280, height: 860 });
  if (!await joinRoom(g, "Nadia")) await fail("guest never reached the room");
  log(`PASS guest welcomed — room "${(await g.textContent("#roomName")).trim()}"`);
  await shot(g, "ui-1-guest-room.png");

  log("guest: join call");
  await g.click("#callBtn");
  if (!await bothConnected(g)) await fail("the call never connected on both sides");
  log("PASS call connected");
  await sleep(2000);

  // ---- 1. the member shares: it must take the stage, not become a box ----
  log("member: share screen (with sound, via the shareAudio fallback)");
  await m.click('[aria-label="Share screen"]');
  const remoteTile = await g
    .waitForSelector("#tiles .tile.has-video:not(.self) video", { timeout: 25000 })
    .catch(() => null);
  if (!remoteTile) await fail("the member's screen share produced no tile on the guest");
  await sleep(3500); // let frames decode and the videoMeta note land

  let st = await gstate(g);
  const hostShare = st.tiles.find((t) => !t.self && t.kind === "screen");
  if (!hostShare) await fail(`no remote SCREEN tile — kinds: ${JSON.stringify(st.tiles.map((t) => t.kind))}`);
  if (!hostShare.focused || st.focus !== hostShare.key)
    await fail("a shared screen did not take the stage — guest still has to hunt for it in a grid");
  if (!(hostShare.w > 0 && hostShare.h > 0))
    await fail(`focused share has no picture (${hostShare.w}x${hostShare.h})`);
  log(`PASS shared screen auto-focused in theater: ${hostShare.w}x${hostShare.h}, key ${hostShare.key}`);
  await shot(g, "ui-2-theater-host-share.png");

  // ---- 2. the guest's OWN screen: a self-preview with real frames ----
  log("guest: share screen");
  await g.click("#shareBtn");
  const selfTile = await g
    .waitForSelector("#tiles .tile.self.has-video video", { timeout: 20000 })
    .catch(() => null);
  if (!selfTile) await fail("guest sharing produced no self-preview tile");
  await sleep(2500);
  const t0 = await gstate(g);
  const s0 = t0.tiles.find((t) => t.key === "self:screen");
  await sleep(1500);
  const s1 = (await gstate(g)).tiles.find((t) => t.key === "self:screen");
  if (!s0 || !s1) await fail("the self screen tile vanished");
  if (!(s1.w > 0 && s1.h > 0)) await fail(`self-preview has no picture (${s1.w}x${s1.h})`);
  if (!(s1.time > s0.time))
    await fail(`self-preview is frozen: currentTime ${s0.time} → ${s1.time} (a still, not a preview)`);
  log(`PASS guest sees their own screen: ${s1.w}x${s1.h}, currentTime ${s0.time.toFixed(2)} → ${s1.time.toFixed(2)}`);
  // …and the member is decoding it, so the preview is not a local-only illusion.
  const mfd0 = sum(await inbound(m), "video", "framesDecoded");
  await sleep(2500);
  const mfd1 = sum(await inbound(m), "video", "framesDecoded");
  if (mfd1 <= mfd0) await fail("the member is not decoding the guest's share");
  log(`PASS member decodes the guest's share (framesDecoded +${mfd1 - mfd0})`);

  // ---- 3. two screens live: sound follows the one you watch ----
  st = await gstate(g);
  const shares = st.tiles.filter((t) => t.kind === "screen");
  if (shares.length < 2) await fail(`expected 2 screen tiles, got ${shares.length}`);
  log(`two screens live: ${shares.map((s) => s.key).join(", ")}`);
  const shareAudio = st.audio.filter((a) => a.share);
  const voice = st.audio.filter((a) => !a.share);
  if (!shareAudio.length)
    await fail(
      "the member's share carried no audio track, so ducking cannot be measured — " +
        "check that prefs.shareAudioId was set before the app booted (see memberPrefs)",
    );
  if (!voice.length) await fail("no voice stream classified — the host's mic was mistaken for share audio");
  await shot(g, "ui-3-two-shares-self-focused.png");

  // Focused: the guest's own screen. The HOST's share is the "other" one, so it
  // must duck — and the voice must not move at all.
  const sid = shareAudio[0].streamId;
  const readOne = async (label) => {
    const s = await gstate(g);
    const a = s.audio.find((x) => x.streamId === sid);
    const v = s.audio.find((x) => !x.share);
    log(
      `   ${label}: focus=${s.focus} share gain=${a.gain} vol=${a.volume} state=${a.state} | ` +
        `voice gain=${v.gain} vol=${v.volume}`,
    );
    return { a, v, focus: s.focus };
  };
  await g.evaluate(() => document.querySelector('[data-key="self:screen"]').click());
  await sleep(400);
  const ducked = await readOne("watching MY screen");
  if (ducked.focus !== "self:screen") await fail("clicking the self share tile did not focus it");
  if (!near(ducked.a.gain, 0.12)) await fail(`the unwatched share did not duck: gain ${ducked.a.gain}`);
  if (!near(ducked.v.gain, 1)) await fail(`VOICE was attenuated (${ducked.v.gain}) — voices must never duck`);

  // Switch to the host's share: now it is the one being watched, so it comes
  // back to full gain. This is the switch affordance and the audio rule at once.
  await g.evaluate((k) => document.querySelector(`[data-key="${k}"]`).click(), hostShare.key);
  await sleep(400);
  const full = await readOne("watching the HOST's screen");
  if (full.focus !== hostShare.key) await fail("switching focus to the other share did not work");
  if (!near(full.a.gain, 1)) await fail(`the watched share is not at full gain: ${full.a.gain}`);
  if (!near(full.v.gain, 1)) await fail(`voice moved when switching shares: ${full.v.gain}`);
  log("PASS sound isolation: watched share 1.00, other share 0.12, voice 1.00 throughout");
  await shot(g, "ui-4-two-shares-host-focused.png");

  // The per-tile override, so the behaviour is not a black box.
  await g.evaluate((k) => document.querySelector(`[data-key="${k}"] .snd`).click(), hostShare.key);
  await sleep(300);
  const off = (await gstate(g)).audio.find((x) => x.streamId === sid);
  if (!(off.gain === 0 && off.muted)) await fail(`the 🔊 badge did not mute that stream: ${JSON.stringify(off)}`);
  log("PASS the per-tile sound badge mutes one stream by hand (gain 0, element muted)");
  await g.evaluate((k) => document.querySelector(`[data-key="${k}"] .snd`).click(), hostShare.key);

  // ---- 3b. the Opus ask actually reaches the wire ----
  // The page rewrites its ANSWER's fmtp lines, which is the only way the side
  // LISTENING to a share can ask for it in stereo. A port that quietly does
  // nothing is worse than no port, so check the description we really sent.
  const sdpInfo = await g.evaluate(() => {
    const pc = (window.__pcs || [])[0];
    const sdp = pc?.localDescription?.sdp || "";
    return {
      audioMLines: (sdp.match(/^m=audio/gm) || []).length,
      fullband: (sdp.match(/maxplaybackrate=48000/g) || []).length,
      stereo: (sdp.match(/stereo=1/g) || []).length,
      bitrates: [...sdp.matchAll(/maxaveragebitrate=(\d+)/g)].map((x) => Number(x[1])),
    };
  });
  log(`   guest answer SDP: ${JSON.stringify(sdpInfo)}`);
  if (!sdpInfo.audioMLines) await fail("the guest's local description has no audio m-line at all");
  if (sdpInfo.fullband < sdpInfo.audioMLines)
    await fail(`the Opus tuning did not reach every audio m-line (${sdpInfo.fullband}/${sdpInfo.audioMLines})`);
  if (!sdpInfo.bitrates.some((b) => b >= 64000))
    await fail(`the guest is still asking for the browser default bitrate: ${sdpInfo.bitrates}`);
  log(
    `PASS the guest asks for full-band Opus on all ${sdpInfo.audioMLines} audio m-lines ` +
      `(bitrates ${sdpInfo.bitrates.join("/")}, stereo asks: ${sdpInfo.stereo})`,
  );

  // ---- 4. getting out of the big view ----
  await g.keyboard.press("Escape");
  await sleep(300);
  st = await gstate(g);
  if (st.focus !== null) await fail("Escape did not leave the big view");
  if (await g.$("#tiles.theater")) await fail("the tiles are still in theater layout with nothing focused");
  log("PASS Escape leaves theater and everything goes back to a grid");
  await shot(g, "ui-5-grid.png");

  // ---- 5. sound settings: enumerate, then actually switch ----
  await g.click("#setBtn");
  await g.waitForSelector("#sheet.on", { timeout: 5000 });
  // enumerateDevices is async, so the pickers fill a tick after the sheet opens.
  await g.waitForFunction(() => document.getElementById("selMic").options.length > 1, null, { timeout: 8000 })
    .catch(() => {});
  const opts = await g.evaluate(() => ({
    mic: [...document.getElementById("selMic").options].map((o) => o.textContent),
    spk: [...document.getElementById("selSpk").options].map((o) => o.textContent),
    cam: [...document.getElementById("selCam").options].map((o) => o.textContent),
    canPickOutput: !document.getElementById("selSpk").disabled,
  }));
  log(`   devices — mic: ${JSON.stringify(opts.mic)}`);
  log(`   devices — speaker: ${JSON.stringify(opts.spk)} (selectable: ${opts.canPickOutput})`);
  log(`   devices — camera: ${JSON.stringify(opts.cam)}`);
  if (opts.mic.length < 2) await fail("the mic picker enumerated no real devices");
  if (opts.cam.length < 2) await fail("the camera picker enumerated no real devices");
  if (opts.canPickOutput && opts.spk.length < 2) await fail("the speaker picker enumerated no real devices");
  await sleep(400); // the sheet fades in; a shot at t=0 is a half-transparent lie
  await shot(g, "ui-6-settings.png");

  // Switching the mic must not drop the call or the audio: the page swaps the
  // track with replaceTrack, so packets keep arriving at the member.
  const before = sum(await inbound(m), "audio", "packetsReceived");
  const micId = await g.evaluate(() => {
    const s = document.getElementById("selMic");
    const pick = [...s.options].find((o) => o.value)?.value || "";
    s.value = pick;
    s.dispatchEvent(new Event("change"));
    return pick;
  });
  // …and turn on the boost + gate at the same time, which is what BUILDS the
  // WebAudio chain and replaces the outgoing track with a processed one.
  await g.evaluate(() => {
    for (const [id, v] of [["micGain", "180"], ["micGate", "3"], ["outVol", "70"]]) {
      const el = document.getElementById(id);
      el.value = v;
      el.dispatchEvent(new Event("input"));
    }
  });
  await sleep(3500);
  const after = sum(await inbound(m), "audio", "packetsReceived");
  log(`   mic switched to ${micId || "(default)"}, boost 180%, gate 0.03: member audio ${before} → ${after}`);
  if (after <= before) await fail("audio stopped after switching the mic / enabling the chain");
  const chained = await g.evaluate(() => {
    const s = window.__guest();
    return { out: s.output, tracks: (window.__pcs || []).flatMap((pc) => pc.getSenders().map((x) => x.track?.label || "")) };
  });
  if (!near(chained.out, 0.7)) await fail(`output volume slider did not apply: ${chained.out}`);
  const vol = (await gstate(g)).audio.find((x) => !x.share);
  if (!near(vol.volume, 0.7, 0.05)) await fail(`master volume not applied to the voice element: ${vol.volume}`);
  log(`PASS mic swap + boost + gate keep the audio flowing; master volume reaches the elements (${vol.volume})`);
  await g.click("#sheetClose");

  // ---- 6. mute + deafen ----
  await g.click("#micBtn");
  await g.click("#deafBtn");
  await sleep(400);
  st = await gstate(g);
  if (!st.muted || !st.deafened) await fail(`mute/deafen did not stick: ${JSON.stringify(st)}`);
  if (!st.audio.every((a) => a.muted)) await fail("deafen left some remote stream audible");
  if (st.micEnabled !== false) await fail(`muted, but the outgoing mic track is still enabled (${st.micEnabled})`);
  // …and muting a mic must NOT mute the sound of what we're sharing.
  if (st.shareAudioEnabled === false) await fail("muting the mic also silenced the screen share's audio");
  log(
    `PASS mute disables the sent track (chain in play: ${st.chained}); deafen mutes every inbound stream; ` +
      `the share's own audio is untouched (${st.shareAudioEnabled})`,
  );
  await g.click("#deafBtn");
  await g.click("#micBtn");

  // ---- 7. the guest's camera + its own preview ----
  await g.click("#camBtn");
  const camTile = await g.waitForSelector('[data-key="self:camera"].has-video', { timeout: 15000 }).catch(() => null);
  if (!camTile) await fail("the guest's camera produced no self tile");
  await sleep(1500);
  const cam = (await gstate(g)).tiles.find((t) => t.key === "self:camera");
  if (!(cam.w > 0)) await fail(`camera self-preview has no picture (${cam.w}x${cam.h})`);
  log(`PASS guest camera self-preview: ${cam.w}x${cam.h}`);
  await shot(g, "ui-7-camera-and-shares.png");
  await g.click("#camBtn");

  // =====================================================================
  // Phone
  // =====================================================================
  log("phone guest: 390x844");
  const ph = await newGuest("phone", { width: 390, height: 844 });
  if (!await joinRoom(ph, "Sam on a phone")) await fail("phone guest never reached the room");
  await shot(ph, "ui-8-phone-room.png");
  await ph.click("#callBtn");
  const phoneShare = await ph
    .waitForSelector("#tiles .tile.has-video:not(.self) video", { timeout: 30000 })
    .catch(() => null);
  if (!phoneShare) await fail("phone guest never received the member's share");
  await sleep(3000);
  const pst = await gstate(ph);
  if (!pst.focus) await fail("the share did not take the stage on the phone");
  const overflow = await ph.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
  if (overflow > 1) await fail(`the phone layout overflows horizontally by ${overflow}px`);
  log(`PASS phone: share focused (${pst.focus}), no horizontal overflow`);
  await shot(ph, "ui-9-phone-theater.png");
  await ph.click("#setBtn");
  await ph.waitForSelector("#sheet.on");
  await ph.waitForFunction(() => document.getElementById("selMic").options.length > 1, null, { timeout: 8000 })
    .catch(() => {});
  await sleep(400);
  const phSheet = await ph.evaluate(() => {
    const r = document.getElementById("sheet").getBoundingClientRect();
    return { w: Math.round(r.width), h: Math.round(r.height), vw: innerWidth, vh: innerHeight };
  });
  if (phSheet.w < phSheet.vw - 2 || phSheet.h < phSheet.vh - 2)
    await fail(`the settings sheet does not fill the phone: ${JSON.stringify(phSheet)}`);
  await shot(ph, "ui-10-phone-settings.png");
  const sheetOverflow = await ph.evaluate(
    () => document.getElementById("sheet").scrollWidth - document.getElementById("sheet").clientWidth,
  );
  if (sheetOverflow > 1) await fail(`the settings sheet overflows sideways by ${sheetOverflow}px`);
  log(`PASS phone settings sheet fills the screen (${phSheet.w}x${phSheet.h}) with no sideways overflow`);
  await ph.click("#sheetClose");

  // =====================================================================
  // The knock: a locked door must render as a STATE, not "Connecting…"
  // =====================================================================
  log("member: lock the call");
  await m.click('[aria-label="Lock call"]');
  await m.waitForSelector('[aria-label="Unlock call"]', { timeout: 10000 });
  await sleep(1000);

  const k = await newGuest("knock", { width: 1100, height: 800 });
  await k.goto(GUEST_LINK);
  await k.fill("#name", "Priya");
  await k.click("#go");
  const knockOn = await k.waitForSelector("#knock.on", { timeout: 20000 }).catch(() => null);
  if (!knockOn) await fail(`a knocking guest saw no knock state (#err: ${await k.textContent("#err")})`);
  const reason1 = (await k.textContent("#knockReason")).trim();
  const shownName = (await k.textContent("#knockName")).trim();
  log(`   knock card: "${reason1}" / "${shownName}"`);
  if (!/locked/i.test(reason1)) await fail(`the knock card is not showing the host's reason: "${reason1}"`);
  if (!shownName.includes("Priya")) await fail(`the knock card does not show the name the host sees: "${shownName}"`);
  if (await k.$("#joinForm:visible")) await fail("the name form is still showing behind the knock");
  await shot(k, "ui-11-knock.png");

  // The gateway re-sends `waiting` every 15s as its liveness probe. The page must
  // REPLACE the status, not stack it — so wait out one re-send and compare.
  log("   waiting 17s for the host's re-announce (idempotency)");
  await sleep(17000);
  const reason2 = (await k.textContent("#knockReason")).trim();
  if (reason2 !== reason1)
    await fail(`a re-sent "waiting" changed the card: "${reason1}" → "${reason2}" (it must be idempotent)`);
  const dupes = await k.evaluate(() => document.querySelectorAll("#knock .door").length);
  if (dupes !== 1) await fail(`the knock state was appended, not replaced (${dupes} doors)`);
  log("PASS a re-sent waiting frame replaces the status instead of stacking it");

  // Admit, and the same page must become a full room.
  await m.click(".knock-admit");
  const inRoom = await k.waitForSelector("#room.on", { timeout: 20000 }).catch(() => null);
  if (!inRoom) await fail("the admitted guest never entered the room");
  if (await k.$("#knock.on")) await fail("the knock card is still up after admission");
  log("PASS admitted guest goes straight from the door into the room");
  await shot(k, "ui-12-admitted.png");

  // This context has never asked for media. Browsers hide device NAMES until
  // some permission has been granted, and the invariant that matters is: either
  // the pickers are usable, or the sheet explains why not and offers the unlock.
  // (Chromium under --use-fake-ui-for-media-stream happens to reveal them
  // immediately, so both branches have to be allowed for.)
  await k.click("#setBtn");
  await k.waitForSelector("#sheet.on");
  await sleep(600);
  let dev = await k.evaluate(() => ({
    hint: getComputedStyle(document.getElementById("permHint")).display !== "none",
    names: [...document.getElementById("selMic").options].slice(1).map((o) => o.textContent),
  }));
  log(`   before any grant — hint: ${dev.hint}, mic names: ${JSON.stringify(dev.names)}`);
  const anonymous = !dev.names.length || dev.names.some((n) => n === "(unnamed device)");
  if (anonymous && !dev.hint)
    await fail("the mic picker has no usable names AND the sheet does not say why");
  await shot(k, "ui-14-settings-before-call.png");
  if (dev.hint) {
    await k.click("#permBtn");
    await k.waitForFunction(
      () => [...document.getElementById("selMic").options].slice(1).some((o) => o.textContent !== "(unnamed device)"),
      null,
      { timeout: 10000 },
    );
    dev = await k.evaluate(() => ({
      names: [...document.getElementById("selMic").options].slice(1).map((o) => o.textContent),
    }));
  }
  log(`PASS device names are usable without joining a call: ${JSON.stringify(dev.names)}`);
  await k.click("#sheetClose");

  // Being removed must read as a sentence in the room, not a dead socket: the
  // banner is new, and #err (which the other harness reads) must still be set.
  const admit = (await m.evaluate(() => window.__rpc || []))
    .filter((r) => r.method === "SignalCall" && r.args?.[1] === "admit")
    .pop();
  if (!admit) await fail("no admit RPC recorded — cannot address the kick");
  await rpc("SignalCall", [CHANNEL_ID, "disconnect", admit.args[2], ""]);
  const banner = await k.waitForSelector("#over.on", { timeout: 15000 }).catch(() => null);
  if (!banner) await fail("a removed guest got no banner explaining why the room stopped working");
  const overText = (await k.textContent("#over")).trim();
  const errText = (await k.textContent("#err")).trim();
  if (!/removed/i.test(overText)) await fail(`the removal banner reads "${overText}"`);
  if (errText !== overText) await fail(`#err ("${errText}") and the banner ("${overText}") disagree`);
  if (!(await k.evaluate(() => document.getElementById("msg").disabled)))
    await fail("the composer is still enabled after the meeting ended for us");
  log(`PASS removal renders verbatim: "${overText}" (banner + #err, composer disabled)`);
  await shot(k, "ui-13-removed.png");

  await m.click('[aria-label="Unlock call"]').catch(() => {});
  await browser.close();
  console.log("PASS: guest page — theater focus/switch/exit, self-preview, per-stream ducking, device pickers, phone layout, knock");
  console.log("screenshots:\n  " + shots.join("\n  "));
} catch (err) {
  await fail(String(err && err.message ? err.message : err));
}
