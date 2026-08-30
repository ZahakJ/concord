// Which screen share the call is looking at.
//
// Theater focus lives on VoicePanel, which unmounts the moment you walk into a
// DM. The mini dock still has to know which picture to keep showing, so the
// key lives here — written by the stage, read by the dock, and harmless if
// stale: watchedShare() only returns a tile that is still live.

import { S } from "./state.svelte.js";

let key = $state("");

export function setWatchedShare(k) {
  key = k || "";
}

export function watchedShare() {
  const tiles = S.videoTiles.filter((t) => t.kind === "screen");
  if (!tiles.length) return null;
  return tiles.find((t) => t.key === key) || tiles[0];
}
