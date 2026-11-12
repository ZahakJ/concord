// The sidebar's guild strip is a shelf of every guild. A wheel or the arrow
// keys move along it without switching guild (only a click or Enter does);
// the open guild is brought into view whichever road you took to it; and a
// faded end says when something waiting for you is past it. With motion
// reduced — by the OS or by Appearance › Motion — nothing on it, or on a
// dialog, moves or rests transformed.
import { test } from "../harness.mjs";

const NAMES = ["Allotment", "Bread Club", "Chess at Noon", "Darkroom", "Embroidery", "Fell Runners", "Garden Swap", "Harbour Rowing", "Ink and Quill", "Jazz Night", "Kite Flyers", "Loom Workshop", "Moth Watch"];

const shelfState = (app) =>
  app.page.evaluate(() => {
    const shelf = document.querySelector('nav[aria-label="Guilds"] .shelf');
    const s = shelf.getBoundingClientRect();
    const tiles = [...shelf.querySelectorAll(".tile")].map((t) => {
      const r = t.getBoundingClientRect();
      return { name: t.getAttribute("aria-label"), current: t.classList.contains("current"), inView: r.left >= s.left && r.right <= s.right };
    });
    return { left: shelf.scrollLeft, max: shelf.scrollWidth - shelf.clientWidth, tiles, focus: document.activeElement?.getAttribute("aria-label") };
  });
const currentName = async (app) => (await shelfState(app)).tiles.find((t) => t.current)?.name;

test("the strip holds every guild, and moving along it never switches guild", async ({ world, assert, step, waitFor, shot }) => {
  const [alice, bashir] = await world.peers(2);
  for (const n of NAMES) await alice.rpc("CreateGuild", n);
  const far = await world.guild({ name: "Zebra Yard", members: [alice, bashir] });

  const a = await alice.open({ viewport: "desktop" });
  await a.openGuild("Allotment");
  await waitFor(async () => (await currentName(a)) === "Allotment", { message: "Allotment to be the open guild" });
  const st0 = await shelfState(a);
  assert.equal(st0.tiles.length, NAMES.length + 1, "a tile for every guild, no overflow tile");
  assert.equal(await a.page.locator('nav[aria-label="Guilds"]').getByRole("button", { name: /^All guilds/ }).count(), 0);
  assert.ok(st0.max > 0, "fourteen guilds overflow a 280px sidebar, so the shelf scrolls");

  step("something aimed at Alice in the guild at the far end shows at the faded right edge");
  await a.page.evaluate(() => document.querySelector('nav[aria-label="Guilds"] .shelf').scrollTo({ left: 0, behavior: "instant" }));
  await bashir.send(far.channel("general"), "@everyone the gate code changed");
  const right = a.page.locator('nav[aria-label="Guilds"] .edge.r');
  await right.waitFor({ timeout: 60000 });
  assert.equal(await right.getAttribute("aria-label"), "1 mention further right", "the hidden mention, said by the chip");
  assert.ok(await right.evaluate((el) => el.classList.contains("you")), "in your accent, since it is aimed at you");
  assert.equal(await a.page.locator('nav[aria-label="Guilds"] .edge.l').count(), 0, "nothing hidden to the left at the start");
  // The chip sits on the faded ground, clear of every tile drawn in full: it
  // is not read as some tile's own mark.
  const clear = await a.page.evaluate(() => {
    const c = document.querySelector('nav[aria-label="Guilds"] .edge.r .edot').getBoundingClientRect();
    const shelf = document.querySelector('nav[aria-label="Guilds"] .shelf').getBoundingClientRect();
    return [...document.querySelectorAll('nav[aria-label="Guilds"] .shelf .tile')]
      .map((t) => t.getBoundingClientRect())
      .filter((r) => r.left >= shelf.left && r.right <= shelf.right - 36)
      .every((r) => r.right + 4 <= c.left);
  });
  assert.ok(clear, "the edge dot is clear of every tile in full view");
  await shot(a, "edge news");

  step("a wheel notch moves the shelf one tile and selects nothing; the caption names the tile under the pointer");
  const box = await a.page.locator('nav[aria-label="Guilds"] .shelf').boundingBox();
  const pitch = await a.page.evaluate(() => {
    const w = document.querySelectorAll('nav[aria-label="Guilds"] .shelf > .tile-wrap');
    return w[1].offsetLeft - w[0].offsetLeft;
  });
  // Over a tile's centre (the middle of the shelf may be a gap between two).
  const spot = await a.page.evaluate(() => {
    const s = document.querySelector('nav[aria-label="Guilds"] .shelf').getBoundingClientRect();
    const t = [...document.querySelectorAll('nav[aria-label="Guilds"] .shelf .tile')].map((x) => x.getBoundingClientRect()).find((r) => r.left > s.left + s.width / 3);
    return [t.left + t.width / 2, t.top + t.height / 2];
  });
  await a.page.mouse.move(spot[0], spot[1]);
  const caption = a.page.locator('nav[aria-label="Guilds"] .caption');
  await waitFor(async () => (await caption.evaluate((el) => el.classList.contains("on"))) && (await caption.innerText()).length > 0, { message: "the caption under the pointer" });
  // Within 3px: the tiles beside the open guild sit 2px aside, and the snap
  // stops follow the tiles as drawn.
  const left0 = (await shelfState(a)).left;
  await a.page.mouse.wheel(0, 120);
  await waitFor(async () => Math.abs((await shelfState(a)).left - left0 - pitch) <= 3, { message: "one notch to move exactly one tile" });
  await a.page.waitForTimeout(400);
  assert.ok(Math.abs((await shelfState(a)).left - left0 - pitch) <= 3, "and to stay there");
  for (let i = 0; i < 3; i++) await a.page.mouse.wheel(0, 120);
  await waitFor(async () => Math.abs((await shelfState(a)).left - left0 - 4 * pitch) <= 3 || (await shelfState(a)).left >= (await shelfState(a)).max - 1, { message: "four notches, four tiles" });
  await a.page.waitForTimeout(400);
  const under = await a.page.evaluate(([x, y]) => document.elementFromPoint(x, y)?.closest(".tile")?.getAttribute("aria-label"), spot);
  assert.ok(under, "a tile under the pointer");
  assert.ok((await caption.innerText()).startsWith(under), `the caption names the tile under the pointer (${under})`);
  assert.equal(await currentName(a), "Allotment", "the wheel moved no selection");
  assert.match(await a.page.locator("[data-place-head]").innerText(), /Allotment/);

  step("scrolled to the end: the far guild is in view, so its edge mark goes");
  await a.page.mouse.move(box.x + box.width / 2, box.y + 200);
  await a.page.evaluate(() => {
    const s = document.querySelector('nav[aria-label="Guilds"] .shelf');
    s.scrollTo({ left: s.scrollWidth, behavior: "instant" });
  });
  await waitFor(async () => (await right.count()) === 0, { message: "the right edge mark to go once nothing is past it" });

  step("the arrows move the highlight; Enter switches");
  await a.page.locator('nav[aria-label="Guilds"] .tile.current').focus();
  await a.page.keyboard.press("ArrowRight");
  await a.page.keyboard.press("ArrowRight");
  await waitFor(async () => (await shelfState(a)).focus === "Chess at Noon", { message: "focus two tiles along" });
  assert.equal(await currentName(a), "Allotment", "arrowing switched nothing");
  await waitFor(async () => (await caption.innerText()).startsWith("Chess at Noon"), { message: "the caption to name the highlighted tile" });
  await waitFor(async () => (await shelfState(a)).tiles.find((t) => t.name === "Chess at Noon").inView, { message: "the highlighted tile in view" });
  await a.page.keyboard.press("Enter");
  await waitFor(async () => (await currentName(a)) === "Chess at Noon", { message: "Enter to switch" });
  assert.match(await a.page.locator("[data-place-head]").innerText(), /Chess at Noon/);

  step("switching through the switcher brings the open guild into view");
  await a.openSwitcher();
  await a.page.locator('[role="dialog"][aria-label="Guilds"] [role="option"]', { hasText: "Zebra Yard" }).first().click();
  await waitFor(async () => {
    const t = (await shelfState(a)).tiles.find((x) => x.name === "Zebra Yard");
    return t.current && t.inView;
  }, { message: "Zebra Yard current and in view" });
  await shot(a, "after switcher");
});

const still = (app) =>
  app.page.evaluate(() => {
    const ms = (v) => Math.max(...v.split(",").map((x) => parseFloat(x) * (x.trim().endsWith("ms") ? 1 : 1000)));
    const nav = document.querySelector('nav[aria-label="Guilds"]');
    const out = { motion: document.documentElement.dataset.motion, worst: 0, transformed: [] };
    for (const el of nav.querySelectorAll(".tile-wrap, .tile")) {
      const cs = getComputedStyle(el);
      out.worst = Math.max(out.worst, ms(cs.transitionDuration), ms(cs.animationDuration));
      if (!["none", "1"].includes(cs.scale) || !["none", "0px", "0px 0px"].includes(cs.translate) || cs.transform !== "none") out.transformed.push(el.getAttribute("aria-label") || el.className);
    }
    return out;
  });

test("with motion reduced, the strip and dialogs hold still", async ({ world, assert, step, waitFor }) => {
  const [alice] = await world.peers(1);
  for (const n of NAMES.slice(0, 4)) await alice.rpc("CreateGuild", n);

  step("the OS asks for reduced motion");
  const a = await alice.open({ viewport: "desktop", reducedMotion: true });
  await a.openGuild("Bread Club");
  await waitFor(async () => (await currentName(a)) === "Bread Club", { message: "Bread Club open" });
  let s = await still(a);
  assert.equal(s.motion, "reduced");
  assert.ok(s.worst <= 0.02, `strip transitions are instant (worst ${s.worst}ms)`);
  assert.deepEqual(s.transformed, [], "the open guild's tile rests at its plain size, its neighbours unmoved");
  await a.openSettings("Appearance");
  const dlg = await a.page.evaluate(() => {
    const d = document.querySelector('[role="dialog"]');
    return [d, ...d.querySelectorAll("*")].slice(0, 200).map((e) => getComputedStyle(e)).reduce((m, cs) => Math.max(m, parseFloat(cs.animationDuration) * (cs.animationDuration.endsWith("ms") ? 1 : 1000), parseFloat(cs.transitionDuration) * (cs.transitionDuration.endsWith("ms") ? 1 : 1000)), 0);
  });
  assert.ok(dlg <= 0.02, `the dialog does not animate (worst ${dlg}ms)`);
  await a.closeModals();

  step("the same from Appearance › Motion, with the OS asking for nothing");
  const b = await alice.open({ viewport: "desktop" });
  await b.openGuild("Bread Club");
  await waitFor(async () => (await currentName(b)) === "Bread Club", { message: "Bread Club open" });
  s = await still(b);
  assert.equal(s.motion, "full");
  assert.ok(s.transformed.length > 0, "with full motion the open guild's tile sits raised (the check can see a transform)");
  await b.openSettings("Appearance");
  await b.dialog().getByRole("radio", { name: "Reduced" }).click();
  await b.closeModals();
  await waitFor(async () => (await still(b)).motion === "reduced", { message: "the pref to set the root attribute" });
  await b.page.waitForTimeout(300);
  s = await still(b);
  assert.ok(s.worst <= 0.02, `strip transitions are instant (worst ${s.worst}ms)`);
  assert.deepEqual(s.transformed, [], "nothing rests transformed");
});
