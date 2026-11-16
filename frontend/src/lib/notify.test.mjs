// The notification doors for a deleted or expired message, desktop and
// Android, driven through notify() and retract() as state.svelte.js calls them.
import test from "node:test";
import assert from "node:assert/strict";

const posted = [];
class FakeNotification {
  static permission = "granted";
  constructor(title, opts) {
    this.title = title;
    this.opts = opts;
    this.closed = 0;
    this.listeners = {};
    posted.push(this);
  }
  addEventListener(ev, fn) {
    this.listeners[ev] = fn;
  }
  close() {
    this.closed++;
    this.listeners.close?.();
  }
}
globalThis.Notification = FakeNotification;
globalThis.document = { hidden: true, hasFocus: () => false, addEventListener() {} };
globalThis.window = { focus() {} };

const { notify, retract } = await import("./notify.js");
const { setNotifyMode } = await import("./notifprivacy.js");
setNotifyMode("full");

const msg = (id, channelId, content = "hello") => ({ id, channelId, kind: "", sender: "peer", senderName: "Ada", content, sent: new Date().toISOString() });

test("desktop: a deleted message closes its conversation's notification", () => {
  posted.length = 0;
  notify(msg("m1", "c1"), { selfFpr: "me", direct: true });
  assert.equal(posted.length, 1);
  retract({ ...msg("m1", "c1"), deleted: true });
  assert.equal(posted[0].closed, 1);
});

test("desktop: deleting an earlier message leaves the notification showing a later one", () => {
  posted.length = 0;
  notify(msg("m2", "c2"), { selfFpr: "me", direct: true });
  notify(msg("m3", "c2"), { selfFpr: "me", direct: true });
  retract({ ...msg("m2", "c2"), deleted: true });
  assert.equal(posted[1].closed, 0, "the notification on screen shows m3, not m2");
  retract({ ...msg("m3", "c2"), deleted: true });
  assert.equal(posted[1].closed, 1);
});

test("android: retract hands the message to the native tray", async () => {
  const calls = [];
  globalThis.window.Capacitor = {
    getPlatform: () => "android",
    Plugins: { ConcordCore: { retractNotification: async (a) => calls.push(a) } },
  };
  try {
    retract({ ...msg("m4", "c4"), deleted: true });
    assert.deepEqual(calls, [{ tag: "c4", id: "m4" }]);
  } finally {
    delete globalThis.window.Capacitor;
  }
});
