// "Make admin" asks first, names what it grants, and reuses the guild's
// existing Admin role rather than minting a second one.
import { test } from "../harness.mjs";

test("make admin confirms and reuses the Admin role", async ({ world, assert, step, waitFor, shot }) => {
  const [alice, bob, eve] = await world.peers(3, { names: ["Alice", "Bob", "Eve"] });
  const g = await world.guild({ name: "Crowns", members: [alice, bob, eve] });
  const adminId = await alice.rpc("UpsertRole", g.id, "", "Admin", "#e91e63", 63, 3);
  await alice.rpc("AssignRole", g.id, bob.fingerprint, adminId, true);

  const a = await alice.open();
  await a.openChannel(g.allChannels[0].name, "Crowns");
  step("Alice: Make admin on Eve");
  await a.page.locator(".mname", { hasText: "Eve" }).first().click({ button: "right" });
  await a.page.locator(".cm[role=menu]").getByText("Make admin", { exact: true }).click();
  const dlg = a.dialog();
  await dlg.waitFor({ state: "visible" });
  await shot(a, "make admin confirm");
  assert.match(await dlg.innerText(), /Make Eve an admin\?/);
  assert.match(await dlg.innerText(), /Manage members/);
  assert.equal((await alice.rpc("Members", g.id)).find((m) => m.fingerprint === eve.fingerprint).roleIds?.length || 0, 0);
  await dlg.getByRole("button", { name: "Make admin" }).click();
  await waitFor(
    async () => (await alice.rpc("Members", g.id)).find((m) => m.fingerprint === eve.fingerprint).roleIds?.[0] === adminId,
    { message: "Eve never got the existing Admin role" },
  );
  const admins = (await alice.rpc("Roles", g.id)).filter((r) => r.name === "Admin");
  assert.equal(admins.length, 1);
});
