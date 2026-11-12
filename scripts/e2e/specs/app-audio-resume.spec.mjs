// A phone suspends the page's audio context behind its back (backgrounded,
// screen off, audio focus taken). Every tap must start it again — not only
// the first of the session — or the next chime (the voice-join sound, say)
// waits for some later tap and arrives late. Headless stands in for the
// phone by suspending the context itself.
import { test } from "../harness.mjs";

test("a tap restarts a suspended audio context, not only the first tap", async ({ world, assert, waitFor }) => {
  const [alice] = await world.peers(1);
  const a = await alice.open({ viewport: "laptop" });
  await a.ctx.addInitScript(() => {
    const Orig = window.AudioContext;
    window.__acs = [];
    window.AudioContext = class extends Orig {
      constructor(...x) {
        super(...x);
        window.__acs.push(this);
      }
    };
  });
  await a.reload();
  const tap = () => a.page.mouse.click(700, 400);
  const state = () => a.page.evaluate(() => window.__acs.at(-1)?.state || "none");

  // The first gesture of the session creates and starts the context.
  await tap();
  await waitFor(async () => (await state()) === "running", { timeout: 15000, message: "the context to start" });

  // The OS takes it away; the user taps again.
  await a.page.evaluate(() => window.__acs.at(-1).suspend());
  assert.equal(await state(), "suspended");
  await tap();
  await waitFor(async () => (await state()) === "running", { timeout: 15000, message: "a later tap to restart the context" });
});
