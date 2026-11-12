// On a phone, a call stays in reach wherever you go, without covering the way
// out of where you are: in another conversation it is the strip under the top
// bar (clear of the composer), and on a full-screen page (the Inbox tab, a
// settings page) it is a band above the page — it used to float over the
// page's × and the Inbox's tabs.
import { test } from "../harness.mjs";

const rect = (loc) => loc.first().evaluate((e) => {
  const b = e.getBoundingClientRect();
  return { top: b.top, bottom: b.bottom, left: b.left, right: b.right };
});

test("phone: the call is shown away from its room and never sits on a page's controls", async ({ world, assert, step, waitFor, shot }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Studio", members: [alice, bashir], channels: [{ name: "Lounge", type: "voice" }] });
  const general = g.allChannels.find((c) => c.name === "general");
  const a = await alice.open({ viewport: "phone" });
  const { page } = a;

  step("Alice joins Lounge from Home on her phone");
  await a.openChannel("general", "Studio");
  await a.back();
  await page.locator(".channel", { hasText: "Lounge" }).first().tap();
  await waitFor(async () => (await page.evaluate(() => !!document.querySelector(".voice-bar, .phone-call"))) || false, {
    timeout: 45000,
    message: "the call to come up",
  });

  step("In #general the strip sits under the top bar and above the composer");
  await a.openChannel(general.name, "Studio");
  const bar = page.locator(".phone-call .callbar");
  await bar.waitFor({ state: "visible", timeout: 30000 });
  const strip = await rect(bar);
  const top = await rect(page.locator(".page .mtopbar"));
  const composer = await rect(page.locator(".page .composer"));
  assert.ok(strip.top >= top.bottom - 1, `strip ${strip.top} starts under the top bar ${top.bottom}`);
  assert.ok(strip.bottom <= composer.top, `strip ${strip.bottom} ends above the composer ${composer.top}`);
  await shot(a, "strip in another channel");

  step("On the Inbox tab the controls are a band above the page, not over it");
  await a.back();
  await page.getByRole("button", { name: /^Inbox/ }).tap();
  const band = page.locator(".callmini");
  await band.waitFor({ state: "visible" });
  const dialog = page.locator(".dialog").last();
  await dialog.waitFor({ state: "visible" });
  const b1 = await rect(band);
  const d1 = await rect(dialog);
  assert.ok(d1.top >= b1.bottom - 1, `the Inbox page (${d1.top}) starts below the call band (${b1.bottom})`);
  await shot(a, "band over inbox");

  // What a thumb reaches at the centre of a control is that control, not the
  // band. Polled: a page slides in before it settles.
  const reaches = async (loc, label) => {
    await loc.waitFor({ state: "visible" });
    await waitFor(async () => {
      const c = await rect(loc);
      return loc.first().evaluate((el, { x, y }) => {
        const hit = document.elementFromPoint(x, y);
        return !!hit && (hit === el || el.contains(hit));
      }, { x: (c.left + c.right) / 2, y: (c.top + c.bottom) / 2 });
    }, { timeout: 5000, message: `a tap on ${label} reaches it` });
  };

  step("Voice & video, opened from You: its ‹ is still the thing under a thumb");
  await page.getByRole("button", { name: /^You/ }).tap();
  await page.getByRole("button", { name: /^Voice & video/ }).first().tap();
  await page.getByText("Microphone", { exact: true }).first().waitFor({ state: "visible" });
  await reaches(page.locator(".dialog").last().getByRole("button", { name: /^Back/ }).first(), "the page's ‹");
  await shot(a, "band over voice and video");

  step("Guild settings, a page with a ×: the × is under a thumb too");
  await page.getByRole("button", { name: /^Guilds/ }).tap();
  await page.getByRole("button", { name: "Guild settings" }).first().tap();
  await reaches(page.locator(".dialog").last().getByRole("button", { name: "Close" }).first(), "the page's ×");
  await shot(a, "band over guild settings");

  step("Leave from the band");
  await band.getByRole("button", { name: "Leave call" }).tap();
  await waitFor(async () => !(await band.count()), { message: "the band went with the call" });
});
