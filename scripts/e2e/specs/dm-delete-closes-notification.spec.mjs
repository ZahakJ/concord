// A message deleted by its author leaves the desktop notification that shows
// it: the notification closes when the deleted message is the one it shows, and
// stays when it shows a later message.
import { test, waitFor } from "../harness.mjs";

// Runs in Bashir's page before the app's first script. The OS notification is
// replaced by a recorder, and the window can be sent to the background from the
// test (window.__away), which is when a message is notified at all.
function recordNotifications() {
  window.__notifs = [];
  window.__away = false;
  class FakeNotification extends EventTarget {
    static permission = "granted";
    static requestPermission() {
      return Promise.resolve("granted");
    }
    constructor(title, opts = {}) {
      super();
      this.title = title;
      this.tag = opts.tag || "";
      this.closed = false;
      window.__notifs.push(this);
    }
    close() {
      if (this.closed) return;
      this.closed = true;
      this.dispatchEvent(new Event("close"));
    }
  }
  window.Notification = FakeNotification;
  Object.defineProperty(document, "hidden", { get: () => window.__away, configurable: true });
  const hasFocus = document.hasFocus.bind(document);
  document.hasFocus = () => !window.__away && hasFocus();
}

test("a deleted direct message closes the notification that shows it", async ({ world, assert, step }) => {
  const [alice, bashir] = await world.peers(2);
  await world.guild({ name: "Mutual", members: [alice, bashir] });
  const dm = await world.dm(alice, bashir);

  const b = await bashir.open({ unlock: false });
  await b.ctx.addInitScript(recordNotifications);
  await b.unlock();
  await b.page.evaluate(() => (window.__away = true));

  const notes = () => b.page.evaluate(() => window.__notifs.map((n) => ({ tag: n.tag, closed: n.closed })));
  const waitNotes = (pred, message) =>
    waitFor(async () => {
      const list = await notes();
      return pred(list) ? list : null;
    }, { timeout: 30000, interval: 250, message });

  step("two messages from Alice: two notifications for the conversation");
  const first = await alice.send(dm.channelId, "the first thing I said");
  await waitNotes((l) => l.length === 1, "Bashir's notification for the first message");
  const second = await alice.send(dm.channelId, "the second thing I said");
  await waitNotes((l) => l.length === 2, "Bashir's notification for the second message");
  assert.deepEqual(
    (await notes()).map((n) => n.tag),
    [dm.channelId, dm.channelId],
  );

  step("Alice deletes the first: the notification on screen shows the second and stays");
  await alice.rpc("DeleteMessage", dm.channelId, first.id);
  await bashir.waitForMessage(dm.channelId, (m) => m.id === first.id && m.deleted);
  // The event reaches the page after the store; give it the same moment the
  // next step waits for, then check nothing closed.
  await new Promise((r) => setTimeout(r, 1500));
  assert.deepEqual(
    (await notes()).map((n) => n.closed),
    [false, false],
  );

  step("Alice deletes the second: its notification closes");
  await alice.rpc("DeleteMessage", dm.channelId, second.id);
  await bashir.waitForMessage(dm.channelId, (m) => m.id === second.id && m.deleted);
  const after = await waitNotes((l) => l[1]?.closed, "the notification showing the deleted message to close");
  assert.equal(after[0].closed, false, "the earlier notification object is not the one on screen");
});
