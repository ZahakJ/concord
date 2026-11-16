// Zero-dependency test for the image viewer's geometry (`npm test`).
//
// A phone screenshot (1080x2400), a wide panorama (4000x600) and a tiny icon
// (40x40), each in a landscape desktop window (1440x900) and a portrait one
// (900x1440): the viewer opens every one of them whole, inside the margin, and
// never larger than it is.
import { clampPan, fitSize, MARGIN_DESKTOP, maxZoom, toggleZoom, zoomAt } from "./viewerfit.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};
const near = (a, b, msg, eps = 1e-6) => assert(Math.abs(a - b) <= eps, `${msg}\n  got ${a}\n  exp ${b}`);

const IMAGES = { tall: { w: 1080, h: 2400 }, wide: { w: 4000, h: 600 }, tiny: { w: 40, h: 40 } };
const WINDOWS = { landscape: { w: 1440, h: 900 }, portrait: { w: 900, h: 1440 } };
const M = MARGIN_DESKTOP;

// ---- the fit ---------------------------------------------------------------
for (const [wn, vp] of Object.entries(WINDOWS)) {
  for (const [iname, img] of Object.entries(IMAGES)) {
    const f = fitSize(img, vp, M);
    const tag = `${iname} in ${wn}`;
    assert(f.w <= vp.w - 2 * M.x + 1e-9, `${tag}: width ${f.w} fits ${vp.w} less the margin`);
    assert(f.h <= vp.h - 2 * M.y + 1e-9, `${tag}: height ${f.h} fits ${vp.h} less the margin`);
    assert(f.w <= img.w && f.h <= img.h, `${tag}: never upscaled (${f.w}x${f.h})`);
    near(f.w / f.h, img.w / img.h, `${tag}: keeps its shape`);
    near(f.scale, f.w / img.w, `${tag}: scale is displayed / natural`);
  }
}
// The binding side, worked by hand.
{
  const f = fitSize(IMAGES.tall, WINDOWS.landscape, M); // height-bound: 900 - 128 = 772
  near(f.h, 772, "tall in landscape is as tall as the window allows");
  near(f.w, 1080 * (772 / 2400), "tall in landscape: width follows");
  const g = fitSize(IMAGES.wide, WINDOWS.landscape, M); // width-bound: 1440 - 48 = 1392
  near(g.w, 1392, "wide in landscape is as wide as the window allows");
  const p = fitSize(IMAGES.wide, WINDOWS.portrait, M); // 900 - 48 = 852
  near(p.w, 852, "wide in portrait is as wide as the window allows");
  const t = fitSize(IMAGES.tiny, WINDOWS.landscape, M);
  near(t.w, 40, "tiny stays at its natural size");
  near(t.scale, 1, "tiny: scale 1");
}

// ---- zoom toward the pointer -------------------------------------------------
for (const [wn, vp] of Object.entries(WINDOWS)) {
  for (const [iname, img] of Object.entries(IMAGES)) {
    const f = fitSize(img, vp, M);
    const size = { w: f.w, h: f.h };
    const tag = `${iname} in ${wn}`;
    // A point on the picture, a third of the way from its centre to its corner.
    const p = { x: size.w / 6, y: size.h / 6 };
    let view = { zoom: 1, tx: 0, ty: 0 };
    // Picture coordinates of whatever is under p, before and after each step.
    const under = (v) => ({ x: (p.x - v.tx) / v.zoom, y: (p.y - v.ty) / v.zoom });
    for (let i = 0; i < 3; i++) {
      const before = under(view);
      const next = zoomAt(view, p, view.zoom * 2, size, vp, f.scale);
      // Wherever the zoomed picture overflows the window on an axis, the point
      // under the pointer is still the same point of the picture.
      // Where the clamp does not bind, the point under the pointer is still the
      // same point of the picture; where it binds, the picture sits at its
      // edge on that axis.
      const after = under(next);
      const k = next.zoom / view.zoom;
      const free = { x: p.x - (p.x - view.tx) * k, y: p.y - (p.y - view.ty) * k };
      const maxX = Math.max(0, (size.w * next.zoom - vp.w) / 2);
      const maxY = Math.max(0, (size.h * next.zoom - vp.h) / 2);
      if (Math.abs(free.x) <= maxX) near(after.x, before.x, `${tag}: x under the pointer after step ${i + 1}`);
      else near(Math.abs(next.tx), maxX, `${tag}: x held at the edge after step ${i + 1}`);
      if (Math.abs(free.y) <= maxY) near(after.y, before.y, `${tag}: y under the pointer after step ${i + 1}`);
      else near(Math.abs(next.ty), maxY, `${tag}: y held at the edge after step ${i + 1}`);
      assert(next.zoom <= maxZoom(f.scale), `${tag}: zoom capped`);
      view = next;
    }
    // Unclamped by the window: the anchored formula itself.
    const big = { w: 1e6, h: 1e6 };
    const z = zoomAt({ zoom: 1.5, tx: 30, ty: -20 }, { x: 100, y: 50 }, 3, big, vp, f.scale);
    near((100 - z.tx) / z.zoom, (100 - 30) / 1.5, `${tag}: anchored zoom keeps x`);
    near((50 - z.ty) / z.zoom, (50 + 20) / 1.5, `${tag}: anchored zoom keeps y`);
    // Back to fit re-centres.
    const back = zoomAt(view, p, 1, size, vp, f.scale);
    assert(back.zoom === 1 && back.tx === 0 && back.ty === 0, `${tag}: back at fit, centred`);
  }
}

// ---- the pan clamp ---------------------------------------------------------
for (const [wn, vp] of Object.entries(WINDOWS)) {
  for (const [iname, img] of Object.entries(IMAGES)) {
    const f = fitSize(img, vp, M);
    const size = { w: f.w, h: f.h };
    const tag = `${iname} in ${wn}`;
    for (const zoom of [1, 2, 4, maxZoom(f.scale)]) {
      for (const far of [-1e6, 1e6]) {
        const c = clampPan({ zoom, tx: far, ty: far }, size, vp);
        const W = size.w * zoom;
        const H = size.h * zoom;
        // The picture's box, in window coordinates.
        const left = vp.w / 2 + c.tx - W / 2;
        const top = vp.h / 2 + c.ty - H / 2;
        if (W <= vp.w) near(c.tx, 0, `${tag} @${zoom}: narrower than the window stays centred`);
        else assert(left <= 1e-6 && left + W >= vp.w - 1e-6, `${tag} @${zoom}: wide picture still covers the window across (${left})`);
        if (H <= vp.h) near(c.ty, 0, `${tag} @${zoom}: shorter than the window stays centred`);
        else assert(top <= 1e-6 && top + H >= vp.h - 1e-6, `${tag} @${zoom}: tall picture still covers the window down (${top})`);
      }
    }
  }
}

// ---- double-click ------------------------------------------------------------
{
  const f = fitSize(IMAGES.tall, WINDOWS.landscape, M);
  near(toggleZoom(1, f.scale), 1 / f.scale, "fit -> 100%");
  near(toggleZoom(1 / f.scale, f.scale), 1, "100% -> fit");
  near(toggleZoom(1, 1), 2, "a picture fitted at its natural size doubles");
  assert(maxZoom(f.scale) >= 2 / f.scale, "200% of natural is always reachable");
}

if (failures) {
  console.error(`viewerfit: ${failures} failure(s)`);
  process.exit(1);
}
console.log("viewerfit: ok");
