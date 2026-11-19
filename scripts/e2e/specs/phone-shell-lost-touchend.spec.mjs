// A finger drag on the phone shell whose touchend never reaches it (the
// element under the finger was re-rendered away mid-drag) must not leave the
// conversation page stuck mid-slide: the next tap settles it.
import { test } from "../harness.mjs";

// Dispatches a touch event at (x, y) on `el`, as the browser would for a
// finger: touches holds the finger while it is down, and is empty on lift.
function touchScript() {
  window.__touch = (el, type, x, y) => {
    const t = new Touch({ identifier: 7, target: el, clientX: x, clientY: y, pageX: x, pageY: y });
    const down = type !== "touchend";
    el.dispatchEvent(
      new TouchEvent(type, {
        bubbles: true,
        cancelable: true,
        composed: true,
        touches: down ? [t] : [],
        targetTouches: down ? [t] : [],
        changedTouches: [t],
      }),
    );
  };
}

const pageState = (p) =>
  p.evaluate(() => {
    const el = document.querySelector(".mshell .page");
    return { x: Math.round(el.getBoundingClientRect().x), drag: el.classList.contains("drag") };
  });

test("phone: a drag that loses its touchend does not leave the page stuck mid-slide", async ({
  world,
  assert,
  step,
  sleep,
}) => {
  const [alice] = await world.peers(1);
  const g = await world.guild({ name: "Slide", members: [alice] });
  const ch = g.allChannels[0];
  const a = await alice.open({ viewport: "phone" });
  await a.openChannel(ch.name, "Slide");
  await a.settled();
  const p = a.page;
  await p.evaluate(touchScript);

  step("Drag the page rightward, then lose the finger's touchend");
  await p.evaluate(() => {
    const page = document.querySelector(".mshell .page");
    const y = Math.round(page.getBoundingClientRect().height / 2);
    // A throwaway target inside the page, so it can vanish mid-drag.
    const el = document.createElement("div");
    el.style.cssText = `position:absolute;left:0;top:${y - 20}px;width:60px;height:40px`;
    page.appendChild(el);
    window.__touch(el, "touchstart", 30, y);
    for (const x of [60, 100, 140, 180]) window.__touch(el, "touchmove", x, y);
    el.remove();
    // The lift lands on a detached node and never bubbles to the shell.
    window.__touch(el, "touchend", 180, y);
  });
  await sleep(800);
  const stuck = await pageState(p);
  assert.ok(stuck.drag && stuck.x > 0, `the page should be held mid-slide by the lost lift: ${JSON.stringify(stuck)}`);

  step("A plain tap settles it");
  await p.evaluate(() => {
    const page = document.querySelector(".mshell .page");
    window.__touch(page, "touchstart", 200, 300);
    window.__touch(page, "touchend", 200, 300);
  });
  // The drag never ended, so the page goes back where it was: in, at x = 0.
  await a.settled({ timeout: 5000 });
  const after = await pageState(p);
  assert.ok(!after.drag && after.x === 0, `the page should be at rest: ${JSON.stringify(after)}`);
  assert.deepEqual(a.errors, [], a.errors.join("\n"));
});

test("phone: a touch that loses its touchend before it drags leaves no start for the next touch", async ({
  world,
  assert,
  step,
  sleep,
}) => {
  const [alice] = await world.peers(1);
  const g = await world.guild({ name: "Slide", members: [alice] });
  const ch = g.allChannels[0];
  const a = await alice.open({ viewport: "phone" });
  await a.openChannel(ch.name, "Slide");
  await a.settled();
  const p = a.page;
  await p.evaluate(touchScript);

  step("Touch the page, move too little to drag, then lose the finger's touchend");
  const at = await p.evaluate(() => {
    const box = document.querySelector("textarea.draft").getBoundingClientRect();
    const x = Math.round(box.left + 20);
    const y = Math.round(box.top + box.height / 2);
    const page = document.querySelector(".mshell .page");
    const el = document.createElement("div");
    el.style.cssText = "position:absolute;left:0;top:0;width:60px;height:40px";
    page.appendChild(el);
    // Level with the composer, so the later move reads as sideways from here.
    window.__touch(el, "touchstart", 10, y);
    window.__touch(el, "touchmove", 14, y);
    el.remove();
    window.__touch(el, "touchend", 14, y);
    return { x, y };
  });

  step("Slide a finger sideways inside the composer");
  await p.evaluate(({ x, y }) => {
    const ta = document.querySelector("textarea.draft");
    window.__touch(ta, "touchstart", x, y);
    for (const dx of [20, 60, 100, 140]) window.__touch(ta, "touchmove", x + dx, y);
  }, at);
  await sleep(400);
  const mid = await pageState(p);
  await p.evaluate(({ x, y }) => window.__touch(document.querySelector("textarea.draft"), "touchend", x + 140, y), at);
  // A swipe inside the composer is the textarea's own; the page stays in.
  assert.ok(!mid.drag && mid.x === 0, `the page should not move for a swipe inside the composer: ${JSON.stringify(mid)}`);
  await a.settled({ timeout: 5000 });
  assert.deepEqual(a.errors, [], a.errors.join("\n"));
});
