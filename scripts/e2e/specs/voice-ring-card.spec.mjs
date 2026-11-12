// The ring card says who is calling — the person, not the whole group DM —
// and, when answering would pull you out of the call you are in, says that
// before the click instead of after it.
import { test } from "../harness.mjs";

const ringCard = (app) => app.page.locator(".ring-card:not(.invite)");

test("answering a DM call from a guild call says it will leave the guild call", async ({ world, step }) => {
  const [alice, bashir] = await world.peers(2);
  await world.guild({ name: "Studio", members: [alice, bashir], channels: [{ name: "Lounge", type: "voice" }] });
  await world.dm(alice, bashir);
  const a = await alice.open({ viewport: "laptop" });
  const b = await bashir.open({ viewport: "laptop" });

  step("Bashir sits in Lounge; Alice calls him from their DM");
  await b.openGuild("Studio");
  await b.page.locator(".channel", { hasText: "Lounge" }).first().click();
  // The sidebar's call bar: whole in the call's room, one line elsewhere.
  await b.page.locator(".voice-bar, .voice-line").waitFor({ timeout: 45000 });
  await a.openDM("Bashir");
  await a.page.getByRole("button", { name: "Start a call" }).click();

  await ringCard(b).getByText("Answering leaves Lounge · Studio").waitFor({ timeout: 45000 });
});

test("a group DM's ring card names the caller, then the group", async ({ world, assert, step, waitFor, shot }) => {
  const [alice, bashir, chen] = await world.peers(3);
  await world.guild({ name: "Studio", members: [alice, bashir, chen] });
  for (const p of [bashir, chen]) await alice.rpc("VerifyFingerprint", p.fingerprint);
  await alice.rpc("CreateGroupDM", [bashir.fingerprint, chen.fingerprint]);
  for (const p of [alice, bashir]) {
    await waitFor(async () => (await p.guilds()).some((g) => g.kind === "dm" && (g.dmMembers ?? 0) >= 3), {
      timeout: 90000,
      message: `${p.name} to hold the group DM with everyone in it`,
    });
  }
  const a = await alice.open({ viewport: "laptop" });
  const b = await bashir.open({ viewport: "laptop" });

  step("Alice calls the group");
  await a.openDM("Chen");
  await a.page.getByRole("button", { name: "Start a call" }).click();
  await ringCard(b).waitFor({ timeout: 45000 });
  const text = (await ringCard(b).locator(".ring-info").innerText()).replace(/\s+/g, " ").trim();
  await shot(b, "group ring");
  assert.match(text, /^Alice is calling you · /, `card reads "${text}"`);
});
