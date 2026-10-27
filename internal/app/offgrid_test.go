package app

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/transport"
)

// A guild message must still arrive when the internet between two members is
// gone and only an off-grid link joins them — and must arrive once, as the
// same signed, MLS-sealed message, not as something the link could read.
func TestMessageCrossesOffGridLinkWithoutInternet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	air := transport.NewAir()
	var tapMu sync.Mutex
	var tapped [][]byte
	air.Drop = func(f []byte) bool {
		tapMu.Lock()
		tapped = append(tapped, append([]byte(nil), f...))
		tapMu.Unlock()
		return false
	}
	caps := transport.Caps{MTU: 200, Bitrate: 20000, Encryption: true, Local: true, Mode: transport.Live}
	saved := offgridFactories
	offgridFactories = append(append([]func(string) transport.Transport(nil), saved...), func(string) transport.Transport {
		return transport.NewMemLink(air, "sim", "Simulated radio", caps)
	})
	t.Cleanup(func() { offgridFactories = saved })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := startService(t, ctx)
	b := startService(t, ctx)
	for _, s := range []*Service{a, b} {
		for _, v := range s.OffGrid().Links() {
			if v.Running {
				t.Fatalf("%s was running before the user enabled it", v.Name)
			}
		}
		if err := s.OffGrid().SetEnabled("sim", true); err != nil {
			t.Fatal(err)
		}
	}
	rb := &recorder{}
	b.OnMessage(rb.add)

	g, err := a.CreateGuild("off-grid")
	if err != nil {
		t.Fatal(err)
	}
	code, err := a.InviteCode(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatal(err)
	}
	waitMembers(t, 20*time.Second, 2, a, b)

	// The internet goes away: B's libp2p host is shut, so gossipsub has no
	// path between them at all.
	_ = b.host.Close()
	time.Sleep(200 * time.Millisecond)

	const secret = "meet at the north ridge"
	tapMu.Lock()
	tapped = nil // count only what this one line costs on the air
	tapMu.Unlock()
	if _, err := a.SendMessage(g.Channels[0].ID, secret, "", ""); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for !rb.has(secret) {
		if time.Now().After(deadline) {
			t.Fatal("message did not cross the off-grid link")
		}
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	n := 0
	rb.mu.Lock()
	for _, m := range rb.msgs {
		if m.Content == secret {
			n++
			if len(m.Sig) == 0 {
				t.Error("message arrived without its author signature")
			}
		}
	}
	rb.mu.Unlock()
	if n != 1 {
		t.Fatalf("delivered %d times, want once", n)
	}
	tapMu.Lock()
	defer tapMu.Unlock()
	for _, f := range tapped {
		if bytes.Contains(f, []byte("north ridge")) {
			t.Fatal("plaintext crossed the air")
		}
	}
	if len(tapped) == 0 {
		t.Fatal("nothing crossed the air")
	}
	t.Logf("a %d-byte chat line crossed as %d frames of ≤%d bytes", len(secret), len(tapped), caps.MTU)
}
