// A reply whose original was deleted for this person alone quotes it as
// "Deleted for you", the words of the action that removed it, and never shows
// the original's text.
import { test } from "../harness.mjs";

test("a reply to a message deleted for you says so in its quote", async ({ world, assert, step, waitFor }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Quotes", members: [alice, bashir] });
  const ch = g.allChannels[0];
  const a = await alice.open();
  await a.openChannel(ch.name, "Quotes");

  const original = await bashir.send(ch.id, "plain words to delete");
  await bashir.sendRaw(ch.id, "an answer underneath", original.id);
  await a.waitForMessage("an answer underneath");
  const quote = a.messageRow("an answer underneath").locator(".reply-ref .reply-snippet");
  await waitFor(async () => (await quote.textContent())?.trim() === "plain words to delete", {
    message: "the reply to quote the original first",
  });

  step("Alice deletes the original for herself only");
  await a.messageAction("plain words to delete", "Delete for me");
  await a.dialog().getByRole("button", { name: "Delete for me", exact: true }).click();
  await a.waitForNoMessage("plain words to delete");

  await waitFor(async () => (await quote.textContent())?.trim() === "Deleted for you", {
    message: 'the reply quote to read "Deleted for you"',
  });
  const html = await a.messageRow("an answer underneath").innerHTML();
  assert.ok(!html.includes("plain words to delete"), html.slice(0, 500));
  assert.deepEqual(a.errors, [], a.errors.join("\n"));
});
