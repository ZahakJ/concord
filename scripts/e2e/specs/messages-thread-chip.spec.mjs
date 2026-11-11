// A message that a thread was started from says so: a chip under it names
// the thread and opens it, for the person who started it and for everyone
// else — read back from the thread's opener, with nothing new on the wire.
import { test } from "../harness.mjs";

test("a message with a thread shows a chip that opens it, on both peers", async ({ world, assert, step, waitFor }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Threads", members: [alice, bashir] });
  const general = g.allChannels[0];
  await bashir.send(general.id, "what glue holds paper to bamboo?");

  const a = await alice.open();
  await a.openChannel(general.name, "Threads");
  await a.waitForMessage("what glue holds paper to bamboo?");

  step("Alice starts a thread from Bashir's message");
  await a.messageAction("what glue holds paper to bamboo?", "Start a thread");
  const title = a.page.getByRole("textbox", { name: "Thread title" });
  await title.fill("Glue for lanterns");
  await title.press("Enter");
  await waitFor(async () => (await a.composer().getAttribute("placeholder"))?.includes("thread"), { message: "inside the new thread" });

  step("back in #general, the message carries the thread's chip");
  await a.openChannel(general.name, "Threads");
  const chip = a.messageRow("what glue holds paper to bamboo?").getByRole("button", { name: /Glue for lanterns/ });
  await chip.waitFor({ state: "visible", timeout: 15000 });

  step("Bashir sees the same chip once the thread reaches him, and it opens the thread");
  const b = await bashir.open();
  await waitFor(async () => (await bashir.guilds()).find((x) => x.id === g.id)?.channels?.some((c) => c.name === "Glue for lanterns"), {
    timeout: 45000,
    message: "the thread reached Bashir",
  });
  await b.openChannel(general.name, "Threads");
  const bchip = b.messageRow("what glue holds paper to bamboo?").getByRole("button", { name: /Glue for lanterns/ });
  await bchip.waitFor({ state: "visible", timeout: 30000 });
  await bchip.click();
  await waitFor(async () => (await b.composer().getAttribute("placeholder"))?.includes("thread"), { message: "the chip opened the thread" });
  assert.ok(true);
});
