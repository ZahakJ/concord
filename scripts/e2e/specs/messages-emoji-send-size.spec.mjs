// Sending an emoji never flashes it at a size it does not settle at. Every
// frame from the key press until the message has settled is sampled; no emoji
// image anywhere on the page may be larger than the size it ends at (a little
// tolerance), and nothing may reach outside the feed or the window.
import { test } from "../harness.mjs";

// Starts a requestAnimationFrame sampler: every frame, the box of every emoji
// image in the document, and anything wider or taller than the window.
async function startSampler(page, frames = 60) {
  await page.evaluate((frames) => {
    window.__samp = [];
    let n = 0;
    const tick = () => {
      const vw = innerWidth;
      const vh = innerHeight;
      const feed = document.querySelector(".feedwrap")?.getBoundingClientRect();
      const f = { n, emoji: [], out: [] };
      for (const el of document.querySelectorAll("img.emoji, img.cemoji")) {
        const r = el.getBoundingClientRect();
        if (!r.width && !r.height) continue;
        f.emoji.push({ w: r.width, h: r.height, where: el.closest(".pbody") ? "pending" : el.closest(".body") ? "body" : "other" });
        if (r.right > vw + 1 || r.bottom > vh + 1 || (feed && el.closest(".feedwrap") && (r.right > feed.right + 1 || r.left < feed.left - 1))) {
          f.out.push({ w: r.width, h: r.height, x: r.x, y: r.y });
        }
      }
      window.__samp.push(f);
      if (++n < frames) requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  }, frames);
}

async function sendAndSample(app, text, { button = false } = {}) {
  const box = app.composer();
  const rows = await app.page.locator(".msg[data-msg-id]").count();
  await box.click();
  await box.fill(text);
  await startSampler(app.page);
  if (button) await app.page.getByRole("button", { name: "Send", exact: true }).click();
  else await box.press("Enter");
  // Emoji are images, so the row is found by count, not by its text: a new
  // stored row, and no pending row left.
  await app.page.waitForFunction(
    (rows) => document.querySelectorAll(".msg[data-msg-id]").length > rows && !document.querySelector(".pbody"),
    rows,
    { timeout: 45000 },
  );
  await app.page.waitForFunction(() => window.__samp.length >= 60);
  return app.page.evaluate(() => window.__samp);
}

for (const vp of ["desktop", "phone"]) {
  test(`an emoji sent on ${vp} is never drawn bigger than it settles at`, async ({ world, assert, step, shot }) => {
    const [alice] = await world.peers(1);
    const g = await world.guild({ name: "Emoji", members: [alice] });
    const a = await alice.open({ viewport: vp });
    await a.openChannel(g.allChannels[0].name || "general", "Emoji");

    for (const text of ["😀", "😀😀😀", "a word and 😀 in it"]) {
      step(`${JSON.stringify(text)}`);
      const samp = await sendAndSample(a, text, { button: vp === "phone" });
      const settled = Math.max(...samp.at(-1).emoji.filter((e) => e.where === "body").map((e) => e.w), 0);
      assert.ok(settled > 0, "the sent emoji is on screen at the end");
      for (const f of samp) {
        for (const e of f.emoji) {
          assert.ok(e.w <= settled + 2 && e.h <= settled + 2, `frame ${f.n}: a ${e.where} emoji drawn ${Math.round(e.w)}x${Math.round(e.h)}, settles at ${Math.round(settled)}`);
        }
        assert.equal(f.out.length, 0, `frame ${f.n}: an emoji outside the feed or the window: ${JSON.stringify(f.out)}`);
      }
    }
    await shot(a, "after the sends");
  });
}
