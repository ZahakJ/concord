// Avatar decorations, avatar rings and profile effects are gone. Nothing in
// the app offers them, and a peer on an older build who still sends the ids
// (in `frame`, `effect` and `style`) is drawn as a plain avatar: no ring or
// decoration element, no console error, the same box as anyone else, and the
// rest of the profile (name, bio, banner) as before.
import { test } from "../harness.mjs";

const WORDS = /decorat|avatar ring|\brings?\b|effect|\bframes?\b/i;

// Visible text of a locator, for the assertion message.
const textOf = (loc) => loc.evaluate((el) => el.innerText || "");

test("nothing offers a decoration, ring or profile effect", async ({ world, assert, step, shot }) => {
  const [alice] = await world.peers(1);
  const a = await alice.open({ viewport: "desktop" });

  step("the profile studio");
  const prof = await a.openSettings("Your profile");
  const profText = await textOf(prof);
  assert.ok(!/decorat|avatar ring|\brings?\b|profile effect|card effect|card frame/i.test(profText), `Your profile offers a cosmetic:\n${profText}`);
  assert.equal(await prof.locator(".ring-entry, [data-dec], [data-ring]").count(), 0, "no decoration entry or drawn piece in the studio");
  await shot(a, "profile studio");

  step("Appearance");
  const app = await a.openSettings("Appearance");
  const appText = await textOf(app);
  assert.ok(!/decorat|avatar ring|profile effect|card effect|card frame/i.test(appText), `Appearance mentions a cosmetic:\n${appText}`);
  await a.closeModals();

  step("the self panel: status and your own card");
  await a.page.getByRole("button", { name: "Set status" }).click();
  const status = a.page.locator('[role="dialog"]').last();
  await status.waitFor({ state: "visible" });
  assert.ok(!WORDS.test(await textOf(status)), "the status popover offers a cosmetic");
  await a.page.keyboard.press("Escape");
  await a.page.getByRole("button", { name: "Your profile", exact: true }).click();
  const card = a.page.locator(".pop").first();
  await card.waitFor({ state: "visible" });
  assert.ok(!/decorat|avatar ring|profile effect/i.test(await textOf(card)), "your card offers a cosmetic");
  assert.equal(await card.locator("[data-dec], [data-ring]").count(), 0);
  await a.page.keyboard.press("Escape");

  step("the command palette");
  for (const q of ["decoration", "ring", "effect", "frame"]) {
    await a.page.keyboard.press("Control+k");
    const input = a.page.locator('[aria-label="Command palette"] input').first();
    await input.waitFor({ state: "visible" });
    await input.fill(q);
    await a.page.waitForTimeout(300);
    const labels = await a.page
      .locator('[aria-label="Command palette"] [role="option"]')
      .evaluateAll((els) => els.map((e) => (e.innerText || "").trim()));
    const hits = labels.filter((l) => l.toLowerCase().includes(q));
    assert.deepEqual(hits, [], `Ctrl+K "${q}" offers ${JSON.stringify(hits)}`);
    await a.page.keyboard.press("Escape");
    await a.page.waitForTimeout(150);
  }
  assert.deepEqual(a.errors, [], a.errors.join("\n"));
});

test("an old ring, decoration and effect id is drawn as a plain avatar", async ({ world, assert, step, waitFor, shot }) => {
  const [alice, bashir, chen] = await world.peers(3);
  const g = await world.guild({ name: "Wardrobe", members: [alice, bashir, chen] });
  const ch = g.allChannels[0];
  // What an older build writes: a gradient ring in `frame`, a card effect, and
  // a drawn decoration, its colourway, a card frame and a rider in `style`.
  // All of them exist in v0.60.x, so an older build draws every one.
  const style = JSON.stringify({ dec: "gold-filigree", dc: "gold", cf: "old-card", sat: "🚀", speed: "fast", angle: 120 });
  await bashir.rpc("SetProfile", "Bashir", "", "", "#3366cc", "", "preset:galaxy", "online", "an old look", "#cc6633", "orbit", "sparkle-card", style, "");
  await chen.rpc("SetProfile", "Chen", "", "", "#33cc66", "", "", "online", "", "", "", "", "", "");
  await bashir.send(ch.id, "wearing old things");
  await chen.send(ch.id, "wearing nothing");

  const me = await bashir.rpc("Identity");
  assert.equal(me.frame, "orbit", "the wire still carries the old ring id");
  assert.equal(me.style?.dec, "gold-filigree", "the wire still carries the old decoration id");
  // And Alice holds them: without this the page checks below would pass
  // just as well if the old ids had never reached her.
  await waitFor(
    async () => {
      const m = (await alice.rpc("Members", g.id)).find((x) => x.fingerprint === bashir.fingerprint);
      return m?.frame === "orbit" && m?.effect === "sparkle-card" && m?.style?.dec === "gold-filigree";
    },
    { message: "Alice never received Bashir's old ring, effect and decoration ids" },
  );

  const a = await alice.open({ viewport: "desktop" });
  await a.openChannel(ch.name, "Wardrobe");
  await a.waitForMessage("wearing old things");
  await a.waitForMessage("wearing nothing");

  const rowOf = (name) => a.page.locator(".member", { has: a.page.locator(".mname", { hasText: name }) }).first();
  await rowOf("Bashir").waitFor();
  await rowOf("Chen").waitFor();
  // The tables used to arrive on their own chunk; give a ring time to show up.
  await a.page.waitForTimeout(1500);

  step("no ring or decoration anywhere on the page");
  assert.equal(await a.page.locator("[data-ring], [data-dec], .avatar.ringed").count(), 0, "a ring or decoration was drawn");
  const strays = await a.page.evaluate(() =>
    [...document.querySelectorAll(".avatar")].flatMap((av) =>
      [...av.children].filter((c) => !(c.tagName === "IMG" || c.classList.contains("dot") || c.classList.contains("mood"))).map((c) => c.outerHTML.slice(0, 120)),
    ),
  );
  assert.deepEqual(strays, [], "an avatar holds something besides its picture and its presence mark");

  step("the same box as a peer without them");
  const box = async (name) => {
    const row = rowOf(name);
    const av = await row.locator(".avatar").boundingBox();
    const nm = await row.locator(".mname").boundingBox();
    const r = await row.boundingBox();
    return { w: Math.round(av.width), h: Math.round(av.height), nameX: Math.round(nm.x - r.x), rowH: Math.round(r.height) };
  };
  const b = await box("Bashir");
  const c = await box("Chen");
  assert.deepEqual(b, c, `Bashir ${JSON.stringify(b)} vs Chen ${JSON.stringify(c)}`);
  await shot(a, "member list");

  step("the profile card: plain avatar, name, bio and banner");
  await rowOf("Bashir").click();
  const card = a.page.locator(".pop").first();
  await card.waitFor({ state: "visible" });
  await waitFor(async () => (await card.innerText()).includes("an old look"), { message: "the bio on the card" });
  assert.ok((await card.innerText()).includes("Bashir"), "the name on the card");
  assert.equal(await card.locator("[data-ring], [data-dec], .avatar.ringed").count(), 0, "a ring or decoration on the card");
  const av = await card.locator(".avatar").first().evaluate((el) => ({ w: el.offsetWidth, h: el.offsetHeight }));
  assert.deepEqual(av, { w: 72, h: 72 });
  const bnr = await card.locator(".bnr").first().getAttribute("style");
  assert.ok(bnr && bnr.includes("background") && !bnr.includes("#3366cc"), `the galaxy banner is drawn (${bnr})`);
  await shot(a, "profile card");
  await a.page.keyboard.press("Escape");

  assert.deepEqual(a.errors, [], a.errors.join("\n"));
});
