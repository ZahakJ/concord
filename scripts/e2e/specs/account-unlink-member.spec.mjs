// An ordinary member of someone else's guild unlinks one of their two devices.
// The member may not remove anyone there, and committing the removal anyway
// used to cut the member's own remaining device out of the guild while the
// unlink said it had worked. Now the unlink succeeds, shows no error, names
// where the device is still listed and who can remove it there, and the
// member's remaining device keeps talking with the guild both ways.
import { test } from "../harness.mjs";

test("an ordinary member unlinks a device: success, where it is still listed, and messages still flow both ways", async ({ world, assert, step }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Alice's Place", members: [alice, bashir] });
  const ch = g.allChannels[0];
  const phone = await world.linkDevice(bashir, { name: "BashirPhone" });
  await world.settleGuild({ id: g.id, name: g.name }, [alice, bashir, phone]);

  step("the phone goes off; Bashir unlinks it from his desktop");
  await phone.stop();
  const app = await bashir.open();
  const dlg = await app.openSettings("Your devices");
  // A linked device is listed by its platform, and only the other one has the button.
  await dlg.locator("li", { hasText: "Linux desktop" }).getByRole("button", { name: "Unlink" }).click();
  const confirm = app.dialog();
  assert.ok((await confirm.innerText()).includes("Anywhere else it stays listed"), "the confirmation does not say the device can stay listed");
  await confirm.getByRole("button", { name: "Unlink", exact: true }).click();

  step("it says unlinked, and where it is still listed and who can remove it, without an error");
  await app.page.locator(".toast.success", { hasText: "Linux desktop unlinked" }).waitFor();
  const note = app.page.locator(".pending[role=status]");
  await note.waitFor();
  const text = await note.innerText();
  for (const part of ["Linux desktop is still listed in:", "Alice's Place", "An owner or moderator there can remove it", "it can still receive new messages there"])
    assert.ok(text.includes(part), `the note lacks "${part}": ${text}`);
  assert.equal(await app.page.locator(".toast.error").count(), 0, "the unlink showed an error");

  step("Bashir's desktop and Alice still exchange messages both ways");
  await bashir.send(ch.id, "bashir after the unlink");
  await alice.waitForMessage(ch.id, "bashir after the unlink");
  await alice.send(ch.id, "alice after the unlink");
  await bashir.waitForMessage(ch.id, "alice after the unlink");
});
