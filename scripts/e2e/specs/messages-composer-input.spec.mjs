// The composer's completion list and file intake. A #channel after an "@"
// earlier on the line got no list at all (the "@" swallowed the line as one
// long @-query), and an empty file was staged, sent and failed with a raw
// backend error and a Retry that could only fail again.
import fs from "node:fs";
import path from "node:path";
import { test, sleep } from "../harness.mjs";

test("#channel completes after an @, and an empty file is refused up front", async ({ world, assert, step }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Typing", members: [alice, bashir], channels: ["random"] });
  const a = await alice.open();
  await a.openChannel("general", "Typing");
  const pop = a.page.locator(".suggest-pop");

  step("an @ earlier on the line does not hide the #channel list");
  await a.composer().click();
  await a.composer().pressSequentially("@nobody look at #ran", { delay: 15 });
  await pop.waitFor();
  assert.match(await pop.innerText(), /random/);
  await a.composer().press("Enter");
  assert.match(await a.composer().inputValue(), /#random $/);
  await a.composer().fill("");

  step("an empty file is refused with a sentence, and nothing is staged or sent");
  const empty = path.join(world.dir, "empty.txt");
  fs.writeFileSync(empty, "");
  const before = (await alice.messages(g.channels.general)).length;
  await a.attach(empty);
  await a.page.getByText("empty.txt is empty").waitFor();
  assert.equal(await a.page.locator(".attach-tray .att-chip").count(), 0);
  await a.composer().press("Enter");
  await sleep(1500);
  assert.equal((await alice.messages(g.channels.general)).length, before);
  assert.equal(await a.page.getByText("attachment must be a data URL").count(), 0);
});
