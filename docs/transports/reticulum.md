# Reticulum (RNS) link

Concord can carry its traffic over a [Reticulum](https://reticulum.network)
mesh: LoRa radios (RNodes), serial lines, packet modems, LANs, or any mix of
them, routed over multiple hops with no internet, tower or satellite. The link
is called **Reticulum mesh (RNS)** in Settings → Off-grid links and is off
until you turn it on.

What crosses the mesh is exactly what crosses gossipsub: MLS ciphertext signed
by its author, wrapped in Concord's bundle header (`internal/transport/bundle.go`)
and fragmented to 380-byte frames. RNS then encrypts each frame again to the
receiving node's RNS identity. Neither the link nor the sidecar ever sees a key
or plaintext.

## Design: a Python sidecar, not a Go port

| | Sidecar (chosen) | Native Go RNS |
|---|---|---|
| Protocol fidelity | The reference implementation, by definition | A re-implementation of a protocol with no formal spec; the Python code *is* the spec and still changes (1.x) |
| Interfaces | All of them: RNode, Auto, TCP, UDP, I2P, KISS, serial, pipe, multi-RNode | Each one ported and kept in step by hand |
| Effort / risk | ~250 lines of Python + ~500 of Go | Thousands of lines of crypto, routing, announce and interface code to own and audit |
| Cost | Needs Python 3 + `rns` on the machine, and a second process | Single binary |

The deciding factor is that a subtly wrong routing or announce implementation
silently fails to interoperate with the rest of the mesh, and the existing
Go ports are partial. The sidecar gets every interface RNS supports for free.
If a mature Go RNS appears, it can replace the sidecar behind the same
`Transport` without touching anything above it.

### How it works

`internal/transport/reticulum` embeds `concord-rns.py` (canonical copy in that
package, symlinked at `scripts/concord-rns.py`) and, when you enable the link,
runs it with `python -c` from the RNS config dir. They talk over stdin/stdout
with `type(1) | length(2, BE) | payload` frames (`SEND`, `ANNOUNCE` to the
sidecar; `RECV`, `READY`, `ERROR`, `PEERS` back). Logs go to stderr and into
Concord's log. The sidecar:

- starts its own RNS instance on the config dir, and refuses to go on if RNS
  attached to a *shared* instance instead (`is_connected_to_shared_instance`):
  a shared instance's interfaces are not in our config and cannot be checked;
- makes a **fresh RNS identity in memory** on every start — never written to
  disk — and replaces it every 24 hours with a new identity and destination,
  announced at once. The previous destination keeps receiving until the next
  rotation. The identity exists only so RNS can route; Concord's security is
  MLS, and a long-lived routing identity would only let a listener follow a
  device around. (RNS's own `storage/transport_identity` is still kept by RNS;
  it identifies this node on air only when it acts as a transport node. The
  sidecar leaves it alone because the config dir may be one you share with
  other RNS tools.);
- registers an IN **SINGLE** destination `concord.bundle`, announces it at
  start and every 10 minutes (`announceIntervalSec`);
- remembers peers that announce `concord.bundle` (table capped at 256) and
  forgets any not heard for 3 announce intervals;
- sends each frame as one RNS packet to the **8 most recently heard** live
  peers. SINGLE was chosen because RNS drops PLAIN and GROUP packets that have
  travelled more than one hop (`Transport.inbound` in RNS 1.5.7), while SINGLE
  packets follow the path an announce set up through transport nodes;
- requests a path for a peer it has no route to at most once per peer every 5
  minutes and 10 times a minute overall: each request is a broadcast on every
  interface;
- exits when its stdin closes. Stop closes stdin, waits 3 s, then kills.

The link is marked `SelfMeshing`: RNS does its own routing, so Concord's
Manager does not relay bundles back onto it.

**Airtime is per copy.** The Manager's airtime estimate (frames × MTU ÷
Bitrate) is for one copy of each frame; the sidecar sends up to 8, so real
airtime can be up to 8× the estimate.

### Caps

| Field | Value | Why |
|---|---|---|
| MTU | 380 | `RNS.Packet.ENCRYPTED_MDU` is 383 in RNS 1.5.7 (`Reticulum.MTU` 500, `MDU` 464). That figure already allows for the 16-byte transport header a multi-hop packet gains, so 380 fits one packet at any hop count. |
| Bitrate | 1000 bit/s | Conservative for an RNode on LoRa at a mid spreading factor: a few kbit/s on air, ~76% of each packet is our payload, and the channel is shared. LAN/TCP are much faster, but the Manager uses this only to avoid choking a radio with big messages. |
| Latency | 3 s | One LoRa hop for a full packet, plus queueing. |
| Encryption | true | Refused on amateur-only interfaces (below). |
| Local | true | RNode, serial and LAN need no infrastructure. A user *can* add a TCP interface to an internet RNS node; that is their config choice. |
| Mode / cost | Live, free | |

## Install (no sudo)

Any one of these. Concord looks for Python in this order: `CONCORD_RNS_PYTHON`
or `"python"` in `reticulum.json`; `<data>/reticulum/venv`; a pipx `rns` venv;
`python3` on PATH.

```sh
# pipx: isolated, and Concord finds it automatically
pipx install rns

# or a venv inside Concord's RNS config dir (also found automatically)
python3 -m venv ~/.local/share/concord/reticulum/venv     # adjust to your data dir
~/.local/share/concord/reticulum/venv/bin/pip install rns

# or uv
uv venv /some/where && uv pip install --python /some/where/bin/python rns
export CONCORD_RNS_PYTHON=/some/where/bin/python
```

## Configuration

Concord uses its own RNS config dir, `<data dir>/reticulum`, never
`~/.reticulum`. Override with `CONCORD_RNS_CONFIG` or `reticulum.json` in the
data dir:

```json
{ "python": "/opt/rns/bin/python", "config": "/home/me/.config/concord-rns", "announceIntervalSec": 900 }
```

`CONCORD_RNS_SCRIPT` runs a sidecar file instead of the embedded one (for
development). If the config dir has no `config`, Concord writes one with a
private instance (`share_instance = No`) and **no interface enabled**, only
commented-out examples: which radio or network Concord transmits on is your
choice. Until you enable one, Settings shows "no Reticulum interface set up"
and turning the link on fails with "no interface enabled". Enable or add
interfaces in `<config>/config`:

```ini
[reticulum]
  enable_transport = No       # Yes makes this node route for others
  share_instance = No
  panic_on_interface_error = No

[interfaces]
  [[Local network]]
    type = AutoInterface
    enabled = Yes

  # RNode on US ISM 915 MHz (EU: 868.1 MHz, 863-870 band; mind 1% duty cycle)
  [[LoRa]]
    type = RNodeInterface
    enabled = Yes
    port = /dev/ttyACM0
    frequency = 915000000
    bandwidth = 125000
    txpower = 17
    spreadingfactor = 8
    codingrate = 5

  # A friend's RNS node or a transport node over TCP (needs a network)
  [[Friend]]
    type = TCPClientInterface
    enabled = Yes
    target_host = 192.168.1.20
    target_port = 4242

  # Point-to-point serial line or a non-amateur radio modem. Serial, KISS
  # and pipe interfaces can sit on any radio, so they need your say-so:
  [[Serial]]
    type = SerialInterface
    enabled = Yes
    port = /dev/ttyUSB0
    speed = 115200
    concord_non_amateur = yes
```

If you already run `rnsd`, either give Concord its own RNode, or add a
`TCPClientInterface` to your rnsd's `TCPServerInterface`. Keep
`share_instance = No`: if Concord's RNS attaches to rnsd as a shared-instance
client, it would be using rnsd's interfaces, which it cannot check, so the
sidecar refuses to run. (If Concord's own instance *becomes* the shared
instance, that is allowed: its interfaces are the ones it checked.)

## The amateur-band rule

Amateur radio forbids messages encoded to obscure their meaning (FCC §97.113(a)(4)
in the US; equivalent rules almost everywhere). Concord has nothing to send but
ciphertext, so the sidecar fails closed: it **refuses to start** (exits
non-zero, and the settings row shows why) if any enabled interface:

- is an `AX25KISSInterface` (amateur packet, requires a callsign);
- sets `callsign` or `id_callsign` (amateur station identification);
- has a `frequency` in an amateur-only band: 50–54, 144–148, 222–225,
  420–450 or 1240–1300 MHz (this covers the popular 433 MHz LoRa);
- is an `RNodeInterface` (or `RNodeMultiInterface` sub-interface) with no
  `frequency`;
- is any type that can drive an arbitrary radio, meaning `KISSInterface`,
  `SerialInterface`, `PipeInterface`, `WeaveInterface` or any type not known to
  be IP-only, unless its section sets `concord_non_amateur = yes`. That line is
  your statement that the radio behind it is not on amateur spectrum. IP-only
  types (Auto, TCP, UDP, I2P, Backbone) need no flag.

It also refuses when no interface is enabled, and when RNS attached to a shared
instance (see above).

902–928 MHz (US ISM) and 863–870 MHz (EU SRD) are licence-free and allowed,
within their power and duty-cycle limits. Disabled interfaces are ignored.
GMRS/FRS can't be configured through RNS and are likewise not for ciphertext.

## What is tested

`CONCORD_RNS_PYTHON=/path/to/python go test -race ./internal/transport/reticulum/`
(RNS tests skip if python can't `import RNS`):

- pipe framing round trip; oversize lengths and torn frames rejected;
- a fake sidecar (`/bin/sh`) that emits garbage is killed and Send fails; a
  sidecar error before READY comes back from Start; Stop exits gracefully on
  EOF, kills a sidecar that ignores stdin, and context cancel kills it too;
- ham refusal: 433 MHz and 145.5 MHz RNodes, AX.25, `id_callsign` on 915 MHz,
  an RNode without a frequency, serial/KISS without the flag, a pipe with the
  flag set to `no`, and a flagged KISS on 144.39 MHz are all refused. 915 and
  868 MHz RNodes, flagged serial/KISS, TCP, and a disabled 433 MHz RNode pass
  `--check-only`;
- no interface: Probe says so, Start refuses, and the generated config enables
  nothing and has `share_instance = No`;
- shared instance: a second sidecar with the same `share_instance = Yes`
  config attaches to the first one's instance and refuses to run;
- sidecar peer table (fake clock): expiry after the TTL, cap eviction, the
  fan-out cap of 8, most recent first; path requests limited per peer and
  overall;
- Start does not block Probe or Send (Probe says "starting"), a second Start
  returns "already starting", Stop during Start aborts it, and Stop kills a
  grandchild that holds stdout (process group);
- end to end: two real sidecars joined by `TCPServerInterface` /
  `TCPClientInterface` on loopback, each under its own `transport.Manager`;
  a 1000-byte ciphertext (3 frames at MTU 380) crosses A→B and a 200-byte one
  B→A; Close reaps both processes.

Not tested here: real radios, multi-hop through a transport node, and
`AutoInterface` (needs IPv6 link-local multicast).

## Hardware test plan

1. **Hardware**: two RNode-capable boards for your region's ISM band, e.g.
   LilyGO T-Beam (SX1262, 915 MHz) or Heltec WiFi LoRa 32 V3, each with a
   proper antenna attached before power-on.
2. **Flash**: `rnodeconf --autoinstall` (from the `rns` package) on each board;
   check with `rnodeconf /dev/ttyACM0 --info`.
3. **Config**: on two laptops, enable the RNode block above with the same
   frequency/bandwidth/SF/CR, Concord data dirs on each, link enabled.
4. **Bench**: confirm both sides log `peer … (1 known)`; send a short message,
   then a 1–2 KB one; check `rnstatus --config <data>/reticulum` traffic counters.
5. **Range**: one node fixed; walk/drive the other out at SF8 then SF10/SF12,
   recording distance, RSSI/SNR (`rnstatus`), and delivery rate of 20 messages
   per point. Expect 1–3 km urban, 5–10+ km line of sight.
6. **Multi-hop**: a third RNode with `rnsd` and `enable_transport = Yes` in the
   middle, the ends out of each other's range; confirm delivery and `rnpath`
   showing 2 hops.
7. **Legal check**: point an RNode at 433 MHz in config and confirm Concord
   refuses to enable the link.

## Limitations

- **Python dependency**: needs Python 3 and `rns`; Probe reports "no hardware"
  with install hints when it can't find them (it checks files only, never runs
  Python).
- **Desktop only**: Android/iOS cannot run the sidecar. A mobile path would be
  a native RNS port or Sideband-style bundling.
- **Announce traffic**: every node announces every 10 minutes (~170 bytes on
  air each); on busy LoRa channels raise `announceIntervalSec`. A 24-hour
  identity rotation briefly makes a node look like two peers to its
  neighbours, until the old one expires (3 announce intervals).
- **Fan-out cost**: each frame goes once to each of up to 8 peers (unicast
  SINGLE packets), so airtime is up to 8× the Manager's estimate, and in a mesh
  of more than 8 Concord nodes, the ones heard least recently are not sent to
  directly. A large mesh would want RNS GROUP destinations, which don't route
  beyond one hop today.
- **No delivery receipts**: packets are fire-and-forget (`create_receipt=False`);
  Concord's normal sync heals gaps once any path returns.
