// A forum board can be laid out as List (the default), Gallery or Cover, from
// its toolbar or its settings, and the choice holds on this device; a picture
// uploaded as the forum's header art is drawn for every member.
import { test } from "../harness.mjs";

// A 1x1 PNG, the smallest image the core accepts as a forum banner.
const PNG = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==";

test("a board's layout is chosen and kept, and its header picture is drawn", async ({ world, assert, step, waitFor, shot }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Lanterns", members: [alice, bashir], channels: [{ name: "patterns", type: "forum" }] });
  const forumId = g.channel("patterns");
  for (const t of ["Cube lantern", "Star lantern", "Paper boat"]) {
    await alice.rpc("CreateThread", g.id, forumId, t, `How I made the ${t.toLowerCase()}.`, []);
  }
  const a = await alice.open({ viewport: "desktop" });
  await a.openGuild("Lanterns");
  await a.page.locator(".channel", { hasText: "patterns" }).first().click();
  const posts = a.page.locator("ul.posts");
  await posts.waitFor();
  assert.match(await posts.getAttribute("class"), /\blist\b/, "List is the default");

  step("the toolbar's Layout → Gallery");
  await a.page.getByRole("button", { name: /^Layout: / }).click();
  await a.page.getByRole("menuitem", { name: /^Gallery/ }).click();
  await waitFor(async () => /\bgallery\b/.test(await posts.getAttribute("class")), { message: "the gallery layout" });
  await shot(a, "gallery");

  step("Board settings → Cover, kept across a reload");
  await a.page.getByRole("button", { name: "Board settings" }).first().click();
  await a.dialog().getByRole("radio", { name: /Cover/ }).click();
  await a.closeModals();
  await waitFor(async () => /\bcover\b/.test(await posts.getAttribute("class")), { message: "the cover layout" });
  await a.reload();
  await a.openGuild("Lanterns");
  await a.page.locator(".channel", { hasText: "patterns" }).first().click();
  await waitFor(async () => /\bcover\b/.test((await a.page.locator("ul.posts").getAttribute("class")) || ""), { message: "cover after a reload" });
  await shot(a, "cover");

  step("a header picture set by the owner is drawn for a member");
  await alice.rpc("SetForumBanner", g.id, forumId, PNG);
  const b = await bashir.open({ viewport: "desktop" });
  await b.openGuild("Lanterns");
  await b.page.locator(".channel", { hasText: "patterns" }).first().click();
  await b.page.locator("header.hero img.hero-img").waitFor({ timeout: 30000 });
  await shot(b, "header picture");
});
