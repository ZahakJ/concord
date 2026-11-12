// A channel whose history fails to load says so and offers to try again. It
// used to show the brand-new-channel welcome ("Say hi") over a channel with
// history in it, so the reader was told the conversation was empty.
import { test, sleep } from "../harness.mjs";

test("a failed history load is an error with a retry, not an empty channel", async ({ world, assert, step, shot }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Errors", members: [alice, bashir], channels: ["help"] });
  await alice.send(g.channels.help, "an answer worth keeping");
  await bashir.waitForMessage(g.channels.help, "an answer worth keeping");
  const b = await bashir.open();
  await b.openChannel("general", "Errors");

  step("the next history fetch fails");
  let failNext = true;
  await b.page.route("**/rpc", async (route) => {
    const body = route.request().postData() || "";
    if (failNext && /"method"\s*:\s*"Messages"/.test(body)) {
      failNext = false;
      return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ error: "history: database is locked" }) });
    }
    return route.continue();
  });
  await b.page.locator(".channel").filter({ hasText: /^\s*#?\s*help(\s|$)/ }).first().click();
  const err = b.page.getByRole("alert").filter({ hasText: "Couldn't load" });
  await err.waitFor();
  await shot(b, "history failed");
  await sleep(500);
  assert.equal(await b.page.locator(".feed .empty").count(), 0, "the empty-channel welcome was shown over a failed load");

  step("Try again loads it");
  await err.getByRole("button", { name: "Try again" }).click();
  await b.waitForMessage("an answer worth keeping");
  assert.equal(await err.count(), 0);
});
