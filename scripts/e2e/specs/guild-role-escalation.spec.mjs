// An admin's role editor does not let them tick a permission they lack, and
// says why, instead of saving a role that silently never exists.
import { test } from "../harness.mjs";

test("role editor locks permissions the editor lacks", async ({ world, assert, step, waitFor, shot }) => {
  const [alice, bob] = await world.peers(2, { names: ["Alice", "Bob"] });
  const g = await world.guild({ name: "Escalators", members: [alice, bob] });
  const role = await alice.rpc("UpsertRole", g.id, "", "Admin", "#e91e63", 1 | 4 | 16, 3);
  await alice.rpc("AssignRole", g.id, bob.fingerprint, role, true);
  await waitFor(async () => (await bob.guilds()).find((x) => x.id === g.id)?.myPerms & 16, {
    message: "Bob never got Manage roles",
  });
  await assert.rejects(bob.rpc("UpsertRole", g.id, "", "SuperBob", "#123456", 1 | 64, 1), /permissions you have/);

  const b = await bob.open();
  await b.openChannel(g.allChannels[0].name, "Escalators");
  step("Bob opens Roles and starts a new role");
  await b.page.locator(".guild-header").first().click();
  await b.page.getByRole("button", { name: /^Roles/ }).first().click();
  await b.page.getByRole("button", { name: /New role/ }).first().click();
  const sync = b.page.getByRole("checkbox", { name: /Sync host/ });
  await sync.waitFor({ state: "visible" });
  await shot(b, "role editor");
  assert.equal(await sync.isDisabled(), true);
  assert.equal(await b.page.getByRole("checkbox", { name: /Manage channels/ }).isDisabled(), false);
  assert.match(await sync.innerText(), /only grant permissions you have/);
});
