# Off-grid transports

Concord normally moves messages over the internet: libp2p gossipsub, with a rendezvous and relays. This page covers everything else: the links that carry the same messages when the internet isn't there.

You turn them on in **Settings → This device → Off-grid links**. Every link starts **off**. That switch is the only thing that powers a radio, opens a microphone or touches a serial port.

## What crosses the link

Exactly the bytes gossipsub would carry: the MLS ciphertext of a message that is already signed by its author. On top of that, a 12-byte bundle header:

| bytes | field | why |
|---|---|---|
| 1 | format | `0xC1` |
| 1 | hops left | relay budget (3) |
| 2 | day number | delivery window (7 days) and tag rotation |
| 8 | tag = HMAC(topic, day) | lets a member recognise its channel |

The header has no sender, recipient, guild or topic. The tag rotates at midnight UTC, so outsiders can't follow a channel from one day to the next. A link never sees plaintext and can't ask for it. A relaying phone can't read what it carries, and MLS rejects anything forged or tampered with. Code: `internal/transport/`.

**Measured cost of one chat line:** a 23-byte message is about 600 B on the air. MLS framing is 146 B of that; the rest is the signed JSON message. That works out to:

- 4 LoRa frames (200 B each)
- 3 Iridium frames
- 5 audio frames
- 4 QR codes

Larger messages are fragmented and reassembled, up to 255 fragments. The same message arriving over BLE, LoRa and gossipsub is shown **once**.

**Behaviour:**

- **Live links** (BLE, LoRa, Reticulum) send automatically. A live link skips any message that would take more than 60 s of airtime on it.
- **Manual links** (audio, QR, file) queue messages until you press send, play, show or save.
- **Paid links** (satellite) queue too, and show the total before anything is spent. If the total changes after you've seen it, sending is refused.
- **Relay** (on by default, and it only runs on links you've enabled): your device passes other people's sealed bundles a few hops on free local links, so a room of phones becomes a mesh. It is rate-limited to 30 a minute. Links that mesh on their own (Meshtastic, Reticulum) are left to do it.
- **Gateway:** when a member receives a message off-grid, their device republishes it to gossipsub, so one phone with signal bridges everyone near it.
- **Offline mode:** only local links run, so no satellite.
- **Voice and video** stay on IP links: LAN, Wi-Fi, the internet. No radio here has the bandwidth for them.

## The links

| Link | Range | Speed (useful) | Cost | Ciphertext legal? | Status |
|---|---|---|---|---|---|
| **LAN / Wi-Fi** (mDNS + libp2p) | the network | Mbps | free | yes | **Works today.** Needs no internet: two nodes found each other by mDNS on this box. Voice and video work too. |
| **Wi-Fi Direct / hotspot** | ~50–100 m | Mbps | free | yes | **Nothing to build.** It's an OS-level link that produces an IP network, and Concord's LAN path runs over it unchanged. Linux needs root (wpa_supplicant P2P); Android needs a native plugin (future). |
| **Bluetooth LE** (BlueZ GATT) | ~10–50 m | ~20 kbit/s | free | yes | **Tested on air on one adapter** (advertising, rotation, scan, GATT registration, clean stop), checked with an HCI capture. **Not yet tested device-to-device** (no second radio here). Linux only; Android/iOS is planned. [ble.md](transports/ble.md) |
| **Audio modem** (16-FSK ×4, RS + CRC) | same room, ~1–5 m | ~29 B/s | free | yes | **Tested through the real sound stack** (PipeWire play → monitor → decode, byte-exact). **Not tested acoustically:** this box has only an S/PDIF output, and the JV603 mic reads digital silence (probably hardware-muted). [audio.md](transports/audio.md) |
| **QR sequence** | line of sight to a screen | a few codes per second | free | yes | **Tested in software** with the real `qrcode` encoder and the `jsQR` decoder. Not yet tested camera-to-screen. [qr.md](transports/qr.md) |
| **File / USB** | wherever you carry it | — | free | yes | **Works.** Pack export/import is tested end to end. |
| **LoRa via Meshtastic** (serial/TCP API) | 1–10 km line of sight per hop | ~250 bit/s | free | yes (ISM band) | **Simulated.** Real protocol against a simulated mesh with loss and rebroadcast. Refuses a node in licensed (ham) mode. [meshtastic.md](transports/meshtastic.md) |
| **Reticulum** (rnsd sidecar) | per interface: RNode LoRa, serial, LAN, I2P | interface-bound | free | yes, except ham interfaces | **Integrated.** Tested with two live RNS 1.5.7 instances over TCP. Refuses AX.25 and callsign interfaces. Needs Python, so desktop only. [reticulum.md](transports/reticulum.md) |
| **Iridium SBD** (RockBLOCK 9603) | global | ~60 bit/s, ~1 min a session | **~$0.75 per frame per peer** (5 credits) | yes | **Simulated.** Full AT driver against a simulated modem and constellation. Never spends without approval; mailbox polling is off by default because empty checks are billed. [iridium.md](transports/iridium.md) |
| **Starlink** | global | Mbps | your plan | yes | **Nothing to build:** it's an IP link, and Concord works over it as over any ISP. |
| **Blockstream Satellite** | most continents | downlink only | free | n/a | **Skipped.** Broadcast downlink only, with no way to send. It could one day carry a guild's public channel or app updates. |
| **Ham packet** (AX.25/APRS, Direwolf) | 10–1000+ km | 1200 bit/s | free | **no** (FCC §97.113) | **Skipped, on purpose.** Every Concord message is MLS ciphertext, and these bands forbid encrypted content. Carrying signed *plaintext* would mean a second, unencrypted message model, which is the new plaintext path Concord is built not to have. The `Encryption: false` capability exists so such a link could only ever be listed, never used. |
| **GMRS / FRS** | 1–30 km | — | GMRS licence $35 | **no** (§95.1733) | **Skipped**, for the same reason. FRS/GMRS data devices are also locked to their own formats. |

### What can't be done, and why

- **Phone satellite SOS / messaging** (Apple Emergency SOS & Messages via satellite, Android Skylo/T-Mobile T-Satellite): no third-party data API. The OS owns the radio, and apps can't send arbitrary bytes.
- **Garmin inReach / Zoleo / SPOT**: closed devices with a closed network, and no API for sending app data from the device. The inReach IPC API is inbound only and enterprise-gated.
- **Bluetooth Classic / Wi-Fi Aware on Linux**, and **anything privileged on this box**: needs root (Wi-Fi Direct, btvirt virtual controllers, LE privacy config).
- **Voice and video over radio**: no link below Wi-Fi has the bandwidth.

## What it looks like for this machine

- **BLE won't start on this box by default:** BlueZ has LE privacy off, so it would advertise from the adapter's fixed public address. Concord refuses that. To fix it, set `Privacy = device` in `/etc/bluetooth/main.conf` and restart `bluetooth` (needs root).
- **Audio:** plug speakers or headphones into an analog jack, check the JV603's mute button, then follow the two-laptop procedure in [audio.md](transports/audio.md).

## Shopping list: test the simulated links for real

Prices are approximate, checked October 2026.

| Item | Why | Approx. price |
|---|---|---|
| 2× RAK WisBlock Meshtastic Starter Kit (RAK4631, 915 MHz) | Meshtastic over serial/BLE, low power | ~$25–40 each |
| 1× Heltec WiFi LoRa 32 V3 (or V4) | Meshtastic over TCP (has Wi-Fi), cheap third hop; can be flashed as an RNode for Reticulum | ~$18–30 |
| 1× LILYGO T-Beam (Supreme or v1.2) | GPS relay node / second RNode | ~$35–50 |
| Tuned 915 MHz antennas | the stock ones are poor | ~$10 each |
| 1× RockBLOCK 9603 (Ground Control) | Iridium SBD; one is enough for a loopback test via a delivery group, two for real | ~$290–300, plus **$17/month** line rental, plus credits ($10 for 50, down to ~$0.10 each in bulk) |
| 1× RTL-SDR Blog V4 + antenna | listen to the 915 MHz and BLE-adjacent air and confirm only ciphertext goes out | ~$30–40 (price not re-checked) |
| A second BLE device: any Linux laptop or USB BLE 5 dongle (~$12), or an Android phone once the mobile link lands | first device-to-device BLE run | ~$12 |

Each `docs/transports/*.md` has the step-by-step hardware test plan.
