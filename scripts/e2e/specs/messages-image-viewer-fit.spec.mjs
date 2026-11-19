// The image viewer opens a picture whole: a phone screenshot (1080x2400) and a
// wide panorama (4000x600) each fit inside a 1440x900 window, never drawn
// larger than they are. Double-click toggles fit and 100%, a drag is clamped,
// the wheel zooms by how far it scrolls, a resize keeps a zoomed picture
// covering the window, a click zooms one step, focus stays inside the viewer
// and returns to the thumbnail, Esc and a backdrop click close. In the feed, the
// screenshot's thumbnail stays within the thumbnail height cap.
import fs from "node:fs";
import path from "node:path";
import zlib from "node:zlib";
import { test } from "../harness.mjs";

// A minimal PNG writer, so the fixtures are made here rather than committed:
// horizontal bands of colour, which compress to almost nothing.
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
function bandsPng(w, h) {
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(w, 0);
  ihdr.writeUInt32BE(h, 4);
  ihdr[8] = 8; // bit depth
  ihdr[9] = 2; // RGB
  const raw = Buffer.alloc((w * 3 + 1) * h);
  for (let y = 0; y < h; y++) {
    const band = Math.floor((y / h) * 6);
    const rgb = [[200, 60, 60], [60, 160, 80], [60, 90, 200], [220, 180, 40], [150, 70, 180], [40, 170, 170]][band];
    const row = y * (w * 3 + 1);
    for (let x = 0; x < w; x++) raw.set(rgb, row + 1 + x * 3);
  }
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk("IHDR", ihdr),
    chunk("IDAT", zlib.deflateSync(raw)),
    chunk("IEND", Buffer.alloc(0)),
  ]);
}

// The feed's thumbnail height cap (Attachment.svelte, MAXH).
const THUMB_MAX_H = 280;

test("the image viewer opens tall and wide pictures fitted inside the window", async ({ world, assert, step, waitFor }) => {
  const [alice] = await world.peers(1);
  const g = await world.guild({ name: "Pictures", members: [alice] });
  const ch = g.allChannels[0];
  const a = await alice.open({ viewport: "desktop" });
  await a.openChannel(ch.name, "Pictures");
  const page = a.page;
  const vp = page.viewportSize();

  const frames = page.locator(".msg[data-msg-id] .frame.done img");
  for (const [name, w, h] of [
    ["screenshot", 1080, 2400],
    ["panorama", 4000, 600],
  ]) {
    step(`send the ${w}x${h} ${name}`);
    const file = path.join(world.dir, `${name}.png`);
    fs.writeFileSync(file, bandsPng(w, h));
    const before = await frames.count();
    await a.attach(file);
    await waitFor(async () => (await page.locator(".attach-tray .att-chip").count()) === 1, { message: `the ${name} to be staged` });
    await a.composer().press("Enter");
    await waitFor(async () => (await frames.count()) === before + 1, { timeout: 45000, message: `the ${name} to land in the feed` });
    const thumb = frames.nth(before);
    await thumb.scrollIntoViewIfNeeded();

    if (name === "screenshot") {
      const box = await thumb.boundingBox();
      assert.ok(box.height <= THUMB_MAX_H + 0.5, `the screenshot's feed thumbnail is ${box.height}px tall, over the ${THUMB_MAX_H}px cap`);
    }

    step(`open the ${name} in the viewer`);
    await thumb.click();
    const big = page.locator(".lightbox img");
    await big.waitFor({ state: "visible" });
    // Measured once the opening fade and any transform transition are over.
    await page.waitForTimeout(600);
    const m = await big.evaluate((el) => {
      const r = el.getBoundingClientRect();
      return { left: r.left, top: r.top, right: r.right, bottom: r.bottom, w: r.width, h: r.height, nw: el.naturalWidth, nh: el.naturalHeight };
    });
    const tag = `${name}: ${JSON.stringify(m)}`;
    assert.ok(m.nw > 0 && m.nh > 0, `the viewer's picture decoded (${tag})`);
    assert.ok(m.left >= -0.5 && m.top >= -0.5, `the picture starts inside the window (${tag})`);
    assert.ok(m.right <= vp.width + 0.5 && m.bottom <= vp.height + 0.5, `the picture ends inside the window (${tag})`);
    assert.ok(m.w <= m.nw + 0.5 && m.h <= m.nh + 0.5, `the picture is not drawn larger than its natural size (${tag})`);
    assert.ok(Math.abs(m.w / m.h - m.nw / m.nh) < 0.02, `the picture keeps its shape (${tag})`);

    if (name === "screenshot") {
      const boxOf = () => big.evaluate((el) => {
        const r = el.getBoundingClientRect();
        return { left: r.left, top: r.top, right: r.right, bottom: r.bottom, w: r.width, h: r.height };
      });
      const inViewer = () => page.evaluate(() => !!document.activeElement?.closest(".lightbox"));
      assert.ok(await inViewer(), "focus is inside the viewer when it opens");

      step("double-click shows 100%, a second double-click fits again");
      const cx = (m.left + m.right) / 2;
      const cy = (m.top + m.bottom) / 2;
      await page.mouse.dblclick(cx, cy);
      await page.waitForTimeout(500);
      const full = await boxOf();
      assert.ok(Math.abs(full.w - m.nw) < 1 && Math.abs(full.h - m.nh) < 1, `double-click shows the natural size: ${JSON.stringify(full)}`);
      assert.ok(full.top <= 0.5 && full.bottom >= vp.height - 0.5, `at 100% the tall picture still covers the window top to bottom: ${JSON.stringify(full)}`);

      step("a drag far past the edge stops with the picture still covering the window");
      await page.mouse.move(cx, cy);
      await page.mouse.down();
      await page.mouse.move(cx, cy + 3000, { steps: 10 });
      await page.mouse.up();
      await page.waitForTimeout(500);
      const dragged = await boxOf();
      assert.ok(dragged.top <= 0.5 && dragged.bottom >= vp.height - 0.5, `the drag is clamped: ${JSON.stringify(dragged)}`);

      await page.mouse.dblclick(cx, Math.min(cy, vp.height - 50));
      await page.waitForTimeout(500);
      const back = await boxOf();
      assert.ok(Math.abs(back.w - m.w) < 1 && Math.abs(back.h - m.h) < 1, `a second double-click fits again: ${JSON.stringify(back)}`);

      step("the wheel zooms in a step toward the pointer");
      await page.mouse.move(cx, cy);
      await page.mouse.wheel(0, -100);
      await page.waitForTimeout(500);
      const wheeled = await boxOf();
      assert.ok(wheeled.h > back.h * 1.1, `the wheel zoomed in: ${JSON.stringify(wheeled)}`);

      step("a touchpad's stream of small wheel events adds up instead of stepping on each one");
      await page.keyboard.press("0");
      await page.waitForTimeout(400);
      for (let i = 0; i < 40; i++) await page.mouse.wheel(0, -5);
      await page.waitForTimeout(500);
      const pad = await boxOf();
      // 200px of scroll is two steps (1.25 x 1.25), not forty.
      assert.ok(pad.h > back.h * 1.1 && pad.h < back.h * 2, `forty 5px wheel events zoomed about two steps: ${JSON.stringify(pad)} from ${JSON.stringify(back)}`);

      step("a window resize while zoomed keeps the picture covering the window");
      await page.mouse.move(cx, cy);
      await page.mouse.down();
      await page.mouse.move(cx, cy - 3000, { steps: 10 });
      await page.mouse.up();
      await page.waitForTimeout(400);
      const small = { width: 1000, height: 600 };
      await page.setViewportSize(small);
      await page.waitForTimeout(600);
      const resized = await boxOf();
      assert.ok(resized.top <= 0.5 && resized.bottom >= small.height - 0.5, `after the resize the zoomed picture still covers the window: ${JSON.stringify(resized)}`);
      await page.setViewportSize(vp);
      await page.waitForTimeout(400);

      step("a single click on the picture zooms in one step");
      await page.keyboard.press("0");
      await page.waitForTimeout(400);
      const fitted = await boxOf();
      await page.mouse.click((fitted.left + fitted.right) / 2, (fitted.top + fitted.bottom) / 2);
      await page.waitForTimeout(700);
      const clicked = await boxOf();
      assert.ok(Math.abs(clicked.h / fitted.h - 2) < 0.02, `a click doubled the zoom: ${JSON.stringify(clicked)} from ${JSON.stringify(fitted)}`);
      await page.keyboard.press("0");

      step("Tab and Shift+Tab keep focus inside the viewer");
      for (const key of ["Tab", "Tab", "Tab", "Tab", "Shift+Tab", "Shift+Tab", "Shift+Tab", "Shift+Tab", "Shift+Tab"]) {
        await page.keyboard.press(key);
        assert.ok(await inViewer(), `focus is still inside the viewer after ${key}`);
      }

      step("Esc closes the viewer and focus returns to the thumbnail");
      await page.keyboard.press("Escape");
      await page.locator(".lightbox").waitFor({ state: "detached" });
      assert.ok(await thumb.evaluate((el) => document.activeElement === el.closest(".frame")), "focus is back on the thumbnail");
    } else {
      step("a click on the backdrop closes the viewer");
      await page.mouse.click(vp.width / 2, Math.max(5, m.top / 2));
    }
    await page.locator(".lightbox").waitFor({ state: "detached" });
  }
});

test("phone: a tapped screenshot opens fitted inside the screen", async ({ world, assert, waitFor }) => {
  const [alice] = await world.peers(1);
  const g = await world.guild({ name: "Pictures", members: [alice] });
  const ch = g.allChannels[0];
  const a = await alice.open({ viewport: "phone" });
  await a.openChannel(ch.name, "Pictures");
  await a.settled();
  const page = a.page;
  const vp = page.viewportSize();
  const file = path.join(world.dir, "screenshot.png");
  fs.writeFileSync(file, bandsPng(1080, 2400));
  await a.attach(file);
  await waitFor(async () => (await page.locator(".attach-tray .att-chip").count()) === 1, { message: "the screenshot to be staged" });
  await page.getByRole("button", { name: "Send", exact: true }).tap();
  const thumb = page.locator(".msg[data-msg-id] .frame.done img").last();
  await thumb.waitFor({ state: "visible", timeout: 45000 });
  await thumb.tap();
  const big = page.locator(".lightbox img");
  await big.waitFor({ state: "visible" });
  await page.waitForTimeout(600);
  const m = await big.evaluate((el) => {
    const r = el.getBoundingClientRect();
    return { left: r.left, top: r.top, right: r.right, bottom: r.bottom, w: r.width, h: r.height, nw: el.naturalWidth, nh: el.naturalHeight };
  });
  assert.ok(m.left >= -0.5 && m.top >= -0.5 && m.right <= vp.width + 0.5 && m.bottom <= vp.height + 0.5, `inside the phone screen: ${JSON.stringify(m)}`);
  assert.ok(m.w <= m.nw + 0.5 && m.h <= m.nh + 0.5, `not upscaled: ${JSON.stringify(m)}`);
});
