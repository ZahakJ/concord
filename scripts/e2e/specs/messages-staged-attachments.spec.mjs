// A file staged in the composer belongs to the conversation it was staged in,
// like the text draft does. It used to follow you: attach a photo in a DM, open
// a guild channel, answer something there, and the photo was posted to the
// guild along with the answer.
import fs from "node:fs";
import path from "node:path";
import { test } from "../harness.mjs";

// A 2x2 PNG, small enough to write inline.
const PNG = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAYAAABytg0kAAAAFElEQVR4nGP8z8DwnwEIGBmQAQMAKf4C/k9rnZgAAAAASUVORK5CYII=",
  "base64",
);

test("staged attachments stay with their own conversation", async ({ world, assert, step, waitFor, shot }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Trays", members: [alice, bashir], channels: ["random"] });
  const dm = await world.dm(alice, bashir);
  const general = g.channels.general;
  const random = g.channels.random;
  const pic = path.join(world.dir, "tray.png");
  fs.writeFileSync(pic, PNG);

  const a = await alice.open();
  const tray = () => a.page.locator(".attach-tray .att-chip");
  const sentImages = async (channelId) =>
    (await alice.messages(channelId)).filter((m) => m.content.includes("concord://attach/")).length;

  step("channel to channel: stage in #random, send text in #general");
  await a.openChannel("random", "Trays");
  await a.attach(pic);
  await waitFor(async () => (await tray().count()) === 1, { message: "the image to be staged in #random" });
  await a.composer().fill("caption meant for random");
  await a.openChannel("general", "Trays");
  assert.equal(await tray().count(), 0, "#general's composer must not hold #random's file");
  await a.send("hello general");
  await alice.waitForMessage(general, "hello general");
  assert.equal(await sentImages(general), 0, "nothing staged in #random may reach #general");

  step("back in #random the file and the caption are both still waiting");
  await a.openChannel("random", "Trays");
  await waitFor(async () => (await tray().count()) === 1, { message: "#random's tray to come back" });
  assert.equal(await a.composer().inputValue(), "caption meant for random");
  await shot(a, "random keeps its tray");

  step("guild to DM: the DM's composer starts empty, and sending there takes no file");
  await a.openDM("Bashir");
  assert.equal(await tray().count(), 0, "the DM must not inherit the guild channel's file");
  await a.send("just text in the DM");
  await alice.waitForMessage(dm.channelId, "just text in the DM");
  assert.equal(await sentImages(dm.channelId), 0);

  step("DM to guild: a file staged in the DM stays there");
  await a.attach(pic);
  await waitFor(async () => (await tray().count()) === 1, { message: "the image to be staged in the DM" });
  await a.openChannel("general", "Trays");
  assert.equal(await tray().count(), 0, "#general must not inherit the DM's file");
  await a.send("general again, text only");
  await alice.waitForMessage(general, "general again, text only");
  assert.equal(await sentImages(general), 0, "the DM's file must not be posted to the guild");

  step("each tray still sends where it was staged");
  await a.openChannel("random", "Trays");
  await waitFor(async () => (await tray().count()) === 1);
  await a.composer().press("Enter");
  await waitFor(async () => (await sentImages(random)) === 1, { message: "#random's image to be sent to #random" });
  await a.openDM("Bashir");
  await waitFor(async () => (await tray().count()) === 1, { message: "the DM's tray to come back" });
  await a.composer().press("Enter");
  await waitFor(async () => (await sentImages(dm.channelId)) === 1, { message: "the DM's image to be sent to the DM" });
  assert.equal(await sentImages(general), 0);
});
