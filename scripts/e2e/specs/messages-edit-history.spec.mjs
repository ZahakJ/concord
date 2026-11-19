// Where an edited message's history opens. On desktop the "(edited)" mark is a
// button that opens the card. On a phone the mark is plain text, too small to
// tap, and "Edit history" in the message's long-press menu opens the card.
import { test } from "../harness.mjs";

async function editedMessage(world) {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "History", members: [alice, bashir] });
  const ch = g.allChannels[0];
  const first = await bashir.send(ch.id, "said it one way");
  await bashir.rpc("EditMessage", ch.id, first.id, "said it better", "");
  await alice.waitForMessage(ch.id, (m) => m.id === first.id && m.edited);
  return { alice, ch };
}

const card = (a) => a.page.locator('.eh-card[role="dialog"][aria-label="Edit history"]');

test("desktop: the (edited) mark opens the edit history", async ({ world, assert, step }) => {
  const { alice, ch } = await editedMessage(world);
  const a = await alice.open();
  await a.openChannel(ch.name, "History");
  await a.waitForMessage("said it better");
  const mark = a.messageRow("said it better").locator(".eh button.eh-btn");
  assert.equal(await mark.count(), 1, "no (edited) button on desktop");

  step("clicking the mark opens the card with both versions");
  await mark.click();
  await card(a).waitFor({ state: "visible" });
  await card(a).getByText("said it one way").waitFor();
  assert.equal(await mark.getAttribute("aria-expanded"), "true");
  assert.deepEqual(a.errors, [], a.errors.join("\n"));
});

test("phone: (edited) is a plain mark and the long-press menu opens the history", async ({ world, assert, step }) => {
  const { alice, ch } = await editedMessage(world);
  const a = await alice.open({ viewport: "phone" });
  await a.openChannel(ch.name, "History");
  await a.waitForMessage("said it better");
  const row = a.messageRow("said it better");
  assert.equal(await row.locator(".eh button").count(), 0, "the (edited) mark is still a button on a phone");
  assert.equal((await row.locator(".eh .eh-mark").textContent()).trim(), "(edited)");

  step("tapping the mark opens nothing");
  await row.locator(".eh .eh-mark").tap();
  await a.page.waitForTimeout(400);
  assert.equal(await card(a).count(), 0, "a tap on the plain mark opened the card");

  step("long-press menu: Edit history");
  await a.longPress(a.message("said it better"));
  await a.page.getByRole("menuitem", { name: "Edit history", exact: true }).tap();
  await card(a).waitFor({ state: "visible" });
  await card(a).getByText("said it one way").waitFor();
  assert.deepEqual(a.errors, [], a.errors.join("\n"));
});
