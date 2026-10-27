// qrseq.js — a manual link's queued frames as an animated sequence of QR
// codes, and the scanner side that collects them.
//
// One code is one transport frame (bundle header + MLS ciphertext, never
// plaintext), as text:
//
//   CQR1:<set>:<index>:<count>:<base64 frame>
//
// <set> names one showing of a sequence, so a receiver can say "12/15
// scanned" and not confuse two sequences. Frames are self-describing to the
// transport stack (it fragments and reassembles on its own), so each new code
// is handed in the moment it is read — a sequence scanned only partly still
// delivers every message whose frames all arrived.

const PREFIX = "CQR1:";
const PACK_MAGIC = "CONCORD-BUNDLES/1\n";
export const MAX_CODES = 4096;

export function bytesToB64(bytes) {
  let bin = "";
  for (let i = 0; i < bytes.length; i += 0x8000) bin += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return btoa(bin);
}

export function b64ToBytes(b64) {
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

// decodePack reads ExportLink's base64 pack (transport.EncodePack: a magic
// line, then uvarint length + bytes per frame) into frames.
export function decodePack(b64) {
  const raw = b64ToBytes(b64);
  const magic = new TextEncoder().encode(PACK_MAGIC);
  if (raw.length < magic.length || magic.some((b, i) => raw[i] !== b)) throw new Error("not a Concord pack");
  const frames = [];
  let p = magic.length;
  while (p < raw.length) {
    let len = 0;
    let shift = 0;
    for (;;) {
      if (p >= raw.length || shift > 28) throw new Error("malformed pack");
      const b = raw[p++];
      len += (b & 0x7f) * 2 ** shift;
      shift += 7;
      if (!(b & 0x80)) break;
    }
    if (len === 0 || p + len > raw.length) throw new Error("malformed pack");
    frames.push(raw.subarray(p, p + len));
    p += len;
    if (frames.length > MAX_CODES) throw new Error("pack too large");
  }
  return frames;
}

export function newSetId() {
  const b = new Uint8Array(4);
  crypto.getRandomValues(b);
  return Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
}

// encodeCodes turns frames into the texts of a QR sequence.
export function encodeCodes(frames, set = newSetId()) {
  const n = frames.length;
  return frames.map((f, i) => `${PREFIX}${set}:${i}:${n}:${bytesToB64(f)}`);
}

// parseCode reads one scanned text, or returns null if it is not ours.
export function parseCode(text) {
  if (typeof text !== "string" || !text.startsWith(PREFIX)) return null;
  const parts = text.slice(PREFIX.length).split(":");
  if (parts.length !== 4) return null;
  const [set, is, ns, b64] = parts;
  if (!/^[0-9a-f]{8}$/.test(set) || !/^\d+$/.test(is) || !/^\d+$/.test(ns)) return null;
  const i = +is;
  const n = +ns;
  if (n < 1 || n > MAX_CODES || i >= n || !b64 || !/^[A-Za-z0-9+/]+={0,2}$/.test(b64)) return null;
  return { set, i, n, b64 };
}

// Collector tracks what a scanner has read. add() reports whether a code was
// new (and so should be imported); progress() is the "12/15 scanned" line for
// the sequence seen most recently.
export class Collector {
  constructor() {
    this.sets = new Map(); // set → { n, have: Set<index> }
    this.current = null;
  }

  add(text) {
    const c = parseCode(text);
    if (!c) return { kind: "foreign" };
    let s = this.sets.get(c.set);
    if (!s) {
      if (this.sets.size >= 16) this.sets.delete(this.sets.keys().next().value);
      s = { n: c.n, have: new Set() };
      this.sets.set(c.set, s);
    }
    if (s.n !== c.n) return { kind: "foreign" };
    this.current = c.set;
    if (s.have.has(c.i)) return { kind: "dup", ...c };
    s.have.add(c.i);
    return { kind: "new", ...c };
  }

  // forget un-collects a code whose import failed, so it counts as missing
  // and is taken again when the loop comes round.
  forget(set, i) {
    this.sets.get(set)?.have.delete(i);
  }

  has(set, i) {
    return !!this.sets.get(set)?.have.has(i);
  }

  progress() {
    const s = this.current && this.sets.get(this.current);
    if (!s) return { have: 0, total: 0, done: false, missing: [] };
    const missing = [];
    for (let i = 0; i < s.n && missing.length < 50; i++) if (!s.have.has(i)) missing.push(i + 1);
    return { have: s.have.size, total: s.n, done: s.have.size === s.n, missing };
  }
}

// progressLine is the scanner's status text.
export function progressLine(p) {
  if (!p.total) return "Point at the QR codes on the other device";
  if (p.done) return `All ${p.total} scanned`;
  const miss = p.missing.length <= 8 ? ` — missing ${p.missing.join(", ")}` : "";
  return `${p.have}/${p.total} scanned${miss}`;
}
