// Renaming follows the same rules as creating: a text channel rename is
// slugged with a preview, the Rename-guild dialog says Rename and starts from
// the current name, and Guild settings takes as long a name as Create did.
import { test } from "../harness.mjs";

test("rename uses the create rules", async ({ world, assert, step, waitFor, shot }) => {
  const [alice] = await world.peers(1, { names: ["Alice"] });
  const g = await world.guild({ name: "Naming Hall", members: [alice] });
  const ch = g.allChannels[0];
  const a = await alice.open();
  await a.openChannel(ch.name, "Naming Hall");

  step("rename a channel to free text");
  await a.page.getByRole("button", { name: new RegExp(ch.name) }).first().click({ button: "right" });
  await a.page.locator(".cm[role=menu]").getByText("Rename channel").click();
  const box = a.dialog().getByRole("textbox", { name: "Channel name" });
  await box.fill("Feature Ideas!!");
  await a.dialog().getByText("Will be renamed to").waitFor({ state: "visible" });
  await shot(a, "rename channel preview");
  await a.dialog().getByRole("button", { name: "Rename" }).click();
  await waitFor(
    async () => (await alice.guilds()).find((x) => x.id === g.id).channels.find((c) => c.id === ch.id).name === "feature-ideas",
    { message: "the channel was not renamed to its slug" },
  );

  step("rename the guild");
  await a.page.locator('nav[aria-label="Guilds"]').getByRole("button", { name: "Naming Hall", exact: true }).first().click({ button: "right" });
  await a.page.locator(".cm[role=menu]").getByText("Rename guild").click();
  const dlg = a.dialog();
  await dlg.waitFor({ state: "visible" });
  await shot(a, "rename guild dialog");
  assert.equal(await dlg.getByRole("button", { name: "Create" }).count(), 0);
  const field = dlg.getByRole("textbox", { name: "Guild name" });
  assert.equal(await field.inputValue(), "Naming Hall");
  const long = "A".repeat(20) + " " + "B".repeat(27);
  await field.fill(long);
  await dlg.getByRole("button", { name: "Rename" }).click();
  await waitFor(async () => (await alice.guilds()).find((x) => x.id === g.id).name === long, { message: "rename did not land" });
});
