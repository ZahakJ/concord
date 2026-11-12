// The Join dialog names why a code did not get you in — clipped code, ban,
// inviter offline — ties a bad code to the field, never prints the core's
// raw string, and offers "Open it" for a guild you are already in.
import { test } from "../harness.mjs";

test("join failures say what went wrong", async ({ world, assert, step, waitFor, shot }) => {
  const [alice, bashir, dave, eve] = await world.peers(4, { names: ["Alice", "Bashir", "Dave", "Eve"] });
  const g = await world.guild({ name: "Front Door", members: [alice, bashir, dave] });
  const code = await alice.rpc("InviteCode", g.id);

  const openJoin = async (app) => {
    await app.page.keyboard.press("Escape");
    await app.switcherAction("Join with an invite code");
    const box = app.page.getByRole("textbox", { name: "Invite code" });
    await box.waitFor({ state: "visible" });
    return box;
  };
  const submit = (app) => app.dialog().getByRole("button", { name: /^(Join|Open it)$/ }).click();

  step("Bashir pastes a code for a guild he is already in");
  const b = await bashir.open();
  let box = await openJoin(b);
  await box.fill(code);
  await b.dialog().getByText("You're already in this guild.").waitFor({ state: "visible" });
  assert.equal(await b.dialog().getByRole("button", { name: "Open it" }).count(), 1);
  await submit(b);
  await b.dialog().waitFor({ state: "detached" });

  step("a clipped code is the field's fault");
  box = await openJoin(b);
  await box.fill(code.slice(0, 40));
  await submit(b);
  await b.dialog().getByText("That isn't a whole invite code.").waitFor({ state: "visible" });
  assert.equal(await box.getAttribute("aria-invalid"), "true");
  assert.doesNotMatch(await b.dialog().innerText(), /bad invite code|invite rejected/);

  step("Dave is banned and tries again");
  await alice.rpc("BanMember", g.id, dave.fingerprint, "");
  await waitFor(async () => (await dave.guilds()).find((x) => x.id === g.id)?.evicted, {
    timeout: 30000,
    message: "Dave never learned he was out",
  });
  const d = await dave.open();
  box = await openJoin(d);
  await box.fill(code);
  await submit(d);
  await d.dialog().getByText("You're banned from this guild.").waitFor({ state: "visible", timeout: 45000 });
  await shot(d, "banned join");
  assert.doesNotMatch(await d.dialog().innerText(), /invite rejected|fresh one/);
  assert.match(await d.dialog().innerText(), /Front Door/);

  step("Eve's inviter is offline");
  await alice.stop();
  const e = await eve.open();
  box = await openJoin(e);
  await box.fill(code);
  await submit(e);
  await e.dialog().getByText("The code is fine, but its owner isn't reachable.").waitFor({ state: "visible", timeout: 90000 });
  await shot(e, "inviter offline");
  assert.match(await e.dialog().innerText(), /needs to be online/);
  assert.match(await e.dialog().innerText(), /Front Door/);
  assert.equal(await e.page.getByText(/Joining “”/).count(), 0);
});
