//go:build linux && !android

package ble

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// TestHardware runs the link on a real adapter. It transmits, so it only runs
// when asked:
//
//	CONCORD_BLE_HW=1 go test -run Hardware -v ./internal/transport/ble
//
// CONCORD_BLE_ADAPTER picks the adapter (default: first).
// CONCORD_BLE_UNSAFE_PUBLIC=1 allows an adapter without LE privacy.
// CONCORD_BLE_PEER=1 also waits for a frame from a second machine running
// concord-blecheck (or this test) within range.
func TestHardware(t *testing.T) {
	if os.Getenv("CONCORD_BLE_HW") != "1" {
		t.Skip("set CONCORD_BLE_HW=1 to transmit on a real adapter")
	}
	cfg := BlueZConfig{Adapter: os.Getenv("CONCORD_BLE_ADAPTER"), AllowPublicAddress: os.Getenv("CONCORD_BLE_UNSAFE_PUBLIC") == "1"}
	radio := NewBlueZ(cfg)
	link := New(radio)
	t.Logf("probe: %+v", link.Probe())

	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	adapter, before, err := findAdapter(conn, cfg.Adapter)
	if err != nil {
		t.Fatal(err)
	}
	ad := conn.Object(bluezBus, adapter)
	instances := func() byte {
		v, err := ad.GetProperty(ifAdvMgr + ".ActiveInstances")
		if err != nil {
			t.Fatal(err)
		}
		return v.Value().(byte)
	}
	prop := func(name string) any {
		v, err := ad.GetProperty(ifAdapter + "." + name)
		if err != nil {
			t.Fatal(err)
		}
		return v.Value()
	}
	hasService := func() bool {
		for _, u := range prop("UUIDs").([]string) {
			if strings.EqualFold(u, ServiceUUID) {
				return true
			}
		}
		return false
	}
	inst0 := instances()

	got := make(chan []byte, 16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := link.Start(ctx, func(f []byte) {
		select {
		case got <- f:
		default:
		}
	}); err != nil {
		t.Fatal(err)
	}
	stopped := false
	defer func() {
		if !stopped {
			_ = link.Stop()
		}
	}()

	if n := instances(); n != inst0+1 {
		t.Errorf("ActiveInstances %d, want %d", n, inst0+1)
	}
	if !hasService() {
		t.Error("GATT service not registered on the adapter")
	}
	if !prop("Discovering").(bool) {
		t.Error("not discovering")
	}
	if a := prop("Alias").(string); a != genericAlias {
		t.Errorf("alias %q while running", a)
	}
	if !radio.Advertising() {
		t.Error("advertisement not registered")
	}
	if prop("Pairable").(bool) {
		t.Error("adapter pairable while the link runs")
	}
	time.Sleep(3 * time.Second)
	all, concord := radio.Counts()
	t.Logf("heard %d LE devices, %d Concord", all, concord)

	if os.Getenv("CONCORD_BLE_PEER") == "1" {
		deadline := time.After(90 * time.Second)
		tick := time.NewTicker(2 * time.Second)
		defer tick.Stop()
	wait:
		for {
			select {
			case f := <-got:
				t.Logf("received %d-byte frame from a peer", len(f))
				break wait
			case <-tick.C:
				_ = link.Send(ctx, append([]byte{0xbe}, []byte("concord ble hw test")...))
			case <-deadline:
				t.Fatalf("no frame from a peer in 90s (stats %+v)", link.Stats())
			}
		}
	}

	if err := link.Stop(); err != nil {
		t.Errorf("stop: %v", err)
	}
	stopped = true
	if n := instances(); n != inst0 {
		t.Errorf("ActiveInstances %d after stop, want %d", n, inst0)
	}
	if hasService() {
		t.Error("GATT service still registered after stop")
	}
	if prop("Discovering").(bool) {
		t.Error("still discovering after stop")
	}
	if a, b := prop("Alias").(string), before["Alias"].Value().(string); a != b {
		t.Errorf("alias %q after stop, was %q", a, b)
	}
	if p, b := prop("Pairable").(bool), before["Pairable"].Value().(bool); p != b {
		t.Errorf("pairable %v after stop, was %v", p, b)
	}
	if p, b := prop("Powered").(bool), before["Powered"].Value().(bool); p != b {
		t.Errorf("powered %v after stop, was %v", p, b)
	}
}
