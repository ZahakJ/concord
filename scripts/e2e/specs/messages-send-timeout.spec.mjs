// A send the core never answers stops saying "Sending…" and offers Retry and
// Discard. It used to stay clocked forever with no way out, the draft already
// gone from the box; and a send refused for want of a route printed the core's
// own words ("send: no route to peer: context deadline exceeded").
import { test } from "../harness.mjs";

test("a hung send gives up with Retry and Discard, and a refused one says why in words", async ({ world, assert, step, shot }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Sends", members: [alice, bashir] });
  const a = await alice.open();
  await a.openChannel("general", "Sends");

  step("SendMessage never answers");
  let mode = "hang";
  await a.page.route("**/rpc", async (route) => {
    const body = route.request().postData() || "";
    if (!/"method"\s*:\s*"SendMessage"/.test(body)) return route.continue();
    if (mode === "hang") return; // never answered
    if (mode === "noroute")
      return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ error: "send: no route to peer: context deadline exceeded" }) });
    return route.continue();
  });
  await a.composer().fill("hanging message");
  await a.composer().press("Enter");
  const row = a.page.locator(".pmsg").filter({ hasText: "hanging message" });
  await row.getByText("No answer from Concord's core yet").waitFor({ timeout: 45000 });
  await row.getByRole("button", { name: "Retry" }).waitFor();
  await shot(a, "timed out");

  step("Discard takes the row away");
  await row.getByRole("button", { name: "Discard" }).click();
  await row.waitFor({ state: "detached" });

  step("a refusal for want of a route is a sentence, and Retry sends once the route is back");
  mode = "noroute";
  await a.composer().fill("no route message");
  await a.composer().press("Enter");
  const row2 = a.page.locator(".pmsg").filter({ hasText: "no route message" });
  await row2.getByText("Couldn't reach them").waitFor();
  assert.equal(await row2.getByText("context deadline exceeded").count(), 0);
  mode = "pass";
  await row2.getByRole("button", { name: "Retry" }).click();
  await alice.waitForMessage(g.allChannels[0].id, "no route message");
  await row2.waitFor({ state: "detached" });
});
