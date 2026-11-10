// An admin's guild rename made while the owner was offline survives the
// owner coming back, even after the owner had saved the guild profile.
import { test } from "../harness.mjs";

const PNG =
  "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==";

test("offline owner does not revert an admin's rename", async ({ world, assert, step, waitFor, sleep }) => {
  const [alice, bob, carol] = await world.peers(3, { names: ["Alice", "Bob", "Carol"] });
  const g = await world.guild({ name: "Rig Guild", members: [alice, bob, carol] });
  const role = await alice.rpc("UpsertRole", g.id, "", "Admin", "#e91e63", 8, 3);
  await alice.rpc("AssignRole", g.id, bob.fingerprint, role, true);
  await waitFor(async () => (await bob.guilds()).find((x) => x.id === g.id)?.myPerms & 8, {
    message: "Bob never got Manage guild",
  });
  step("Alice saves the guild profile (icon + name), then goes offline");
  await alice.rpc("SetGuildProfile", g.id, "Rig Guild renamed", PNG, "", "");
  const nameOn = async (p) => (await p.guilds()).find((x) => x.id === g.id)?.name;
  await waitFor(async () => (await nameOn(carol)) === "Rig Guild renamed", { message: "profile never propagated" });
  await alice.stop();

  step("Bob renames while Alice is away");
  await bob.rpc("RenameGuild", g.id, "Rig Guild offline-edit");
  await waitFor(async () => (await nameOn(carol)) === "Rig Guild offline-edit", { message: "rename never reached Carol" });

  step("Alice returns; everyone stays on Bob's name");
  await alice.start();
  await alice.unlock();
  await waitFor(async () => (await nameOn(alice)) === "Rig Guild offline-edit", {
    timeout: 60000,
    message: "Alice never caught up to the newer name",
  });
  await sleep(20000);
  for (const p of [alice, bob, carol]) assert.equal(await nameOn(p), "Rig Guild offline-edit");
});
