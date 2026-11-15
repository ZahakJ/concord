// Delivery / read ticks for the open DM, kept as a map from message id to the
// backend's ReceiptView. Refetched whole for the channel on each "receipts"
// event: the backend coalesces them, and a DM's worth of rows is small.
import { api, on } from "./api.js";

export const R = $state({ channelId: "", byMsg: {}, readOn: false });

let seq = 0;
export async function refreshReceipts(channelId) {
  if (!channelId) return;
  const mine = ++seq;
  try {
    const v = await api.receipts(channelId);
    if (mine !== seq) return; // a newer request owns the answer
    R.channelId = channelId;
    R.byMsg = v || {};
  } catch {
    /* older backend: no ticks */
  }
}

export async function refreshReadReceiptsSetting() {
  try {
    R.readOn = !!(await api.readReceiptsEnabled());
  } catch {
    R.readOn = false;
  }
}

/** Subscribe to backend receipt events; `activeChannel` returns the open channel id. */
export function watchReceipts(activeChannel) {
  refreshReadReceiptsSetting();
  return on("receipts", (r) => {
    if (!r?.channelId) {
      // The setting changed (here or on another device): redraw everything.
      refreshReadReceiptsSetting();
      refreshReceipts(activeChannel());
    } else if (r.channelId === activeChannel()) {
      refreshReceipts(r.channelId);
    }
  });
}
