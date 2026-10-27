//go:build linux && !android

package ble

import (
	"bytes"
	"sync"
	"testing"

	"github.com/godbus/dbus/v5"
)

// recorder captures Events for the BlueZ signal-handling tests.
type recorder struct {
	mu      sync.Mutex
	seen    []string
	ads     [][]byte
	conns   []bool
	lost    []string
	inbound [][]byte
	subs    []bool
}

func (r *recorder) events() Events {
	return Events{
		Inbound: func(f []byte) { r.mu.Lock(); r.inbound = append(r.inbound, f); r.mu.Unlock() },
		Seen: func(id string, ad []byte, c bool) {
			r.mu.Lock()
			r.seen, r.ads, r.conns = append(r.seen, id), append(r.ads, ad), append(r.conns, c)
			r.mu.Unlock()
		},
		Lost:       func(id string) { r.mu.Lock(); r.lost = append(r.lost, id); r.mu.Unlock() },
		Notified:   func(string, []byte) {},
		Subscribed: func(on bool) { r.mu.Lock(); r.subs = append(r.subs, on); r.mu.Unlock() },
	}
}

// fakeUp puts a BlueZ into the state Up leaves it in, minus the bus.
func fakeUp(t *testing.T) (*BlueZ, *recorder) {
	b := NewBlueZ(BlueZConfig{})
	r := &recorder{}
	b.conn = &dbus.Conn{} // non-nil marks "up"; never used for I/O here
	b.adapter = "/org/bluez/hci0"
	b.ev = r.events()
	b.devices = map[dbus.ObjectPath]*devState{}
	b.txPaths = map[dbus.ObjectPath]dbus.ObjectPath{}
	b.seenAll = map[dbus.ObjectPath]struct{}{}
	return b, r
}

func sdVariant(uuid string, data []byte) dbus.Variant {
	return dbus.MakeVariant(map[string]dbus.Variant{uuid: dbus.MakeVariant(data)})
}

func TestBlueZMatchesOnlyConcordDevices(t *testing.T) {
	b, r := fakeUp(t)
	const us, them, other = "/org/bluez/hci0/dev_AA", "/org/bluez/hci0/dev_BB", "/org/bluez/hci0/dev_CC"
	ad := encodeAd(nonce{1, 2, 3, 4})
	// Upper-case UUID as some stacks report it.
	b.device(us, map[string]dbus.Variant{
		"ServiceData": sdVariant("1C317699-815C-4666-9F0B-3452F8763F76", ad),
		"Connected":   dbus.MakeVariant(false),
	})
	b.device(them, map[string]dbus.Variant{"UUIDs": dbus.MakeVariant([]string{ServiceUUID})}) // iOS-style
	b.device(other, map[string]dbus.Variant{
		"ServiceData": sdVariant("0000feaa-0000-1000-8000-00805f9b34fb", []byte{1}),
		"Name":        dbus.MakeVariant("someone's headphones"),
	})
	// An RSSI update on a known device refreshes it with the cached data.
	b.device(us, map[string]dbus.Variant{"RSSI": dbus.MakeVariant(int16(-60))})
	// An RSSI update on a stranger is ignored.
	b.device(other, map[string]dbus.Variant{"RSSI": dbus.MakeVariant(int16(-70))})

	if got := len(r.seen); got != 3 {
		t.Fatalf("seen %d events (%v), want 3", got, r.seen)
	}
	if !bytes.Equal(r.ads[0], ad) || !bytes.Equal(r.ads[2], ad) || r.ads[1] != nil {
		t.Fatalf("ads %x", r.ads)
	}
	if all, concord := b.Counts(); all != 3 || concord != 2 {
		t.Fatalf("counts %d/%d", all, concord)
	}

	b.device(us, map[string]dbus.Variant{"Connected": dbus.MakeVariant(true)})
	if !r.conns[len(r.conns)-1] {
		t.Fatal("connected flag not passed")
	}
	b.device(us, map[string]dbus.Variant{"Connected": dbus.MakeVariant(false)})
	b.forget(them)
	b.forget(other) // not ours: no Lost
	if len(r.lost) != 2 || r.lost[0] != us || r.lost[1] != them {
		t.Fatalf("lost %v", r.lost)
	}
}

func TestBlueZIgnoresDevicesOnOtherAdapters(t *testing.T) {
	b, r := fakeUp(t)
	if b.under("/org/bluez/hci1/dev_AA") || !b.under("/org/bluez/hci0/dev_AA") || b.under("/org/bluez/hci0") {
		t.Fatal("under() wrong")
	}
	_ = r
}

func TestRxCharFraming(t *testing.T) {
	b, r := fakeUp(t)
	rx := rxChar{b}
	if err := rx.WriteValue([]byte{0xc1, 1, 2}, map[string]dbus.Variant{"type": dbus.MakeVariant("command")}); err != nil {
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
	tx := txChar{b}
	_ = tx.StartNotify()
	_ = tx.StopNotify()
	if len(r.subs) != 2 || !r.subs[0] || r.subs[1] {
		t.Fatalf("subs %v", r.subs)
	}
}

// What BlueZ reads to build the advertisement: service data and the
// discoverable flag, nothing that names or identifies the device.
func TestAdvertisementObjectHasNoIdentity(t *testing.T) {
	b, _ := fakeUp(t)
	b.advData = encodeAd(nonce{9, 8, 7, 6})
	adv := b.objects()[b.advPath()][ifAdvert]
	for k := range adv {
		switch k {
		case "Type", "ServiceData", "Discoverable":
		default:
			t.Errorf("advertisement carries %q", k)
		}
	}
	sd := adv["ServiceData"].Value().(map[string]dbus.Variant)
	if len(sd) != 1 || !bytes.Equal(sd[ServiceUUID].Value().([]byte), b.advData) {
		t.Fatalf("service data %v", sd)
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
