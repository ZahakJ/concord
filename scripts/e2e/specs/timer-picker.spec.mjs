// The disappearing-messages picker says only what the timer does: it applies
// to what you send from now on, and devices of yours still on 0.60 never
// learn it, so it must not promise "every device".
import { test } from "../harness.mjs";

test("the timer picker's intro promises only what the timer does", async ({ world, assert, step, waitFor }) => {
  const [alice] = await world.peers(1);
  const notes = (await alice.rpc("NotesDM")).channels[0].id;
  const a = await alice.open();
  const { page } = a;

  step("open the picker from the conversation's header menu");
  await a.openDM("Notes");
  await page.locator(".chat-head").getByRole("button", { name: "More" }).click();
  await page.getByRole("menuitem", { name: "Disappearing messages" }).click();
  const dialog = page.getByRole("dialog", { name: "Disappearing messages" });
  const intro = (await dialog.locator(".intro").innerText()).replace(/\s+/g, " ").trim();
  assert.ok(!/every device/i.test(intro), `the intro promises every device: "${intro}"`);
  assert.match(intro, /^Messages you send in this conversation from now on are deleted after the time you choose\./);
  assert.match(intro, /0\.60/, "the intro does not say older devices skip the timer");

  step("a choice is kept by the core for this chat");
  await dialog.getByRole("radio", { name: "5 minutes" }).click();
  await waitFor(async () => ((await alice.rpc("ChatPrefs"))?.[notes]?.t || 0) === 300, { message: "the core keeps the 5-minute timer" });
});
