# Bluetooth LE link

`internal/transport/ble` — Concord's off-grid link for people in the same room,
campsite or building. It moves `transport` frames (MLS ciphertext behind the
12-byte bundle header) between nearby devices with no internet. The
`transport.Manager` above it does fragmentation, dedupe, relay and the outbox;
this package only moves opaque frames of at most 180 bytes.

## Design

Every device plays both roles at once.

- **Peripheral.** One GATT service, `1c317699-815c-4666-9f0b-3452f8763f76`, with
  two characteristics:
  - `…3f77` *rx* — flags `write`, `write-without-response`. A central writes
    one frame per write.
  - `…3f78` *tx* — flag `notify`. One notification carries one frame.
  The service is advertised through BlueZ's `LEAdvertisingManager1`.
- **Central.** LE discovery (`Transport=le`, `DuplicateData=true`). A device
  counts as Concord when its service data is keyed by the service UUID, or (for
  iOS, which cannot send service data) when its UUID list holds that UUID. We
  connect, wait for `ServicesResolved`, check the ATT MTU, subscribe to *tx* and
  write to *rx*.
- **One connection per pair.** The advertisement carries a random 4-byte
  nonce. Whichever side has the lower nonce dials. If the higher side hears
  nothing from the lower one for 20 s (say an iOS phone in the background, or a
  controller with no free slots), it dials anyway. We never dial a device that
  is already connected to us. A single LE connection carries both directions:
  we write to peers we dialed and notify peers that dialed us.
- **Broadcast `Send`.** A frame goes out on every connection. If nobody is in
  range that is not an error; as on any radio, the frame just goes into empty
  air. `Send` fails only when there were peers and every one refused the frame.
  A peer whose write fails is dropped and redialled with backoff (10 s, then
  doubling, capped at 5 min).
- **Framing.** One GATT write or notification is exactly one frame, with no
  extra link header. Bundle frames identify themselves (`0xC1`/`0xC2`) and the
  Manager validates them. Long writes (offset ≠ 0) are refused. Peers whose
  negotiated ATT MTU is below 183 are refused too, so a frame is never
  truncated or split by ATT.
- **Layers.** `Link` (ble.go) holds the peer table, tie-break, rotation, dial
  cap (7 outbound) and backoff. It works against a `Radio` interface
  (radio.go). `BlueZ` (bluez_linux.go) is the D-Bus implementation, using
  `godbus/dbus/v5`, which was already a dependency. Tests run `Link` on a fake
  radio (fake_test.go), including three real `transport.Manager`s relaying
  across a fake A–B–C mesh.
- **Lifecycle.** `Probe` reads D-Bus `ObjectManager`, sysfs rfkill and the
  kernel mgmt "Read Controller Info" reply. It transmits nothing and changes
  nothing. `Start` is the only call that powers the adapter, advertises or
  scans. `Stop` stops discovery, unregisters the advertisement and the GATT
  application, closes every connection, restores the adapter alias, powers the
  adapter back off if `Start` turned it on and nothing else has connected
  since, and closes its private D-Bus connection. If the process dies, BlueZ
  drops everything tied to that connection by itself.

### Caps

| field | value | why |
|---|---|---|
| MTU | 180 B | ATT_MTU 185 (iOS's ceiling, and the floor of what Android and BlueZ negotiate) − 3 ATT header − 2 slack |
| Bitrate | 20 kbit/s | Connection intervals, several peers sharing one radio, and D-Bus overhead per frame. Not the 1–2 Mbit/s PHY rate |
| Latency | 150 ms | one write or notification on a 30–50 ms connection interval, plus D-Bus |
| Encryption | true | ciphertext is lawful on 2.4 GHz ISM |
| Local / Mode | true / Live | |
| Range | ~10–50 m | indoors, phone to laptop |

A 255-fragment bundle (~44 KB) takes about 18 s, inside the Manager's 60 s
live-airtime limit.

## Privacy: what an observer can see

The advertisement is fixed in code, and it was checked byte for byte on air
(see below):

```
02 01 06                      flags: LE General Discoverable, BR/EDR not supported
16 21 <service UUID, 16 B>    service data, 128-bit UUID
      01 <nonce, 4 B>         version, random nonce
```

That is 26 bytes. It carries no LocalName, no appearance, no TX power, no
manufacturer data, no UUID list, and no key or anything derived from one. The
nonce comes from `crypto/rand` and is replaced every ~15 min (±10% jitter, so
devices that started together do not rotate in step). Re-registering the
advertisement is what rotates it.

An observer in radio range learns:

1. **That a device runs Concord's BLE link.** The service UUID has to be
   fixed for strangers to find each other. This cannot be avoided.
2. **The advertising address.**
   - With LE privacy on (required by default), this is a resolvable private
     address that the kernel rotates (default every 15 min). We never pair or
     bond: no characteristic asks for encryption, so the IRK and identity
     address are never handed to anyone, and nobody can resolve the address.
   - With LE privacy off, it is the adapter's **fixed public address**, a
     permanent tracking handle. So `Start` **refuses** an adapter without
     privacy, and `Probe` reports why: *set `Privacy = device` under
     `[General]` in `/etc/bluetooth/main.conf` and restart bluetooth*. Only
     `BlueZConfig.AllowPublicAddress` overrides this. Concord never sets it;
     it exists for `concord-blecheck -unsafe-public-address` on test machines.
     BlueZ does not show privacy on D-Bus, so it is read from the kernel's
     management socket (Read Controller Information, which the kernel allows
     an unprivileged socket; nothing is sent over the air).
3. **Timing, RSSI and traffic volume** of the connections, but not their
   contents. Every frame is MLS ciphertext with the 12-byte bundle header (day
   number, hop count, and a per-channel tag that rotates daily; see
   `transport/bundle.go`). LE link-layer encryption is *not* used, because it
   needs pairing.
4. **Linkability across a rotation.** The nonce and the RPA rotate on separate
   timers, so an observer who listens without a break can chain an old
   (address, nonce) to the new one across one boundary. Continuous presence
   also allows RSSI and timing correlation. Rotation protects against an
   observer who comes and goes. It does not protect against one who stays.

Anyone who **connects** to us (BLE GATT has no access control here, by
design, because strangers relay for each other) can also read:

- **The GAP Device Name.** On Linux this is the adapter alias, usually the
  hostname. `Start` sets the alias to the generic `Bluetooth` while the link
  runs and `Stop` puts the old one back. If the process crashes before `Stop`,
  the alias stays `Bluetooth`. That is harmless.
- **BlueZ's Device Information service** (PnP ID: "BlueZ 5.x"). It identifies
  the software, not the person.
- **Our tx notifications.** That is ciphertext, the same as in the clear.

What we hear from scanning: active scans send SCAN_REQ from a random
non-resolvable address. This was checked on air: own-address type `0x01` in
`LE Set Extended Scan Parameters`, even with privacy off.

## Tested on air vs. simulated

**On air, on this box** (hci0, Realtek USB controller, BlueZ 5.72, privacy off, so
every run used `-unsafe-public-address`, a public address on air for under a
minute each):

- `Probe` without the override: reports the privacy refusal. `Start` refuses
  with `ErrNoPrivacy`, which the gated test confirmed.
- With the adapter powered off: `Probe` left it off. `Start` powered it on, and
  `Stop` powered it back off. (I then turned it back on myself.)
- GATT application registered: the service UUID appeared in `Adapter1.UUIDs`.
  The advertisement registered: `ActiveInstances` went 0 → 1. Discovery was on
  and the alias was `Bluetooth` while running.
- HCI capture (`dumpcap -i bluetooth-monitor`; the wireshark group can do this
  without root), decoded:
  - `LE Set Extended Advertising Parameters`: legacy connectable/scannable
    PDU, interval 1.28 s, own-address type public (expected, since privacy is
    off).
  - `LE Set Extended Advertising Data`: exactly the 26 bytes above.
  - Each rotation: `Ext Adv Enable 0` → `Remove Adv Set` → new parameters →
    new data with a fresh nonce → enable.
  - `Stop` sent `Ext Adv Enable 0`, `Remove Adv Set`, `Ext Scan Enable 0`.
- After `Stop`: `ActiveInstances` 0, service UUID gone from the adapter,
  `Discovering` false, alias restored, power state restored.
- Central role: discovery heard 2–4 nearby LE devices (other people's
  hardware). The UUID/service-data match accepted none of them as Concord,
  which is correct. Nothing was connected to or paired with.

**Not tested on air:** connecting to another Concord device, the GATT
write/notify data path, the MTU check, and the tie-break between two real
radios. There is no second adapter here, and `/dev/vhci` (btvirt) needs root.
Those paths are covered only by the fake-radio tests (`Link`) and by unit tests
of the BlueZ signal parsing and `WriteValue` framing (bluez_linux_test.go). The
D-Bus *call* shapes for Connect/WriteValue/StartNotify follow BlueZ's
documented API, but no real peer has exercised them. Expect the first
two-machine run to find something.

## Two-machine hardware test

On two Linux machines with BlueZ ≥ 5.62, within a few metres:

```sh
# once, as root, on each (or accept the public-address warning below):
#   /etc/bluetooth/main.conf  [General]  Privacy = device
#   systemctl restart bluetooth
go build -o concord-blecheck ./cmd/concord-blecheck
./concord-blecheck -probe              # read-only; should say "available"
./concord-blecheck -for 2m             # add -unsafe-public-address if privacy is off
```

Each side prints a status line every 5 s: LE devices heard, Concord devices,
connections dialed, and whether a central is subscribed to us. It sends a
180-byte numbered test frame every 2 s and prints every frame it gets, with
`intact=true` when the contents survived. Expect within ~10 s: `Concord devices
1` on both sides, `dialed 1` on the lower-nonce side, `subscribed-to-us true`
on the other, then `recv … intact=true` lines on both. `-rotate 30s` exercises
rotation while connected (connections should stay up). The same check as a
test is:

```sh
CONCORD_BLE_HW=1 CONCORD_BLE_PEER=1 go test -run Hardware -v ./internal/transport/ble
```

Run it on one machine while the other runs `concord-blecheck`.

## Mobile

The phone shell is Capacitor plus a gomobile-bound Go core (`mobile/`,
`apps/mobile/{android,ios}`). The BlueZ radio does not build there (`linux &&
!android`). The `Radio` interface is the seam: a native class implements
`Probe/Up/Advertise/Scan/Connect/Notify/Down`, passes events back through a
gomobile-exported callback object, and `ble.New(nativeRadio)` registers it.
None of `Link`'s logic is duplicated.

- **Android.** Feasible. `BluetoothLeAdvertiser` takes the same 26-byte
  service-data advertisement, `BluetoothGattServer` serves rx/tx, and
  `BluetoothLeScanner` with a `ScanFilter` on service data finds peers.
  `requestMtu(517)` clears the 183 floor. Android has LE privacy on by
  default and advertises and connects from rotating random addresses, so the
  privacy requirement holds without root (verify on the target devices). It needs
  `BLUETOOTH_ADVERTISE/CONNECT/SCAN` (`neverForLocation`) runtime permissions,
  and a foreground service (one exists: `ConcordForegroundService`) to keep
  running with the screen off. Roughly 400 lines of Java plus a gomobile
  bridge.
- **iOS.** Workable in the foreground, weak in the background. In the
  foreground, CoreBluetooth gives `CBPeripheralManager` (advertise and serve)
  and `CBCentralManager`. It **cannot advertise service data**, only the UUID
  and a local name, and we set no name. So iOS peers advertise
  UUID-only, which `Link` already handles: a device with no nonce is always
  dialed. In the background, the advertisement moves to Apple's overflow area
  and only other iOS devices that are scanning for that UUID see it. Scans are
  throttled and get no duplicates. With `bluetooth-central` and
  `bluetooth-peripheral` background modes, a phone in a pocket still relays
  but discovers peers slowly (minutes). iOS randomises its address itself.
  Max MTU is 185, which is why MaxFrame is 180.

Not implemented: neither platform can be tested without devices, and emulators
have no BLE. The Android side is the cheaper one to do first.

## Known limits

- **LE privacy must be on**, and on most distributions it is off by default.
  Turning it on needs root (`main.conf`), so on a stock Linux box the link
  shows *error* with the fix until someone does that. This is deliberate.
- No access control at the GATT layer. Any nearby device can connect, write
  junk (the Manager drops malformed frames; tracked devices are capped at 256)
  or take up our 7 outbound dial slots with spoofed adverts. That is a local
  denial of service, not a confidentiality problem.
- Notifications go out as `PropertiesChanged` on *tx*, so BlueZ sends one per
  subscriber. Each costs a D-Bus round trip; `AcquireNotify`/`AcquireWrite`
  file descriptors would be faster.
- Writes are write-without-response. The LE link layer still acknowledges and
  retransmits, so a frame is lost only if the connection drops. There is no
  per-frame acknowledgement; the Manager's dedupe and relay are the
  redundancy.
- Discovery runs continuously while the link is on, and advertising is at
  BlueZ's default 1.28 s interval: a few seconds to find a peer, and a
  noticeable battery cost on laptops.
- On Android the binary would include none of this; see Mobile.
