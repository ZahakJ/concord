import { Collector, b64ToBytes, decodePack, encodeCodes, parseCode, progressLine } from "./qrseq.js";

let failures = 0;
const eq = (name, got, want) => {
  if (JSON.stringify(got) !== JSON.stringify(want)) {
    console.error(`  FAIL ${name}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
    failures++;
  }
};

// A pack as transport.EncodePack writes it, with one frame long enough to
// need a two-byte uvarint length.
const frames = [new Uint8Array([0xc1, 3, 0, 1, 9, 9]), new Uint8Array(300).map((_, i) => i & 255), new Uint8Array([0xc2, 7])];
const magic = new TextEncoder().encode("CONCORD-BUNDLES/1\n");
const parts = [magic];
for (const f of frames) {
  const len = f.length < 128 ? [f.length] : [(f.length & 0x7f) | 0x80, f.length >> 7];
  parts.push(new Uint8Array(len), f);
}
const packBytes = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
let o = 0;
for (const p of parts) packBytes.set(p, (o += p.length) - p.length);
const pack = btoa(String.fromCharCode(...packBytes));

const got = decodePack(pack);
eq("pack frame count", got.length, 3);
eq("pack frame 2", Array.from(got[1]), Array.from(frames[1]));

let threw = false;
try {
  decodePack(btoa("CONCORD-BUNDLES/1\n\x05ab"));
} catch {
  threw = true;
}
eq("truncated pack refused", threw, true);
threw = false;
try {
  decodePack(btoa("hello"));
} catch {
  threw = true;
}
eq("foreign pack refused", threw, true);

const codes = encodeCodes(got, "deadbeef");
eq("one code per frame", codes.length, 3);
eq("code shape", codes[0].startsWith("CQR1:deadbeef:0:3:"), true);
const p = parseCode(codes[1]);
eq("parse index", [p.set, p.i, p.n], ["deadbeef", 1, 3]);
eq("parse round trip", Array.from(b64ToBytes(p.b64)), Array.from(frames[1]));
eq("foreign text", parseCode("https://example.com"), null);
eq("index past count", parseCode("CQR1:deadbeef:3:3:AAAA"), null);
eq("bad base64", parseCode("CQR1:deadbeef:0:3:not base64!"), null);
eq("bad set", parseCode("CQR1:xyz:0:3:AAAA"), null);

// Out of order, with repeats, as a camera reads a looping sequence.
const c = new Collector();
eq("empty progress", progressLine(c.progress()), "Point at the QR codes on the other device");
eq("first new", c.add(codes[2]).kind, "new");
eq("repeat is dup", c.add(codes[2]).kind, "dup");
eq("foreign ignored", c.add("hello").kind, "foreign");
eq("partial", progressLine(c.progress()), "1/3 scanned — missing 1, 2");
c.add(codes[0]);
eq("one left", c.progress().missing, [2]);
eq("not done", c.progress().done, false);
c.add(codes[1]);
eq("done", c.progress().done, true);
eq("done line", progressLine(c.progress()), "All 3 scanned");

// A code whose import failed is forgotten and taken again next loop.
c.forget("deadbeef", 1);
eq("forgotten is missing", c.progress().missing, [2]);
eq("forgotten not has", c.has("deadbeef", 1), false);
eq("forgotten comes back new", c.add(codes[1]).kind, "new");
eq("done again", c.progress().done, true);

// A second sequence takes over the progress line without losing the first.
const other = encodeCodes([frames[0], frames[2]], "0badf00d");
eq("other new", c.add(other[1]).kind, "new");
eq("other progress", [c.progress().have, c.progress().total], [1, 2]);
eq("first still known", c.add(codes[0]).kind, "dup");

// Reassembly: every frame comes back byte-for-byte from its code.
const back = new Map();
for (const t of [...codes].reverse()) {
  const x = parseCode(t);
  back.set(x.i, b64ToBytes(x.b64));
}
eq("reassembled", [0, 1, 2].map((i) => Array.from(back.get(i))), frames.map((f) => Array.from(f)));

if (failures) process.exit(1);
console.log("qrseq: ok");
