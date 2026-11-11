// A guild you own says so on its strip tile (a ring, described as "you own
// this guild") and its owner wears the crown in the member list; a member's
// tile carries no such mark.
import { test } from "../harness.mjs";

test("the owner's guild and the owner are marked", async ({ world, assert, step, shot }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Workshop", members: [alice, bashir] });
  const a = await alice.open({ viewport: "desktop" });
  const b = await bashir.open({ viewport: "desktop" });
  await a.openChannel(g.allChannels[0].name || "general", "Workshop");
  await b.openChannel(g.allChannels[0].name || "general", "Workshop");

  step("Alice's tile is marked owned and described so; Bashir's is not");
  const tileA = a.page.locator('nav[aria-label="Guilds"]').getByRole("button", { name: "Workshop", exact: true });
  assert.match(await tileA.getAttribute("class"), /\bowned\b/);
  assert.match(await a.page.locator(`#strip-d-${g.id}`).innerText(), /you own this guild/);
  const tileB = b.page.locator('nav[aria-label="Guilds"]').getByRole("button", { name: "Workshop", exact: true });
  assert.doesNotMatch(await tileB.getAttribute("class"), /\bowned\b/);

  step("the crown beside the owner in the member list");
  await b.page.locator(".member", { hasText: "Alice" }).locator(".own-crown").waitFor();
  await shot(b, "owner crown in members");
});
