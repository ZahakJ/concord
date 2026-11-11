// Guild settings are in sight: a gear in the place header opens them for
// someone who can change the guild, and a short menu of their own choices for
// everyone else. The chat header's ⋯ shows all of its rows, Guild settings
// included, down to a 1280x720 window.
import { test } from "../harness.mjs";

const head = (app) => app.page.locator("[data-place-head]");

test("the owner's gear opens Guild settings; a member's opens their own short menu", async ({ world, assert, step, shot }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Workshop", members: [alice, bashir] });
  const a = await alice.open({ viewport: "desktop" });
  const b = await bashir.open({ viewport: "desktop" });
  await a.openChannel(g.allChannels[0].name || "general", "Workshop");
  await b.openChannel(g.allChannels[0].name || "general", "Workshop");

  step("Alice owns it: the gear is Guild settings");
  await head(a).getByRole("button", { name: "Guild settings" }).click();
  await a.dialog().getByText("Overview").first().waitFor();
  await shot(a, "owner gear");
  await a.closeModals();

  step("Bashir is a member: Guild options, with no Guild settings in it");
  await head(b).getByRole("button", { name: "Guild options" }).click();
  for (const item of ["All messages", "Mark as read", "Leave guild"]) {
    await b.page.getByRole("menuitem", { name: item }).waitFor();
  }
  assert.equal(await b.page.getByRole("menuitem", { name: "Guild settings" }).count(), 0);
  await shot(b, "member gear");
});

test("the chat header's ⋯ shows Guild settings without scrolling at 1280x720", async ({ world, assert }) => {
  const [alice] = await world.peers(1);
  const g = await world.guild({ name: "Workshop", members: [alice] });
  const a = await alice.open({ viewport: { width: 1280, height: 720 } });
  await a.openChannel(g.allChannels[0].name || "general", "Workshop");
  await a.page.locator(".chat-head").getByRole("button", { name: "More" }).click();
  const geo = await a.page.evaluate(() => {
    const m = [...document.querySelectorAll('[role="menu"]')].pop();
    const item = [...m.querySelectorAll(".menu-item")].find((x) => /Guild settings/.test(x.textContent));
    const r = m.getBoundingClientRect();
    return { item: item?.getBoundingClientRect().bottom, bottom: r.bottom, vh: innerHeight };
  });
  assert.ok(geo.item && geo.item <= geo.bottom && geo.bottom <= geo.vh, `Guild settings inside the menu and the window: ${JSON.stringify(geo)}`);
});
