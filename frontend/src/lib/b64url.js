// b64url.js — the alphabet every in-band content token is written in.
//
// `concord://` payloads are base64url with the padding stripped, matching Go's
// base64.RawURLEncoding on the other side of the bridge, and matching the
// `[A-Za-z0-9_-]` charset every token regex admits.
//
// Two pairs, because tokens carry two different kinds of payload. Polls and
// embeds carry JSON, so they want a STRING codec. Doodles, sound recipes and
// anything else that packs fields into fixed-width fields want a BYTE codec —
// going through a string would mean an extra UTF-8 round trip in each
// direction and a chance to corrupt a byte that happens not to be a character.
//
// Decoding never throws. A token is attacker-supplied and arrives on the render
// path of a windowed message list; the caller's job is to check a value, not to
// wrap every parse in a try block, so a payload that is not base64 comes back
// as "" or null and fails closed from there.

export function b64urlEncode(str) {
  return btoa(unescape(encodeURIComponent(str)))
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
}

export function b64urlDecode(s) {
  try {
    return decodeURIComponent(escape(atob(String(s).replace(/-/g, "+").replace(/_/g, "/"))));
  } catch {
    return "";
  }
}

export function bytesToB64url(bytes) {
  let s = "";
  for (let i = 0; i < bytes.length; i++) s += String.fromCharCode(bytes[i]);
  return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

export function b64urlToBytes(s) {
  try {
    const bin = atob(String(s).replace(/-/g, "+").replace(/_/g, "/"));
    const out = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
    return out;
  } catch {
    return null;
  }
}
