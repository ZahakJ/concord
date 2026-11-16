import test from "node:test";
import assert from "node:assert/strict";
import { redactNotification, normalizeNotifyMode, DEFAULT_NOTIFY_MODE, setNotifyMode, trackNotification } from "./notifprivacy.js";

const sample = {
  message: { title: "Ada in #general · Dee Guild", body: "lunch at 12?" },
  mention: { title: "@ Ada in #general · Dee Guild", body: "@me lunch?" },
  request: { title: "Ada", body: "hi, it's Ada" },
  call: { title: "Ada", body: "is calling" },
};

for (const kind of Object.keys(sample)) {
  const generic = kind === "call" ? "Incoming call" : "New message";
  test(`${kind}: full shows everything`, () => {
    assert.deepEqual(redactNotification("full", kind, sample[kind]), sample[kind]);
  });
  test(`${kind}: name keeps the title only`, () => {
    assert.deepEqual(redactNotification("name", kind, sample[kind]), { title: sample[kind].title, body: generic });
  });
  test(`${kind}: nothing is generic and leaks no text`, () => {
    const r = redactNotification("nothing", kind, sample[kind]);
    assert.deepEqual(r, { title: "Concord", body: generic });
    assert.ok(!JSON.stringify(r).includes("Ada"));
  });
  test(`${kind}: missing or unknown falls back to nothing`, () => {
    for (const v of [undefined, null, "", "bogus", 3]) {
      assert.deepEqual(redactNotification(v, kind, sample[kind]), { title: "Concord", body: generic });
    }
  });
}

test("default is nothing", () => {
  assert.equal(DEFAULT_NOTIFY_MODE, "nothing");
  assert.equal(normalizeNotifyMode(undefined), "nothing");
  assert.equal(normalizeNotifyMode("name"), "name");
});

test("a change of mode closes what is still on screen", () => {
  setNotifyMode("full");
  let closed = 0;
  const fake = () => ({ close: () => closed++ });
  trackNotification(fake());
  trackNotification(fake());
  setNotifyMode("full"); // same mode: nothing closes
  assert.equal(closed, 0);
  setNotifyMode("nothing");
  assert.equal(closed, 2);
  setNotifyMode("name"); // already closed, not closed twice
  assert.equal(closed, 2);
});

test("a deleted message closes the notification that shows it, and only that one", async () => {
  const { retractNotification } = await import("./notifprivacy.js");
  setNotifyMode("full");
  const fake = () => {
    const n = { closed: 0, close: () => n.closed++ };
    return n;
  };
  const a1 = trackNotification(fake(), { tag: "chan-a", id: "m1" });
  const a2 = trackNotification(fake(), { tag: "chan-a", id: "m2" }); // replaced a1 on screen
  const b = trackNotification(fake(), { tag: "chan-b", id: "m3" });
  assert.equal(retractNotification("chan-a", "m1"), false, "m1 is no longer what chan-a shows");
  assert.equal(a1.closed + a2.closed, 0);
  assert.equal(retractNotification("chan-a", "m2"), true);
  assert.equal(a2.closed, 1, "the notification showing the deleted message closed");
  assert.equal(b.closed, 0, "another conversation's notification stayed");
  assert.equal(retractNotification("chan-a", "m2"), false, "closed once");
  assert.equal(retractNotification("chan-b", ""), false);
  assert.equal(retractNotification("", "m3"), false);
  setNotifyMode("nothing"); // closes b; a2 is no longer tracked
  assert.equal(b.closed, 1);
  assert.equal(a2.closed, 1);
});
