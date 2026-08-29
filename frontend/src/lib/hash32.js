// hash32.js — FNV-1a over code units.
//
// Small, fast, and — the only property that matters — stable: the same string
// gives the same number on every device, in every session, without storing or
// syncing anything. It is what makes generated colour deterministic, so a forum
// looks like itself for everyone and a person whose profile this device has
// never learned still gets their own plate rather than sharing the default with
// every other stranger.
//
// It lived in forum.js, where the board art needed it first. It is here because
// state.svelte.js needs it too and must not pull a rendering module in for one
// function.
export function hash32(s) {
  let h = 0x811c9dc5;
  const str = String(s || "");
  for (let i = 0; i < str.length; i++) {
    h ^= str.charCodeAt(i);
    h = (h * 0x01000193) >>> 0;
  }
  return h >>> 0;
}
