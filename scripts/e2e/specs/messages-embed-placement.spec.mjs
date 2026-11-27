// The sender chooses where a link's player sits: above or below the words.
// The choice is a chip in the composer that appears once the draft has a
// link and words, and it travels in the message itself (as the order of its
// lines), so every peer draws the same thing, and an older peer that knows
// nothing of the chip still shows the link first.
import { test } from "../harness.mjs";

const URL = "https://youtu.be/dQw4w9WgXcQ";

test("the sender puts the player above the words, and the peer sees it there", async ({ world, assert, step, waitFor }) => {
  const [alice, bashir] = await world.peers(2, { names: ["Alice", "Bashir"] });
  const g = await world.guild({ name: "Placement", members: [alice, bashir] });
  const ch = g.allChannels.find((c) => c.name === "general") || g.allChannels[0];
  const a = await alice.open();
  await a.openChannel(ch.name, "Placement");
  const b = await bashir.open();
  await b.openChannel(ch.name, "Placement");

  // Where the player sits relative to the words, in one page.
  const order = (page, words) =>
    page.evaluate((words) => {
      // The innermost element holding the words, then its own message row
      // (a continuation row is a row of its own, under the first).
      const text = [...document.querySelectorAll(".feed [data-msg-id] *")].filter((e) => e.textContent.includes(words) && ![...e.children].some((c) => c.textContent.includes(words))).at(-1);
      const row = text?.closest("[data-msg-id]");
      const player = row?.querySelector(".ytw, .lp");
      if (!player || !text) return null;
      return player.compareDocumentPosition(text) & Node.DOCUMENT_POSITION_FOLLOWING ? "above" : "below";
    }, words);

  step("The chip appears for a draft with a link and words, Below by default");
  await a.composer().fill(`${URL} the stall music, as promised`);
  const chip = a.page.locator(".link-chip");
  await chip.waitFor({ state: "visible" });
  assert.equal(await chip.getByRole("button", { name: /Below/ }).getAttribute("aria-pressed"), "true", "Below is the default");

  step("Above is chosen and sent");
  await chip.getByRole("button", { name: /Above/ }).click();
  assert.equal(await chip.getByRole("button", { name: /Above/ }).getAttribute("aria-pressed"), "true", "Above is pressed");
  await a.composer().press("Enter");
  await waitFor(async () => (await order(a.page, "the stall music")) === "above", { message: "Alice's own copy shows the player above" });
  await waitFor(async () => (await order(b.page, "the stall music")) === "above", { message: "Bashir sees the player above" });
  assert.equal(await chip.count(), 0, "the chip goes with the draft");

  step("The next link defaults to below again");
  await a.composer().fill(`${URL} and the second take`);
  await chip.waitFor({ state: "visible" });
  assert.equal(await chip.getByRole("button", { name: /Below/ }).getAttribute("aria-pressed"), "true", "Below again");
  await a.composer().press("Enter");
  await waitFor(async () => (await order(b.page, "the second take")) === "below", { message: "Bashir sees the second player below" });
});
