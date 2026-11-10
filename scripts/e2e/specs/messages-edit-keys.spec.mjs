// Editing a message only ever saves on Enter or Save. Clicking somewhere else
// used to publish the half-typed edit to everyone; emptying the text used to
// close the editor and say nothing; Shift on the delete button is the fast
// path that skips the question.
import { test, sleep } from "../harness.mjs";

test("an edit saves on Enter only, and an emptied edit offers to delete", async ({ world, assert, step, shot }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Edits", members: [alice, bashir], channels: ["random"] });
  const ch = g.channels.general;
  const a = await alice.open();
  await a.openChannel("general", "Edits");
  await a.send("the original words");
  const orig = await alice.waitForMessage(ch, "the original words");
  const editBox = () => a.page.locator(".edit-wrap textarea");

  step("click into the composer mid-edit: the editor stays open and nothing is saved");
  await a.messageAction("the original words", "Edit");
  await editBox().fill("HALF TYPED");
  await a.composer().click();
  await sleep(1000);
  assert.equal(await editBox().count(), 1, "the editor closed when focus left it");
  assert.equal((await alice.messages(ch)).find((m) => m.id === orig.id).content, "the original words");

  step("switch channels mid-edit: still nothing saved");
  await a.openChannel("random", "Edits");
  await a.openChannel("general", "Edits");
  await sleep(1000);
  const after = (await alice.messages(ch)).find((m) => m.id === orig.id);
  assert.equal(after.content, "the original words", "walking away published the half-typed edit");
  assert.ok(!after.edited);

  step("Escape cancels, Enter saves");
  await a.messageAction("the original words", "Edit");
  await editBox().fill("not this");
  await editBox().press("Escape");
  await sleep(500);
  assert.equal((await alice.messages(ch)).find((m) => m.id === orig.id).content, "the original words");
  await a.editMessage("the original words", "the corrected words");
  await bashir.waitForMessage(ch, "the corrected words");

  step("emptying the text asks whether to delete instead");
  await a.messageAction("the corrected words", "Edit");
  await editBox().fill("");
  await editBox().press("Enter");
  const dlg = a.dialog();
  await dlg.getByText("Delete this message instead?").waitFor();
  await shot(a, "emptied edit offers delete");
  await dlg.getByRole("button", { name: "Delete", exact: true }).click();
  await bashir.waitForMessage(ch, (m) => m.id === orig.id && m.deleted, { deleted: true });

  step("Shift on the delete button deletes without asking");
  await a.send("delete me fast");
  const fast = await alice.waitForMessage(ch, "delete me fast");
  const id = await a.messageRow("delete me fast").getAttribute("data-msg-id");
  const row = a.page.locator(`.msg[data-msg-id="${id}"]`);
  await row.locator(".msg-main").first().hover();
  await row
    .getByRole("toolbar", { name: "Message actions" })
    .getByRole("button", { name: "Delete", exact: true })
    .click({ modifiers: ["Shift"] });
  await alice.waitForMessage(ch, (m) => m.id === fast.id && m.deleted, { deleted: true });
  assert.equal(await a.page.locator('[role="dialog"]').count(), 0, "Shift+Delete still asked");
});
