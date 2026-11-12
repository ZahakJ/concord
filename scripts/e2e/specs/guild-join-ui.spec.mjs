// Two people meet: one creates a guild in the UI, the other pastes the invite
// code into the Join dialog, and messages typed in each window arrive in the
// other.
import { test } from "../harness.mjs";

test("create, join by the Join dialog, message both ways", async ({ world, step }) => {
  const [alice, bashir] = await world.peers(2);
  const a = await alice.open();
  const b = await bashir.open();

  step("Alice creates the guild in the UI");
  await a.createGuild("Lantern Club");
  const guild = (await alice.guilds()).find((g) => g.name === "Lantern Club");
  const code = await alice.rpc("InviteCode", guild.id);

  step("Bashir joins with the code through the Join dialog");
  await b.joinGuild(code, "Lantern Club");
  const g = await world.settleGuild({ id: guild.id, name: guild.name }, [alice, bashir]);
  const general = g.allChannels.find((c) => c.type === "text" || !c.type).name;

  await a.openChannel(general, "Lantern Club");
  await b.openChannel(general, "Lantern Club");

  step("Alice → Bashir");
  await a.send("hello from alice");
  await b.waitForMessage("hello from alice");

  step("Bashir → Alice");
  await b.send("hi alice, bashir here");
  await a.waitForMessage("hi alice, bashir here");
});
