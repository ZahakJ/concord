// On a phone, Reply, Edit, opening a DM from a profile card and tapping a
// conversation that holds a draft all land in a text box with the keyboard up.
// A mobile browser raises the keyboard only for a focus made while it handles
// the tap, so each test reads document.activeElement at the end of the tap's
// click (a window listener runs after the app's): a text field there is a
// keyboard on the phone, <body> there is a caret with no keyboard under it.
// Reply also keeps the answered message on screen while the keyboard (stood in
// for by the --kb the Android shell pushes) shrinks the feed under it.
import { test } from "../harness.mjs";

// Arms one-shot window listeners that record what has focus once the tap's
// click has been handled, and its form submission if the tap submits one (that
// runs after the click's listeners, in the same task).
async function armTapProbe(page) {
  await page.evaluate(() => {
    window.__tapFocus = null;
    const read = () => {
      const el = document.activeElement;
      window.__tapFocus = el ? `${el.tagName.toLowerCase()}.${[...el.classList].join(".")}` : "none";
    };
    window.addEventListener("click", read, { once: true });
    window.addEventListener("submit", read, { once: true });
  });
}
const tapFocus = (page) => page.evaluate(() => window.__tapFocus);

test("phone: Reply and Edit focus a text box inside the tap, and the replied-to message stays in view", async ({
  world,
  assert,
  step,
  waitFor,
  sleep,
}) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Focus", members: [alice, bashir] });
  const ch = g.allChannels[0];
  for (let i = 1; i <= 30; i++) await bashir.send(ch.id, `line ${String(i).padStart(2, "0")} from bashir`);
  await alice.send(ch.id, "mine to fix");
  const a = await alice.open({ viewport: "phone" });
  await a.openChannel(ch.name, "Focus");
  await a.waitForMessage("mine to fix");
  const p = a.page;

  step("Reply from the long-press sheet");
  // Put the answered message near the bottom edge of the feed but not at the
  // end, where the keyboard arriving would cover it.
  const target = "line 24 from bashir";
  await a.message(target).waitFor();
  await p.evaluate((t) => {
    const f = document.querySelector(".feed");
    const row = [...f.querySelectorAll(".msg[data-msg-id]")].find((r) => r.textContent.includes(t));
    const fr = f.getBoundingClientRect();
    f.scrollTop += row.getBoundingClientRect().bottom - fr.bottom + 60;
  }, target);
  await sleep(500);
  await a.longPress(a.message(target));
  await armTapProbe(p);
  await p.getByRole("menuitem", { name: "Reply", exact: true }).first().tap();
  assert.match(String(await tapFocus(p)), /^textarea\..*draft/, `focus at the end of the Reply tap: ${await tapFocus(p)}`);
  // The keyboard: what the Android shell pushes when the IME opens.
  await p.evaluate(() => document.documentElement.style.setProperty("--kb", "300px"));
  await sleep(1200);
  const seen = await p.evaluate((t) => {
    const f = document.querySelector(".feed").getBoundingClientRect();
    const row = [...document.querySelectorAll(".feed .msg[data-msg-id]")].find((r) => r.textContent.includes(t));
    const r = row.getBoundingClientRect();
    return { rowTop: r.top, rowBottom: r.bottom, feedTop: f.top, feedBottom: f.bottom };
  }, target);
  assert.ok(seen.rowBottom <= seen.feedBottom + 1 && seen.rowTop >= seen.feedTop - 1, `the answered message is out of view: ${JSON.stringify(seen)}`);
  assert.ok(await p.locator("textarea.draft").evaluate((el) => el === document.activeElement), "the composer kept focus");
  await p.evaluate(() => document.documentElement.style.setProperty("--kb", "0px"));
  await p.getByRole("button", { name: "Cancel reply" }).first().tap();

  step("Edit from the long-press sheet");
  await a.longPress(a.message("mine to fix"));
  await armTapProbe(p);
  await p.getByRole("menuitem", { name: "Edit", exact: true }).first().tap();
  assert.match(String(await tapFocus(p)), /^textarea\..*edit-input/, `focus at the end of the Edit tap: ${await tapFocus(p)}`);
  const caret = await p.locator(".edit-input").evaluate((el) => [el.selectionStart, el.value.length]);
  assert.equal(caret[0], caret[1], "the caret waits at the end of the text");
  assert.deepEqual(a.errors, [], a.errors.join("\n"));
});

test("phone: a DM from a profile card and a conversation with a draft open with the keyboard up", async ({
  world,
  assert,
  step,
  waitFor,
}) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Cards", members: [alice, bashir] });
  const ch = g.allChannels[0];
  await bashir.send(ch.id, "tap my face");
  const a = await alice.open({ viewport: "phone" });
  await a.openChannel(ch.name, "Cards");
  const p = a.page;
  const composerFocused = () => p.evaluate(() => document.activeElement?.matches?.("textarea.draft") || false);

  step("Message from Bashir's profile card");
  await a.messageRow("tap my face").getByRole("button", { name: "View profile" }).first().tap();
  await armTapProbe(p);
  await p.getByRole("button", { name: "Send message", exact: true }).tap();
  // Inside the tap a stand-in holds the keyboard while the DM is created.
  assert.match(String(await tapFocus(p)), /^textarea\./, `focus at the end of the tap: ${await tapFocus(p)}`);
  await waitFor(async () => (await a.composer().getAttribute("placeholder"))?.includes("Bashir"), {
    timeout: 20000,
    message: "the DM with Bashir to open",
  });
  await waitFor(composerFocused, { timeout: 5000, message: "the DM's composer to hold focus" });

  step("A draft left in the DM, then the DM tapped from the Messages list");
  await a.composer().fill("half a thought");
  await p.evaluate(() => document.activeElement?.blur());
  await a.back();
  const row = p.locator(".dm-item", { hasText: "Draft:" }).first();
  await row.waitFor();
  await armTapProbe(p);
  await row.tap();
  assert.match(String(await tapFocus(p)), /^textarea\./, `focus at the end of the tap: ${await tapFocus(p)}`);
  await waitFor(composerFocused, { timeout: 10000, message: "the composer to hold focus" });
  assert.equal(await a.composer().inputValue(), "half a thought");
  const caret = await a.composer().evaluate((el) => [el.selectionStart, el.value.length]);
  assert.equal(caret[0], caret[1], "the caret waits after the draft");
  assert.deepEqual(a.errors, [], a.errors.join("\n"));
});
