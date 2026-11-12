// On a phone a member's card is a bottom sheet. It opens with focus on the
// card, not on a role's ×; that × is a finger-sized target; and Undo on the
// "Removed …" toast puts the role back while the card stays open (a tap on the
// toast used to count as a tap outside the sheet and shut it).
import { test } from "../harness.mjs";

test("phone: the profile sheet opens safe and its role removal is undoable in place", async ({ world, assert, step, waitFor, shot }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Pocket", members: [alice, bashir] });
  const ch = g.allChannels[0];
  const organiser = await alice.rpc("UpsertRole", g.id, "", "Organiser", "#3ba55c", 1, 1);
  await alice.rpc("AssignRole", g.id, bashir.fingerprint, organiser, true);
  const holds = async (want) => {
    const ms = await bashir.rpc("Members", g.id);
    return !!ms.find((m) => m.fingerprint === bashir.fingerprint)?.roleIds?.includes(organiser) === want;
  };
  await waitFor(() => holds(true), { message: "Bashir never saw his Organiser role" });

  const a = await alice.open({ viewport: "phone" });
  const { page } = a;
  await a.openChannel(ch.name, "Pocket");

  step("Members page, then Bashir's card by touch");
  await page.getByRole("button", { name: "Members", exact: true }).first().tap();
  await page.locator(".drawer .mname", { hasText: "Bashir" }).first().tap();
  const card = page.locator('[role="dialog"][aria-label="Bashir profile"]');
  await card.waitFor({ state: "visible" });
  const focus = await waitFor(async () => {
    const f = await page.evaluate(() => ({
      role: document.activeElement?.getAttribute("role") || "",
      label: document.activeElement?.getAttribute("aria-label") || "",
    }));
    return f.role === "dialog" && f;
  }, { message: "focus moved onto the card" });
  assert.equal(focus.label, "Bashir profile");

  step("The role's × is a finger-sized target");
  const x = card.getByRole("button", { name: "Remove the Organiser role" });
  const hit = await x.evaluate((el) => {
    const a = getComputedStyle(el, "::after");
    const b = el.getBoundingClientRect();
    return { w: Math.max(b.width, parseFloat(a.width) || 0), h: Math.max(b.height, parseFloat(a.height) || 0) };
  });
  assert.ok(hit.w >= 44 && hit.h >= 44, `role × hit box ${hit.w}x${hit.h}`);

  step("Remove it, then Undo from the toast: the role comes back and the card stays");
  await x.tap();
  const toast = page.locator(".toast", { hasText: "Removed Organiser from Bashir" });
  await toast.waitFor({ state: "visible" });
  await waitFor(() => holds(false), { message: "the role never went" });
  await shot(a, "removed");
  await toast.getByRole("button", { name: "Undo" }).tap();
  await waitFor(() => holds(true), { message: "Undo never put the role back" });
  await card.getByRole("button", { name: "Remove the Organiser role" }).waitFor({ state: "visible" });
  assert.equal(await card.count(), 1, "the card closed under the Undo");
  await shot(a, "after undo");
});
