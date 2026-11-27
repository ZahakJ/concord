// The feed's edges fade instead of cutting. Messages passing under the header
// or toward the composer dissolve through a mask on the scroller (app.css
// .edge-fade), not under a solid band: an edge fades only while there is
// content beyond it, so at rest at the newest message the bottom is clear and
// the last message reads at full strength. The typing line sits over the
// feed's bottom edge, in the room the feed keeps under its last row, rather
// than in a band of its own. On a phone and on the desktop.
import { test } from "../harness.mjs";

for (const phone of [true, false]) {
  test(`${phone ? "phone" : "desktop"}: the feed fades at its edges, no band above the composer`, async ({ world, assert, step, waitFor }) => {
    const [alice, bashir] = await world.peers(2, { names: ["Alice", "Bashir"] });
    const g = await world.guild({ name: "Edges", members: [alice, bashir] });
    const ch = g.allChannels.find((c) => c.name === "general") || g.allChannels[0];
    for (let i = 1; i <= 40; i++) await bashir.send(ch.id, `shade ${i}: the frosted panel goes on the north side`);
    const a = await alice.open(phone ? { viewport: { width: 390, height: 780 }, mobile: true } : {});
    const { page } = a;
    await a.openChannel(ch.name, "Edges");
    await a.settled();
    await page.locator(".feed").getByText("shade 40:").first().waitFor();

    const feed = page.locator(".feed").first();
    const read = () =>
      feed.evaluate((el) => {
        const cs = getComputedStyle(el);
        const r = el.getBoundingClientRect();
        const typing = document.querySelector(".typing-line")?.getBoundingClientRect();
        const input = document.querySelector(".composer .input-box, .composer")?.getBoundingClientRect();
        return {
          fade: el.dataset.fade ?? null,
          mask: cs.maskImage || cs.webkitMaskImage || "",
          bottom: r.bottom,
          typingTop: typing?.top ?? null,
          inputTop: input?.top ?? null,
        };
      });

    step("At rest at the newest message, only the top edge fades");
    await waitFor(async () => (await read()).fade === "top", { message: "data-fade is top at the end of the feed" });
    let s = await read();
    assert.ok(/gradient/.test(s.mask), `the feed carries a mask: ${s.mask}`);

    step("The feed runs down to the composer; the typing line sits over its edge");
    assert.ok(s.typingTop !== null && s.typingTop < s.bottom, `typing line (${s.typingTop}) overlaps the feed (ends ${s.bottom})`);
    assert.ok(s.inputTop - s.bottom < 12, `no band between the feed (${s.bottom}) and the composer (${s.inputTop})`);

    step("Scrolled into the middle, both edges fade");
    // A reader's scroll, so the feed stops following the newest message.
    if (phone) await feed.evaluate((el) => (el.scrollTop = el.scrollHeight / 2));
    else {
      await feed.hover();
      await page.mouse.wheel(0, -300);
    }
    await waitFor(async () => (await read()).fade === "both", { message: "data-fade is both mid-feed" });

    step("At the top, only the bottom edge fades");
    await feed.evaluate((el) => (el.scrollTop = 0));
    await waitFor(async () => ["bottom", "both"].includes((await read()).fade), { message: "the bottom fades at the top" });
    // The jump pill floats in the fade; it takes no height off the feed, so
    // the dissolve is right above the composer, not a band's height above it.
    await page.locator(".older-bar, .new-below").first().waitFor();
    s = await read();
    assert.ok(s.inputTop - s.bottom < 12, `with the pill up, the feed (${s.bottom}) still runs to the composer (${s.inputTop})`);
  });
}
