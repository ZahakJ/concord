// The composer's completion list and file intake. A #channel after an "@"
// earlier on the line got no list at all (the "@" swallowed the line as one
// long @-query), and an empty file was staged, sent and failed with a raw
// backend error and a Retry that could only fail again.
import fs from "node:fs";
import path from "node:path";
import { test, sleep } from "../harness.mjs";

test("#channel completes after an @, and an empty file is refused up front", async ({ world, assert, step }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Typing", members: [alice, bashir], channels: ["random"] });
  const a = await alice.open();
  await a.openChannel("general", "Typing");
  const pop = a.page.locator(".suggest-pop");

  step("an @ earlier on the line does not hide the #channel list");
  await a.composer().click();
  await a.composer().pressSequentially("@nobody look at #ran", { delay: 15 });
  await pop.waitFor();
  assert.match(await pop.innerText(), /random/);
  await a.composer().press("Enter");
  assert.match(await a.composer().inputValue(), /#random $/);
  await a.composer().fill("");

  step("an empty file is refused with a sentence, and nothing is staged or sent");
  const empty = path.join(world.dir, "empty.txt");
  fs.writeFileSync(empty, "");
  const before = (await alice.messages(g.channels.general)).length;
  await a.attach(empty);
  // The toast itself, not the copy the live region holds for screen readers.
  await a.page.getByText("empty.txt is empty").and(a.page.locator(":not(.sr-only)")).waitFor();
  assert.equal(await a.page.locator(".attach-tray .att-chip").count(), 0);
  await a.composer().press("Enter");
  await sleep(1500);
  assert.equal((await alice.messages(g.channels.general)).length, before);
  assert.equal(await a.page.getByText("attachment must be a data URL").count(), 0);
});

// Accepting a completion parks the caret after the inserted text, and does it
// before the keystroke's handling ends, so a select-all and Delete that come
// before the next animation frame clear the whole box. The frame is held here
// to fix the order: completion, select-all, frame, Delete. Playwright's
// fill("") is exactly select-all then Delete.
test("a box cleared right after a completion stays cleared and sends nothing", async ({ world, assert, step }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Typing", members: [alice, bashir], channels: ["random"] });
  const a = await alice.open();
  await a.openChannel("general", "Typing");
  const pop = a.page.locator(".suggest-pop");

  step("complete #random while animation frames are held");
  await a.composer().click();
  await a.composer().pressSequentially("look at #ran", { delay: 15 });
  await pop.waitFor();
  await a.page.evaluate(() => {
    const held = [];
    const raf = window.requestAnimationFrame;
    window.requestAnimationFrame = (cb) => held.push(cb);
    window.__releaseFrame = () => {
      window.requestAnimationFrame = raf;
      const t = performance.now();
      for (const cb of held.splice(0)) cb(t);
    };
  });
  await a.composer().press("Enter");
  assert.equal(await a.composer().inputValue(), "look at #random ");

  step("select all, let the frame run, then Delete: the box is empty");
  await a.composer().evaluate((el) => el.setSelectionRange(0, el.value.length));
  await a.page.evaluate(() => window.__releaseFrame());
  await a.composer().press("Delete");
  assert.equal(await a.composer().inputValue(), "");

  step("Enter on the empty box sends nothing");
  const before = (await alice.messages(g.channels.general)).length;
  await a.composer().press("Enter");
  await sleep(1500);
  assert.equal((await alice.messages(g.channels.general)).length, before);
});

// The same order after an emoji is picked from the picker: the pick places the
// caret before the click's handling ends, so a select-all and Delete before
// the next frame empty the box. The cell is clicked from the page because
// Playwright's own click waits on animation frames, which are held.
test("a box cleared right after an emoji pick stays cleared and sends nothing", async ({ world, assert, step }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Typing", members: [alice, bashir] });
  const a = await alice.open();
  await a.openChannel("general", "Typing");

  step("pick an emoji from the picker while animation frames are held");
  await a.composer().click();
  await a.composer().pressSequentially("hi ", { delay: 15 });
  await a.page.locator('button[aria-label="Emoji picker"]').first().click();
  const cell = a.page.locator(".grid button.cell").first();
  await cell.waitFor();
  await a.page.evaluate(() => {
    const held = [];
    const raf = window.requestAnimationFrame;
    window.requestAnimationFrame = (cb) => held.push(cb);
    window.__releaseFrame = () => {
      window.requestAnimationFrame = raf;
      const t = performance.now();
      for (const cb of held.splice(0)) cb(t);
    };
  });
  await cell.evaluate((el) => el.click());
  const picked = await a.composer().inputValue();
  assert.ok(picked.length > 3 && picked.startsWith("hi "), `picked: ${JSON.stringify(picked)}`);

  step("select all, let the frame run, then Delete: the box is empty");
  await a.composer().evaluate((el) => el.setSelectionRange(0, el.value.length));
  await a.page.evaluate(() => window.__releaseFrame());
  await a.composer().press("Delete");
  assert.equal(await a.composer().inputValue(), "");

  step("Enter on the empty box sends nothing");
  const before = (await alice.messages(g.channels.general)).length;
  await a.composer().press("Enter");
  await sleep(1500);
  assert.equal((await alice.messages(g.channels.general)).length, before);
});
