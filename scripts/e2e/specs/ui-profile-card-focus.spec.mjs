// A member's card opened from the keyboard parks focus on the card itself, not
// on its first control. That control was the × of the member's first role, so
// Enter on a member row and Enter again took the role away without a word.
// Taking a role off from the card now says so and offers it back, and the Undo
// reaches the other members like any other role change.
import { test } from "../harness.mjs";

const focused = (page) =>
  page.evaluate(() => {
    const el = document.activeElement;
    return {
      tag: el?.tagName || "",
      role: el?.getAttribute("role") || "",
      label: (el?.getAttribute("aria-label") || el?.textContent || "").trim().replace(/\s+/g, " "),
      inCard: !!el?.closest('[role="dialog"]'),
    };
  });

test("a member's card opened by keyboard is not one Enter from removing a role", async ({ world, assert, step, waitFor, sleep, shot }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Cards", members: [alice, bashir] });
  const ch = g.allChannels[0];
  const organiser = await alice.rpc("UpsertRole", g.id, "", "Organiser", "#3ba55c", 1, 1);
  await alice.rpc("AssignRole", g.id, bashir.fingerprint, organiser, true);
  const bashirHolds = async (want) => {
    const ms = await bashir.rpc("Members", g.id);
    return !!ms.find((m) => m.fingerprint === bashir.fingerprint)?.roleIds?.includes(organiser) === want;
  };
  await waitFor(() => bashirHolds(true), { message: "Bashir never saw his Organiser role" });
  await alice.send(ch.id, "@Bashir are you around?");
  await bashir.send(ch.id, "here");

  const a = await alice.open();
  const { page } = a;
  await a.openChannel(ch.name, "Cards");
  await a.waitForMessage("here");
  if (!(await page.locator(".member").count())) await page.keyboard.press("Control+u");
  const card = page.locator('[role="dialog"][aria-label="Bashir profile"]');

  // Each way the card opens; every one of them must leave focus somewhere an
  // Enter does nothing.
  const opens = {
    "Enter on his member row": async () => {
      await page.locator(".member", { hasText: "Bashir" }).first().focus();
      await page.keyboard.press("Enter");
    },
    "a click on his name in the feed": () => a.messageRow("here").locator("button.sender").first().click(),
    "a click on the @Bashir mention": () => a.message("are you around?").locator(".mention").first().click(),
  };
  for (const [how, open] of Object.entries(opens)) {
    step(`open the card by ${how}`);
    await page.keyboard.press("Escape");
    await waitFor(async () => !(await card.count()), { message: "the card closed" });
    await open();
    await card.waitFor({ state: "visible" });
    const f = await waitFor(async () => {
      const f = await focused(page);
      return f.inCard && f;
    }, { message: `focus moved into the card (${how})` });
    assert.doesNotMatch(f.label, /^Remove /, `focus landed on a destructive control after ${how}: ${f.label}`);
    assert.equal(f.role, "dialog", `the card itself holds focus after ${how}, not ${f.tag} "${f.label}"`);
    if (how.startsWith("Enter")) await shot(a, "card opened from the member list");

    step("Enter on what has focus changes nothing");
    await page.keyboard.press("Enter");
    // Long enough for a role change to be applied and gossiped; the assertion
    // is the state afterwards, not the pause.
    await sleep(1500);
    assert.ok(await bashirHolds(true), `Enter after ${how} took Bashir's role away`);
    const mine = await alice.rpc("Members", g.id);
    assert.ok(mine.find((m) => m.fingerprint === bashir.fingerprint).roleIds.includes(organiser), "Alice's own view lost the role");
  }

  step("Tab to the role's × and remove it on purpose: a toast says so, with Undo");
  if (!(await card.count())) {
    await opens["Enter on his member row"]();
    await card.waitFor({ state: "visible" });
  }
  for (let i = 0; i < 20 && (await focused(page)).label !== "Remove the Organiser role"; i++) await page.keyboard.press("Tab");
  assert.equal((await focused(page)).label, "Remove the Organiser role");
  await page.keyboard.press("Enter");
  const toast = page.locator(".toast", { hasText: "Removed Organiser from Bashir" });
  await toast.waitFor({ state: "visible" });
  await waitFor(() => bashirHolds(false), { message: "Bashir's peer never saw the role go" });
  await shot(a, "removed with undo");

  step("Undo gives it back, everywhere");
  await toast.getByRole("button", { name: "Undo" }).click();
  await waitFor(() => bashirHolds(true), { message: "Bashir's peer never saw the role come back" });
  await waitFor(async () => !(await toast.count()), { message: "the toast went with its Undo" });
});
