// Notes and one-to-one DMs cannot be shared with an invite code.
import { test } from "../harness.mjs";

test("no invite code for Notes or a 1:1", async ({ world, assert }) => {
  const [alice, bashir] = await world.peers(2);
  await world.guild({ name: "Shared", members: [alice, bashir] });
  const dm = await world.dm(alice, bashir);
  const notes = (await alice.guilds()).find((g) => g.kind === "dm" && g.dmNotes);
  assert.ok(notes, "Alice has Notes");
  await assert.rejects(alice.rpc("InviteCode", notes.id), /can't be shared/);
  await assert.rejects(alice.rpc("InviteCode", dm.id), /can't be shared/);
});
