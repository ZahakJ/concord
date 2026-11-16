// Opening the channel you are already in, just after it landed on its NEW
// divider, shows its rows again. The landing holds the feed's window on the
// divider for a moment; reopening empties the feed and fills it under the same
// channel id, and that refill used to wait out the hold with nothing to ask
// again afterwards, so the channel stayed blank under "This is the start of
// #busy" until a new message or a scroll. sync-offline-catchup met it as a
// member coming back to messages missed while offline.
import { test } from "../harness.mjs";

test("reopening a channel right after it lands on unread keeps its rows", async ({ world, assert, step }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Reopen", members: [alice, bashir], channels: ["busy", "quiet"] });
  const busy = g.channels.busy;

  step("Bashir has read busy up to a point, then misses five messages");
  await alice.send(busy, "seen before the gap");
  const seen = await bashir.waitForMessage(busy, "seen before the gap");
  await bashir.rpc("MarkRead", busy, Date.parse(seen.sent));
  const missed = [1, 2, 3, 4, 5].map((i) => `missed while away ${i}`);
  for (const t of missed) await alice.sendRaw(busy, t);
  const last = await bashir.waitForMessage(busy, missed.at(-1));

  step("he opens the app somewhere else");
  const b = await bashir.open();
  await b.openChannel("quiet", "Reopen");
  // The core's mark has reached the page, so busy will land on its divider.
  await b.page.waitForFunction((id) => !!JSON.parse(localStorage.getItem("concord.lastRead") || "{}")[id], busy);

  step("opens busy, and opens it again as soon as it has landed and been read");
  // Every history fetch takes a few frames, as it does on a busy device, so
  // the emptied feed is drawn before the refill arrives.
  await b.page.route("**/rpc", async (route) => {
    if (/"method"\s*:\s*"Messages"/.test(route.request().postData() || "")) await new Promise((r) => setTimeout(r, 100));
    return route.continue();
  });
  const res = await b.page.evaluate(
    async ({ id, newest }) => {
      const row = () =>
        [...document.querySelectorAll(".channel")].find((el) => /^\s*#?\s*busy(\s|$)/.test(el.textContent));
      const until = async (f, ms) => {
        const t = performance.now();
        while (performance.now() - t < ms) {
          if (f()) return true;
          await new Promise((r) => setTimeout(r, 5));
        }
        return false;
      };
      row().click();
      if (!(await until(() => document.querySelector(".feed .new-divider"), 15000))) return { error: "no NEW divider" };
      const landed = performance.now();
      const readTo = () => Date.parse(JSON.parse(localStorage.getItem("concord.lastRead") || "{}")[id] || 0);
      if (!(await until(() => readTo() >= newest, 2000))) return { error: "the landing did not mark busy read" };
      const gap = performance.now() - landed;
      row().click();
      return { gap };
    },
    { id: busy, newest: Date.parse(last.sent) },
  );
  assert.ok(!res.error, res.error);
  // Inside the landing's hold, or this run says nothing about it.
  assert.ok(res.gap < 450, `reopened ${Math.round(res.gap)}ms after landing; the hold is 700ms`);

  step("the rows are on screen");
  await b.waitForMessage(missed.at(-1), { timeout: 10000 });
  await b.waitForMessage("seen before the gap", { timeout: 2000 });
});
