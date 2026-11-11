// Header search results cover whatever the conversation column is showing.
// The results panel is drawn over the channel view without unmounting it, so
// the view's own layers (a forum board's sticky toolbar, a post's panel) used
// to compete with it and win: from a forum channel the "Narrow it" filters and
// the first results were painted underneath the board's toolbar, visible but
// out of reach. The check is a hit-test, because "it is in the DOM" and "it is
// visible" were both true while nobody could click it.
import { test } from "../harness.mjs";

// What a click at the centre of the first element matching `sel` would land on.
const hitTest = (page, sel) =>
  page.evaluate((sel) => {
    const el = document.querySelector(sel);
    if (!el) return { found: false };
    const r = el.getBoundingClientRect();
    const at = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
    return { found: true, ok: !!at && (at === el || el.contains(at)), at: at ? `${at.tagName}.${[...at.classList].join(".")}` : "nothing" };
  }, sel);

async function searchFromHeader(a, waitFor, words) {
  const box = a.page.getByRole("searchbox", { name: "Search messages across all conversations" }).or(
    a.page.locator('input[aria-label="Search messages across all conversations"]'),
  );
  await box.first().click();
  await box.first().fill(words);
  await a.page.keyboard.press("Enter");
  await waitFor(async () => (await a.page.locator(".search-panel .sp-hit").count()) > 0, { message: "search results listed" });
}

async function assertReachable(a, assert, waitFor, where) {
  for (const [sel, what] of [
    [".search-panel .sp-add", "the first filter chip"],
    [".search-panel .sp-hit", "the first result"],
  ]) {
    const hit = await waitFor(async () => {
      const h = await hitTest(a.page, sel);
      return h.found && h;
    }, { message: `${what} is rendered (${where})` });
    assert.ok(hit.ok, `${what} can be clicked from ${where}; a click there lands on ${hit.at}`);
  }
}

test("search opened from a forum board or post covers it and can be clicked", async ({ world, assert, step, waitFor }) => {
  const [alice] = await world.peers(1);
  const g = await world.guild({ name: "Lanterns", members: [alice], channels: [{ name: "patterns", type: "forum" }] });
  const board = g.channel("patterns");
  await alice.send(g.channel("general"), "the lantern frame is glued");
  const post = await alice.rpc("CreateThread", g.id, board, "Cube lantern", "Four panels and a lantern base.", []);

  const a = await alice.open();
  step("open the forum board, search from the header");
  await a.page.locator(".channel").filter({ hasText: /patterns/ }).first().click();
  await a.page.locator('input[aria-label="Search posts"]').waitFor({ state: "visible" });
  await searchFromHeader(a, waitFor, "lantern");
  await assertReachable(a, assert, waitFor, "a forum board");

  step("close the search, open a post, search again");
  await a.page.keyboard.press("Escape");
  await a.page.keyboard.press("Escape");
  await waitFor(async () => (await a.page.locator(".search-panel").count()) === 0, { message: "search closed" });
  await a.page.locator(".card").filter({ hasText: "Cube lantern" }).first().click();
  await waitFor(async () => (await a.composer().getAttribute("placeholder"))?.length > 0 && (await a.page.locator(".feedwrap.aspanel").count()) > 0, {
    message: `the post ${post.id} is open as a panel`,
  });
  await searchFromHeader(a, waitFor, "lantern");
  await assertReachable(a, assert, waitFor, "an open forum post");
});
