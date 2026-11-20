// The guest page's and booking page's WebCrypto code, run on Node's WebCrypto
// against the Go side's known answers (internal/app/testdata/guest-kat.json,
// written by internal/app/guestkat_test.go). The pages are plain HTML served by
// the rendezvous, so the code is cut out of them between its marker comments
// rather than imported (`npm test` runs it).
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../../../", import.meta.url));
const read = (p) => readFileSync(root + p, "utf8");

function block(html, name, ret) {
  const m = html.match(new RegExp(`// --- ${name} begin\\n([\\s\\S]*?)// --- ${name} end`));
  if (!m) throw new Error(`no ${name} block`);
  return new Function(`${m[1]}\nreturn ${ret};`)();
}
const GC = block(read("cmd/rendezvous/guestpage/index.html"), "guest-crypto", "GC");
const BC = block(read("cmd/rendezvous/bookpage/index.html"), "booking-crypto", "BC");
const v = JSON.parse(read("internal/app/testdata/guest-kat.json"));

let failures = 0;
const check = (name, ok) => {
  if (!ok) {
    failures++;
    console.error("FAIL:", name);
  }
};
const hex = (b) => Buffer.from(b).toString("hex");
const unhex = (s) => new Uint8Array(Buffer.from(s, "hex"));
const subtle = globalThis.crypto.subtle;
const ecPriv = (pkcs8Hex) =>
  subtle.importKey("pkcs8", unhex(pkcs8Hex), { name: "ECDH", namedCurve: "P-256" }, false, ["deriveBits"]);

// Base64url round trip, including lengths that need padding restored.
for (const n of [0, 1, 2, 3, 16, 32, 65]) {
  const b = GC.random(n);
  check(`base64url round trip of ${n} bytes`, hex(GC.unb64u(GC.b64u(b))) === hex(b));
}
check("base64url refuses standard base64", (() => { try { GC.unb64u("a+b/"); return false; } catch { return true; } })());

// Link: the id sent in clear and the key that never leaves the page.
const S = GC.unb64u(v.S);
const { linkId, K } = await GC.linkKeys(S);
check("linkId", linkId === v.linkId);
check("K", hex(K) === v.K_hex);

// Ephemeral ECDH, both ways round.
const gsk = await ecPriv(v.gsk_pkcs8_hex);
const hsk = await ecPriv(v.hsk_pkcs8_hex);
const gpk = GC.unb64u(v.gpk), hpk = GC.unb64u(v.hpk);
const Z = await GC.ecdh(gsk, hpk);
check("Z from the guest's side", hex(Z) === v.Z_hex);
check("Z from the host's side", hex(await GC.ecdh(hsk, gpk)) === v.Z_hex);

// Session keys.
const keys = await GC.sessionKeys(K, Z, GC.unb64u(v.gn), GC.unb64u(v.hn), linkId, v.hostPeerId, gpk, hpk);
check("k_g2h", hex(keys.g2h) === v.k_g2h_hex);
check("k_h2g", hex(keys.h2g) === v.k_h2g_hex);

// Records: nonce layout, padding, sealing and opening in counter order.
const aes = { g2h: await GC.aesKey(keys.g2h), h2g: await GC.aesKey(keys.h2g) };
const te = new TextEncoder();
for (const r of v.records) {
  const at = `${r.dir}/${r.counter}`;
  check(`nonce ${at}`, hex(GC.nonce(r.counter)) === r.nonce_hex);
  const pt = te.encode(r.plaintext);
  const padded = GC.pad(pt);
  check(`padded length ${at}`, padded.length === r.padded_len && padded.length % 256 === 0);
  check(`padding is spaces ${at}`, padded.slice(pt.length).every((b) => b === 0x20));
  const ct = await GC.seal(aes[r.dir], r.counter, pt);
  check(`sealed record ${at}`, GC.b64u(ct) === r.c);
  check(`framed line ${at}`, JSON.stringify({ type: "e", c: GC.b64u(ct) }) === r.line);
  const opened = new TextDecoder().decode(await GC.open(aes[r.dir], r.counter, GC.unb64u(r.c)));
  check(`opens ${at}`, JSON.stringify(JSON.parse(opened)) === JSON.stringify(JSON.parse(r.plaintext)));
}
// A record does not open under the wrong counter or the other direction.
const r0 = v.records.find((r) => r.dir === "h2g");
const refuses = async (key, ctr) => {
  try { await GC.open(key, ctr, GC.unb64u(r0.c)); return false; } catch { return true; }
};
check("a record does not open at the next counter", await refuses(aes.h2g, r0.counter + 1));
check("a host record does not open under the guest's key", await refuses(aes.g2h, r0.counter));

// Booking: the visitor's key opens the host's answer; any other key does not.
const b = v.booking;
const esk = await ecPriv(b.esk_pkcs8_hex);
const got = await BC.openSealed(esk, BC.unb64u(b.epk), BC.unb64u(b.spk), BC.unb64u(b.sealed));
check("booking seal opens to the meeting URL and ics", JSON.stringify(got) === JSON.stringify(JSON.parse(b.plaintext)));
const other = await BC.ephemeral();
check("booking seal refuses another visitor key", await (async () => {
  try { await BC.openSealed(other.priv, BC.unb64u(b.epk), BC.unb64u(b.spk), BC.unb64u(b.sealed)); return false; } catch { return true; }
})());
check("booking seal refuses a swapped epk in the salt", await (async () => {
  try { await BC.openSealed(esk, other.pub, BC.unb64u(b.spk), BC.unb64u(b.sealed)); return false; } catch { return true; }
})());

if (failures) process.exit(1);
console.log("guestcrypto.test.mjs: ok");
