// offgrid.js — how Settings → Off-grid links describes a link in one line.
//
// Plain numbers, the way a person compares a Bluetooth chip to a satellite
// modem: how far, how big a message, how fast, what it costs.

export function fmtRate(bps) {
  if (!bps) return "";
  if (bps >= 1e6) return `${+(bps / 1e6).toFixed(1)} Mbps`;
  if (bps >= 1e3) return `${+(bps / 1e3).toFixed(1)} kbps`;
  return `${bps} bps`;
}

export function fmtCost(micro) {
  if (!micro) return "free";
  const usd = micro / 1e6;
  return usd >= 0.995 ? `$${usd.toFixed(2)}` : `${+(usd * 100).toFixed(1)}¢`;
}

export function capsLine(l) {
  const c = l.caps || {};
  const parts = [];
  if (c.range) parts.push(c.range);
  if (c.bitrate) parts.push(fmtRate(c.bitrate));
  if (c.mtu && c.mtu < 4096) parts.push(`${c.mtu} B frames`);
  parts.push(c.costMicroUSD ? `${fmtCost(c.costMicroUSD)} per frame` : "free");
  return parts.join(" · ");
}

// statusLine is the row's second line: what is true right now, then the caps.
export function statusLine(l) {
  if (!l.caps?.encryption) return "Off limits — these bands forbid encrypted messages";
  if (l.error) return `Problem: ${l.error}`;
  if (l.status?.state === "no-hardware") return `No hardware found${l.status.detail ? ` — ${l.status.detail}` : ""}`;
  if (l.status?.state === "error") return `Not working — ${l.status.detail || "unknown error"}`;
  return capsLine(l);
}

// usable says whether the switch may be flipped on.
export function usable(l) {
  return !!l.caps?.encryption && (l.enabled || l.status?.state === "available");
}
