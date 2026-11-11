// Real QR encode → rasterize → jsQR decode for full-size frames: the codes
// the sequence shows are readable by the scanner the app uses, and stay at
// the small QR version the Go link's MTU was chosen for.
import QRCode from "qrcode";
import jsQR from "jsqr";
import { Collector, b64ToBytes, encodeCodes } from "./qrseq.js";

let failures = 0;
const fail = (m) => {
  console.error("  FAIL " + m);
  failures++;
};

const MTU = 192; // internal/transport/qr.MTU
const frames = [];
for (let k = 0; k < 6; k++) {
  const f = new Uint8Array(k === 5 ? 40 : MTU);
  for (let i = 0; i < f.length; i++) f[i] = (i * 31 + k * 7) & 255;
  frames.push(f);
}
const codes = encodeCodes(frames);

function raster(text, scale = 4, margin = 4) {
  const q = QRCode.create(text, { errorCorrectionLevel: "M" });
  const n = q.modules.size;
  const w = (n + 2 * margin) * scale;
  const px = new Uint8ClampedArray(w * w * 4).fill(255);
  for (let y = 0; y < n; y++)
    for (let x = 0; x < n; x++) {
      if (!q.modules.get(y, x)) continue;
      for (let dy = 0; dy < scale; dy++)
        for (let dx = 0; dx < scale; dx++) {
          const o = (((y + margin) * scale + dy) * w + (x + margin) * scale + dx) * 4;
          px[o] = px[o + 1] = px[o + 2] = 0;
        }
    }
  return { px, w, version: q.version };
}

const c = new Collector();
const got = new Map();
// Shown out of order and twice, as a looping sequence is scanned.
for (const i of [3, 0, 5, 3, 1, 2, 4, 0]) {
  const { px, w, version } = raster(codes[i]);
  if (version > 12) fail(`code ${i} needs QR version ${version}, want ≤ 12`);
  const hit = jsQR(px, w, w);
  if (!hit) {
    fail(`code ${i} did not decode`);
    continue;
  }
  const r = c.add(hit.data);
  if (r.kind === "new") got.set(r.i, b64ToBytes(r.b64));
}
const p = c.progress();
if (!p.done || p.total !== 6) fail(`progress ${JSON.stringify(p)}`);
for (let i = 0; i < frames.length; i++) {
  const g = got.get(i);
  if (!g || g.length !== frames[i].length || g.some((b, j) => b !== frames[i][j])) fail(`frame ${i} differs`);
}

if (failures) process.exit(1);
console.log("qrseq codec: ok");
