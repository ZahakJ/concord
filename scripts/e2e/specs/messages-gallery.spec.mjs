// The conversation gallery (inspector → Media). It lists the whole history
// this device holds, not only what the feed has loaded: photos and videos as
// a grid with month headings, files as a list. Tapping a photo opens it fitted
// with "Go to message"; right-click (long-press on a phone) offers Go to
// message, Save and Copy image; a message deleted while the grid is open
// leaves it at once. In a DM the Media button opens the same tab, on the
// desktop and on a phone. A last test measures opening and flinging a grid of
// 5,000 items with the CPU throttled 4x (direction only: the owner's phone is
// the measurement that counts).
//
// E2E_SHOTS_DIR, when set, receives the captures (desktop 1280, phone 390).
import fs from "node:fs";
import path from "node:path";
import zlib from "node:zlib";
import { test, sleep } from "../harness.mjs";

const SHOTS = process.env.E2E_SHOTS_DIR || "";
async function capture(app, name) {
  if (!SHOTS) return;
  fs.mkdirSync(SHOTS, { recursive: true });
  await app.page.screenshot({ path: path.join(SHOTS, `${name}.png`) });
}

const CRC = Array.from({ length: 256 }, (_, n) => {
  let c = n;
  for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
  return c >>> 0;
});
function crc32(buf) {
  let c = 0xffffffff;
  for (const b of buf) c = CRC[(c ^ b) & 0xff] ^ (c >>> 8);
  return (c ^ 0xffffffff) >>> 0;
}
function chunk(type, data) {
  const len = Buffer.alloc(4);
  len.writeUInt32BE(data.length);
  const td = Buffer.concat([Buffer.from(type, "ascii"), data]);
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(crc32(td));
  return Buffer.concat([len, td, crc]);
}
// A solid-colour PNG, so each picture is distinct and tiny.
function png(w, h, rgb) {
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(w, 0);
  ihdr.writeUInt32BE(h, 4);
  ihdr[8] = 8;
  ihdr[9] = 2;
  const raw = Buffer.alloc((w * 3 + 1) * h);
  for (let y = 0; y < h; y++) for (let x = 0; x < w; x++) raw.set(rgb, y * (w * 3 + 1) + 1 + x * 3);
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk("IHDR", ihdr),
    chunk("IDAT", zlib.deflateSync(raw)),
    chunk("IEND", Buffer.alloc(0)),
  ]);
}
const dataUrl = (buf, mime = "image/png") => `data:${mime};base64,${buf.toString("base64")}`;
const COLOURS = [[200, 60, 60], [60, 160, 80], [60, 90, 200], [220, 180, 40]];

// Sends a captioned picture over RPC and returns the stored message.
async function sendPicture(peer, channelId, i, w = 320, h = 240) {
  await peer.rpc("SendAttachment", channelId, dataUrl(png(w, h, COLOURS[i % COLOURS.length])), w, h, "", false, "", "", `pic ${i}`, "");
  return peer.waitForMessage(channelId, (m) => m.content.includes(`pic ${i}`));
}

const tiles = (page) => page.locator('[data-testid="gallery-scroll"] .tile');

async function openMediaTab(app) {
  const page = app.page;
  if (!(await page.locator('aside[aria-label="Inspector"]').count())) {
    await page.getByRole("button", { name: "Members, pins and files" }).click();
  }
  await page.locator('aside[aria-label="Inspector"]').getByRole("tab", { name: "Media" }).click();
  await page.locator('[data-testid="gallery-scroll"]').waitFor();
}

test("desktop: the Media tab lists every picture and file, opens one fitted, and goes to its message", async ({ world, assert, step, waitFor }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Gallery", members: [alice, bashir] });
  const ch = g.allChannels[0];

  step("Alice sends three pictures and a PDF; then enough chat that the first picture is far above the loaded feed");
  const pics = [];
  for (let i = 1; i <= 3; i++) pics.push(await sendPicture(alice, ch.id, i));
  await alice.rpc("SendFile", ch.id, dataUrl(Buffer.from("%PDF-1.4 tiny"), "application/pdf"), "minutes.pdf", "", "the minutes", "");
  await alice.waitForMessage(ch.id, (m) => m.content.includes("the minutes"));
  for (const p of pics) await bashir.waitForMessage(ch.id, (m) => m.id === p.id, { timeout: 60000 });
  await bashir.waitForMessage(ch.id, (m) => m.content.includes("the minutes"), { timeout: 60000 });
  for (let i = 0; i < 260; i++) await alice.sendRaw(ch.id, `chatter ${i}`);
  await bashir.waitForMessage(ch.id, "chatter 259", { timeout: 90000 });

  const b = await bashir.open({ viewport: { width: 1280, height: 800 } });
  await b.openChannel(ch.name, "Gallery");
  const page = b.page;
  assert.equal(await page.locator(".msg[data-msg-id]", { hasText: "pic 1" }).count(), 0, "the first picture is not in the loaded feed");

  step("Media → Photos and videos: three tiles, newest first, under a month heading");
  await openMediaTab(b);
  await waitFor(async () => (await tiles(page).count()) === 3, { message: "three tiles" });
  const order = await tiles(page).evaluateAll((els) => els.map((e) => e.dataset.msg));
  assert.deepEqual(order, [pics[2].id, pics[1].id, pics[0].id], "newest first");
  assert.ok(await page.locator(".gal-head").first().isVisible(), "a month heading");
  await capture(b, "desktop-1280-grid");

  step("Files: the PDF, with its kind and size");
  await page.getByRole("radio", { name: "Files" }).click();
  const frow = page.locator(".frow");
  await frow.first().waitFor();
  assert.equal(await frow.count(), 1);
  assert.match(await frow.first().innerText(), /minutes\.pdf[\s\S]*PDF/);
  await capture(b, "desktop-1280-files");
  await page.getByRole("radio", { name: "Photos and videos" }).click();
  await waitFor(async () => (await tiles(page).count()) === 3, { message: "back to three tiles" });

  step("a tap opens the oldest picture fitted inside the window");
  await tiles(page).nth(2).click();
  const img = page.locator('[role="dialog"][aria-label="Image viewer"] img');
  await img.waitFor({ state: "visible", timeout: 30000 });
  await page.waitForTimeout(400);
  const box = await img.evaluate((el) => {
    const r = el.getBoundingClientRect();
    return { l: r.left, t: r.top, r: r.right, b: r.bottom, nw: el.naturalWidth, w: r.width };
  });
  assert.ok(box.nw === 320 && box.l >= 0 && box.t >= 0 && box.r <= 1280 && box.b <= 800, `fitted: ${JSON.stringify(box)}`);
  assert.ok(box.w <= 320.5, "never drawn larger than it is");
  await capture(b, "desktop-1280-viewer");

  step("Go to message closes the viewer and brings the message into the feed");
  await page.getByRole("button", { name: "Go to message" }).click();
  await page.locator('[role="dialog"][aria-label="Image viewer"]').waitFor({ state: "detached" });
  await waitFor(async () => (await page.locator(`.msg[data-msg-id="${pics[0].id}"]`).count()) === 1, { timeout: 30000, message: "the message to be loaded" });
  await waitFor(
    async () =>
      page.locator(`.msg[data-msg-id="${pics[0].id}"] .body`).evaluate((el) => {
        const r = el.getBoundingClientRect();
        const f = document.querySelector(".feed").getBoundingClientRect();
        return r.bottom > f.top && r.top < f.bottom;
      }),
    { message: "the message to be on screen" },
  );

  step("the tile now draws the picture the viewer decrypted; right-click offers the three actions");
  await waitFor(async () => (await tiles(page).nth(2).locator("img").count()) === 1, { message: "the tile to show the picture" });
  await tiles(page).nth(0).click({ button: "right" });
  for (const label of ["Go to message", "Save", "Copy image"]) {
    assert.ok(await page.getByRole("menuitem", { name: label }).isVisible(), `menu has ${label}`);
  }
  await page.keyboard.press("Escape");

  step("Alice deletes the newest picture: it leaves Bashir's open grid");
  await alice.rpc("DeleteMessage", ch.id, pics[2].id);
  await waitFor(async () => (await tiles(page).count()) === 2, { timeout: 30000, message: "the deleted picture to leave the grid" });
  assert.equal(await page.locator(`.tile[data-msg="${pics[2].id}"]`).count(), 0);

  step("Bashir hides one for himself: it leaves too");
  await bashir.rpc("HideMessage", ch.id, pics[1].id);
  await waitFor(async () => (await tiles(page).count()) === 1, { timeout: 15000, message: "the hidden picture to leave" });
  assert.equal(b.errors.length, 0, `page errors: ${b.errors.join(" | ")}`);
});

test("phone: a DM's Media page, the viewer and Go to message", async ({ world, assert, step, waitFor }) => {
  const [alice, bashir] = await world.peers(2);
  await world.guild({ name: "Shared", members: [alice, bashir] });
  const dm = await world.dm(alice, bashir);
  const p = await sendPicture(alice, dm.channelId, 1, 600, 900);
  await bashir.waitForMessage(dm.channelId, (m) => m.id === p.id, { timeout: 60000 });

  const b = await bashir.open({ viewport: "phone" });
  await b.openDM("Alice");
  const page = b.page;
  step("the DM's top bar has Media and files");
  await page.getByRole("button", { name: "Media and files" }).click();
  await waitFor(async () => (await tiles(page).count()) === 1, { message: "the DM's picture" });
  // The feed has the picture decrypted already (or is about to): the tile
  // draws it from the renderer's cache, whichever finished first.
  await waitFor(async () => (await tiles(page).first().locator("img").count()) === 1, { timeout: 30000, message: "the tile to draw the feed's picture" });
  const tab = await page.locator('aside[aria-label="Inspector"] [role="tab"]').allInnerTexts();
  assert.deepEqual(tab.map((t) => t.trim()), ["Media"], "a DM's inspector is its media alone");
  const w = await tiles(page).first().evaluate((el) => el.getBoundingClientRect().width);
  assert.ok(w > 100 && w < 140, `three across a 390 px phone (tile ${w}px)`);
  await page.waitForTimeout(500); // the page's slide-in
  await capture(b, "phone-390-grid");

  step("tap: the viewer, fitted to the phone");
  await tiles(page).first().tap();
  const img = page.locator('[role="dialog"][aria-label="Image viewer"] img');
  await img.waitFor({ state: "visible", timeout: 30000 });
  await page.waitForTimeout(400);
  const r = await img.evaluate((el) => el.getBoundingClientRect().toJSON());
  assert.ok(r.left >= 0 && r.right <= 390.5 && r.top >= 0, `fitted: ${JSON.stringify(r)}`);
  await capture(b, "phone-390-viewer");

  step("Go to message: back in the conversation, the Media page closed");
  await page.getByRole("button", { name: "Go to message" }).tap();
  await page.locator('[role="dialog"][aria-label="Image viewer"]').waitFor({ state: "detached" });
  await waitFor(async () => !(await page.locator('aside[aria-label="Members"][aria-hidden="false"]').count()), { message: "the page to close" });
  assert.equal(b.errors.length, 0, `page errors: ${b.errors.join(" | ")}`);
});

// ---- performance (direction only) ----------------------------------------
const pct = (a, q) => {
  const s = [...a].sort((x, y) => x - y);
  return s[Math.min(s.length - 1, Math.floor((q / 100) * s.length))];
};
const hex = (n) => n.toString(16).padStart(64, "0");
const KEYS = "K".repeat(75);
const token = (i) =>
  i % 10 === 9
    ? `[file](concord://file/v1/${hex(i)}/${KEYS}/${1000 + i}/dmlkZW8vbXA0/Y2xpcC5tcDQ)`
    : `![image](concord://attach/v1/${hex(i)}/${KEYS}/jpeg/${400 + (i % 5) * 100}x${300 + (i % 3) * 100})`;

test(
  "performance: a 5,000-item grid opens and flings within its frame budget (CPU 4x slower)",
  async ({ world, assert, step, log, waitFor }) => {
    const [alice] = await world.peers(1);
    const g = await world.guild({ name: "Big", members: [alice] });
    const ch = g.allChannels[0];
    const N = Number(process.env.GALLERY_PERF_N || 5000);
    step(`seed ${N} media messages over RPC`);
    const t0 = Date.now();
    for (let i = 0; i < N; i++) await alice.sendRaw(ch.id, token(i));
    await waitFor(async () => (await alice.rpc("MediaPage", ch.id, 1, "", 2000)).items.length >= Math.min(2000, N * 0.9), { timeout: 120000, message: "the seeded items to be indexed" });
    log(`seeded in ${Date.now() - t0} ms`);

    const a = await alice.open({ viewport: { width: 1280, height: 800 } });
    await a.openChannel(ch.name, "Big");
    const page = a.page;
    const cdp = await a.ctx.newCDPSession(page);
    await cdp.send("Emulation.setCPUThrottlingRate", { rate: 4 });
    // The inspector may already be docked open on a wide window.
    if (!(await page.locator('aside[aria-label="Inspector"]').count())) {
      await page.getByRole("button", { name: "Members, pins and files" }).click();
    }
    await page.locator('aside[aria-label="Inspector"]').getByRole("tab", { name: "Members" }).click();
    await page.locator('aside[aria-label="Inspector"]').getByRole("tab", { name: "Media" }).waitFor();
    await page.waitForTimeout(500); // the inspector's entrance
    const heap0 = await page.evaluate(() => performance.memory?.usedJSHeapSize || 0);

    step("open: tap on the Media tab to the first frame with tiles");
    const openMs = await page.evaluate(
      () =>
        new Promise((res) => {
          const tabEl = [...document.querySelectorAll('aside[aria-label="Inspector"] [role="tab"]')].find((t) => t.textContent.trim() === "Media");
          const t0 = performance.now();
          tabEl.click();
          const tick = () => {
            if (document.querySelectorAll('[data-testid="gallery-scroll"] .tile').length > 0) return res(performance.now() - t0);
            if (performance.now() - t0 > 20000) return res(-1);
            requestAnimationFrame(tick);
          };
          requestAnimationFrame(tick);
        }),
    );

    // Two scrolls. "steady" is a hand's fling: 100 px a frame (6,000 px/s at
    // 60 Hz) for 300 frames. "jump" goes top to bottom at nine tenths of a
    // screen a frame, so every mounted tile is replaced on every frame and
    // every page is fetched on the way: the worst case, not a gesture.
    step("steady scroll, then the whole grid a screen per frame");
    const runScroll = (stepPx, maxFrames) =>
      page.evaluate(
        async ({ stepPx, maxFrames }) => {
          const el = document.querySelector('[data-testid="gallery-scroll"]');
          const deltas = [];
          let maxTiles = 0;
          const frame = () => new Promise((r) => requestAnimationFrame(r));
          await frame();
          let last = performance.now();
          const start = last;
          for (let i = 0; i < maxFrames; i++) {
            el.scrollTop += stepPx || el.clientHeight * 0.9;
            await frame();
            const now = performance.now();
            deltas.push(now - last);
            last = now;
            maxTiles = Math.max(maxTiles, el.querySelectorAll(".tile").length);
            if (!stepPx && el.scrollTop + el.clientHeight >= el.scrollHeight - 2 && !document.querySelector(".gal-more")) {
              await frame();
              await frame();
              if (el.scrollTop + el.clientHeight >= el.scrollHeight - 2) break;
            }
          }
          return { deltas, maxTiles, ms: performance.now() - start, height: el.querySelector(".gal-space").style.height };
        },
        { stepPx, maxFrames },
      );
    const summary = (r) => {
      const d = r.deltas.slice(1);
      return {
        frames: d.length,
        p50: +pct(d, 50).toFixed(1),
        p95: +pct(d, 95).toFixed(1),
        max: +Math.max(...d).toFixed(1),
        over16_7pct: +((100 * d.filter((x) => x > 16.7).length) / d.length).toFixed(1),
        maxTiles: r.maxTiles,
        ms: Math.round(r.ms),
      };
    };
    const steady = await runScroll(100, 300);
    await page.evaluate(() => (document.querySelector('[data-testid="gallery-scroll"]').scrollTop = 0));
    const fling = await runScroll(0, 20000);
    const heap1 = await page.evaluate(() => performance.memory?.usedJSHeapSize || 0);
    await cdp.send("Emulation.setCPUThrottlingRate", { rate: 1 });
    // The same two scrolls unthrottled, for scale.
    await page.evaluate(() => (document.querySelector('[data-testid="gallery-scroll"]').scrollTop = 0));
    const steady1 = await runScroll(100, 300);
    await page.evaluate(() => (document.querySelector('[data-testid="gallery-scroll"]').scrollTop = 0));
    const jump1 = await runScroll(0, 20000);
    const result = {
      items: N,
      cpuThrottle: 4,
      openMs: Math.round(openMs),
      steady: summary(steady),
      jump: { ...summary(fling), gridHeight: fling.height },
      heapGrowthMB: +((heap1 - heap0) / 1048576).toFixed(1),
      unthrottled: { steady: summary(steady1), jump: summary(jump1) },
    };
    log(`GALLERY-PERF ${JSON.stringify(result)}`);
    if (SHOTS) fs.writeFileSync(path.join(SHOTS, "perf.json"), JSON.stringify(result, null, 1));
    assert.ok(openMs > 0, "the grid opened");
    assert.ok(Math.max(steady.maxTiles, fling.maxTiles) <= 90, `at most 90 tiles mounted at once (saw ${Math.max(steady.maxTiles, fling.maxTiles)})`);
    // Direction only under emulation; generous enough not to flake on a loaded
    // box, tight enough to catch a grid that renders every tile.
    assert.ok(openMs < 2500, `opening took ${openMs} ms`);
    assert.ok(result.steady.p95 < 100, `steady-scroll p95 frame ${result.steady.p95} ms`);
    await sleep(10);
  },
  { timeout: 30 * 60 * 1000 },
);
