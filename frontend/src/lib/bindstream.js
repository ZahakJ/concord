// Bind a MediaStream (by S.videoTiles key) to a <video>. The element stays
// visually off until it has a real frame, and if it sits in a share box the
// box's aspect-ratio follows the picture so a 16:9 desktop is not stretched
// into the leftover column.
import { getVideoStream } from "./state.svelte.js";

export function bindStream(node, key) {
  const go = () => {
    if (!(node.videoWidth > 0)) return;
    node.classList.add("ready");
    const host = node.closest("[data-share-box]");
    if (host) {
      host.style.setProperty("--share-ar", `${node.videoWidth} / ${node.videoHeight}`);
      host.style.setProperty("--share-ar-n", String(node.videoWidth / node.videoHeight));
    }
  };
  const attach = (k) => {
    node.classList.remove("ready");
    node.srcObject = getVideoStream(k);
    if (!node.srcObject) return;
    node.addEventListener("loadeddata", go);
    node.addEventListener("playing", go);
    node.addEventListener("resize", go);
    go();
  };
  const detach = () => {
    node.removeEventListener("loadeddata", go);
    node.removeEventListener("playing", go);
    node.removeEventListener("resize", go);
    node.srcObject = null;
    node.classList.remove("ready");
  };
  attach(key);
  return {
    update: (k) => {
      detach();
      attach(k);
    },
    destroy: detach,
  };
}
