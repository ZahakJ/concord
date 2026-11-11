//go:build linux && !android

package ble

import (
	"bytes"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// recorder captures Events for the BlueZ signal-handling tests.
type recorder struct {
	mu      sync.Mutex
	seen    []string
	conns   []bool
	lost    []string
	inbound [][]byte
	subs    []bool
}

func (r *recorder) events() Events {
	return Events{
		Inbound: func(f []byte) { r.mu.Lock(); r.inbound = append(r.inbound, f); r.mu.Unlock() },
		Seen: func(id string, c bool) {
			r.mu.Lock()
			r.seen, r.conns = append(r.seen, id), append(r.conns, c)
			r.mu.Unlock()
		},
		Lost:       func(id string) { r.mu.Lock(); r.lost = append(r.lost, id); r.mu.Unlock() },
		Notified:   func(string, []byte) {},
		Subscribed: func(on bool) { r.mu.Lock(); r.subs = append(r.subs, on); r.mu.Unlock() },
	}
}

// fakeUp puts a BlueZ into the state Up leaves it in, minus the bus. Only
// paths that need no D-Bus call may be exercised.
func fakeUp(t *testing.T) (*BlueZ, *recorder) {
	b := NewBlueZ(BlueZConfig{})
	r := &recorder{}
	b.conn = &dbus.Conn{} // non-nil marks "up"; never used for I/O here
	b.adapter = "/org/bluez/hci0"
	b.ev = r.events()
	b.devices = map[dbus.ObjectPath]*devState{}
	b.known = map[dbus.ObjectPath]bool{}
	b.dialed = map[dbus.ObjectPath]bool{}
	b.writers = map[dbus.ObjectPath]time.Time{}
	b.inboundSince = map[dbus.ObjectPath]time.Time{}
	b.txPaths = map[dbus.ObjectPath]dbus.ObjectPath{}
	return b, r
}

func uuids(u ...string) dbus.Variant { return dbus.MakeVariant(u) }
func rssi(v int16) dbus.Variant      { return dbus.MakeVariant(v) }

func TestBlueZMatchesOnlyConcordDevicesHeardOnAir(t *testing.T) {
	b, r := fakeUp(t)
	const us, cached, other = "/org/bluez/hci0/dev_AA", "/org/bluez/hci0/dev_BB", "/org/bluez/hci0/dev_CC"
	// Heard on air, with the UUID in upper case as some stacks report it.
	b.device(us, map[string]dbus.Variant{
		"UUIDs": uuids("1C317699-815C-4666-9F0B-3452F8763F76"), "RSSI": rssi(-50),
		"Connected": dbus.MakeVariant(false),
	})
	// A Concord device straight out of BlueZ's cache: no RSSI, not heard.
	b.device(cached, map[string]dbus.Variant{"UUIDs": uuids(ServiceUUID), "Connected": dbus.MakeVariant(false)})
	b.device(other, map[string]dbus.Variant{
		"UUIDs": uuids("0000180f-0000-1000-8000-00805f9b34fb"), "RSSI": rssi(-40),
		"Name": dbus.MakeVariant("someone's headphones"),
	})
	b.device(us, map[string]dbus.Variant{"RSSI": rssi(-60)})     // refresh of a known device
	b.device(other, map[string]dbus.Variant{"RSSI": rssi(-70)})  // stranger: ignored, no lookup
	b.device(cached, map[string]dbus.Variant{"RSSI": rssi(-65)}) // cached one finally heard

	if want := []string{us, us, cached}; len(r.seen) != 3 || r.seen[0] != want[0] || r.seen[1] != want[1] || r.seen[2] != want[2] {
		t.Fatalf("seen %v, want %v", r.seen, want)
	}
	if all, concord := b.Counts(); all != 3 || concord != 2 {
		t.Fatalf("counts %d/%d", all, concord)
	}

	// A Connected flag on its own does not count as heard, but is passed on
	// so that Link does not dial back over an existing link.
	b.device(us, map[string]dbus.Variant{"Connected": dbus.MakeVariant(true)})
	if !r.conns[len(r.conns)-1] {
		t.Fatal("connected flag not passed")
	}
	b.device(us, map[string]dbus.Variant{"Connected": dbus.MakeVariant(false)})
	b.forget(cached)
	b.forget(other) // not ours, so no Lost
	if len(r.lost) != 2 || r.lost[0] != us || r.lost[1] != cached {
		t.Fatalf("lost %v", r.lost)
	}
}

func TestBlueZDeviceTableEvictsStalest(t *testing.T) {
	b, _ := fakeUp(t)
	for i := 0; i < maxDevices+5; i++ {
		p := dbus.ObjectPath("/org/bluez/hci0/dev_" + string(rune('A'+i%26)) + "_" + itoa(i))
		b.device(p, map[string]dbus.Variant{"UUIDs": uuids(ServiceUUID), "RSSI": rssi(-50)})
	}
	if _, concord := b.Counts(); concord != maxDevices {
		t.Fatalf("tracked %d, cap %d", concord, maxDevices)
	}
}

func itoa(i int) string {
	var buf [8]byte
	n := len(buf)
	for {
		n--
		buf[n] = byte('0' + i%10)
		i /= 10
		if i == 0 {
			return string(buf[n:])
		}
	}
}

func TestBlueZIgnoresDevicesOnOtherAdapters(t *testing.T) {
	b, _ := fakeUp(t)
	if b.under("/org/bluez/hci1/dev_AA") || !b.under("/org/bluez/hci0/dev_AA") || b.under("/org/bluez/hci0") {
		t.Fatal("under() wrong")
	}
}

func TestRxCharFramingAndWriterTracking(t *testing.T) {
	b, r := fakeUp(t)
	rx := rxChar{b}
	dev := dbus.MakeVariant(dbus.ObjectPath("/org/bluez/hci0/dev_DD"))
	if err := rx.WriteValue([]byte{0xc1, 1, 2}, map[string]dbus.Variant{"type": dbus.MakeVariant("command"), "device": dev}); err != nil {
		t.Fatal(err)
	}
	if err := rx.WriteValue([]byte{0xc1}, map[string]dbus.Variant{"offset": dbus.MakeVariant(uint16(20))}); err == nil {
		t.Fatal("accepted a long-write fragment")
	}
	if err := rx.WriteValue(make([]byte, MaxFrame+1), nil); err == nil {
		t.Fatal("accepted an oversized frame")
	}
	if err := rx.WriteValue(nil, nil); err == nil {
		t.Fatal("accepted an empty frame")
	}
	if len(r.inbound) != 1 || !bytes.Equal(r.inbound[0], []byte{0xc1, 1, 2}) {
		t.Fatalf("inbound %x", r.inbound)
	}
	if _, ok := b.writers["/org/bluez/hci0/dev_DD"]; !ok {
		t.Fatal("writer not remembered for Down to disconnect")
	}
	tx := txChar{b}
	_ = tx.StartNotify()
	_ = tx.StopNotify()
	if len(r.subs) != 2 || !r.subs[0] || r.subs[1] {
		t.Fatalf("subs %v", r.subs)
	}
}

// What BlueZ reads to build the advertisement: the service UUID and the
// discoverable flag. Nothing that names or distinguishes the device.
func TestAdvertisementObjectHasNoIdentity(t *testing.T) {
	b, _ := fakeUp(t)
	adv := b.objects()[b.advPath()][ifAdvert]
	for k := range adv {
		switch k {
		case "Type", "ServiceUUIDs", "Discoverable":
		default:
			t.Errorf("advertisement carries %q", k)
		}
	}
	if u := adv["ServiceUUIDs"].Value().([]string); len(u) != 1 || u[0] != ServiceUUID {
		t.Fatalf("UUIDs %v", u)
	}
	app, _ := gattApp{b}.GetManagedObjects()
	if _, ok := app[b.advPath()]; ok {
		t.Fatal("advertisement leaked into the GATT tree")
	}
	for _, p := range []dbus.ObjectPath{b.rxPath(), b.txPath()} {
		for _, f := range app[p][ifChar]["Flags"].Value().([]string) {
			if f != "write" && f != "write-without-response" && f != "notify" {
				t.Errorf("%s has flag %q (would trigger pairing or reads)", p, f)
			}
		}
	}
}
