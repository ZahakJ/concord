# QR-code sequence link (`qr`)

Queued frames leave as an animated, looping sequence of QR codes on one
screen and come in through another device's camera. Only light crosses — the
link for an air-gapped machine, or a phone that must never touch a network.

Code: `internal/transport/qr/` (link description), `internal/app/offgrid_qr.go`
(registration), `frontend/src/lib/qrseq.js` (codes, collector),
`frontend/src/modals/QRSequence.svelte` (player), `frontend/src/QRScanner.svelte`
(scanner, new `continuous` mode), `internal/bridge/offgrid.go` (`ImportLinkFrames`).

## What a code carries

One transport frame, exactly as gossip would carry it — a bundle header plus
MLS ciphertext, or a fragment of one — as text:

```
CQR1:<set>:<index>:<count>:<base64 frame>
```

- `set`: 8 hex characters, random per showing, so the receiver counts "12/15
  scanned" for the right sequence and two sequences never mix up the counter.
- `index`/`count`: position in the sequence (0-based on the wire, shown 1-based).
- The link's MTU is **192 bytes**, so a code is at most ~280 characters: a
  version-12 QR at error-correction level M (65×65 modules), which a phone
  reads off a laptop screen at arm's length. The Manager fragments larger
  bundles into 192-byte frames itself.

Because frames are self-describing to the transport stack (fragment set id,
index and count are inside them), **each code is imported the moment it is
read**. A partly scanned sequence still delivers every message whose frames all
arrived; dedupe makes re-reading harmless.

## Flow

Sender (Settings → Off-grid links, **QR codes** enabled):

1. Messages queue for the `qr` link like any manual link.
2. **Show N queued as QR codes** calls `ExportLink("qr")` (drains the outbox
   into a pack), the UI decodes the pack and opens a white full-screen player:
   one code at a time, auto-advancing (600 / 350 / 220 ms per code —
   Slow/Normal/Fast), looping, with pause and step controls and a counter
   "3 / 15". The screen is kept awake on Android.
3. Closing the player drops those frames; the messages still sync over the
   internet when it is back (same as an unsaved file export).

Receiver:

1. **Scan QR codes** opens the camera scanner in continuous mode.
2. Every new code is fed through `ImportLinkFrames("qr", [frame])` → the
   Manager's reassembly, freshness check, dedupe, delivery, relay.
3. The scanner's status line shows progress — `12/15 scanned — missing 4, 9, 13`
   (missing indexes listed when 8 or fewer) — and closes itself with
   "Scanned all 15 codes" when the set is complete.

## Throughput

At Normal speed ~2.9 codes/s × 192 B ≈ 550 B/s shown; a camera misses some on
each loop, so a few hundred B/s effective. A typical chat message is 2–3 codes.

## Tests

- `internal/transport/qr`: a 1500-byte message queued on manager A, exported
  as 192-byte frames, fed to manager B shuffled and twice over → delivered once,
  byte-for-byte.
- `frontend/src/lib/qrseq.test.mjs`: pack decoding (incl. multi-byte uvarint,
  truncated and foreign packs refused), code format, parsing of foreign /
  malformed codes, the collector (out-of-order, duplicates, missing list,
  two sequences), reassembly.
- `frontend/src/lib/qrseq.codec.test.mjs`: full 192-byte frames through the
  real `qrcode` encoder, rasterised, and decoded with `jsQR` (the decoder the
  scanner uses): all decode, all fit QR version ≤ 12, all reassemble.

Not tested here: a real camera against a real screen (no camera in the dev
environment). Try it: two devices, sender at Normal speed, receiver 20–40 cm
from the screen; if it stalls, Slow speed and screen brightness up.

## RPC

`ImportLinkFrames(name string, framesB64 []string) (int, error)` — only for a
registered **manual** link, at most 512 frames per call, each frame at most
that link's MTU; malformed ones are skipped. Returns how many were taken.
