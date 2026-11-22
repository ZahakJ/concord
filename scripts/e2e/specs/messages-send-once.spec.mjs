// A YouTube link that "failed" and then arrived three times (owner report,
// 0.62 on a phone). The shape reproduced here: the link's card never comes
// (its fetch hangs until the wait gives up), and the send itself reaches the
// core but its answer is lost on the way back, which the page can only read as
// "not sent". Retry, and the flush when the transport comes back, both send it
// again. Each attempt carries the row's send id, and the core keeps one
// message per id, so both sides hold it once.
import { test, sleep } from "../harness.mjs";

const LINK = "https://www.youtube.com/watch?v=dQw4w9WgXcQ";

test("a link whose card hangs and whose send is slow to answer is one message, retried or not", async ({ world, assert, step, waitFor, log }) => {
  const [alice, bashir] = await world.peers(2, { names: ["Alice", "Bashir"] });
  const g = await world.guild({ name: "Clips", members: [alice, bashir] });
  const general = g.allChannels.find((c) => c.name === "general") || g.allChannels[0];
  const app = await alice.open();
  const { page } = app;
  await page.evaluate(() => {
    const p = JSON.parse(localStorage.getItem("concord.prefs") || "{}");
    localStorage.setItem("concord.prefs", JSON.stringify({ ...p, sendLinkCards: true }));
  });
  await app.reload();
  await app.openChannel("general", "Clips");

  step("the card never comes; the core sends the link but answers after the row's deadline");
  const sends = [];
  let slow = true;
  await page.route("**/rpc", async (route) => {
    let body = {};
    try {
      body = JSON.parse(route.request().postData() || "{}");
    } catch {}
    if (body.method === "LinkCard") return; // never answered: the wait gives up on it
    if (body.method === "SendMessage" && String(body.args?.[1] || "").includes("youtube.com")) {
      sends.push({ body: route.request().postData(), auth: route.request().headers()["authorization"] || "", url: route.request().url() });
      if (slow) {
        slow = false;
        const res = await route.fetch(); // the core has it and sends it now
        await sleep(55000); // well past the row's 30 s deadline
        return route.fulfill({ response: res });
      }
    }
    return route.continue();
  });
  await app.composer().fill(LINK);
  await app.composer().press("Enter");
  await waitFor(async () => (await bashir.messages(general.id)).some((m) => m.content.includes(LINK)), {
    timeout: 30000,
    message: "the link to reach Bashir",
  });
  // Past the deadline: the row must not say "not sent" over a message that
  // is in the channel (0.62 showed Retry here, and Retry sent it again).
  await sleep(30000); // the row's deadline has passed; the answer has not come
  const retry = page.locator(".pmsg").filter({ hasText: "youtube.com" }).getByRole("button", { name: "Retry" });
  assert.equal(await retry.count(), 0, "a delivered message is offered for Retry");

  step("a retry of the same message, as Retry or the reconnect flush sends it");
  assert.equal(sends.length, 1, "one attempt from the page");
  const first = JSON.parse(sends[0].body);
  assert.ok(first.args[4], "the send carries a send id");
  for (let i = 0; i < 2; i++) {
    const res = await fetch(sends[0].url, {
      method: "POST",
      headers: { "Content-Type": "application/json", ...(sends[0].auth ? { Authorization: sends[0].auth } : {}) },
      body: sends[0].body,
    });
    const answer = await res.json();
    log(`retry ${i + 1}: HTTP ${res.status} ${answer.error || "ok"}`);
    assert.ok(res.ok && !answer.error, "the retry is answered as sent");
  }
  await sleep(3000);

  step("one message on each side");
  const mine = (await alice.messages(general.id)).filter((m) => m.content.includes(LINK));
  const theirs = (await bashir.messages(general.id)).filter((m) => m.content.includes(LINK));
  assert.equal(mine.length, 1, `Alice holds ${mine.length}`);
  assert.equal(theirs.length, 1, `Bashir holds ${theirs.length}`);
  assert.equal(await page.locator("[data-msg-id]").filter({ hasText: "youtube.com" }).count(), 1, "the feed draws it once");
  assert.equal(app.errors.length, 0, `page errors: ${app.errors.join(" | ")}`);
});
