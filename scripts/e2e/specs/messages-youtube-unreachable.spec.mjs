// A YouTube player that can't reach YouTube keeps our poster and says so,
// with a retry. Chrome fires the frame's load event for its own error page
// too, so a loaded frame alone is never shown: the frame appears only once
// YouTube's image host answered as well. Chrome's error page is never on
// screen.
import { test } from "../harness.mjs";

const URL = "https://youtu.be/dQw4w9WgXcQ";
// A 1x1 PNG, for a thumbnail host that answers.
const PNG = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==", "base64");

test("an unreachable YouTube keeps the poster and offers a retry; a reachable one plays", async ({ world, assert, step, waitFor }) => {
  const [alice] = await world.peers(1, { names: ["Alice"] });
  const g = await world.guild({ name: "Player", members: [alice] });
  const ch = g.allChannels.find((c) => c.name === "general") || g.allChannels[0];
  const a = await alice.open();
  await a.openChannel(ch.name, "Player");
  const page = a.page;

  // The frame always "loads", the way Chrome's error page does. Whether
  // YouTube is reachable is decided by the image host.
  let reachable = false;
  await page.route("https://www.youtube-nocookie.com/**", (r) => r.fulfill({ status: 200, contentType: "text/html", body: "<!doctype html><title>x</title>" }));
  await page.route("https://i.ytimg.com/**", (r) => (reachable ? r.fulfill({ status: 200, contentType: "image/png", body: PNG }) : r.abort("internetdisconnected")));

  await a.composer().fill(URL);
  await a.composer().press("Enter");
  const yt = page.locator(".feed .ytw").last();
  await yt.waitFor({ state: "visible" });
  const failed = yt.getByText("Can't reach YouTube");
  const shown = yt.locator("iframe.ready");

  step("Unreachable: the frame loads, but it is never shown");
  await yt.getByRole("button", { name: /^Play / }).click();
  await failed.waitFor({ state: "visible", timeout: 12000 });
  assert.equal(await yt.locator("iframe").count(), 0, "the frame is gone, not showing an error page");
  assert.ok(await yt.getByRole("button", { name: "Try again" }).isVisible(), "a retry is offered");

  step("Offline at the tap: the message comes at once, nothing is fetched");
  await page.evaluate(() => Object.defineProperty(navigator, "onLine", { configurable: true, get: () => false }));
  await yt.getByRole("button", { name: "Try again" }).click();
  await failed.waitFor({ state: "visible", timeout: 1000 });
  await page.evaluate(() => delete navigator.onLine);

  step("Going offline while the player loads stops the wait");
  await page.unroute("https://i.ytimg.com/**");
  await page.route("https://i.ytimg.com/**", () => {}); // never answers
  await yt.getByRole("button", { name: "Try again" }).click();
  await yt.locator(".veil").waitFor({ state: "visible" });
  await page.evaluate(() => window.dispatchEvent(new Event("offline")));
  await failed.waitFor({ state: "visible", timeout: 1000 });

  step("Reachable: the retry plays");
  await page.unroute("https://i.ytimg.com/**");
  reachable = true;
  await page.route("https://i.ytimg.com/**", (r) => r.fulfill({ status: 200, contentType: "image/png", body: PNG }));
  await yt.getByRole("button", { name: "Try again" }).click();
  await waitFor(async () => (await shown.count()) === 1, { message: "the frame is shown once YouTube answers" });
  assert.equal(await failed.count(), 0, "no failure message");
});
