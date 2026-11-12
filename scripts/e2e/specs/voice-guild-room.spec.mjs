// A guild voice channel carries real media between three people, joining and
// leaving; and a camera is not a screen share — only a share earns the
// "LIVE — click to watch" badge in other people's channel lists.
import { test } from "../harness.mjs";

const room = (app, name) => app.page.locator(".channel", { hasText: name }).first();
// The sidebar's call bar — whole in the call's own room, folded to one line
// while the floating call window carries the call elsewhere.
const bar = (app) => app.page.locator(".voice-bar, .voice-line");

async function joinRoom(app, guild, name) {
  await app.openGuild(guild);
  await room(app, name).click();
  await bar(app).waitFor({ timeout: 45000 });
}

test("three people in a voice channel hear each other, and a leave is heard", async ({ world, step, waitFor, shot }) => {
  const peers = await world.peers(3);
  await world.guild({ name: "Studio", members: peers, channels: [{ name: "Lounge", type: "voice" }] });
  const apps = [];
  for (const p of peers) apps.push(await p.open({ viewport: "laptop", watchMedia: true }));

  step("all three join Lounge; each receives audio over two connected links");
  for (const app of apps) await joinRoom(app, "Studio", "Lounge");
  for (const app of apps) {
    await waitFor(async () => (await app.inbound()).connected >= 2, { timeout: 90000, message: `${app.peer.name} to hold two connections` });
    await app.waitForMedia("audio");
  }
  await shot(apps[0], "three in Lounge");

  step("Chen leaves; the others drop to one link and keep hearing each other");
  await apps[2].page.getByRole("button", { name: /^(Leave voice|Disconnect|Leave call)$/ }).first().click();
  await bar(apps[2]).waitFor({ state: "detached", timeout: 15000 });
  for (const app of apps.slice(0, 2)) {
    await waitFor(async () => (await app.inbound()).connected === 1, { timeout: 45000, message: `${app.peer.name} to drop Chen` });
    await app.waitForMedia("audio");
  }
});

test("a camera is not badged as a live screen share", async ({ world, assert, step, waitFor, shot }) => {
  const [alice, bashir] = await world.peers(2);
  await world.guild({ name: "Studio", members: [alice, bashir], channels: [{ name: "Lounge", type: "voice" }] });
  const a = await alice.open({ viewport: "laptop", watchMedia: true });
  const b = await bashir.open({ viewport: "laptop", watchMedia: true });
  await joinRoom(a, "Studio", "Lounge");
  await joinRoom(b, "Studio", "Lounge");
  await b.waitForMedia("audio");

  step("Alice turns her camera on: Bashir gets video, and no LIVE badge");
  await a.page.getByRole("button", { name: "Turn on camera" }).first().click();
  await b.waitForMedia("video");
  // Give a wrong badge every chance to appear before saying it did not.
  await waitFor(async () => (await b.inbound()).frames > 10, { timeout: 30000, message: "decoded camera frames" });
  assert.equal(await b.page.locator(".vc-live").count(), 0, "a camera is not LIVE");
  await shot(b, "camera only");

  step("Alice shares her screen: now it is LIVE");
  await a.page.getByRole("button", { name: "Share screen" }).first().click();
  await b.page.locator(".vc-live").first().waitFor({ timeout: 45000 });
  await shot(b, "screen share");
});
