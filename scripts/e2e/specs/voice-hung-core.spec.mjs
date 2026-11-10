// A core that hangs (alive, sockets open, answering nothing) mid-call is shown
// as "Reconnecting…" on its own window, instead of "Voice connected" with the
// clock running while the room drops it — which told the user the others had
// left when it was them. When the core wakes, the call comes back.
import { test } from "../harness.mjs";

const room = (app, name) => app.page.locator(".channel", { hasText: name }).first();
const bar = (app) => app.page.locator(".voice-bar");

test("a hung core reads as Reconnecting, not as everyone leaving", async ({ world, step, waitFor, shot }) => {
  const [alice, bashir] = await world.peers(2);
  await world.guild({ name: "Studio", members: [alice, bashir], channels: [{ name: "Lounge", type: "voice" }] });
  const a = await alice.open({ viewport: "laptop", watchMedia: true });
  const b = await bashir.open({ viewport: "laptop", watchMedia: true });
  for (const app of [a, b]) {
    await app.openGuild("Studio");
    await room(app, "Lounge").click();
    await bar(app).waitFor({ timeout: 45000 });
  }
  await a.waitForMedia("audio");

  step("Alice's core is frozen (SIGSTOP on the process this run started)");
  const pid = alice.child.pid;
  process.kill(pid, "SIGSTOP");
  try {
    await waitFor(async () => /Reconnecting…/.test(await bar(a).innerText()), {
      timeout: 60000,
      message: "Alice's bar to say Reconnecting…",
    });
    await shot(a, "core hung");
  } finally {
    process.kill(pid, "SIGCONT");
  }

  step("the core wakes: the bar stops saying Reconnecting and audio flows again");
  await waitFor(async () => !/Reconnecting…/.test(await bar(a).innerText()), { timeout: 60000, message: "the bar to recover" });
  await a.waitForMedia("audio", { timeout: 90000 });
});
