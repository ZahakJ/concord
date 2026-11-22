// The picture editor on a staged picture: crop, turn, resize and mark up,
// from the first slot of the staged card, and what it hands back.
//
// The picture carries an EXIF block with a marker in it, as a camera's
// location would be. A staged JPEG keeps its own bytes until it is edited;
// the edit is a fresh canvas, and the marker must not survive it.
import fs from "node:fs";
import path from "node:path";
import { test, sleep } from "../harness.mjs";
import { auditLayout } from "../layoutaudit.mjs";

const MARK = "CONCORD-TEST-GPS-51.5N";
const SHOTS = process.env.E2E_SHOTS_DIR || "";
async function capture(app, name) {
  if (!SHOTS) return;
  fs.mkdirSync(SHOTS, { recursive: true });
  await app.page.screenshot({ path: path.join(SHOTS, `${name}.png`) });
}

// A 1600×1000 JPEG drawn in the page, with an APP1 Exif segment holding MARK
// spliced in after the start-of-image marker.
async function cameraJpeg(page, file) {
  const b64 = await page.evaluate(() => {
    const c = Object.assign(document.createElement("canvas"), { width: 1600, height: 1000 });
    const x = c.getContext("2d");
    const g = x.createLinearGradient(0, 0, 1600, 1000);
    g.addColorStop(0, "#f7b267");
    g.addColorStop(1, "#4d7ea8");
    x.fillStyle = g;
    x.fillRect(0, 0, 1600, 1000);
    x.fillStyle = "#fff";
    x.beginPath();
    x.arc(500, 400, 160, 0, 7);
    x.fill();
    return c.toDataURL("image/jpeg", 0.9).split(",")[1];
  });
  const jpg = Buffer.from(b64, "base64");
  const body = Buffer.concat([Buffer.from("Exif\0\0MM\0*\0\0\0\x08\0\0", "binary"), Buffer.from(MARK)]);
  const len = Buffer.alloc(2);
  len.writeUInt16BE(body.length + 2);
  const out = Buffer.concat([jpg.subarray(0, 2), Buffer.from([0xff, 0xe1]), len, body, jpg.subarray(2)]);
  fs.writeFileSync(file, out);
}

const stagedSrc = (page) => page.locator(".attach-tray .att-chip img").first().getAttribute("src");
const stagedDims = (page) => page.locator(".attach-tray .am-num").first().innerText();
const decoded = (src) => Buffer.from(String(src).split(",")[1] || "", "base64").toString("binary");

async function audit(page, phone, log) {
  const hits = await page.evaluate(auditLayout, { phone, edge: 8, screenEdge: 12 });
  for (const h of hits) log(`audit: ${h.kind} ${h.side || ""} ${h.path || ""} ${h.text || ""}`);
  return hits;
}

test("desktop: crop, turn, size and mark up a staged picture; the EXIF does not survive", async ({ world, assert, step, log, waitFor }) => {
  const [alice, bashir] = await world.peers(2, { names: ["Alice", "Bashir"] });
  const g = await world.guild({ name: "Studio", members: [alice, bashir] });
  const general = g.allChannels.find((c) => c.name === "general") || g.allChannels[0];
  const app = await alice.open({ viewport: { width: 1280, height: 860 } });
  const { page } = app;
  await app.openChannel("general", "Studio");
  const file = path.join(world.dir, "camera.jpg");
  await cameraJpeg(page, file);
  await app.attach(file);
  await page.locator(".attach-tray .att-chip img").first().waitFor({ timeout: 20000 });
  const before = decoded(await stagedSrc(page));
  log(`staged as picked: ${before.includes(MARK) ? "carries" : "does not carry"} the EXIF marker`);
  assert.match(await stagedDims(page), /1600×1000/);

  step("Edit is the first action on the card");
  const tools = page.locator(".attach-tray .att-tools").first().getByRole("button");
  assert.equal(await tools.first().getAttribute("aria-label"), "Edit picture");
  await tools.first().click();
  const ed = page.getByTestId("photo-editor");
  await ed.waitFor();
  await page.locator(".pe-canvas").waitFor();
  await sleep(500);
  assert.equal((await audit(page, false, log)).length, 0, "the editor's layout is clean");

  step("crop: the bottom-right corner a quarter in");
  const cv = page.locator(".pe-canvas");
  let b = await cv.boundingBox();
  await page.mouse.move(b.x + b.width - 2, b.y + b.height - 2);
  await page.mouse.down();
  for (let i = 1; i <= 8; i++) await page.mouse.move(b.x + b.width - 2 - (b.width * 0.25 * i) / 8, b.y + b.height - 2 - (b.height * 0.25 * i) / 8);
  await page.mouse.up();
  await waitFor(async () => /1200 × 750/.test(await page.locator(".pe-dims").innerText()), { message: "the crop to 1200 × 750" });

  step("turn right, and Undo and Redo it");
  await page.getByRole("button", { name: "Turn right" }).click();
  await waitFor(async () => /750 × 1200/.test(await page.locator(".pe-dims").innerText()), { message: "the turn" });
  await page.getByRole("button", { name: "Undo" }).click();
  await waitFor(async () => /1200 × 750/.test(await page.locator(".pe-dims").innerText()), { message: "undo" });
  await page.getByRole("button", { name: "Redo" }).click();
  await waitFor(async () => /750 × 1200/.test(await page.locator(".pe-dims").innerText()), { message: "redo" });

  step("mark up: a pen stroke, an arrow and a word");
  await page.getByRole("button", { name: "Mark up" }).click();
  await sleep(300);
  b = await cv.boundingBox();
  const at = (u, v) => [b.x + b.width * u, b.y + b.height * v];
  await page.getByRole("radio", { name: "Pen" }).click();
  await page.mouse.move(...at(0.2, 0.2));
  await page.mouse.down();
  for (let i = 1; i <= 10; i++) await page.mouse.move(...at(0.2 + i * 0.04, 0.2 + Math.sin(i) * 0.05));
  await page.mouse.up();
  await page.getByRole("radio", { name: "Arrow" }).click();
  await page.getByRole("radio", { name: "Colour #ffc53d" }).click();
  await page.mouse.move(...at(0.3, 0.7));
  await page.mouse.down();
  await page.mouse.move(...at(0.5, 0.6), { steps: 6 });
  await page.mouse.up();
  await page.getByRole("radio", { name: "Text" }).click();
  await page.mouse.click(...at(0.5, 0.85));
  await page.getByLabel("Text to place").fill("Kiln 2");
  await page.getByLabel("Text to place").press("Enter");

  step("size: 800 px on the long edge");
  await page.getByRole("button", { name: "Size", exact: true }).click();
  await page.getByRole("radio", { name: "800 px" }).click();
  await waitFor(async () => /500 × 800/.test(await page.locator(".pe-dims").innerText()), { message: "the size" });
  await page.mouse.move(5, 5);
  await sleep(400);
  await capture(app, "photo-editor-desktop");

  step("Done: the staged picture is the edit, without the EXIF");
  await ed.getByRole("button", { name: "Done", exact: true }).click();
  await ed.waitFor({ state: "detached" });
  await waitFor(async () => /500×800/.test(await stagedDims(page)), { message: "the card shows the edited size" });
  const after = decoded(await stagedSrc(page));
  assert.ok(!after.includes(MARK), "the EXIF marker is gone");
  assert.ok(!after.includes("Exif"), "no Exif block at all");

  step("it sends");
  await page.locator(".input-box textarea").first().press("Enter");
  await waitFor(async () => (await bashir.messages(general.id)).some((m) => /concord:\/\/attach/.test(m.content) && /500x800/.test(m.content)), {
    timeout: 30000,
    message: "Bashir to get a 500×800 picture",
  });
  assert.equal(app.errors.length, 0, `page errors: ${app.errors.join(" | ")}`);
});

test("phone: the editor fills the screen, turns, and Cancel leaves the picture alone", async ({ world, assert, step, log, waitFor }) => {
  const [alice, bashir] = await world.peers(2, { names: ["Alice", "Bashir"] });
  await world.guild({ name: "Studio", members: [alice, bashir] });
  const app = await alice.open({ viewport: { width: 412, height: 915 }, mobile: true });
  const { page } = app;
  await app.openChannel("general", "Studio");
  const file = path.join(world.dir, "camera.jpg");
  await cameraJpeg(page, file);
  await app.attach(file);
  await page.locator(".attach-tray .att-chip img").first().waitFor({ timeout: 20000 });
  const src0 = await stagedSrc(page);

  step("the card's options, then Edit picture");
  await page.locator(".attach-tray .att-open").first().tap();
  await page.getByRole("button", { name: "Edit picture" }).tap();
  const ed = page.getByTestId("photo-editor");
  await ed.waitFor();
  await sleep(600);
  const box = await ed.boundingBox();
  assert.ok(box.x <= 0.5 && box.y <= 0.5 && box.width >= 411 && box.height >= 914, `full screen: ${JSON.stringify(box)}`);
  assert.equal((await audit(page, true, log)).length, 0, "the editor's layout is clean on a phone");
  await capture(app, "photo-editor-phone");

  step("turn left; the size follows");
  await page.getByRole("button", { name: "Turn left" }).tap();
  await waitFor(async () => /1000 × 1600/.test(await page.locator(".pe-dims").innerText()), { message: "the turn" });

  step("Cancel: the staged picture is untouched");
  await ed.getByRole("button", { name: "Cancel" }).tap();
  await ed.waitFor({ state: "detached" });
  assert.equal(await stagedSrc(page), src0, "Cancel kept the original bytes");

  step("again, Done: the turn is applied");
  await page.locator(".attach-tray .att-open").first().tap();
  await page.getByRole("button", { name: "Edit picture" }).tap();
  await ed.waitFor();
  await sleep(400);
  await page.getByRole("button", { name: "Turn left" }).tap();
  await ed.getByRole("button", { name: "Done", exact: true }).tap();
  await ed.waitFor({ state: "detached" });
  await waitFor(async () => /1000×1600/.test(await stagedDims(page)), { message: "the card shows the turned size" });
  assert.equal(app.errors.length, 0, `page errors: ${app.errors.join(" | ")}`);
});
