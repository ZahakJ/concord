// The app's own zoom (Ctrl+= / Appearance UI scale) is what someone who needs
// bigger text turns up. At 150% in a laptop window the four columns kept their
// widths and the composer's text box was left about 12px wide.
import { test } from "../harness.mjs";

test("150% UI zoom on a laptop window leaves a usable composer", async ({ world, assert, step, waitFor }) => {
  const [alice] = await world.peers(1);
  const g = await world.guild({ name: "Zoom", members: [alice] });
  const a = await alice.open({ viewport: { width: 1180, height: 720 } });
  await a.openChannel(g.allChannels[0].name, "Zoom");
  step("Ctrl+= five times");
  for (let i = 0; i < 5; i++) await a.page.keyboard.press("Control+=");
  const w = await waitFor(async () => {
    const z = await a.page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue("--ui-zoom"));
    if (Number(z) < 1.5) return false;
    return a.page.evaluate(() => document.querySelector("textarea.draft")?.offsetWidth || 0);
  }, { message: "zoom reached 150%" });
  assert.ok(w >= 240, `the composer's text box is ${w}px wide at 150%`);
  await a.page.keyboard.press("Control+0");
});
