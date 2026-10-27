# Bluetooth LE link

The package is `internal/transport/ble`. It is Concord's off-grid link for
people in the same room, campsite or building: it moves `transport` frames
(MLS ciphertext behind the 12-byte bundle header) between nearby devices
without the internet. The `transport.Manager` above it handles fragmentation,
dedupe, relay and the outbox. This package only moves opaque frames of at
most 180 bytes.

## Design

Every device plays both roles at once.

- **Peripheral.** It serves one GATT service, `1c317699-815c-4666-9f0b-3452f8763f76`,
  with two characteristics:
  - `…3f77` *rx*: flags `write` and `write-without-response`. A central writes
    one frame per write.
  - `…3f78` *tx*: flag `notify`. One notification is one frame.

  The service UUID is advertised through BlueZ's `LEAdvertisingManager1`, and
  the advertisement carries nothing else.
- **Central.** It runs LE discovery with the filter `UUIDs=[service]`,
  `Transport=le`, `DuplicateData=true`. When it finds a peer it connects,
  waits for `ServicesResolved`, checks the ATT MTU, subscribes to *tx* and
  writes to *rx*.
- **One connection per pair, usually.** Each device dials a newly heard peer
  after a random delay of 0–3 s, and never dials a device that is already
  connected to it in either direction. So when two devices hear each other,
  whichever timer fires first dials, and the other sees `Connected` and stands
  down. A single LE connection carries both directions: we write to peers we
  dialed and notify peers that dialed us. If both sides dial within the same
  instant, the pair gets a double path. That costs airtime, not correctness,
  because the Manager dedupes.
- **Broadcast `Send`.** A frame goes on every connection in parallel. Each peer
  has its own 2 s timeout, so a stuck peer cannot stall the others. Nobody in
  range is not an error. `Send` fails only when there were peers and every one
  of them refused the frame. A peer whose write fails is dropped.
- **Backoff and eviction.** These are the defences against failing or hostile
  peers.
  - A failed dial, a failed write, or a connection that drops before it has
    both carried a frame and lasted 30 s all count as a failure.
  - Failures back off at 10 s, doubling up to 5 min. A disconnect never
    shortens an existing backoff, and only a healthy connection resets it.
  - Outbound connections are capped at 7. When all 7 are taken and another
    peer is waiting, the quietest connection that has been idle for 2 min is
    evicted and backed off. So seven devices that accept a connection and then
    say nothing cannot hold every slot.
  - The tables (256 devices in `Link`, 256 in BlueZ) evict the stalest
    unconnected entry instead of refusing new ones.
- **Connection age.** No connection lives longer than 15 min (±10%), in
  either direction. An open connection pins both sides' addresses, so retiring
  it on the cadence of the kernel's address rotation lets the next connection
  use the new addresses.
  - Outbound connections are retired by `Link`.
  - Inbound connections are swept by the BlueZ layer every 10 s. They are
    capped at 7, and the newest ones are dropped first.
  - Inbound Concord centrals are recognised in two ways: they advertise the
    service, or they have written to us. Other connected devices, such as the
    user's headphones, are never touched.
- **Framing.** One GATT write or notification is exactly one frame, with no
  extra link header. Bundle frames identify themselves (`0xC1` or `0xC2`) and
  the Manager validates them. Long writes (offset ≠ 0) are refused. Peers with
  a negotiated ATT MTU below 183 are refused too, so a frame is never truncated
  or split by ATT.
- **Layers.** `Link` (ble.go) owns the peer table, dial policy, backoff,
  eviction and connection age. It works against a `Radio` interface
  (radio.go). `BlueZ` (bluez_linux.go) is the D-Bus implementation, using
  `godbus/dbus/v5`. Tests run `Link` on a fake radio (fake_test.go); one of
  them runs three real `transport.Manager`s relaying across a fake A–B–C mesh.
- **Lifecycle.**
  - `Probe` reads D-Bus `ObjectManager`, sysfs rfkill, and the kernel mgmt
    "Read Controller Info" reply. It transmits nothing and changes nothing.
  - `Start` is the only call that powers the adapter, advertises or scans. It
    also sets the alias to a generic name and `Pairable` to false.
  - The BlueZ layer watches for three things while running:
    - Adapter `Powered`. When the adapter is switched off (by the user,
      suspend or rfkill), `Probe` reports *error: switched off*. We never
      power it back on ourselves. When it comes back, the advertisement and
      discovery are re-registered.
    - bluetoothd restarting (`NameOwnerChanged`). The GATT service is
      re-registered as well.
    - Recovery failing. After five tries, `Probe` reports *error* with the
      reason, so the settings page does not show a running link that does
      nothing.
  - `Stop` does the following, in order:
    1. Stops discovery and unregisters the advertisement and the GATT
       application.
    2. Disconnects every Concord peer in both directions: devices we dialed,
       centrals that wrote to us, and devices heard advertising the service.
    3. Waits for those disconnects (up to 3 s), then removes the unpaired
       ones from BlueZ's cache.
    4. Only then restores `Pairable`, the alias, and power. Power goes back
       off only if `Start` turned it on and nothing else has connected since.
    5. Closes its private D-Bus connection. If the process dies, BlueZ
       itself drops everything tied to that connection.
  - A paired, bonded or trusted device is never disconnected or removed.
- **Mesh mobility.** Devices in BlueZ's cache from before this run are not
  treated as present. A device counts only once it is heard on air (an RSSI
  reading or advertised data) during the run.

### Caps

| field | value | why |
|---|---|---|
| MTU | 180 B | ATT_MTU 185 (iOS's ceiling, and the floor of what Android and BlueZ negotiate), minus 3 for the ATT header, minus 2 slack |
| Bitrate | 20 kbit/s | Accounts for connection intervals, several peers sharing one radio, and a D-Bus round trip per frame. It is far below the 1–2 Mbit/s PHY rate |
| Latency | 150 ms | one write or notification on a 30–50 ms connection interval, plus D-Bus |
| Encryption | true | ciphertext is lawful on 2.4 GHz ISM |
| Local / Mode | true / Live | |
| Range | ~10–50 m | indoors, phone to laptop |

A 255-fragment bundle (about 44 KB) takes roughly 18 s, inside the Manager's
60 s live-airtime limit.

## Privacy: what an observer can see

The advertisement is fixed in code. It was checked byte for byte on air (see
below), and it is 21 bytes:

```
02 01 06                      flags: LE General Discoverable, BR/EDR not supported
11 07 <service UUID, 16 B>    complete list of 128-bit service UUIDs
```

There is no LocalName, appearance, TX power, manufacturer data or service
data. There is no key, and nothing derived from one. Every Concord device's
advertisement is identical.

An earlier draft put a random nonce in the service data, to decide who dials.
It was removed. The nonce and the address rotated on separate timers, so an
observer who listened without a break could chain the old (address, nonce)
pair to the new one, and use it to bridge identities across address
rotations. With an identical advertisement, the only per-device thing on air
is the address the kernel rotates. Dial collisions are now avoided with a
random delay instead.

An observer in radio range learns the following.

1. **That a device runs Concord's BLE link.** The service UUID has to be fixed
   for strangers to find each other, so this is unavoidable.
2. **The advertising address.**
   - With LE privacy on (required by default), this is a resolvable private
     address that the kernel rotates (every 15 min by default). We never pair
     or bond:
     - No characteristic asks for encryption.
     - `Pairable` is off while the link runs, so a stranger's bonding request
       is refused.
     - So the IRK and the identity address never leave the machine, and
       nobody can resolve the address.
   - With LE privacy off, it is the adapter's **fixed public address**, which
     is a permanent tracking handle.
     - `Start` **refuses** an adapter without privacy. `Probe` reports why and
       how to fix it: *set `Privacy = device` under `[General]` in
       `/etc/bluetooth/main.conf` and restart bluetooth*.
     - Only `BlueZConfig.AllowPublicAddress` overrides this. Concord never
       sets it; it exists for `concord-blecheck -unsafe-public-address` on
       test machines.
     - BlueZ does not expose privacy on D-Bus, so it is read from the kernel's
       management socket. The call is Read Controller Information, which the
       kernel allows on an unprivileged socket, and nothing goes over the air.
3. **Timing, RSSI and traffic volume** of connections, but not their
   contents. Every frame is MLS ciphertext with the 12-byte bundle header: a
   day number, a hop count, and a per-channel tag that rotates daily (see
   `transport/bundle.go`). LE link-layer encryption is *not* used, because it
   needs pairing.
4. **Continuity.** An observer who stays in range can still follow a device
   across address rotations by RSSI and timing. Rotation protects against an
   observer who comes and goes, not one who stays. Capping connections at
   15 min stops a connection from carrying an address past its rotation.
5. **Unverified: extended-advertising address mismatch.** The kernel may give
   an extended advertising set its own RPA, separate from the RPA used to
   initiate connections. If so, a peer sees one device under two addresses,
   and the "already connected" check misses, which can cause a double
   connection (wasted airtime only). Nothing new leaks. Whether this happens
   with `Privacy = device` on this controller was not verified, because
   privacy could not be enabled here.

Anyone who **connects** to us can read the following. There is no access
control at the GATT layer, by design, because strangers relay for each other.

- **The GAP Device Name.** On Linux this is the adapter alias, usually the
  hostname.
  - `Start` sets the alias to the generic `Bluetooth`.
  - `Stop` restores the old alias only after every Concord peer has been
    disconnected.
  - If a run crashed and left the alias as `Bluetooth` while the system name
    differs, the next `Start` treats the system name as the alias to restore.
- **BlueZ's Device Information service.** This is the PnP ID ("BlueZ 5.x"). It
  identifies the software, not the person.
- **Our tx notifications.** They are ciphertext, the same as what goes over
  the air.

**Scanning.** Active scans send SCAN_REQ from a random non-resolvable address.
This was checked on air: own-address type `0x01` in `LE Set Extended Scan
Parameters`, even with privacy off.

**Local traces.** BlueZ writes every device it connects to under
`/var/lib/bluetooth`, which would build a growing local log of encounters. To
prevent that, every unpaired device we dialed, every central that wrote to us,
and every device heard advertising the service is removed with
`Adapter1.RemoveDevice`. This happens when the connection drops, when a dial
fails, and on `Stop`. A crash skips that cleanup. BlueZ also expires temporary
entries by itself.

## Tested on air vs. simulated

**On air, on this box.** The adapter is hci0, a Realtek USB controller running
BlueZ 5.72 with privacy off, so every run used `-unsafe-public-address`. That
put the public address on air for under a minute each time.

- **Privacy refusal.** Without the override, `Probe` reports the refusal, and
  the gated test confirms that `Start` fails with `ErrNoPrivacy`.
- **Power-on path.** With the adapter powered off, `Probe` left it off. `Start`
  powered it on, and `Stop` powered it back off. I then turned it back on.
- **Running state.**
  - The GATT application registered: the service UUID appeared in
    `Adapter1.UUIDs`.
  - The advertisement registered: `ActiveInstances` went from 0 to 1.
  - Discovery was on, the alias was `Bluetooth`, and `Pairable` was false.
- **HCI capture.** I captured with `dumpcap -i bluetooth-monitor` (the
  wireshark group can do this without root) and decoded it:
  - `LE Set Extended Advertising Parameters`: a legacy connectable/scannable
    PDU, interval 1.28 s, own-address type public (expected, since privacy is
    off).
  - `LE Set Extended Advertising Data`: exactly the 21 bytes above.
  - `Stop` sent `Ext Adv Enable 0`, `Remove Adv Set` and `Ext Scan Enable 0`.
- **After `Stop`.** `ActiveInstances` was 0, the service UUID was gone from
  the adapter, `Discovering` was false, and the alias, `Pairable` and power
  state were all restored.
- **Power toggle while running.** I ran `bluetoothctl power off`, then `on`.
  While the adapter was off, `Probe` said *switched off*. Within about 2 s of
  power returning, the advertisement and discovery were back, and nothing had
  to be restarted. Before this was fixed, the link stayed silently dead after
  the toggle.
- **Crash-left alias.** I set the alias to `Bluetooth` by hand to simulate a
  crashed run, then ran the link. On `Stop` the alias went back to the system
  name.
- **Central role.** The BlueZ-side UUID filter reported no devices at all.
  Nearby non-Concord hardware (2–4 devices, heard before the filter was added)
  is no longer even reported. I connected to nothing and paired with nothing.

**Not tested on air:**

- connecting to another Concord device
- the GATT write/notify data path
- the MTU check
- dial collisions between two real radios
- inbound-central sweeping and `RemoveDevice` cleanup on a real peer
- recovery after a bluetoothd restart, which would need root to restart the
  daemon

There is no second adapter here, and `/dev/vhci` (btvirt) needs root. These
paths are covered only by the fake-radio tests (for `Link`) and by unit tests
of the BlueZ signal parsing, table limits and `WriteValue` framing
(bluez_linux_test.go). The D-Bus calls for Connect, WriteValue and StartNotify
follow BlueZ's documented API, but no real peer has exercised them. Expect the
first two-machine run to find something.

## Two-machine hardware test

Use two Linux machines with BlueZ 5.62 or later, within a few metres of each
other:

```sh
# once, as root, on each (or accept the public-address warning below):
#   /etc/bluetooth/main.conf  [General]  Privacy = device
#   systemctl restart bluetooth
go build -o concord-blecheck ./cmd/concord-blecheck
./concord-blecheck -probe              # read-only; should say "available"
./concord-blecheck -for 2m             # add -unsafe-public-address if privacy is off
```

Each side prints a status line every 5 s with: devices reported, Concord
devices, connections dialed, whether a central is subscribed to us, whether we
are advertising, and the `Probe` detail. Each side sends a 180-byte numbered
test frame every 2 s and prints every frame it receives, with `intact=true`
when the contents survived.

Within about 10 s you should see:

- `Concord devices 1` on both sides
- `dialed 1` on one side and `subscribed-to-us true` on the other
- `recv … intact=true` lines on both

After 15 min the connection should be retired and re-established.

The same check as a test (run it on one machine while the other runs
`concord-blecheck`):

```sh
CONCORD_BLE_HW=1 CONCORD_BLE_PEER=1 go test -run Hardware -v ./internal/transport/ble
```

Afterwards, `bluetoothctl devices` should not list the other machine unless the
two are paired.

## Mobile

The phone shell is Capacitor plus a gomobile-bound Go core (`mobile/`,
`apps/mobile/{android,ios}`). The BlueZ radio does not build there; it is
built only for `linux && !android`. The `Radio` interface is the seam:

- A native class implements `Probe/Up/Advertise/Scan/Connect/Notify/Down`.
- It passes events back through a gomobile-exported callback object.
- `ble.New(nativeRadio)` registers it.

None of `Link`'s logic is duplicated.

- **Android: feasible.**
  - `BluetoothLeAdvertiser` takes the same UUID-only advertisement,
    `BluetoothGattServer` serves rx/tx, and `BluetoothLeScanner` with a UUID
    `ScanFilter` finds peers. `requestMtu(517)` clears the 183 floor.
  - Android has LE privacy on by default and advertises and connects from
    rotating random addresses, so the privacy requirement holds without root.
    Verify this on the target devices.
  - It needs the `BLUETOOTH_ADVERTISE`, `BLUETOOTH_CONNECT` and
    `BLUETOOTH_SCAN` runtime permissions (`neverForLocation`), plus a
    foreground service to keep running with the screen off. One already
    exists: `ConcordForegroundService`.
  - Estimated size: roughly 400 lines of Java plus a gomobile bridge.
- **iOS: workable in the foreground, weak in the background.**
  - In the foreground, CoreBluetooth provides `CBPeripheralManager` (advertise
    and serve) and `CBCentralManager`. It can advertise the service UUID with
    no name, which is exactly our advertisement, now that the nonce is gone.
  - In the background, the advertisement moves to Apple's overflow area, and
    only other iOS devices scanning for that UUID can see it. Scans are
    throttled and get no duplicates.
  - With the `bluetooth-central` and `bluetooth-peripheral` background modes,
    a phone in a pocket still relays, but it discovers peers slowly (minutes).
  - iOS randomises its address itself. Its maximum MTU is 185, which is why
    MaxFrame is 180.

Neither platform is implemented: neither can be tested without real devices,
and emulators have no BLE. Android is the cheaper one to do first.

## Known limits

- **LE privacy must be on, and most distributions ship with it off.** Turning
  it on needs root (`main.conf`). On a stock Linux box the link therefore shows
  *error*, with the fix, until someone does that. This is deliberate.
- **`Pairable` is off while the link runs.** Pairing new headphones fails
  until the link is switched off. This is deliberate: a bond would hand over
  the IRK.
- **There is no access control at the GATT layer.** Any nearby device can:
  - connect and write junk (the Manager drops malformed frames);
  - spoof adverts (the tables evict, and backoff and idle-eviction keep the
    slots turning over).
  That is a local denial of service, not a confidentiality problem.
- **Notifications are slow.** They go out as `PropertiesChanged` on *tx*, so
  BlueZ sends one per subscriber, and each costs a D-Bus round trip.
  `AcquireNotify` and `AcquireWrite` file descriptors would be faster.
- **No per-frame acknowledgement.** Writes are write-without-response. The LE
  link layer still acknowledges and retransmits, so a frame is lost only if
  the connection drops. The Manager's dedupe and relay provide the redundancy.
- **Discovery is slow and costs battery.** Discovery runs continuously while
  the link is on, and advertising uses BlueZ's default 1.28 s interval. It
  takes a few seconds to find a peer, and the battery cost on laptops is
  noticeable.
- **Some cleanup is skipped on a crash.** The BlueZ cache entries and the
  `Pairable` setting stay as the crashed run left them. The alias is repaired
  on the next `Start`. `Pairable` is not, because whether the user wanted it
  on is unknown.
