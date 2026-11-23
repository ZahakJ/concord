import assert from "node:assert/strict";
import {
  newEdit,
  isPristine,
  turn,
  turnPoint,
  turnedSize,
  clampCrop,
  squareCrop,
  cropHandle,
  dragCrop,
  outputSize,
  encodings,
  editable,
  render,
  tooBig,
  encoded,
  MAX_PIXELS,
  MIN_CROP,
} from "./photoedit.js";

const near = (a, b, msg) => assert.ok(Math.abs(a - b) < 1e-9, `${msg}: ${a} vs ${b}`);

// A quarter turn each way, and four of them, are what they say.
assert.deepEqual(turnPoint([0, 0], 1), [1, 0], "top-left goes top-right clockwise");
assert.deepEqual(turnPoint([0, 0], -1), [0, 1], "and bottom-left anticlockwise");
for (const p of [[0.2, 0.7], [0.9, 0.1]]) {
  let q = p;
  for (let i = 0; i < 4; i++) q = turnPoint(q, 1);
  near(q[0], p[0], "four turns x");
  near(q[1], p[1], "four turns y");
  const back = turnPoint(turnPoint(p, 1), -1);
  near(back[0], p[0], "there and back x");
}
assert.deepEqual(turnedSize(400, 300, 1), { w: 300, h: 400 });

// Turning carries the crop and the marks with the picture.
{
  let e = newEdit();
  e.crop = { x: 0, y: 0, w: 0.5, h: 0.25 }; // the top-left strip
  e.marks = [{ type: "arrow", a: [0, 0], b: [0.5, 0.5], color: "#fff", width: 0.01 }];
  e = turn(e, 1);
  assert.equal(e.turns, 1);
  near(e.crop.x, 0.75, "the strip is on the right after a clockwise turn");
  near(e.crop.w, 0.25, "and as wide as it was tall");
  near(e.crop.h, 0.5, "and as tall as it was wide");
  assert.deepEqual(e.marks[0].a, [1, 0]);
  e = turn(turn(turn(e, 1), 1), 1);
  assert.equal(e.turns, 0);
  near(e.crop.x, 0, "four turns put it back");
}

// The crop stays inside the picture and above the floor.
assert.deepEqual(clampCrop({ x: 0.9, y: -0.2, w: 0.5, h: 0.5 }), { x: 0.5, y: 0, w: 0.5, h: 0.5 });
assert.equal(clampCrop({ x: 0, y: 0, w: 0, h: 0.01 }).w, MIN_CROP);

// Square is square in pixels: on a 2:1 picture half the width is the height.
{
  const s = squareCrop({ x: 0, y: 0, w: 1, h: 1 }, 2);
  near(s.w * 2, s.h, "square in pixels");
  near(s.x, 0.25, "centred");
}

// Handles: corners, edges, the inside, and outside.
{
  const c = { x: 0.2, y: 0.2, w: 0.6, h: 0.6 };
  assert.equal(cropHandle(c, [0.21, 0.19], 0.03, 0.03), "nw");
  assert.equal(cropHandle(c, [0.8, 0.8], 0.03, 0.03), "se");
  assert.equal(cropHandle(c, [0.5, 0.2], 0.03, 0.03), "n");
  assert.equal(cropHandle(c, [0.5, 0.5], 0.03, 0.03), "move");
  assert.equal(cropHandle(c, [0.05, 0.5], 0.03, 0.03), "");
}

// Dragging a corner moves only that corner; moving keeps the size.
{
  const c = { x: 0.2, y: 0.2, w: 0.6, h: 0.6 };
  const r = dragCrop(c, "se", -0.1, -0.2);
  near(r.x, 0.2, "nw corner stays x");
  near(r.w, 0.5, "w shrinks");
  near(r.h, 0.4, "h shrinks");
  const m = dragCrop(c, "move", 0.5, 0);
  near(m.w, 0.6, "size kept");
  near(m.x, 0.4, "stopped at the right edge");
  const nw = dragCrop(c, "nw", 0.7, 0);
  assert.ok(nw.w >= MIN_CROP - 1e-9, "a corner dragged past the other stops at the floor");
  const sq = dragCrop(c, "se", 0, -0.3, true, 1);
  near(sq.w, sq.h, "square drag stays square");
}

// The output size: crop, turn and the long-edge cap.
{
  const e = newEdit();
  assert.deepEqual(outputSize(4000, 3000, e), { w: 4000, h: 3000 });
  e.size = 1280;
  assert.deepEqual(outputSize(4000, 3000, e), { w: 1280, h: 960 });
  e.turns = 1;
  assert.deepEqual(outputSize(4000, 3000, e), { w: 960, h: 1280 });
  e.size = 0;
  e.turns = 0;
  e.crop = { x: 0, y: 0, w: 0.5, h: 0.5 };
  assert.deepEqual(outputSize(4000, 3000, e), { w: 2000, h: 1500 });
  e.size = 8000;
  assert.deepEqual(outputSize(4000, 3000, e), { w: 2000, h: 1500 }, "never enlarged");
}

assert.ok(isPristine(newEdit()));
assert.ok(!isPristine(turn(newEdit(), 1)));
assert.equal(encodings("data:image/png;base64,x")[0][0], "image/png");
assert.equal(encodings("data:image/jpeg;base64,x")[0][0], "image/jpeg");
assert.ok(editable("data:image/jpeg;base64,x"));
assert.ok(!editable("data:image/gif;base64,x"), "an animation is not flattened");

// The render makes exactly one canvas, the output, and draws into it through a
// transform: no full-size intermediate, whatever the turn and crop.
{
  const made = [];
  const calls = [];
  const ctx = new Proxy({}, {
    get: (_, k) => (k in ctx2 ? ctx2[k] : (...a) => calls.push([k, ...a])),
    set: () => true,
  });
  const ctx2 = {};
  const mk = (w, h) => (made.push([w, h]), { width: w, height: h, getContext: () => ctx });
  const e = turn(newEdit(), 1);
  e.crop = { x: 0.25, y: 0, w: 0.5, h: 1 };
  const c = render(mk, {}, 4000, 3000, e);
  assert.deepEqual(made, [[1500, 4000]], "only the output canvas is made");
  assert.equal(c.width, 1500);
  const draw = calls.find(([k]) => k === "drawImage");
  assert.ok(draw, "the picture is drawn into the output");
}

// 24 MP in, 24 MP out.
assert.ok(tooBig(8000, 6000), "48 MP is refused");
assert.ok(!tooBig(6000, 4000), "24 MP opens");
assert.ok(tooBig(0, 100) && tooBig(NaN, 5), "a picture without a size is refused");
{
  const e = newEdit();
  const o = outputSize(6000, 4000, e);
  assert.ok(o.w * o.h <= MAX_PIXELS, `output ${o.w}x${o.h} within the cap`);
  const big = outputSize(20000, 20000, e);
  assert.ok(big.w * big.h <= MAX_PIXELS * 1.001, "a larger crop is scaled to the cap");
}
assert.ok(encoded("data:image/png;base64,AAAA"));
assert.ok(!encoded("data:,"), "a canvas too big to encode is a failure");
assert.ok(!encoded(undefined));

console.log("photoedit: ok");
