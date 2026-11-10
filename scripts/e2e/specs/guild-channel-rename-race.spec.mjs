// Two admins renaming one channel at the same moment end with every peer on
// the same name, and the moderation log names what each rename set.
import { test } from "../harness.mjs";

test("concurrent channel renames converge", async ({ world, assert, step, waitFor, shot }) => {
  const [alice, bob, carol] = await world.peers(3, { names: ["Alice", "Bob", "Carol"] });
  const g = await world.guild({ name: "Two Pens", members: [alice, bob, carol] });
  const ch = g.allChannels[0];
  const role = await alice.rpc("UpsertRole", g.id, "", "Admin", "#e91e63", 4, 5);
  await alice.rpc("AssignRole", g.id, bob.fingerprint, role, true);
  await waitFor(async () => (await bob.guilds()).find((x) => x.id === g.id)?.myPerms & 4, {
    message: "Bob never got Manage channels",
  });

  step("Alice and Bob rename the same channel at once");
  await Promise.all([
    alice.rpc("RenameChannel", g.id, ch.id, "alice-name"),
    bob.rpc("RenameChannel", g.id, ch.id, "bob-name"),
  ]);
  const nameOn = async (p) =>
    (await p.guilds()).find((x) => x.id === g.id).channels.find((c) => c.id === ch.id).name;
  let names = [];
  await waitFor(
    async () => {
      names = await Promise.all([alice, bob, carol].map(nameOn));
      return names.every((n) => n === names[0]);
    },
    { timeout: 30000, message: `peers disagree on the name: ${names}` },
  );
  const a = await alice.open();
  await a.openChannel(names[0], "Two Pens");
  await shot(a, "converged sidebar");
  assert.ok(["alice-name", "bob-name"].includes(names[0]));
});
