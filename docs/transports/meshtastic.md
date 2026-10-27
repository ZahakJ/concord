# LoRa mesh via Meshtastic

Concord can carry its sealed messages over a LoRa mesh through any
[Meshtastic](https://meshtastic.org) node. The node does the radio work; Concord
talks to it with the same client API the Meshtastic phone apps use.

Code: `internal/transport/meshtastic/` (link + codec),
`internal/transport/meshtastic/meshsim/` (simulator), `cmd/concord-meshsim/`
(simulator as a dev tool), registered in `internal/app/offgrid_meshtastic.go`.

## Design

- **Wire.** The stream API — `0x94 0xC3`, a 16-bit big-endian length (≤ 512),
  then one protobuf — over USB serial (115200 8N1) or TCP port 4403 (a node on
  Wi-Fi). It is the same framing on both. On serial the node also prints its debug
  log between frames, so the reader resyncs on the start bytes and counts the
  rest as noise. A bad length is treated as noise and never ends the session.
- **Protobufs.** Encoded by hand with `protowire`, using the field numbers from
  `meshtastic/protobufs` `mesh.proto`: `ToRadio{packet=1, want_config_id=3,
  disconnect=4, heartbeat=7}` and `FromRadio{id=1, packet=2, my_info=3,
  node_info=4, config_complete_id=7, rebooted=8, queueStatus=11}`, plus
  `MeshPacket`, `Data`, `User.is_licensed=6` and `QueueStatus`. There is no
  generated code and no new dependency: `google.golang.org/protobuf` was already
  in `go.mod`. Unknown fields are skipped, so newer firmware adding fields
  breaks nothing.
- **Handshake.** On enable, Concord sends `want_config_id`, reads the config
  dump until the same id comes back in `config_complete_id`, and learns the
  node number. Packets that arrive during the dump are dropped.
- **Traffic.** Every frame goes out as a broadcast `MeshPacket{to: 0xFFFFFFFF,
  hop_limit: 3, decoded: Data{portnum: PRIVATE_APP (256), payload: frame}}` on
  the configured channel. Received packets on the same port, from other nodes,
  go to the Manager. Everything else is ignored, including text messages,
  telemetry and packets the node could not decrypt.
- **Flow control.** The node reports `QueueStatus.free` after each packet. When
  the transmit queue is full, `Send` waits up to 30 s for room rather than let
  the node drop frames.
- **Keepalive and reconnect.** Concord sends a heartbeat every 5 minutes. If the
  node drops (USB unplugged, a `rebooted`), the link reconnects every 5 s and
  redoes the handshake until it is turned off.
- **Payload.** Only bundle frames: MLS ciphertext plus Concord's 12-byte
  header. The node's channel AES (the PSK) is a second layer that Concord does
  not rely on. A node on the default `AQ==` key is as safe for Concord as one on
  a private key.

### Caps

| field | value | why |
|---|---|---|
| MTU | 200 B | `DATA_PAYLOAD_LEN` is 233. 200 leaves room for firmware header growth (PKI, signatures). |
| Bitrate | 250 bit/s | LongFast is SF11/BW250/CR4:5, about 1.07 kbit/s PHY. A 200 B frame is about 1.6 s on air before preamble, and rebroadcasts plus the firmware's channel-utilisation limits (and EU868's 10% duty cycle) share the channel. With this figure the Manager's 60 s airtime cap allows about 9 frames (about 1.7 KB of ciphertext) per message. |
| Latency | 3 s | one hop plus queueing |
| Cost | 0 | ISM band, no subscription |
| Encryption | true | 915/868/433 MHz ISM: ciphertext is lawful |
| Local | true | no infrastructure |
| Mode | Live | |
| Range | "1–10 km line of sight, more per mesh hop" | |

### Amateur (licensed) mode is refused

A node with `is_licensed` set is a ham station. The firmware turns its own
encryption off and the operator is bound by FCC §97.113, which bans messages
encoded to hide their meaning. Concord has only ciphertext to send, so when the
handshake shows our node is licensed, `Start` fails with `ErrLicensed` and the
link never transmits. If a node reboots into licensed mode, the reconnect fails
the same way.

## Configuration

`<dataDir>/meshtastic.json` (all optional):

```json
{"device": "/dev/ttyACM0", "channel": 0, "port": 256, "hopLimit": 3}
```

`device` is a serial path, or `tcp://host[:4403]` for a node on Wi-Fi.
`CONCORD_MESHTASTIC` overrides it. If it is empty, Probe picks the first
`/dev/ttyUSB*` or `/dev/ttyACM*` whose USB vendor (from sysfs, without opening
it) is a Meshtastic board's bridge: Espressif `303a`, CP210x `10c4`,
CH340/CH9102 `1a86`, nRF52 `239a`, RP2040 `2e8a`. FTDI (`0403`) is excluded
because that is the RockBLOCK, and two links must never open one port. Probe
never opens the port or dials TCP. Serial is Linux (and Android) only for now;
elsewhere, use TCP.

Every Concord node must use the same channel (same name and PSK, so the mesh
can decode it) and the same `port`.

## What is simulated

`meshsim` runs nodes that speak the real client API (the config dump with
`my_info`, `node_info` and `config_complete_id`, `queueStatus` after each
packet, and log noise interleaved with frames) over `net.Pipe` or TCP. A
simulated channel between them has per-hop loss, latency and jitter, an
adjacency graph, and managed flooding (rebroadcast while `hop_limit` > 0,
deduplicated by packet id). It does not simulate airtime contention,
collisions, SNR-based rebroadcast timing, or the PKI/DM path.

Tests (`go test -race ./internal/transport/meshtastic/...`) cover: the frame
codec against noise, false starts and oversized lengths; protobuf round trips;
a fuzz target over the decoders; the handshake; licensed-mode refusal; Probe
never dialling; two Concord Managers on the real TCP path through a
three-node line (A–relay–B, A and B out of range), checking fragmentation,
reassembly and no duplicates; 15% loss (what arrives is exact and arrives once,
and the link stays up); Stop and restart; and reconnect after a drop.

Try it by hand:

```sh
go run ./cmd/concord-meshsim -nodes 3 -line -port 14403
CONCORD_MESHTASTIC=tcp://127.0.0.1:14403 <concord instance 1>
CONCORD_MESHTASTIC=tcp://127.0.0.1:14407 <concord instance 2>
```

## Hardware test plan

**Boards** (any two; three to test a hop):

- RAK WisBlock 4631 starter kit (nRF52840 + SX1262). Lowest power; USB shows
  as `/dev/ttyACM*` (vendor `239a`).
- Heltec WiFi LoRa 32 V3 (ESP32-S3 + SX1262). Cheap, has Wi-Fi for the TCP
  path; CP2102 or CH9102 (`/dev/ttyUSB*`).
- LILYGO T-Beam (ESP32 + SX1262/SX1276 + GPS). Good as the relay node.
- An antenna tuned to your region's band on every board. Never transmit
  without one.

**Firmware and settings** (Meshtastic ≥ 2.5, flashed with flasher.meshtastic.org):

1. Region: `US` (915) or `EU_868`. This is the legal limit, so set it first.
2. Modem preset `LONG_FAST` (the default) for the main test; repeat with
   `MEDIUM_FAST` to check the Bitrate figure.
3. Add a private channel on all boards (same name and random PSK): `meshtastic
   --ch-add concord --ch-set psk random --ch-index 1`, then copy the URL to the
   others. Set `"channel": 1` in `meshtastic.json`. Also run once on channel 0
   with the default key, to confirm Concord does not depend on the PSK.
4. Leave `is_licensed` **off**. Then turn it on for one test and confirm Concord
   refuses the link with the licensed-mode error and sends nothing (check with
   `meshtastic --listen` on another node).
5. Wi-Fi on the Heltec for the TCP test (`network.wifi_enabled`).

**Bench (same room):**

1. Plug a board in. Settings → Off-grid links must show "node on /dev/ttyACM0"
   while the link is still off, and `meshtastic --info` must still work, which
   shows Concord has not opened the port.
2. Enable the link. The status shows `connected to node !xxxxxxxx`.
3. Two laptops, each with a board. Pull the Ethernet and Wi-Fi from one (or
   block libp2p). Send a short message, then a ~1 KB one (5–6 frames). Both
   arrive once. `meshtastic --listen` on a third board shows only PRIVATE_APP
   packets with binary payloads, never the text.
4. Unplug and replug the USB while enabled: the link recovers within ~10 s.
5. Reboot the node from the CLI: same.
6. TCP: switch one side to `tcp://<heltec-ip>` and repeat step 3.

**Range:**

1. Fixed node at a high window or on a mast. Walk or drive away with a laptop
   or phone and send one message every 250 m. Log RSSI and SNR from
   `meshtastic --listen` and whether Concord delivered.
2. Note the distance where short (1-frame) messages stop arriving and the
   distance where 5-frame messages stop. Fragment loss compounds, which is the
   argument for a later FEC or retransmit layer.
3. Put the T-Beam halfway as a relay (`ROUTER` or `CLIENT` role) beyond the
   direct range. Confirm delivery through one hop and that `hop_start -
   hop_limit` = 1.
4. Measure airtime: time from send to arrival for 1, 3 and 6 frames. Update
   `Caps.Bitrate` if the effective rate is off by more than 2x.

Pass: no plaintext in any capture, exactly-once delivery, no
transmissions before enable, recovery from unplug and reboot, refusal in licensed mode.
