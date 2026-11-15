// Which sounds a phone makes around messages. Sending makes none (the send
// tick is off by default). A received message sounds when it lands in another
// channel or DM, or while the app is in the background; the channel on screen
// stays silent. A plain message used to sound nowhere, even in a channel set
// to "All messages", which is how a phone "couldn't hear messages".
//
// The tones are read off every oscillator the page starts: each sound in
// lib/sounds.js has its own first pitch.
import { test } from "../harness.mjs";

const SEND = 1320;
const MESSAGE = 659.25;
const MENTION = 880;
const DM = 587.33;
const ARRIVE = 523.25; // "Blip on every message"

test("phone: send is silent; a message elsewhere or in the background sounds, the open channel does not", async ({
  world,
  assert,
  step,
  waitFor,
  sleep,
}) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Sounds", members: [alice, bashir], channels: ["second", "third"] });
  const second = g.allChannels.find((c) => c.name === "second");
  const third = g.allChannels.find((c) => c.name === "third");
  const general = g.allChannels.find((c) => !["second", "third"].includes(c.name));
  const dm = await world.dm(bashir, alice);

  const a = await alice.open({ viewport: "phone" });
  await a.ctx.addInitScript(() => {
    window.__tones = [];
    const start = OscillatorNode.prototype.start;
    OscillatorNode.prototype.start = function (...x) {
      window.__tones.push(Math.round(this.frequency.value * 100) / 100);
      return start.apply(this, x);
    };
  });
  await a.openChannel(general.name, "Sounds");
  // The reload comes back to the channel it left.
  await a.reload();
  await a.composer().waitFor();
  const tones = () => a.page.evaluate(() => window.__tones.slice());
  const clear = () => a.page.evaluate(() => (window.__tones.length = 0));
  // The gate between sounds (a burst plays one) is 1.5s.
  const gap = () => sleep(1700);

  step("sending makes no sound");
  await a.send("hello from the phone");
  await sleep(800);
  assert.ok(!(await tones()).includes(SEND), `the send tick played: ${await tones()}`);

  step("a message in the channel on screen is silent");
  await clear();
  await bashir.send(general.id, "here, on screen");
  await a.waitForMessage("here, on screen");
  await sleep(800);
  assert.deepEqual(await tones(), [], "the open channel made a sound");

  step("a plain message in another channel sounds");
  await gap();
  await bashir.send(second.id, "over in #second");
  await waitFor(async () => (await tones()).includes(MESSAGE), { timeout: 20000, message: "the message sound" });

  step("a mention elsewhere pings");
  await gap();
  await clear();
  await bashir.send(second.id, "@Alice look at this");
  await waitFor(async () => (await tones()).includes(MENTION), { timeout: 20000, message: "the mention ping" });

  step("a DM elsewhere chimes");
  await gap();
  await clear();
  await bashir.send(dm.channelId, "just for you");
  await waitFor(async () => (await tones()).includes(DM), { timeout: 20000, message: "the DM chime" });

  step("a DM right after a note in another channel still chimes");
  await gap();
  await clear();
  await bashir.send(third.id, "chatter in #third");
  await waitFor(async () => (await tones()).includes(MESSAGE), { timeout: 20000, message: "the note for #third" });
  await bashir.send(dm.channelId, "and straight after, to you");
  await waitFor(async () => (await tones()).includes(DM), { timeout: 20000, message: "the DM chime after the note" });

  step("the channel on screen sounds once the app is in the background, once");
  // With "Blip on every message" on, too: the background sound replaces the
  // blip rather than playing beside it.
  await a.page.evaluate(() => {
    const p = JSON.parse(localStorage.getItem("concord.prefs") || "{}");
    localStorage.setItem("concord.prefs", JSON.stringify({ ...p, soundOnArrive: true }));
  });
  await a.reload();
  await a.composer().waitFor();
  await a.composer().tap(); // the gesture that lets the page's audio start
  await gap();
  await clear();
  await a.page.evaluate(() => {
    Object.defineProperty(document, "hidden", { configurable: true, get: () => true });
    Object.defineProperty(document, "visibilityState", { configurable: true, get: () => "hidden" });
    document.dispatchEvent(new Event("visibilitychange"));
  });
  await bashir.send(general.id, "while you were away");
  await waitFor(async () => (await tones()).includes(MESSAGE), { timeout: 20000, message: "a sound in the background" });
  await sleep(500);
  assert.ok(!(await tones()).includes(ARRIVE), `the blip played beside the background sound: ${await tones()}`);
  assert.deepEqual(a.errors, [], a.errors.join("\n"));
});
