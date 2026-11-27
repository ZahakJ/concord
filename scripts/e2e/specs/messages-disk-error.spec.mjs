// The owner's report: "disk I/O error", Retry, and the message was there
// twice. A send the core refuses on a failing (or full) disk is one row in the
// feed that says it was not sent and why, with Retry; Retry sends it once, and
// a retry whose first attempt did go out sends nothing more. A reaction retried
// after a failure is one reaction, not a toggle back off.
import { test, sleep } from "../harness.mjs";

test("a send that hits a disk error is one message with a clear retry", async ({ world, assert, step, waitFor }) => {
  const [alice, bashir] = await world.peers(2, { names: ["Alice", "Bashir"] });
  const g = await world.guild({ name: "Disk", members: [alice, bashir] });
  const general = g.allChannels.find((c) => c.name === "general") || g.allChannels[0];
  const app = await alice.open();
  const { page } = app;
  await app.openChannel("general", "Disk");

  for (const [n, error] of [
    [1, "store: save message: disk I/O error (10)"],
    [2, "store: set setting: database or disk is full (13)"],
  ]) {
    const body = `kiln schedule, take ${n}`;
    step(`the core refuses the send: ${error}`);
    const sent = [];
    let fail = true;
    await page.route("**/rpc", async (route) => {
      let req = {};
      try {
        req = JSON.parse(route.request().postData() || "{}");
      } catch {}
      if (req.method === "SendMessage" && String(req.args?.[1] || "").includes(body)) {
        sent.push(req.args[4]);
        if (fail) {
          fail = false;
          return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ error }) });
        }
      }
      return route.continue();
    });
    await app.composer().fill(body);
    await app.composer().press("Enter");
    const row = page.locator(".pmsg.failed").filter({ hasText: body });
    await row.waitFor({ timeout: 15000 });
    assert.equal(await page.locator(".pmsg").filter({ hasText: body }).count(), 1, "one pending row");
    const status = (await row.locator(".pfail").innerText()).trim();
    assert.ok(/disk/i.test(status) && !/store:|\(1[03]\)/.test(status), `the reason is said plainly: "${status}"`);
    assert.ok(/not sent/i.test(await row.innerText()), "the row says it was not sent");

    step("Retry sends it once, under the same send id");
    await row.getByRole("button", { name: "Retry" }).click();
    await waitFor(async () => (await bashir.messages(general.id)).some((m) => m.content.includes(body)), {
      timeout: 30000,
      message: "the retried message to reach Bashir",
    });
    await waitFor(async () => (await page.locator(".pmsg").filter({ hasText: body }).count()) === 0, {
      timeout: 15000,
      message: "the pending row to give way to the message",
    });
    assert.equal(sent.length, 2, "two attempts from the page");
    assert.ok(sent[0] && sent[0] === sent[1], "the retry carries the first attempt's send id");
    await sleep(1500);
    assert.equal((await alice.messages(general.id)).filter((m) => m.content.includes(body)).length, 1, "Alice holds it once");
    assert.equal((await bashir.messages(general.id)).filter((m) => m.content.includes(body)).length, 1, "Bashir holds it once");
    assert.equal(await page.locator("[data-msg-id]").filter({ hasText: body }).count(), 1, "the feed draws it once");
    await page.unroute("**/rpc");
  }

  step("a reaction retried under the same id is one reaction, not a toggle back off");
  const target = (await alice.messages(general.id)).find((m) => m.content.includes("take 1"));
  await alice.rpc("ToggleReaction", general.id, target.id, "👍", "tap-1");
  await alice.rpc("ToggleReaction", general.id, target.id, "👍", "tap-1");
  await waitFor(async () => {
    const m = (await bashir.messages(general.id)).find((x) => x.id === target.id);
    return (m?.reactions?.["👍"] || []).length === 1;
  }, { timeout: 30000, message: "Bashir to see the reaction" });
  await sleep(2000);
  for (const [name, p] of [["Alice", alice], ["Bashir", bashir]]) {
    const m = (await p.messages(general.id)).find((x) => x.id === target.id);
    assert.equal((m?.reactions?.["👍"] || []).length, 1, `${name}: the reaction is on once`);
  }
  assert.equal(app.errors.length, 0, `page errors: ${app.errors.join(" | ")}`);
});
