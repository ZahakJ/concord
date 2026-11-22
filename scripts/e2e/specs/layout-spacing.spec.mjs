// Spacing audit: walks the main screens at 1440, 1024, 768, 412 and 360 wide,
// in the dark and the light theme, and runs layoutaudit.mjs on each — text,
// icons and controls hard against the edge of their panel or of a phone's
// screen, siblings drawn over each other, sideways overflow, and text cut
// without an ellipsis. Any hit fails the test.
//
// E2E_SHOTS_DIR, when set, receives an annotated capture of every screen that
// has hits and audit.json with all of them. E2E_AUDIT_REPORT_ONLY=1 records
// without failing (to count the hits on a tree being fixed).
import fs from "node:fs";
import path from "node:path";
import zlib from "node:zlib";
import { test, sleep } from "../harness.mjs";
import { auditLayout, markHits, clearHits } from "../layoutaudit.mjs";

const SHOTS = process.env.E2E_SHOTS_DIR || "";
const REPORT_ONLY = process.env.E2E_AUDIT_REPORT_ONLY === "1";
const WIDTHS = [
  { w: 1440, h: 900, mobile: false },
  { w: 1024, h: 768, mobile: false },
  { w: 768, h: 1024, mobile: true },
  { w: 412, h: 915, mobile: true },
  { w: 360, h: 740, mobile: true },
];

// A small banded PNG: enough of a picture for tiles and the viewer.
function png(w, h, seed) {
  const raw = Buffer.alloc((w * 3 + 1) * h);
  for (let y = 0; y < h; y++)
    for (let x = 0; x < w; x++) {
      const o = y * (w * 3 + 1) + 1 + x * 3;
      raw[o] = (seed * 70 + y) & 255;
      raw[o + 1] = (120 + x / 2) & 255;
      raw[o + 2] = (200 - seed * 30) & 255;
    }
  const crcT = Array.from({ length: 256 }, (_, n) => {
    let c = n;
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    return c >>> 0;
  });
  const chunk = (type, data) => {
    const td = Buffer.concat([Buffer.from(type, "ascii"), data]);
    let c = 0xffffffff;
    for (const b of td) c = crcT[(c ^ b) & 0xff] ^ (c >>> 8);
    const len = Buffer.alloc(4);
    len.writeUInt32BE(data.length);
    const crc = Buffer.alloc(4);
    crc.writeUInt32BE((c ^ 0xffffffff) >>> 0);
    return Buffer.concat([len, td, crc]);
  };
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(w, 0);
  ihdr.writeUInt32BE(h, 4);
  ihdr[8] = 8;
  ihdr[9] = 2;
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk("IHDR", ihdr),
    chunk("IDAT", zlib.deflateSync(raw)),
    chunk("IEND", Buffer.alloc(0)),
  ]);
}

test("layout: nothing touches an edge, overlaps or overflows", async ({ world, step, log }) => {
  const [alice, bashir] = await world.peers(2, { names: ["Alice", "Bashir"] });
  const g = await world.guild({ name: "Lantern Makers", members: [alice, bashir], channels: ["ideas"] });
  const general = g.allChannels.find((c) => c.name === "general") || g.allChannels[0];
  step("seeding");
  await bashir.send(general.id, "Morning! Kiln is warm, first batch of shades going in at ten.");
  for (let i = 0; i < 3; i++) {
    const [w, h] = [[640, 420], [420, 560], [600, 600]][i];
    const data = `data:image/png;base64,${png(w, h, i).toString("base64")}`;
    await bashir.rpc("SendAttachment", general.id, data, w, h, "", false, `photo-${i}.png`, "", i === 0 ? "Sunrise from the workshop roof" : "", "");
  }
  await alice.send(general.id, "That light is unreal. Can we use it for the stall banner on Saturday morning?");
  await bashir.waitForMessage(general.id, (m) => m.content.includes("stall banner"));
  const dm = await world.dm(alice, bashir);
  await bashir.send(dm.channelId, "Glaze test, batch two is out of the kiln.");
  for (let i = 0; i < 4; i++) {
    const data = `data:image/png;base64,${png(480, 480, i + 3).toString("base64")}`;
    await bashir.rpc("SendAttachment", dm.channelId, data, 480, 480, "", false, `glaze-${i}.png`, "", "", "");
  }
  const locked = (await world.peers(1, { names: ["Dana"] }))[0];
  await locked.stop();
  await locked.start();
  const staged = [0, 1, 2].map((i) => {
    const f = path.join(world.dir, `staged-${i}.png`);
    fs.writeFileSync(f, png(400, 300 + i * 40, i));
    return f;
  });

  const all = [];
  async function audit(app, screen, tag, vp) {
    await sleep(500);
    const hits = await app.page.evaluate(auditLayout, { phone: vp.w <= 768, edge: 8, screenEdge: 12 });
    for (const h of hits) all.push({ screen, tag, ...h });
    log(`${screen} ${tag}: ${hits.length}`);
    if (SHOTS && hits.length) {
      fs.mkdirSync(SHOTS, { recursive: true });
      await app.page.evaluate(markHits, hits);
      await app.page.screenshot({ path: path.join(SHOTS, `${screen}-${tag}.png`) });
      await app.page.evaluate(clearHits);
    }
  }
  async function screen(name, tag, fn) {
    try {
      await fn();
    } catch (e) {
      all.push({ screen: name, tag, kind: "unreached", text: String(e.message || e).split("\n")[0] });
      log(`unreached ${name} ${tag}: ${String(e.message || e).split("\n")[0]}`);
    }
  }
  const setTheme = (page, t) =>
    page.evaluate((t) => {
      const p = JSON.parse(localStorage.getItem("concord.prefs") || "{}");
      p.theme = t;
      localStorage.setItem("concord.prefs", JSON.stringify(p));
    }, t);

  for (const theme of ["dark", "light"]) {
    for (const vp of WIDTHS) {
      const tag = `${vp.w}-${theme}`;
      step(tag);
      const phone = vp.w <= 768;
      const app = await alice.open({ viewport: { width: vp.w, height: vp.h }, mobile: vp.mobile, colorScheme: theme });
      await setTheme(app.page, theme);
      await app.reload();
      const { page } = app;
      page.setDefaultTimeout(10000);

      // A phone opens on the last conversation; the lists are a Back away.
      const home = async () => {
        if (!phone) return;
        for (let i = 0; i < 2; i++) await app.back().catch(() => {});
      };
      await screen("chat", tag, async () => {
        await home();
        await app.openChannel("general", "Lantern Makers");
        await app.message("stall banner").waitFor();
        await page.locator(".msg img").first().waitFor();
        await audit(app, "chat", tag, vp);
      });
      await screen("menu", tag, async () => {
        const row = app.message("stall banner");
        if (vp.mobile) await app.longPress(row);
        else await row.click({ button: "right" });
        await page.getByRole("menuitem").first().waitFor();
        await audit(app, "menu", tag, vp);
        await page.keyboard.press("Escape");
        await app.closeModals().catch(() => {});
      });
      await screen("staged", tag, async () => {
        await app.attach(...staged);
        await page.waitForFunction(() => document.querySelectorAll(".attach-tray .att-chip img").length === 3, null, { timeout: 20000 });
        await audit(app, "staged", tag, vp);
        await app.reload();
      });
      await screen("gallery", tag, async () => {
        if (!phone) {
          await app.openChannel("general", "Lantern Makers");
          if (!(await page.locator('aside[aria-label="Inspector"]').count())) {
            await page.getByRole("button", { name: "Members, pins and files" }).click();
          }
          await page.locator('aside[aria-label="Inspector"]').getByRole("tab", { name: "Media" }).click();
        } else {
          await home();
          await page.locator('nav[aria-label="Sections"]').getByRole("button", { name: /^Messages/ }).click();
          await page.locator(".dm-item").filter({ hasText: "Bashir" }).first().click();
          await page.getByRole("button", { name: "Media and files" }).click();
        }
        await page.locator('[data-testid="gallery-scroll"]').waitFor({ timeout: 10000 });
        await sleep(600);
        await audit(app, "gallery", tag, vp);
        await page.keyboard.press("Escape");
        await app.closeModals().catch(() => {});
        await app.reload();
      });
      await screen("settings", tag, async () => {
        await home();
        // A 768 px tablet has the phone's lists but the desktop's gear.
        await app.openSettings("Appearance").catch(async () => {
          await page.locator('button[aria-label="Settings"]').last().click();
          await page.getByRole("dialog").getByText("Appearance", { exact: true }).first().click();
        });
        await audit(app, "settings", tag, vp);
        await app.closeModals().catch(() => {});
      });
      await screen("whatsnew", tag, async () => {
        await page.goto(`${alice.url}/?t=${alice.token}`, { waitUntil: "domcontentloaded" });
        await page.evaluate(() => localStorage.setItem("concord.seenVersion", "0.0.1"));
        const pass = page.locator('input[placeholder="Passphrase"]');
        await pass.waitFor({ timeout: 20000 }).catch(() => {});
        if (await pass.isVisible().catch(() => false)) {
          await pass.fill(alice.passphrase);
          await pass.press("Enter");
        }
        await page.locator(".wn").waitFor({ timeout: 20000 });
        await sleep(1200);
        await audit(app, "whatsnew", tag, vp);
        await page.locator(".wn .go").click();
        await sleep(1200);
        await audit(app, "whatsnew1", tag, vp);
        await app.closeModals().catch(() => {});
      });
      await app.close();
      await screen("login", tag, async () => {
        const l = await locked.open({ viewport: { width: vp.w, height: vp.h }, mobile: vp.mobile, colorScheme: theme, unlock: false });
        await l.page.goto(`${locked.url}/?t=${locked.token}`, { waitUntil: "domcontentloaded" });
        await setTheme(l.page, theme);
        await l.page.goto(`${locked.url}/?t=${locked.token}`, { waitUntil: "domcontentloaded" });
        await l.page.locator('input[placeholder="Passphrase"]').waitFor({ timeout: 20000 });
        await sleep(900);
        await audit(l, "login", tag, vp);
        await l.close();
      });
    }
  }

  const byKind = {};
  for (const h of all) byKind[h.kind] = (byKind[h.kind] || 0) + 1;
  log(`audit: ${all.length} hits ${JSON.stringify(byKind)}`);
  if (SHOTS) fs.writeFileSync(path.join(SHOTS, "audit.json"), JSON.stringify(all, null, 1));
  if (!REPORT_ONLY && all.length) {
    const sample = all
      .slice(0, 12)
      .map((h) => `${h.screen} ${h.tag} ${h.kind}${h.side ? ":" + h.side : ""} ${h.path || ""} ${h.text || ""}`)
      .join("\n  ");
    throw new Error(`${all.length} layout hits:\n  ${sample}`);
  }
}, { timeout: 30 * 60 * 1000 });
