// A moderator's menu on an admin (a senior role) offers no Mute, Kick or Ban,
// while the same menu on a plain member still does.
import { test } from "../harness.mjs";

test("moderation menus respect role rank", async ({ world, assert, step, waitFor, shot }) => {
  const [alice, bob, carol, dave] = await world.peers(4, { names: ["Alice", "Bob", "Carol", "Dave"] });
  const g = await world.guild({ name: "Ranked", members: [alice, bob, carol, dave] });
  const ch = g.allChannels[0];

  step("Bob is Admin (position 5), Carol is Moderator (position 1)");
  const admin = await alice.rpc("UpsertRole", g.id, "", "Admin", "#e91e63", 1 | 16 | 32, 5);
  const mod = await alice.rpc("UpsertRole", g.id, "", "Moderator", "#5865f2", 1 | 32, 1);
  await alice.rpc("AssignRole", g.id, bob.fingerprint, admin, true);
  await alice.rpc("AssignRole", g.id, carol.fingerprint, mod, true);
  await waitFor(
    async () => {
      const ms = await carol.rpc("Members", g.id);
      return ms.find((m) => m.fingerprint === bob.fingerprint)?.roleIds?.length === 1 &&
        ms.find((m) => m.fingerprint === carol.fingerprint)?.roleIds?.length === 1;
    },
    { message: "roles never reached Carol" },
  );
  const ms = await carol.rpc("Members", g.id);
  assert.equal(ms.find((m) => m.fingerprint === bob.fingerprint).outranked, false);
  assert.equal(ms.find((m) => m.fingerprint === dave.fingerprint).outranked, true);

  const c = await carol.open();
  await c.openChannel(ch.name, "Ranked");
  const menuFor = async (name) => {
    await c.page.keyboard.press("Escape");
    await c.page.locator(".mname", { hasText: name }).first().click({ button: "right" });
    const menu = c.page.locator(".cm[role=menu]");
    await menu.waitFor({ state: "visible" });
    return menu;
  };

  step("Carol's menu on Bob (senior)");
  let menu = await menuFor("Bob");
  await shot(c, "moderator menu on admin");
  const seniorItems = await menu.innerText();
  assert.match(seniorItems, /View profile/);
  assert.equal(await menu.getByText("Kick", { exact: true }).count(), 0);
  assert.equal(await menu.getByText(/^Mute/).count(), 0);
  assert.equal(await menu.getByText("Ban", { exact: true }).count(), 0);

  step("Carol's menu on Dave (no role)");
  menu = await menuFor("Dave");
  await shot(c, "moderator menu on member");
  assert.equal(await menu.getByText("Kick", { exact: true }).count(), 1);
});
