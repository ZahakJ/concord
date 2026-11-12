// A 1:1 call is one honest state machine on both sides: a decline reaches the
// caller and ends the call, an unanswered ring stops on both sides at the same
// timeout, "End call" ends it for both, a reload holds the call briefly instead
// of ending it, and calling someone offline says so —
// with each side's conversation recording what happened on that side.
import { test } from "../harness.mjs";

async function pair(world) {
  const [alice, bashir] = await world.peers(2);
  await world.guild({ name: "Calls", members: [alice, bashir] });
  const dm = await world.dm(alice, bashir);
  return { alice, bashir, dm };
}

// The sidebar's call bar: the label every surface shares (callHealth).
const bar = (app) => app.page.locator(".voice-bar");
const ringCard = (app) => app.page.locator(".ring-card:not(.invite)");
const callLine = (app, text) => app.page.locator(".system-msg.call-missed", { hasText: text });

async function startCall(app, name) {
  await app.openDM(name);
  await app.page.getByRole("button", { name: "Start a call" }).click();
  await bar(app).waitFor({ timeout: 30000 });
}

test("a declined call is told to the caller and ends", async ({ world, assert, step, waitFor, shot }) => {
  const { alice, bashir, dm } = await pair(world);
  const a = await alice.open({ viewport: "laptop" });
  const b = await bashir.open({ viewport: "laptop" });

  step("Alice calls; her bar says Calling, not Voice connected, and no clock runs");
  await startCall(a, "Bashir");
  await waitFor(async () => /Calling…/.test(await bar(a).innerText()), { timeout: 30000, message: "Calling… on the bar" });
  assert.doesNotMatch(await bar(a).innerText(), /Voice connected|\d:\d\d/);

  step("Bashir is rung by name and declines");
  await ringCard(b).waitFor({ timeout: 45000 });
  assert.match(await ringCard(b).innerText(), /Alice[\s\S]*is calling you/);
  await shot(b, "ringing");
  await ringCard(b).getByRole("button", { name: "Decline" }).click();
  await ringCard(b).waitFor({ state: "detached", timeout: 10000 });

  step("Alice's call ends by itself, and her conversation says Bashir declined");
  await bar(a).waitFor({ state: "detached", timeout: 30000 });
  await callLine(a, "Bashir declined").waitFor({ timeout: 30000 });
  await shot(a, "caller after decline");
  const line = await bashir.waitForMessage(dm.channelId, (m) => m.kind === "call-missed", { timeout: 30000 });
  assert.equal(line.content, "Declined");

  step("Bashir's side of the same conversation says he declined");
  await b.openDM("Alice");
  await callLine(b, "You declined Alice's call").waitFor({ timeout: 30000 });
  assert.equal(await callLine(b, "Missed call").count(), 0);
});

test("an unanswered ring stops on both sides", async ({ world, assert, step, waitFor, shot }) => {
  const { alice, bashir, dm } = await pair(world);
  const a = await alice.open({ viewport: "laptop" });
  const b = await bashir.open({ viewport: "laptop" });

  await startCall(a, "Bashir");
  await ringCard(b).waitFor({ timeout: 45000 });
  const rangAt = Date.now();

  step("nobody answers: the caller hangs up and the callee's ring card goes, on the same clock");
  await bar(a).waitFor({ state: "detached", timeout: 75000 });
  await ringCard(b).waitFor({ state: "detached", timeout: 30000 });
  const rang = (Date.now() - rangAt) / 1000;
  assert.ok(rang < 60, `the callee rang for ${rang}s`);
  await shot(a, "caller after no answer");

  step("each side's record says what happened to it");
  await callLine(a, "You called — no answer").waitFor({ timeout: 30000 });
  await bashir.waitForMessage(dm.channelId, (m) => m.kind === "call-missed", { timeout: 30000 });
  await b.openDM("Alice");
  await callLine(b, "Missed call from Alice").waitFor({ timeout: 30000 });

  step("and the caller's DM list does not claim she missed it");
  await a.page.locator('nav[aria-label="Guilds"]').getByRole("button", { name: /^Messages/ }).click();
  const row = a.page.locator(".dm-item").filter({ has: a.page.locator(".dm-name", { hasText: "Bashir" }) }).first();
  await waitFor(async () => /You called — no answer/.test(await row.innerText()), { timeout: 30000, message: "the DM row preview" });
  assert.doesNotMatch(await row.innerText(), /You: Missed call/);
});

test("End call in a 1:1 ends it for both", async ({ world, assert, step, waitFor, shot }) => {
  const { alice, bashir, dm } = await pair(world);
  const a = await alice.open({ viewport: "laptop", watchMedia: true });
  const b = await bashir.open({ viewport: "laptop", watchMedia: true });

  await startCall(a, "Bashir");
  await ringCard(b).waitFor({ timeout: 45000 });
  step("Bashir answers; audio flows both ways and Alice's clock starts");
  await ringCard(b).getByRole("button", { name: "Answer" }).click();
  await a.waitForMedia("audio");
  await b.waitForMedia("audio");
  await waitFor(async () => /Voice connected[\s\S]*\d:\d\d/.test(await bar(a).innerText()), {
    timeout: 30000,
    message: "Alice's bar to say connected with a clock",
  });
  await shot(a, "in call");

  step("Bashir presses End call: Alice's call ends too, and neither is told Ringing");
  await b.page.getByRole("button", { name: "End call" }).click();
  await bar(b).waitFor({ state: "detached", timeout: 15000 });
  await bar(a).waitFor({ state: "detached", timeout: 30000 });
  await shot(a, "after the other side hung up");

  step("both conversations record the call's length, locally");
  await callLine(a, "Call ended ·").waitFor({ timeout: 30000 });
  await b.openDM("Alice");
  await callLine(b, "Call ended ·").waitFor({ timeout: 30000 });
  const mine = (await alice.messages(dm.channelId)).filter((m) => m.kind === "call");
  assert.equal(mine.length, 1, "exactly one record on Alice's side");
  assert.equal((await alice.messages(dm.channelId)).filter((m) => m.kind === "call-missed").length, 0);
});

// A reload sends a goodbye too, but it is somebody who means to be right back:
// the other side holds the call behind "Reconnecting…" for a short grace, the
// call carries on if they rejoin inside it, and ends as before if they do not.
async function answeredCall(world) {
  const { alice, bashir, dm } = await pair(world);
  const a = await alice.open({ viewport: "laptop", watchMedia: true });
  const b = await bashir.open({ viewport: "laptop", watchMedia: true });
  await startCall(a, "Bashir");
  await ringCard(b).waitFor({ timeout: 45000 });
  await ringCard(b).getByRole("button", { name: "Answer" }).click();
  await a.waitForMedia("audio");
  await b.waitForMedia("audio");
  return { alice, bashir, dm, a, b };
}

test("a reload in a 1:1 holds the call, and Rejoin carries it on", async ({ world, step, waitFor, shot }) => {
  const { a, b } = await answeredCall(world);

  step("Bashir reloads: Alice's call is not over, it is Reconnecting…");
  const reloading = b.reload();
  await waitFor(async () => /Reconnecting…/.test(await bar(a).innerText().catch(() => "")), {
    timeout: 15000,
    message: "Alice's bar to say Reconnecting…",
  });
  await shot(a, "other side reloading");
  await reloading;

  step("Bashir rejoins inside the grace: the call carries on for both");
  // The reloaded page offers the call back ("You left Alice · Call again").
  const card = b.page.locator(".ring-card", { hasText: "You left Alice" });
  await card.getByRole("button", { name: /Call again|Rejoin/ }).click({ timeout: 15000 });
  await bar(b).waitFor({ timeout: 30000 });
  await waitFor(async () => /Voice connected/.test(await bar(a).innerText()), {
    timeout: 30000,
    message: "Alice's bar to say connected again",
  });
  await a.waitForMedia("audio", { timeout: 60000 });
  await callLine(a, "Call ended ·").waitFor({ state: "detached", timeout: 1000 });
});

test("a reload in a 1:1 that does not come back ends the call after the grace", async ({ world, assert, step, waitFor, shot }) => {
  const { alice, dm, a, b } = await answeredCall(world);

  step("Bashir closes the page: Alice sees Reconnecting…, then the call ends");
  await b.close();
  await waitFor(async () => /Reconnecting…/.test(await bar(a).innerText().catch(() => "")), {
    timeout: 15000,
    message: "Alice's bar to say Reconnecting…",
  });
  await bar(a).waitFor({ state: "detached", timeout: 30000 });
  await callLine(a, "Call ended ·").waitFor({ timeout: 30000 });
  await shot(a, "ended after the grace");
  assert.equal((await alice.messages(dm.channelId)).filter((m) => m.kind === "call").length, 1);
});

test("calling someone who is offline says so", async ({ world, assert, step, waitFor, shot }) => {
  const { alice, bashir } = await pair(world);
  await bashir.stop();
  const a = await alice.open({ viewport: "laptop" });
  // Alice's node has to have noticed Bashir is gone (the DM's grey dot).
  await waitFor(
    async () => {
      const g = (await alice.guilds()).find((x) => x.kind === "dm" && x.dmPeer === bashir.fingerprint);
      return g && !g.dmPeerOnline;
    },
    { timeout: 90000, message: "Bashir to show offline" },
  );

  step("Alice calls: the bar and the stage say Bashir is offline instead of ringing");
  await startCall(a, "Bashir");
  await waitFor(async () => /They're offline/.test(await bar(a).innerText()), { timeout: 30000, message: "They're offline on the bar" });
  await a.page.getByRole("button", { name: "Open the call" }).first().click();
  // The toast itself, not the copy the live region holds for screen readers.
  await a.page.getByText("Bashir is offline — they'll see a missed call").and(a.page.locator(":not(.sr-only)")).waitFor({ timeout: 15000 });
  await shot(a, "calling offline");
});
