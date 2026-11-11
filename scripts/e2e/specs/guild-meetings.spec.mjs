// An instant meeting and an event's meeting room are reachable from where
// people look for them — the strip's +, the command palette, the calendar's
// header — and a meeting's guest link can be got back after its first dialog
// is closed, as the same link rather than a new one.
import { test } from "../harness.mjs";

const strip = (app) => app.page.locator('nav[aria-label="Guilds"]');

// Points the peer at the e2e rendezvous's guest gateway (the port after the
// rendezvous's own), so it can mint guest links on a 127.0.0.1 world.
async function withGuestLinks(world, peer) {
  const port = Number(/\/tcp\/(\d+)\//.exec(world.bootstrap)[1]);
  peer.env.CONCORD_GUEST_BASE = `http://127.0.0.1:${port + 1}`;
  await peer.restart();
}

// The "Before we open the room" question comes first when no rendezvous is
// saved; either way the meeting's own dialog follows.
async function throughToMeeting(app, waitFor) {
  const ready = app.page.getByRole("button", { name: "Start the meeting", exact: true });
  const live = app.page.getByRole("dialog", { name: /Your meeting is live/ });
  await waitFor(async () => (await ready.isVisible()) || (await live.isVisible()), { timeout: 30000, message: "a meeting dialog" });
  if (await ready.isVisible()) await ready.click();
  await live.waitFor({ timeout: 30000 });
  return live;
}

test("an instant meeting starts from the strip's + and from the palette, and its guest link comes back", async ({ world, assert, step, waitFor, shot }) => {
  const [alice] = await world.peers(1);
  await withGuestLinks(world, alice);
  await world.guild({ name: "Studio", members: [alice] });
  const a = await alice.open({ viewport: "desktop" });

  step("+ at the end of the strip: Create, Join, Instant meeting");
  await strip(a).getByRole("button", { name: "Add a guild" }).click();
  for (const item of ["Create a guild", "Join with an invite code", "Instant meeting"]) {
    await a.page.getByRole("menuitem", { name: new RegExp(item) }).waitFor();
  }
  await a.page.getByRole("menuitem", { name: /Instant meeting/ }).click();
  const live = await throughToMeeting(a, waitFor);
  const link = (await live.locator("code.link").innerText()).trim();
  assert.match(link, /\/guest#/, "a guest link is offered");
  assert.equal(await a.page.getByRole("dialog").count(), 1, "nothing else is open behind the meeting");
  await shot(a, "meeting live");
  await a.closeModals();

  step("the call's Meeting menu gives the same link back, without minting a new one");
  await a.page.locator(".voice-bar").getByRole("button", { name: "Open the call" }).click();
  await a.page.getByRole("button", { name: "Meeting", exact: true }).click();
  await a.page.getByRole("menuitem", { name: "Meeting details" }).click();
  const details = a.page.getByRole("dialog", { name: "Meeting details" });
  await details.waitFor();
  assert.equal((await details.locator("code.link").innerText()).trim(), link, "the same link");
  await shot(a, "meeting details");
  await a.closeModals();
  await a.page.getByRole("button", { name: "Meeting", exact: true }).click();
  await a.page.getByRole("menuitem", { name: "Copy guest link" }).click();
  await a.page.getByText("Guest link copied").first().waitFor();
  await a.page.keyboard.press("Escape");

  step("the palette starts one too, and offers Copy guest link while in a meeting");
  await a.page.keyboard.press("Control+k");
  await a.page.keyboard.type("copy guest link");
  await a.page.getByRole("option", { name: /Copy guest link/ }).first().waitFor();
  await a.page.keyboard.press("Escape");
  await a.page.keyboard.press("Control+k");
  await a.page.keyboard.type("instant meeting");
  await a.page.getByRole("option", { name: /Instant meeting/ }).first().waitFor();
  await a.page.keyboard.press("Enter");
  await throughToMeeting(a, waitFor);
  await a.closeModals();
});

test("the switcher's Instant meeting does not stay open behind the meeting", async ({ world, assert, step, waitFor }) => {
  const [alice] = await world.peers(1);
  await world.guild({ name: "Studio", members: [alice] });
  const a = await alice.open({ viewport: "desktop" });
  step("switcher → ⋯ → Instant meeting");
  await strip(a).getByRole("button", { name: /^All guilds/ }).click();
  await a.page.getByRole("button", { name: "More guild actions" }).click();
  await a.page.getByRole("menuitem", { name: /Instant meeting/ }).click();
  await throughToMeeting(a, waitFor);
  assert.equal(await a.page.getByRole("dialog").count(), 1, "only the meeting's dialog");
  await a.closeModals();
});

test("New event in Your calendar picks the guild and makes a remote event with a guest link", async ({ world, assert, step, shot }) => {
  const [alice] = await world.peers(1);
  await withGuestLinks(world, alice);
  await world.guild({ name: "Studio", members: [alice] });
  const a = await alice.open({ viewport: "desktop" });

  step("calendar button → New event → Studio");
  await a.page.getByRole("button", { name: /^Your calendar/ }).first().click();
  const cal = a.page.getByRole("dialog", { name: "Your calendar" });
  await cal.getByRole("button", { name: "Instant meeting" }).waitFor();
  await cal.getByRole("button", { name: "New event" }).click();
  await a.page.getByRole("menuitem", { name: "Studio" }).click();
  const form = a.dialog();
  await form.getByPlaceholder("What's happening?").fill("Open studio");
  await form.getByRole("group", { name: "When" }).getByRole("button").first().click();
  await form.getByRole("radio", { name: /Remote/ }).click();
  await shot(a, "new remote event");
  await form.getByRole("button", { name: "Create event" }).click();

  step("the card says Copy guest link without opening ⋯");
  await a.page.getByRole("button", { name: "Copy guest link" }).first().waitFor({ timeout: 30000 });
  await shot(a, "event with guest link");
});
