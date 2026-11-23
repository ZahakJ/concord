package httpapi

import (
	"context"
	"crypto/rand"
	"testing"

	"github.com/ZahakJ/concord/internal/bridge"
	"github.com/ZahakJ/concord/internal/identity"
)

// Behind the App Lock gate the event stream carries only the gate's own
// events: a message arriving then is not handed to the page.
func TestTheGateHoldsTheEventStream(t *testing.T) {
	t.Setenv("CONCORD_HOME", t.TempDir())
	t.Setenv("CONCORD_DISABLE_MDNS", "1")
	t.Setenv("CONCORD_BOOTSTRAP", "")
	b := bridge.New(context.Background())
	t.Cleanup(b.Close)
	s := NewWith(b, false)
	s.WireSSE()
	if err := b.Login("ada-pass-1"); err != nil {
		t.Fatal(err)
	}
	slotID, u := make([]byte, identity.DeviceSlotIDSize), make([]byte, identity.DeviceSecretSize)
	_, _ = rand.Read(slotID)
	_, _ = rand.Read(u)
	if err := b.EnrolDeviceSecret(slotID, u, ""); err != nil {
		t.Fatal(err)
	}
	ch := make(chan sseEvent, 8)
	s.mu.Lock()
	s.clients[ch] = struct{}{}
	s.mu.Unlock()

	b.EngageAppLock()
	s.broadcast(sseEvent{"message", "secret text"})
	if err := b.UseDeviceSecret(slotID, u); err != nil {
		t.Fatal(err)
	}
	s.broadcast(sseEvent{"message", "after"})
	var got []string
	for len(ch) > 0 {
		got = append(got, (<-ch).name)
	}
	want := []string{"applock", "applock", "message"}
	if len(got) != len(want) {
		t.Fatalf("events %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("events %v, want %v", got, want)
		}
	}
}
