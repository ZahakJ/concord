// A mention that lands while the window is hidden on the open channel shows in
// the window title, stays unread, and is marked read (through that message)
// the moment the window comes back — not before, and not never.
import { test } from "../harness.mjs";

// Pretend the window is minimised (or back), the way the browser reports it.
async function setHidden(app, hidden) {
  await app.page.evaluate((h) => {
    Object.defineProperty(document, "hidden", { configurable: true, get: () => h });
    Object.defineProperty(document, "visibilityState", { configurable: true, get: () => (h ? "hidden" : "visible") });
    document.hasFocus = () => !h;
    document.dispatchEvent(new Event("visibilitychange"));
    if (!h) window.dispatchEvent(new Event("focus"));
  }, hidden);
}

test("a mention in the open channel while hidden is unread until the window returns", async ({ world, assert, waitFor }) => {
  const [alice, bashir] = await world.peers(2, { names: ["Alice", "Bashir"] });
  const g = await world.guild({ name: "Readers", members: [alice, bashir] });
  const ch = g.allChannels[0];

  const b = await bashir.open();
  await b.openChannel(ch.name, "Readers");
  await setHidden(b, true);

  const sent = await alice.send(ch.id, "@Bashir are you there?");
  await b.waitForMessage("are you there?");
  await waitFor(async () => (await b.page.title()).startsWith("(1) "), {
    message: "the title never counted the mention that arrived while hidden",
  });
  const mention = (await bashir.messages(ch.id)).find((m) => m.content.includes("are you there?"));
  const before = (await bashir.rpc("ReadState"))[ch.id] || 0;
  assert.ok(before < Date.parse(mention.sent), "a hidden window must not mark the mention read");

  await setHidden(b, false);
  await waitFor(async () => ((await bashir.rpc("ReadState"))[ch.id] || 0) >= Date.parse(mention.sent), {
    message: "coming back to the window never marked the channel read",
  });
  await waitFor(async () => !(await b.page.title()).startsWith("("), { message: "the title kept the count" });
  const mark = (await bashir.rpc("ReadState"))[ch.id];
  assert.ok(mark <= Date.now() + 2 * 60_000, "the mark is the message's time, not a clock far ahead");
  void sent;
});
