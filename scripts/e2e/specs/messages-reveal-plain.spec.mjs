// A moderator's "Show original" on a deleted guild message puts the original
// text in place at once: every state the revealed text passes through is the
// original itself, never a run of placeholder characters settling into it.
import { test } from "../harness.mjs";

test("a revealed deleted message shows its original text with no intermediate state", async ({ world, assert, step }) => {
  const [alice] = await world.peers(1);
  const g = await world.guild({ name: "Reveal", members: [alice] });
  const ch = g.allChannels[0];
  const a = await alice.open();

  step("Show deleted messages, so the deleted marker stays in the feed");
  await a.page.evaluate(() => {
    const prefs = JSON.parse(localStorage.getItem("concord.prefs") || "{}") || {};
    localStorage.setItem("concord.prefs", JSON.stringify({ ...prefs, showDeleted: true }));
  });
  await a.reload();
  await a.openChannel(ch.name, "Reveal");

  const text = "the original words, exactly as written";
  await a.send(text);
  const id = await a.messageRow(text).getAttribute("data-msg-id");
  await a.deleteMessage(text);
  await a.page.mouse.move(1, 1); // off the row, so hover does not start the reveal early
  await alice.waitForMessage(ch.id, (m) => m.id === id && m.deleted);
  const row = a.page.locator(`.msg[data-msg-id="${id}"]`);
  const button = row.locator(".reveal-btn");
  await button.waitFor({ state: "visible" });

  step("Record every value the revealed text takes, from before the click until it settles");
  await row.evaluate((el) => {
    const seen = [];
    window.__revealSeen = seen;
    const body = el.querySelector(".body.deleted");
    const note = () => {
      const t = body?.querySelector(".revealed-text");
      if (t && seen[seen.length - 1] !== t.textContent) seen.push(t.textContent);
    };
    new MutationObserver(note).observe(body, { subtree: true, childList: true, characterData: true });
    const tick = () => {
      note();
      if (seen.length < 500) requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  });
  await button.dispatchEvent("click");
  await row.locator(".revealed-text", { hasText: text }).waitFor({ state: "visible" });
  // Long enough for any per-character animation of this length to have run.
  await a.page.waitForTimeout(1500);
  const seen = await a.page.evaluate(() => window.__revealSeen);
  assert.ok(seen.length > 0, "the revealed text was observed");
  assert.deepEqual(seen, [text], `revealed text passed through: ${JSON.stringify(seen.slice(0, 5))}`);
});
