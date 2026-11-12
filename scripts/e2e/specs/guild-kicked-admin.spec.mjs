// A kicked admin's window says so plainly and offers nothing that would act on
// the guild: no Invite, no "+", no call, and its core refuses to mint a code.
import { test } from "../harness.mjs";

test("a kicked admin loses every management affordance", async ({ world, assert, step, waitFor, shot }) => {
  const [owner, admin, carol] = await world.peers(3, { names: ["Alice", "Bob", "Carol"] });
  const g = await world.guild({ name: "Riverside", members: [owner, admin, carol] });
  const ch = g.allChannels[0];

  step("Bob is made an admin with Manage members");
  const roleId = await owner.rpc("UpsertRole", g.id, "", "Admin", "#5865f2", 1 | 4, 5);
  await owner.rpc("AssignRole", g.id, admin.fingerprint, roleId, true);
  await waitFor(async () => (await admin.guilds()).find((x) => x.id === g.id)?.canManage, {
    message: "the admin role never reached Bob",
  });
  const b = await admin.open();
  await b.openChannel(ch.name, "Riverside");
  // Invite people lives in the conversation header's ⋯ menu.
  const headerMenu = b.page.locator("header.chat-head").getByRole("button", { name: "More", exact: true });
  await headerMenu.click();
  await b.page.getByRole("menuitem", { name: "Invite people" }).waitFor({ state: "visible" });
  await b.page.keyboard.press("Escape");

  step("Alice kicks Bob");
  await owner.rpc("RemoveMember", g.id, admin.fingerprint, "");
  await waitFor(async () => (await admin.guilds()).find((x) => x.id === g.id)?.evicted, {
    timeout: 30000,
    message: "Bob's client never learned it was removed",
  });
  const banner = b.page.locator(".gone-banner");
  await banner.waitFor({ state: "visible" });
  await shot(b, "kicked admin");
  assert.match(await banner.innerText(), /You were removed from/);
  assert.match(await banner.innerText(), /new invite/);
  await banner.getByRole("button", { name: "Remove from my list" }).waitFor({ state: "visible" });

  step("nothing that manages the guild is offered");
  await waitFor(async () => (await b.page.getByRole("button", { name: /^Invite/ }).count()) === 0, {
    message: "the kicked admin still has an Invite button",
  });
  await headerMenu.click();
  await b.page.locator('[role="menu"]').waitFor({ state: "visible" });
  assert.equal(await b.page.getByRole("menuitem", { name: "Invite people" }).count(), 0, "the header menu still offers Invite people");
  await b.page.keyboard.press("Escape");
  assert.equal(await b.page.getByRole("button", { name: "Add channel or category" }).count(), 0);
  assert.equal(await b.page.getByRole("button", { name: /Start a call/ }).count(), 0);
  const view = (await admin.guilds()).find((x) => x.id === g.id);
  assert.equal(view.canManage, false);
  assert.equal(view.myPerms, 0);
  await assert.rejects(admin.rpc("InviteCode", g.id), /permission/);
});
