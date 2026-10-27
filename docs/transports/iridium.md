# Satellite via Iridium SBD (RockBLOCK)

Concord can send its sealed messages through Iridium Short Burst Data with a
RockBLOCK 9603, or any 9602 or 9603 modem on a serial port. It works anywhere with
open sky. Every frame costs money, so nothing is sent until the user has seen
the price and pressed send.

Code: `internal/transport/iridium/` (AT driver + link),
`internal/transport/iridium/sbdsim/` (simulated modems and constellation),
registered in `internal/app/offgrid_iridium.go`.

## How a message gets from one RockBLOCK to another

Iridium SBD is modem ↔ gateway only. There is no direct device-to-device path. The
route is: modem A → satellite → Iridium gateway → **Rock7 / Ground Control's
cloud** → gateway → satellite → modem B's mailbox. Modem B picks the message up
on its next SBD session, which it starts when it gets a ring alert. Rock7 forwards
an MO to another device when:

- both RockBLOCKs are on the same Rock7 account and in the same **Delivery
  Group** (set up in the RockBLOCK Management System), and
- the message starts with `RB` plus the destination's 7-digit serial
  (zero-padded). Rock7's "RockBLOCK to RockBLOCK" gateway command; the prefix
  is removed on delivery. The driver also strips it if it is left on.

So every peer is a separate MO: N peers cost N times as much. Recipients are
billed again to *receive*. The HTTP API (`rockblock.rock7.com/rockblock/MT`) can
also inject MT messages from the internet. Concord does not use it, because a
link that needs the internet to reach a satellite user is not an off-grid link.

## Driver

19200 8N1, no flow control. On enable it sends `AT` (retried, since the first
one after power-up is often lost), `ATE0`, `AT&K0` (RockBLOCK is 3-wire),
`AT+SBDD0`, `AT+SBDMTA=1` (ring alerts) and `AT+CSQ`.

Send, for each peer:

1. `AT+SBDWB=<n>` → `READY`. Send `RB<serial>‖frame‖checksum` (the 16-bit byte
   sum, big-endian) and expect `0`.
2. `AT+CSQ` (for the status line), then `AT+SBDIX` →
   `+SBDIX: <MO status>, <MOMSN>, <MT status>, <MTMSN>, <MT len>, <MT queued>`.
   MO status 0–4 is success. Anything else is a failure, and failures are not
   billed. Up to 3 attempts with growing back-off (20 s, 40 s).
3. **Always `AT+SBDD0` afterwards.** The modem keeps the MO buffer after a
   successful session. Without this, the next mailbox check would send the same
   message again and bill for it. The simulator models this on purpose, and the
   tests check that each frame reaches the gateway exactly once.
4. If the session brought down an MT (status 1), `AT+SBDRB` reads it
   (length‖message‖checksum, checked and limited to 270 bytes). If more are
   queued, the mailbox loop drains them.

**A session whose answer is lost is never retried blindly.** This covers a
timeout, the port closing (Stop or unplug) and a garbled `+SBDIX`. The modem
finishes such a session on its own, so a blind retry would send and bill the
message twice. Before the session the driver records MOMSN from `AT+SBDSX`
(a local command: no session, no charge). Afterwards it asks again:

- If MOMSN moved, the session completed. The frame counts as sent and is
  billed once.
- If it didn't move, nothing was sent and the frame is retried.
- If the modem can't answer (the port is gone), the send is marked
  *pending*. No send and no mailbox session runs until the next Start
  settles it with `AT+SBDSX`. Mailbox sessions also move MOMSN, so running
  one first would make the outcome unknowable. Settings shows "send(s)
  awaiting modem confirmation".

Only an `ERROR` answer counts as a known failure. Pending state is kept in
memory, which matches the Manager's outbox. After a process restart both are
gone, and the message still syncs over the internet later.

A frame already delivered to some peers is remembered, so if the Manager
retries a partly failed Flush, those peers are not sent (or billed) again.

Receiving is capped. See "Cost and polling". A `SBDRING` between conversations wakes a mailbox session
(`AT+SBDD0`, then `AT+SBDIXA`), which drains up to 8 queued messages. MT bytes
are handed to the Manager only if they look like a Concord bundle or fragment
(`0xC1` or `0xC2`). Anything else, such as a test message from the Rock7 web
console, is dropped.

Robustness: a reader goroutine fills an 8 KB ring buffer. Lines over 512 bytes
and runs of garbage with no line end are discarded. Ring alerts are recognised
only between conversations or by the command reader, never inside an SBDRB
binary transfer. Every command has a deadline (90 s for SBDIX). Stop closes the
port, which unblocks any in-flight command, and waits for every goroutine.

## Cost and polling

Rock7 bills **1 credit per 50 bytes or part, sent or received, and 1 credit
for a mailbox check that finds nothing**
(docs.groundcontrol.com → "Iridium contract costs"). Line rental is £13 / $17
per month in any month the device is used. Ground Control lists credits at
$0.20 each (50-credit bundle) down to $0.10 (5,000) as of October 2026. The
default assumption is **$0.15/credit**. Set your own with `creditUSD`.

- MTU **241**: with the 9-byte `RB` prefix one MO is exactly 250 B, which is 5
  credits. That is under the 340 B MO buffer, and the delivered MT is under
  the 270 B MT buffer even if the prefix were kept.
- `CostMicroUSD` per frame = peers × 5 credits × price. At the default that is
  **$0.75 per frame per peer**, so a 600-byte ciphertext (3 frames) to one peer
  shows as $2.25 in the outbox. The figure is an upper bound: a short last
  fragment may bill fewer credits.
- Because the cost is non-zero, the Manager never auto-sends. Frames wait in
  the outbox until `Flush`, and relayed traffic from other people never lands
  in this outbox.
- **Daily receive cap.** Answering rings and polling may spend at most
  `receiveCreditsPerDay` credits per UTC day (default 20, about $3). Each MT
  is counted at 1 credit per 50 B and each empty check at 1. Once the cap is
  hit, rings are ignored until midnight UTC. Mail that an approved send's
  session brings down is still read and counted, because SBDIX always
  fetches queued mail and Rock7 has already billed it. Settings shows
  "receive today N/cap", what has been sent since start, and the polling
  cost per day when polling is on.
- **Rings are filtered.**
  - A ring heard before a session that left the mailbox empty is for mail
    that session already fetched, so it isn't answered. This is checked
    again after waiting for the modem, so a ring during a send costs nothing.
  - Three unanswerable rings in a row (no sky) mute ring answers for an hour,
    instead of retrying every 30 s forever.
  - The review asked for a 60 s quiet window after an empty queue. I didn't
    use it: in the simulator it swallowed real mail, since consecutive
    fragments arrive seconds apart. The timestamp rule above has the same
    effect for echoes. A ring that arrives *after* such a session but echoes
    it can still cost one empty check, and that is inside the cap.
- **Ring alerts and SBDREG.** The gateway sends ring alerts only to a modem
  whose location it has registered: after any SBDIX, or `AT+SBDREG`. The
  driver never sends SBDREG on its own, because whether Rock7 bills it is
  unverified. A freshly powered modem may therefore not ring until its
  first send. The hardware plan covers this.
- **Polling is off by default.** An empty check costs a credit, so a 10-minute
  poll is 144 credits/day, about $22/day. Mail is fetched on ring alerts
  (free to receive the alert; the message itself is billed) and in the same
  session as every send. `pollMinutes` turns on a timer for setups without
  ring alerts. The receiving cost is not shown in the outbox, because the
  Manager has no concept of receive-side cost. That is a core change worth
  making (see below).

Caps: MTU 241, Bitrate 60 bit/s (one frame per ~30 s session), Latency 90 s
(the session, plus Rock7 forwarding, plus the far side's ring-alert session),
Encryption true (Iridium carries arbitrary data), Local **false** (offline mode
stops it), Mode Live (the cost gate makes it effectively manual), Range
"global (needs open sky)".

## Configuration

`<dataDir>/iridium.json`:

```json
{"device": "/dev/ttyUSB0", "peers": ["0012345"], "creditUSD": 0.10, "pollMinutes": 0, "receiveCreditsPerDay": 20}
```

`CONCORD_IRIDIUM` overrides the device, and `CONCORD_IRIDIUM_PEERS`
(comma-separated) overrides the peers. If `device` is empty, Probe picks the
first FTDI (`0403`) `/dev/ttyUSB*`. That is RockBLOCK's USB chip, and it is
excluded from Meshtastic's auto-detect. Probe never opens the port. On enable the port is opened exclusively
(`TIOCEXCL`), and only `AT` is written until the device answers `OK`. A
device that doesn't answer is closed with nothing else sent. Without at
least one valid peer serial, the link shows an error and refuses to start.

## What is simulated

`sbdsim` provides fake 9603s over `net.Pipe`. They implement AT, ATE0/1, AT&K0,
AT+CSQ, AT+SBDWB (READY, checksum and size checks), AT+SBDIX/SBDIXA,
AT+SBDRB, AT+SBDD0/1/2 and AT+SBDMTA with unsolicited SBDRING. The fake
constellation routes `RB<serial>` MOs to the target's mailbox as Rock7 does,
and has signal strength (0 fails every session with status 32), a random
session failure rate (status 18), session delay, and Rock7's credit billing
(including the empty-mailbox credit). It does not simulate registration,
real timing, the 9603's power and supercap behaviour, or the HTTP API.

Tests (`go test -race ./internal/transport/iridium/...`):

- checksum and credit maths
- SBDIX parsing (good and bad lines)
- RB-prefix unwrapping
- the line reader against 20 KB of random bytes
- end to end with two Managers: nothing spent before Flush, the outbox shows
  frames × price, the bill is at most the approved cost, each MO reaches the
  gateway exactly once, and the receiver fetches by ring alert with no empty
  checks
- no signal: 3 attempts, 0 credits, the frame is requeued, and a later Flush
  bills exactly once
- partial fan-out is not re-billed
- refusal with no peers or a bad serial
- a negative price cannot make the link free
- Probe never opens the port
- Stop during a session
- opt-in polling

## Hardware test plan

**Kit:** two RockBLOCK 9603s (one is enough for loopback through the
Delivery Group), each with its USB cable or FTDI breakout and the patch antenna,
on one Ground Control account with line rental active and about 100 credits.
You need a clear view of the sky: outdoors, away from buildings, with the
antenna pointing up.

1. **Account:** create a Delivery Group containing both devices. Note the serials.
2. **Bench:** plug in. Settings shows "RockBLOCK found" while the link is
   off, and `screen /dev/ttyUSB0 19200` still works, which shows Concord has
   not opened the port. Write `iridium.json` with the other device as peer.
3. **Enable indoors:** the status shows "modem up". Signal is probably 0–1.
   The Rock7 console must show **no** session and **no** credits used.
4. **Billing check, outdoors:** signal ≥ 2. Send a short message. The outbox shows
   1 frame, $0.75. Press send. Within ~1 minute the sender's console shows one MO
   of ≤ 250 bytes and **5** credits, and the receiver shows a ring alert, one MT,
   and 5 credits. Check that no second MO ever appears. This confirms the
   SBDD0 rule.
5. **Ring alerts:** with the receiver idle, send from the other side. The
   receiver must fetch within ~1–2 minutes with no polling. If it does not, check
   ring alerts in the console (and `AT+SBDMTA?`). Also check whether ring alerts
   need a recent `AT+SBDREG`, which the driver does not send yet. Send
   one by hand and check whether the console bills it.
9. **Lost answers:** unplug the USB while a send's session is running. Plug
   it back in and re-enable. The status shows the pending send settled, and
   the console shows one MO, not two.
10. **Receive cap:** set `receiveCreditsPerDay` to 6. Queue 5 messages from the
   console. Concord stops answering rings at the cap and says so.
6. **Fragmented:** a ~600 B ciphertext (3 frames) arrives whole. Then block
   the antenna halfway through and check the remaining frames stay queued and
   go after a second send.
7. **Failure codes:** antenna unplugged gives status 32/33, which shows in
   the link's error. No credits are used.
8. **Cost reconciliation:** compare the credits used in the console with the
   cost shown in the outbox over the whole test. Shown must be ≥ billed. Update
   `DefaultCreditUSD` if your bundle price differs.

Pass: zero credits spent without pressing send (other than receiving),
exactly one MO per frame per peer, and messages arriving whole.
