// "Recent" in the attach menu: the photos and files this account sent or
// received lately, in any conversation, offered at the top of the desktop +
// menu and the phone's + sheet. A pick is staged like a file from disk and
// sent sealed again: the second conversation gets a new blob under a new key,
// never the first conversation's reference, so its members can open what was
// sent to them and still cannot fetch the original.
//
// Carol shares guild One with Alice; Bashir shares guild Two with Alice and
// is not in One.
//
// E2E_SHOTS_DIR, when set, receives the captures (desktop 1440, phone 390).
import fs from "node:fs";
import path from "node:path";
import zlib from "node:zlib";
import { test } from "../harness.mjs";

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
const PDF = Buffer.from("%PDF-1.4 the minutes of the last meeting");
const TOKEN = /concord:\/\/(attach|file)\/v[12]\/([0-9a-f]{64})\/([A-Za-z0-9_-]{75})\//g;
const tokens = (content) => [...content.matchAll(TOKEN)].map((m) => ({ type: m[1], blob: m[2], key: m[3] }));

// Carol sends a picture and a PDF in guild One; returns their tokens once
// Alice holds both.
async function carolSends(world, alice, carol) {
  const one = await world.guild({ name: "One", members: [carol, alice] });
  const two = await world.guild({ name: "Two", members: [alice, world.bashir] });
  const ch1 = one.allChannels[0];
  const pic = png(240, 160, [200, 60, 60]);
  await carol.rpc("SendAttachment", ch1.id, dataUrl(pic), 240, 160, "", false, "", "", "red square", "");
  await carol.rpc("SendFile", ch1.id, dataUrl(PDF, "application/pdf"), "minutes.pdf", "", "the minutes", "");
  const pm = await alice.waitForMessage(ch1.id, (m) => m.content.includes("red square"), { timeout: 60000 });
  const fm = await alice.waitForMessage(ch1.id, (m) => m.content.includes("the minutes"), { timeout: 60000 });
  return { one, two, ch1, pic, picTok: tokens(pm.content)[0], pdfTok: tokens(fm.content)[0], pdfMsg: fm };
}

// Bashir opens what Alice re-sent, and cannot open the original through Two.
async function checkResent(assert, bashir, ch2, msg, src, expect) {
  const got = tokens(msg.content);
  assert.equal(got.length, 1, "one attachment in the re-sent message");
  const t = got[0];
  assert.notEqual(t.blob, src.blob, "a new blob, not the first conversation's");
  assert.notEqual(t.key, src.key, "a new key, not the first conversation's");
  const opened = t.type === "attach" ? await bashir.rpc("FetchAttachment", ch2, t.blob, t.key, "png") : await bashir.rpc("FetchFile", ch2, t.blob, t.key, "application/pdf");
  assert.equal(opened, expect, "Bashir opens the bytes Carol sent");
  let denied = false;
  try {
    await bashir.rpc(t.type === "attach" ? "FetchAttachment" : "FetchFile", ch2, src.blob, src.key, t.type === "attach" ? "png" : "application/pdf");
  } catch {
    denied = true;
  }
  assert.ok(denied, "the original blob is not served to Bashir through guild Two");
}

test("desktop: + menu offers Recent; a pick is staged, sent sealed again, and opens for the new audience only", async ({ world, assert, step, waitFor }) => {
  const [alice, bashir, carol] = await world.peers(3);
  world.bashir = bashir;

  step("nothing sent or received yet: the + menu has no Recent row");
  const quiet = await world.guild({ name: "Quiet", members: [alice] });
  const a = await alice.open({ viewport: "desktop" });
  await a.openChannel(quiet.allChannels[0].name, "Quiet");
  await a.page.getByRole("button", { name: "Add to your message" }).click();
  await a.page.locator(".menu .menu-item").first().waitFor();
  await a.page.waitForTimeout(800);
  assert.equal(await a.page.locator(".recent").count(), 0, "no Recent row when there is nothing recent");
  await a.page.keyboard.press("Escape");

  const { two, pic, picTok, pdfTok, pdfMsg, ch1 } = await carolSends(world, alice, carol);
  const ch2 = two.allChannels[0];

  step("in guild Two, + shows Carol's picture and PDF under Recent, newest first");
  await a.openChannel(ch2.name, "Two");
  await a.page.getByRole("button", { name: "Add to your message" }).click();
  await a.page.locator(".menu .menu-item").first().waitFor();
  await a.page.waitForTimeout(700);
  await capture(a, "desktop-menu");
  const tiles = a.page.locator(".menu .recent .recent-tile");
  await waitFor(async () => (await tiles.count()) === 2, { message: "two Recent tiles" });
  const labels = await tiles.evaluateAll((els) => els.map((e) => e.getAttribute("aria-label")));
  assert.match(labels[0], /^Add minutes\.pdf again/, "the PDF first (newest)");
  assert.match(labels[1], /^Add photo\.png again/, "then the picture");
  await waitFor(async () => (await tiles.nth(1).locator("img").count()) === 1, { message: "the picture's tile to draw it" });
  const box = await a.page.locator(".menu").evaluate((el) => {
    const r = el.getBoundingClientRect();
    return { top: r.top, bottom: r.bottom };
  });
  const trig = await a.page.getByRole("button", { name: "Add to your message" }).evaluate((el) => el.getBoundingClientRect().top);
  assert.ok(box.bottom <= trig, `the menu stays above its trigger after Recent arrives (${JSON.stringify(box)} vs ${trig})`);
  await capture(a, "desktop-menu-recent");

  step("click the picture: it is staged like a picked file; send it");
  await tiles.nth(1).click();
  await waitFor(async () => (await a.page.locator(".attach-tray .att-chip").count()) === 1, { message: "the picture to be staged" });
  await waitFor(async () => (await a.page.locator(".attach-tray .att-chip img").count()) === 1, { message: "the staged chip to show the picture" });
  await capture(a, "desktop-staged");
  await a.send("again for Two");
  const msg = await bashir.waitForMessage(ch2.id, (m) => m.content.includes("again for Two"), { timeout: 60000 });
  await checkResent(assert, bashir, ch2.id, msg, picTok, dataUrl(pic));

  step("Alice hides the PDF for herself: it leaves Recent");
  await alice.rpc("HideMessage", ch1.id, pdfMsg.id);
  await a.page.getByRole("button", { name: "Add to your message" }).click();
  await a.page.locator(".menu .menu-item").first().waitFor();
  await waitFor(async () => {
    const l = await tiles.evaluateAll((els) => els.map((e) => e.getAttribute("aria-label")));
    return l.length >= 1 && !l.some((x) => x.startsWith("Add minutes.pdf"));
  }, { message: "the hidden PDF to leave Recent" });
  await a.page.keyboard.press("Escape");
  assert.ok(pdfTok.blob, "the PDF had a token");
  assert.equal(a.errors.length, 0, `page errors: ${a.errors.join(" | ")}`);
});

test("phone: the + sheet's Recent row stages several picks, and both go out sealed again", async ({ world, assert, step, waitFor }) => {
  const [alice, bashir, carol] = await world.peers(3);
  world.bashir = bashir;
  const { two, pic, picTok, pdfTok } = await carolSends(world, alice, carol);
  const ch2 = two.allChannels[0];

  const a = await alice.open({ viewport: "phone" });
  await a.openChannel(ch2.name, "Two");
  await a.settled();
  const page = a.page;

  step("tap +: the sheet opens with Recent above the tiles");
  await page.getByRole("button", { name: "Attach a file, GIF, poll, or more" }).tap();
  await page.locator(".sheet-grid").waitFor();
  await page.waitForTimeout(700);
  await capture(a, "phone-sheet");
  const tiles = page.locator(".recent .recent-tile");
  await waitFor(async () => (await tiles.count()) === 2, { message: "two Recent tiles" });
  const r = await tiles.first().evaluate((el) => el.getBoundingClientRect());
  assert.ok(r.width >= 44 && r.height >= 44, `finger-sized tiles (${r.width}x${r.height})`);
  await waitFor(async () => (await tiles.nth(1).locator("img").count()) === 1, { message: "the picture's tile to draw it" });
  await capture(a, "phone-sheet-recent");

  step("tap both: each is ticked and staged, and the sheet stays open for more");
  await tiles.nth(0).tap();
  await tiles.nth(1).tap();
  await waitFor(async () => (await page.locator(".recent-tile.added").count()) === 2, { message: "both tiles ticked" });
  assert.ok(await page.locator(".sheet-grid").isVisible(), "the sheet is still open");
  await capture(a, "phone-sheet-picked");

  step("close the sheet: both are in the tray; send");
  await page.keyboard.press("Escape");
  await page.locator(".sheet-grid").waitFor({ state: "detached" });
  await waitFor(async () => (await page.locator(".attach-tray .att-chip").count()) === 2, { message: "two staged chips" });
  await capture(a, "phone-staged");
  await a.send("both again");

  const msgs = async () => (await bashir.messages(ch2.id)).filter((m) => tokens(m.content).length);
  await waitFor(async () => (await msgs()).length === 2, { timeout: 60000, message: "Bashir to receive both" });
  const got = await msgs();
  const picMsg = got.find((m) => m.content.includes("concord://attach/"));
  const pdfMsg = got.find((m) => m.content.includes("concord://file/"));
  assert.ok(picMsg && pdfMsg, "one picture and one file");
  await checkResent(assert, bashir, ch2.id, picMsg, picTok, dataUrl(pic));
  await checkResent(assert, bashir, ch2.id, pdfMsg, pdfTok, dataUrl(PDF, "application/pdf"));
  assert.equal(a.errors.length, 0, `page errors: ${a.errors.join(" | ")}`);
});
