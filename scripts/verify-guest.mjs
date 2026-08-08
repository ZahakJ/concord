// Independent verification driver. Run via scripts/verify-guest.sh.
//
// The claims under test are about what a stranger at a LOCKED door can see, so
// this does not read the guest's UI to decide: it wraps WebSocket in the page
// before the page's own script runs and asserts on the exact frames received.
// A spinner proves nothing; an empty frame log does.
import path from "node:path";
import { loadChromium } from "./playwright.mjs";

const need = (n) => {
  const v = process.env[n];
  if (!v) { console.error(`FAIL: missing env ${n}`); process.exit(1); }
  return v;
};
const MEMBER_URL = need("MEMBER_URL");
const GATEWAY_URL = need("GATEWAY_URL");
const OUT_DIR = process.env.OUT_DIR || ".";
const CHROMIUM = process.env.CHROMIUM || "/usr/bin/chromium";
const PASSPHRASE = "verify-guest-pass";

const chromium = await loadChromium();

const log = (...a) => console.log(new Date().toISOString().slice(11, 23), ...a);
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const results = [];
const pass = (m) => { results.push(["PASS", m]); log("PASS", m); };
const bad = (m) => { results.push(["FAIL", m]); log("FAIL", m); };
const note = (m) => { results.push(["NOTE", m]); log("NOTE", m); };

async function rpc(method, args = []) {
  const res = await fetch(`${MEMBER_URL}/rpc`, {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ method, args }),
  });
  const body = await res.json();
  if (body.error) throw new Error(`rpc ${method}: ${body.error}`);
  return body.result;
}

// Recorded before any page script runs: every frame in and out, plus a handle
// on the socket so the harness can send frames the page never would.
const wsHook = () => {
  window.__rx = []; window.__tx = [];
  const OW = window.WebSocket;
  window.WebSocket = class extends OW {
    constructor(...a) {
      super(...a);
      window.__ws = this;
      this.addEventListener("message", (e) => window.__rx.push(String(e.data)));
    }
    send(d) { window.__tx.push(String(d)); return super.send(d); }
  };
};
// The member app streams events over SSE. Recording them is how the harness
// learns a guest's fingerprint ("guest:<name>#<session>") without the app
// having to expose it — the same string the host's own UI acts on.
const sseHook = () => {
  window.__sse = [];
  const OE = window.EventSource;
  if (!OE) return;
  // The app subscribes to NAMED events, so a "message" listener sees nothing —
  // piggyback on every subscription the app makes instead.
  window.EventSource = class extends OE {
    constructor(...a) {
      super(...a);
      const orig = this.addEventListener.bind(this);
      this.addEventListener = (type, fn, ...rest) => {
        if (!window.__seen) window.__seen = new Set();
        if (!window.__seen.has(type)) {
          window.__seen.add(type);
          orig(type, (e) => window.__sse.push(type + " " + String(e.data)));
        }
        return orig(type, fn, ...rest);
      };
    }
  };
};
const pcHook = () => {
  const OP = window.RTCPeerConnection;
  if (!OP) return;
  window.__pcs = [];
  window.RTCPeerConnection = class extends OP {
    constructor(...a) { super(...a); window.__pcs.push(this); }
  };
};

// Frames as the page sees them: the gateway may coalesce, so split on newline.
const frames = (page) => page.evaluate(() =>
  (window.__rx || []).join("").split("\n").filter((s) => s.trim())
    .map((s) => { try { return JSON.parse(s); } catch { return { type: "UNPARSEABLE", raw: s }; } }));
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
const shot = async (page, name) => {
  const p = path.join(OUT_DIR, name);
  await page.screenshot({ path: p }).catch(() => {});
  return p;
};

const memberCtx = await browser.newContext({ viewport: { width: 1280, height: 860 } });
await memberCtx.addInitScript(pcHook);
await memberCtx.addInitScript(sseHook);
const m = await memberCtx.newPage();

async function newGuest(width = 1280, height = 860) {
  const ctx = await browser.newContext({ viewport: { width, height } });
  await ctx.addInitScript(wsHook);
  await ctx.addInitScript(pcHook);
  return { ctx, page: await ctx.newPage() };
}

try {
  // ---------- setup: member, meeting, links with chosen lifetimes ----------
  await rpc("Login", [PASSPHRASE]);
  const meeting = await rpc("StartMeeting", []);
  const GUILD = meeting.guild.id, CHANNEL = meeting.guild.channels[0].id;
  log("meeting", GUILD, "channel", CHANNEL);

  // ---------- PHASE A: chosen lifetime ----------
  const t0 = Date.now();
  const link7 = await rpc("CreateGuestLink", [GUILD, 24 * 7]);
  const exp7 = await rpc("MeetingExpiry", [GUILD]);
  const drift7 = Math.abs(exp7 - (t0 + 7 * 24 * 3600e3));
  if (drift7 < 120e3) pass(`7-day lifetime: MeetingExpiry = ${new Date(exp7).toISOString()} (drift ${Math.round(drift7 / 1000)}s)`);
  else bad(`7-day lifetime wrong: expiry ${new Date(exp7).toISOString()}, drift ${Math.round(drift7 / 1000)}s`);

  const link1 = await rpc("CreateGuestLink", [GUILD, 1]);
  const exp1 = await rpc("MeetingExpiry", [GUILD]);
  const drift1 = Math.abs(exp1 - (t0 + 3600e3));
  if (drift1 < 120e3) pass(`re-mint to 1h re-dated the meeting: ${new Date(exp1).toISOString()}`);
  else bad(`1h lifetime wrong: ${new Date(exp1).toISOString()}`);
  if (link1 === link7) pass("re-minting returns the SAME url (a pasted link is not invalidated by changing the duration)");
  else bad(`re-mint changed the url: ${link7} -> ${link1}`);

  for (const badHours of [2, 5, 999, 24 * 31]) {
    let refused = false;
    try { await rpc("CreateGuestLink", [GUILD, badHours]); } catch { refused = true; }
    if (!refused) bad(`off-menu lifetime ${badHours}h was ACCEPTED (should be refused, not rounded)`);
  }
  pass("off-menu lifetimes (2h, 5h, 999h, 31d) all refused rather than rounded");

  // Put it back to something usable for the rest of the run.
  const LINK = await rpc("CreateGuestLink", [GUILD, 24]);
  log("guest link:", LINK);

  // ---------- expired / bogus token, at the wire ----------
  {
    const { ctx, page } = await newGuest();
    await page.goto(`${GATEWAY_URL}/guest#h=${LINK.split("h=")[1].split("&")[0]}&t=` + "A".repeat(32));
    await page.fill("#name", "Bogus");
    await page.click("#go");
    await sleep(4000);
    const fr = await frames(page);
    const end = fr.find((f) => f.type === "end");
    const errText = (await page.textContent("#err").catch(() => "")) || "";
    if (end && /no longer valid/i.test(end.reason || "")) pass(`invalid token refused with words: "${end.reason}" (page shows: "${errText.trim()}")`);
    else bad(`invalid token did not produce a clear end frame: ${JSON.stringify(fr)}`);
    await shot(page, "A-bogus-token.png");
    await ctx.close();
  }

  // ---------- member: open meeting, start call, LOCK ----------
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
  const lockBtn = await m.waitForSelector('[aria-label="Lock call"]', { timeout: 10000 }).catch(() => null);
  if (!lockBtn) { bad("lock button is NOT present in a meeting call — office hours impossible"); throw new Error("no lock button"); }
  pass("lock button present in a MEETING call (the isGuildCall fix is real)");
  await lockBtn.click();
  await m.waitForSelector('[aria-label="Unlock call"]', { timeout: 5000 });
  pass("meeting locked from the member UI");
  await shot(m, "B-member-locked.png");

  // ---------- PHASE B: THE SECURITY QUESTION ----------
  const knocker = await newGuest();
  await knocker.page.goto(LINK);
  await knocker.page.fill("#name", "Knocker");
  await knocker.page.click("#go");
  const gotWaiting = await knocker.page.waitForFunction(
    () => (window.__rx || []).join("").includes('"waiting"'), null, { timeout: 20000 }).then(() => true).catch(() => false);
  if (!gotWaiting) { bad("locked meeting never produced a waiting frame"); }
  else pass("locked meeting turned arrival into a knock (waiting frame received)");

  // Adversarial: try every verb a stranger could send while at the door.
  await knocker.page.evaluate(() => {
    const w = window.__ws;
    w.send(JSON.stringify({ type: "call", action: "join" }));
    w.send(JSON.stringify({ type: "msg", content: "SNEAKY-KNOCKER-MESSAGE" }));
    w.send(JSON.stringify({ type: "signal", data: { sdp: "v=0" } }));
    w.send(JSON.stringify({ type: "admit" }));
    w.send(JSON.stringify({ type: "unlock" }));
    w.send(JSON.stringify({ type: "hello", token: "x", name: "y" }));
  });
  await sleep(6000);

  const kf = await frames(knocker.page);
  const types = [...new Set(kf.map((f) => f.type))];
  log("frames received by the KNOCKING guest:", JSON.stringify(kf, null, 1));
  const leaked = kf.filter((f) => !["waiting"].includes(f.type));
  if (leaked.length === 0) pass(`knocking guest received ONLY waiting frames (${kf.length} frames, types=${JSON.stringify(types)}) — no welcome, no roster, no msg, no sys, no signal`);
  else bad(`knocking guest LEAKED frames: ${JSON.stringify(leaked)}`);

  const st = await knocker.page.evaluate(() => ({
    pcs: (window.__pcs || []).length,
    states: (window.__pcs || []).map((p) => p.connectionState),
    roomOn: document.getElementById("room")?.classList.contains("on") ?? null,
    guest: typeof window.__guest === "function" ? window.__guest() : null,
  }));
  log("knocker page state:", JSON.stringify(st));
  if (st.pcs === 0) pass("knocking guest created NO RTCPeerConnection — there is no media path at all");
  else bad(`knocking guest has ${st.pcs} RTCPeerConnection(s): ${JSON.stringify(st.states)}`);
  if (st.roomOn === false) pass("knocking guest is not in the room UI");
  else bad(`knocking guest room state: ${st.roomOn}`);
  const tiles = st.guest?.tiles || [];
  if (tiles.length === 0) pass("knocking guest has zero video/presence tiles — no participant names");
  else bad(`knocking guest has tiles: ${JSON.stringify(tiles)}`);

  // Their pre-sent chat must not have reached the meeting.
  const msgs = await rpc("Messages", [CHANNEL]);
  if (!msgs.some((x) => (x.content || "").includes("SNEAKY-KNOCKER-MESSAGE"))) pass("chat sent while knocking never reached the meeting");
  else bad("a message sent WHILE KNOCKING was posted into the meeting");
  if (!msgs.some((x) => (x.content || "").includes("Knocker") && /joined as a guest/.test(x.content || "")))
    pass("no arrival notice in the transcript for someone still at the door");
  else bad("a knocking guest was announced as having joined");

  // The host's side: a knock row, and no roster entry.
  const knockRow = await m.textContent(".knock-who").catch(() => null);
  if (knockRow && /Knocker/.test(knockRow) && /guest/i.test(knockRow)) pass(`host sees the knock: "${knockRow.trim()}"`);
  else bad(`host knock row missing or unlabelled: ${JSON.stringify(knockRow)}`);
  await shot(m, "C-member-knock-row.png");
  await shot(knocker.page, "C-guest-knocking.png");

  // ---------- PHASE C: REFUSE ends with words ----------
  await m.click(".knock-deny");
  const refused = await knocker.page.waitForFunction(
    () => (window.__rx || []).join("").includes('"end"'), null, { timeout: 15000 }).then(() => true).catch(() => false);
  const kf2 = await frames(knocker.page);
  const endFr = kf2.find((f) => f.type === "end");
  if (refused && endFr?.reason) pass(`refused guest was TOLD, not left hanging: "${endFr.reason}"`);
  else bad(`refusal produced no end reason: ${JSON.stringify(kf2)}`);
  const errAfter = (await knocker.page.textContent("#err").catch(() => "")) || "";
  if (errAfter.trim()) pass(`refusal is visible on the guest page: "${errAfter.trim()}"`);
  else bad("refusal reason is not rendered on the guest page");
  await shot(knocker.page, "D-guest-refused.png");
  await sleep(1500);
  const knockRowGone = await m.$(".knock-who");
  if (!knockRowGone) pass("host's knock list cleared after refusing");
  else bad("refused knock is still sitting in the host's list");
  await knocker.ctx.close();

  // ---------- PHASE D: ADMIT — media only after ----------
  const gv = await newGuest(1280, 860);
  await gv.page.goto(LINK);
  await gv.page.fill("#name", "Nadia");
  await gv.page.click("#go");
  await gv.page.waitForFunction(() => (window.__rx || []).join("").includes('"waiting"'), null, { timeout: 20000 });
  await gv.page.screenshot({ path: path.join(OUT_DIR, "E-guest-knock-desktop.png") });
  const knockUI = await gv.page.evaluate(() => ({
    visible: getComputedStyle(document.getElementById("knock")).display !== "none",
    name: document.getElementById("knockName")?.textContent || "",
    reason: document.getElementById("knockReason")?.textContent || "",
    title: document.getElementById("title")?.textContent || "",
  }));
  log("knock UI:", JSON.stringify(knockUI));
  if (knockUI.visible && knockUI.reason.trim()) pass(`guest sees a real knock state, not a spinner: title="${knockUI.title}" name="${knockUI.name}" reason="${knockUI.reason}"`);
  else bad(`knock UI not shown: ${JSON.stringify(knockUI)}`);

  const beforeAdmit = await gv.page.evaluate(() => (window.__pcs || []).length);
  await m.waitForSelector(".knock-admit", { timeout: 15000 });
  await m.click(".knock-admit");
  const welcomed = await gv.page.waitForSelector("#room.on", { timeout: 20000 }).then(() => true).catch(() => false);
  if (!welcomed) { bad("admitted guest never entered the room"); throw new Error("admit failed"); }
  const afr = await frames(gv.page);
  const order = afr.map((f) => f.type);
  const wi = order.indexOf("waiting"), ci = order.indexOf("welcome");
  if (wi >= 0 && ci > wi) pass(`frame order is waiting-then-welcome: ${JSON.stringify(order.slice(0, 8))}`);
  else bad(`unexpected frame order: ${JSON.stringify(order)}`);
  if (beforeAdmit === 0) pass("no peer connection existed before admission; media could only start after");
  else bad(`guest had ${beforeAdmit} peer connections BEFORE admission`);

  await gv.page.click("#callBtn");
  let connected = false;
  for (let i = 0; i < 60 && !connected; i++) {
    const gs = await gv.page.evaluate(() => (window.__pcs || []).map((p) => p.connectionState));
    const ms = await m.evaluate(() => (window.__pcs || []).map((p) => p.connectionState));
    connected = gs.includes("connected") && ms.includes("connected");
    if (!connected) await sleep(1000);
  }
  if (connected) pass("admitted guest's call connected on both sides");
  else { bad("admitted guest's call never connected"); }
  await sleep(2500);
  const [a0, b0] = [await inbound(gv.page), await inbound(m)];
  await sleep(3000);
  const [a1, b1] = [await inbound(gv.page), await inbound(m)];
  const gA = [sum(a0, "audio", "packetsReceived"), sum(a1, "audio", "packetsReceived")];
  const mA = [sum(b0, "audio", "packetsReceived"), sum(b1, "audio", "packetsReceived")];
  log(`audio host->guest ${gA[0]}->${gA[1]}  guest->host ${mA[0]}->${mA[1]}`);
  if (gA[1] > gA[0] && mA[1] > mA[0]) pass(`audio flows both ways after admission (+${gA[1] - gA[0]} / +${mA[1] - mA[0]} packets)`);
  else bad(`audio not flowing both ways: ${JSON.stringify({ gA, mA })}`);

  // ---------- PHASE E: theater, self-preview, ducking ----------
  await m.click('[aria-label="Share screen"]');
  await gv.page.waitForSelector("#tiles .tile.has-video:not(.self) video", { timeout: 25000 });
  await sleep(2500);
  await gv.page.click("#shareBtn");
  await sleep(4000);
  let gs = await gv.page.evaluate(() => window.__guest());
  log("guest state after both shares:", JSON.stringify(gs, null, 1));
  const selfShare = (gs.tiles || []).find((t) => t.self && t.kind === "screen");
  if (selfShare && selfShare.w > 0 && selfShare.hasVideo) pass(`guest sees its OWN share: "${selfShare.label}" ${selfShare.w}x${selfShare.h} t=${selfShare.time}`);
  else bad(`no self-preview of the guest's own share: ${JSON.stringify(gs.tiles)}`);
  const t1 = selfShare?.time || 0;
  await sleep(2000);
  gs = await gv.page.evaluate(() => window.__guest());
  const t2 = (gs.tiles || []).find((t) => t.self && t.kind === "screen")?.time || 0;
  if (t2 > t1) pass(`self-preview currentTime advancing ${t1.toFixed(2)} -> ${t2.toFixed(2)} (live, not a frozen frame)`);
  else bad(`self-preview currentTime stuck at ${t1}`);

  const shares = (gs.tiles || []).filter((t) => t.kind === "screen");
  log(`simultaneous shares visible to the guest: ${shares.length} -> ${JSON.stringify(shares.map((t) => t.label))}`);
  if (shares.length >= 2) pass(`TWO simultaneous shares are on screen: ${shares.map((t) => t.label).join(" + ")}`);
  else bad(`only ${shares.length} share tile(s); cannot test focus switching`);

  // Focus one, then the other, measuring gains each time.
  const gains = () => gv.page.evaluate(() => window.__guest());
  // A share auto-takes the stage the first time it appears, and a tile click is
  // a TOGGLE — so clear focus first or clicking the already-focused tile just
  // exits theater and the assertion measures the wrong thing.
  const autoFocused = gs.focus;
  if (autoFocused) pass(`a share auto-took the stage on arrival (focus=${autoFocused})`);
  else bad("no share auto-focused when the first one appeared");
  for (const target of shares) {
    await gv.page.evaluate(() => window.__guest().focus && document.body.dispatchEvent(new KeyboardEvent("keydown")));
    const cur = await gains();
    if (cur.focus) { await gv.page.keyboard.press("Escape"); await sleep(600); }
    await gv.page.evaluate((k) => {
      const t = [...document.querySelectorAll("#tiles .tile")].find((n) => n.dataset.key === k) ||
        [...document.querySelectorAll("#tiles .tile")].find((n) => (n.querySelector(".lbl")?.textContent || "") === k);
      if (t) t.click();
    }, target.key);
    await sleep(1200);
    const s = await gains();
    const focused = (s.tiles || []).filter((t) => t.focused).map((t) => t.label);
    const remote = (s.audio || []);
    log(`focus="${target.label}" -> focused tiles ${JSON.stringify(focused)}; audio ${JSON.stringify(remote)}`);
    if (focused.length === 1 && focused[0] === target.label) pass(`focus switched to "${target.label}" (theater: exactly one focused tile)`);
    else bad(`focus did not land on "${target.label}": ${JSON.stringify(focused)}`);
    const voices = remote.filter((r) => !r.share);
    const otherShares = remote.filter((r) => r.share && r.streamId !== (target.streamId || ""));
    if (voices.length && voices.every((v) => v.gain === 1 && !v.muted)) pass(`voices stay at gain 1.00 while watching "${target.label}" (${JSON.stringify(voices.map((v) => v.gain))})`);
    else if (!voices.length) note(`no voice-classified remote stream while watching "${target.label}" (audio=${JSON.stringify(remote)})`);
    else bad(`a VOICE was attenuated: ${JSON.stringify(voices)}`);
    if (otherShares.length && otherShares.every((o) => o.gain < 1)) pass(`unfocused share audio attenuated to ${JSON.stringify(otherShares.map((o) => o.gain))} while watching "${target.label}"`);
    else if (!otherShares.length) note(`no other share carried audio while watching "${target.label}"`);
    else bad(`unfocused share NOT attenuated: ${JSON.stringify(otherShares)}`);
    await shot(gv.page, `F-theater-${target.label.replace(/\W+/g, "-")}.png`);
  }
  // Exit theater.
  await gv.page.keyboard.press("Escape");
  await sleep(1000);
  const exited = await gains();
  if (!exited.focus) pass("Escape exits theater (focus cleared)");
  else bad(`Escape did not exit theater: focus=${exited.focus}`);
  const after = (exited.audio || []).filter((r) => r.share);
  if (!after.length || after.every((r) => r.gain === 1)) pass(`share audio back to full volume after exiting theater: ${JSON.stringify(after.map((r) => r.gain))}`);
  else bad(`share stayed ducked after exiting theater: ${JSON.stringify(after)}`);
  await shot(gv.page, "G-theater-exited.png");

  // ---------- PHASE F: device pickers actually switch the track ----------
  await gv.page.click("#setBtn");
  await sleep(1200);
  await shot(gv.page, "H-guest-settings-desktop.png");
  const dev = await gv.page.evaluate(() => {
    const opts = (id) => [...document.querySelectorAll(`#${id} option`)].map((o) => ({ v: o.value, t: o.textContent }));
    return { mic: opts("selMic"), spk: opts("selSpk"), cam: opts("selCam") };
  });
  log("device pickers:", JSON.stringify(dev, null, 1));
  if (dev.mic.length && dev.spk.length && dev.cam.length) pass(`pickers enumerate: ${dev.mic.length} mic / ${dev.spk.length} output / ${dev.cam.length} camera, with real labels (e.g. "${dev.mic[0].t}")`);
  else bad(`pickers empty: ${JSON.stringify(dev)}`);

  const trackBefore = await gv.page.evaluate(async () => {
    const pc = (window.__pcs || [])[0];
    const s = pc?.getSenders().find((x) => x.track?.kind === "audio");
    return { label: s?.track?.label || "", id: s?.track?.id || "" };
  });
  if (dev.mic.length > 1) {
    const target = dev.mic[1].v;
    await gv.page.selectOption("#selMic", target);
    await sleep(3000);
    const trackAfter = await gv.page.evaluate(async () => {
      const pc = (window.__pcs || [])[0];
      const s = pc?.getSenders().find((x) => x.track?.kind === "audio");
      const set = s?.track?.getSettings?.() || {};
      return { label: s?.track?.label || "", id: s?.track?.id || "", deviceId: set.deviceId || "" };
    });
    log("mic track before:", JSON.stringify(trackBefore), "after:", JSON.stringify(trackAfter));
    if (trackAfter.id && trackAfter.id !== trackBefore.id) pass(`switching the mic REPLACED the outgoing track (${trackBefore.id.slice(0, 8)} -> ${trackAfter.id.slice(0, 8)}, deviceId=${trackAfter.deviceId.slice(0, 12)}, label="${trackAfter.label}")`);
    else bad(`mic switch did not change the sender's track: ${JSON.stringify({ trackBefore, trackAfter })}`);
  } else note(`only ${dev.mic.length} input device in this Chromium — cannot prove a switch changes the track`);

  if (dev.cam.length > 1) {
    // The settings sheet overlays the control row; leave it before using it.
    await gv.page.click("#sheetClose").catch(() => {});
    await sleep(600);
    await gv.page.click("#camBtn");
    await sleep(3000);
    const camBefore = await gv.page.evaluate(() => {
      const pc = (window.__pcs || [])[0];
      const v = pc?.getSenders().filter((x) => x.track?.kind === "video").map((x) => x.track.id) || [];
      return v;
    });
    await gv.page.click("#setBtn"); // reopen: the picker lives in the sheet
    await sleep(600);
    await gv.page.selectOption("#selCam", dev.cam[1].v);
    await sleep(3000);
    const camAfter = await gv.page.evaluate(() => {
      const pc = (window.__pcs || [])[0];
      return pc?.getSenders().filter((x) => x.track?.kind === "video").map((x) => ({ id: x.track.id, label: x.track.label, dev: x.track.getSettings?.().deviceId })) || [];
    });
    log("camera senders before:", JSON.stringify(camBefore), "after:", JSON.stringify(camAfter));
    if (camAfter.length && !camBefore.includes(camAfter[camAfter.length - 1].id)) pass(`switching the camera replaced the video track (label="${camAfter[camAfter.length - 1].label}")`);
    else note(`camera switch: senders ${JSON.stringify(camAfter)} (before ${JSON.stringify(camBefore)})`);
  }
  await gv.page.click("#sheetClose").catch(() => {});

  // ---------- PHASE G: the guest appears in the host's roster ----------
  // The presence tile shows initials, so look at names/titles too, not just
  // leaf text — an avatar reading "NA" is still the guest being present.
  const roster = await m.evaluate(() => {
    const hits = [];
    for (const n of document.querySelectorAll("*")) {
      const t = (n.getAttribute?.("title") || "") + " " + (n.getAttribute?.("aria-label") || "");
      if (/nadia/i.test(t)) hits.push(t.trim());
      if (!n.children.length && /nadia/i.test(n.textContent || "")) hits.push(n.textContent.trim());
    }
    return [...new Set(hits)];
  });
  log("member DOM mentioning the guest:", JSON.stringify(roster));
  if (roster.length) pass(`admitted guest is named in the host's UI: ${JSON.stringify(roster.slice(0, 3))}`);
  else bad("admitted guest does not appear anywhere in the host's UI");
  await shot(m, "I-member-with-guest.png");

  // ---------- PHASE H: mobile viewport ----------
  const gm = await newGuest(390, 844);
  await gm.page.goto(LINK);
  await gm.page.fill("#name", "Phone");
  await gm.page.click("#go");
  await gm.page.waitForFunction(() => (window.__rx || []).join("").includes('"waiting"'), null, { timeout: 20000 });
  await sleep(800);
  await shot(gm.page, "J-mobile-knocking.png");
  await m.waitForSelector(".knock-admit", { timeout: 15000 });
  await m.click(".knock-admit");
  const mobIn = await gm.page.waitForSelector("#room.on", { timeout: 20000 }).then(() => true).catch(() => false);
  if (mobIn) pass("second guest admitted on a 390x844 viewport");
  else bad("mobile guest never entered the room");
  await gm.page.click("#callBtn");
  await sleep(9000);
  await shot(gm.page, "K-mobile-in-call.png");
  const mobState = await gm.page.evaluate(() => window.__guest());
  log("mobile guest state:", JSON.stringify(mobState, null, 1));
  const mobRemote = (mobState.tiles || []).filter((t) => !t.self && t.hasVideo);
  if (mobRemote.length && mobRemote[0].w > 0) pass(`mobile guest is decoding the host's share (${mobRemote[0].w}x${mobRemote[0].h})`);
  else note(`mobile guest remote tiles: ${JSON.stringify(mobState.tiles)}`);
  await gm.page.click("#setBtn");
  await sleep(1000);
  await shot(gm.page, "L-mobile-settings.png");
  await gm.page.click("#sheetClose").catch(() => {});

  // ---------- PHASE I: eviction ends with words, not silence ----------
  // Straight through the RPC the roster's Disconnect uses, on the admitted
  // desktop guest, so the assertion is about the backend verb and not a
  // selector that could drift.
  // Is there a UI path at all? The app's only Disconnect lives in ChannelList's
  // person context menu, which renders for c.type === "voice" — and a meeting's
  // single channel is a TEXT channel. So check before trusting the claim.
  const rows = await m.$$(".vc-member");
  const localMutes = await m.$$(".local-mute");
  const tileDom = await m.evaluate(() => [...document.querySelectorAll('[class*="tile"], [class*="peer"], .name')]
    .slice(0, 20).map((n) => `${n.tagName}.${n.className}` + (n.textContent.trim().slice(0, 24) ? ` "${n.textContent.trim().slice(0, 24)}"` : "")));
  log("host voice-tile DOM:", JSON.stringify(tileDom, null, 1));
  log(`host UI: ${rows.length} .vc-member rows, ${localMutes.length} .local-mute buttons on voice tiles`);
  if (rows.length === 0) bad(`NO reachable eviction UI in a meeting: 0 .vc-member rows (the only Disconnect menu in the app needs a voice-type channel; a meeting's channel is text), and VoicePanel tiles offer only .local-mute (${localMutes.length} found), which silences the guest for the host alone and sends nothing`);
  else pass(`${rows.length} .vc-member rows exist, so the Disconnect context menu is reachable`);

  // The backend verb, exercised on the real fingerprint lifted off the host's
  // own event stream — so "the wire works, the button is missing" is provable
  // rather than asserted.
  // Nadia specifically: the refused Knocker's fingerprint is also in the log,
  // and disconnecting a session that already ended proves nothing.
  const fpr = await m.evaluate(() => {
    for (const raw of (window.__sse || [])) {
      const mt = raw.match(/guest:Nadia#[0-9a-f]{16}/);
      if (mt) return mt[0];
    }
    return null;
  });
  log("guest fingerprint seen by the host:", JSON.stringify(fpr));
  if (fpr) {
    await rpc("SignalCall", [CHANNEL, "disconnect", fpr, ""]);
    const told = await gv.page.waitForFunction(
      () => (window.__rx || []).join("").includes("removed you"), null, { timeout: 15000 }).then(() => true).catch(() => false);
    const gfr = await frames(gv.page);
    const endF = [...gfr].reverse().find((f) => f.type === "end");
    if (told && endF) pass(`evicted guest is told why: "${endF.reason}"`);
    else bad(`eviction produced no end reason: ${JSON.stringify(gfr.slice(-3))}`);
    await sleep(800);
    const banner = await gv.page.evaluate(() => document.body.innerText.slice(0, 400));
    log("guest page after eviction:", JSON.stringify(banner));
    await shot(gv.page, "M-guest-evicted.png");
    const composer = await gv.page.evaluate(() => {
      const i = document.getElementById("msg");
      return { disabled: !!i?.disabled, exists: !!i };
    });
    if (!composer.exists || composer.disabled) pass("composer disables itself after eviction");
    else bad("evicted guest can still type into the composer");
  } else bad("never saw a guest fingerprint on the host's event stream; could not exercise eviction");

  // ---------- summary ----------
  console.log("\n================ RESULT ================");
  for (const [k, v] of results) console.log(k.padEnd(5), v);
  const fails = results.filter(([k]) => k === "FAIL");
  console.log(`\n${results.filter(([k]) => k === "PASS").length} pass, ${fails.length} fail, ${results.filter(([k]) => k === "NOTE").length} notes`);
  await browser.close();
  process.exit(fails.length ? 2 : 0);
} catch (err) {
  await shot(m, "ZZ-fail-member.png");
  console.error("DRIVER ERROR:", err && err.stack ? err.stack : err);
  console.log("\n================ RESULT (incomplete) ================");
  for (const [k, v] of results) console.log(k.padEnd(5), v);
  await browser.close().catch(() => {});
  process.exit(3);
}
