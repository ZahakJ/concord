package qr

import (
	"bytes"
	"math/rand"
	"sync"
	"testing"

	"github.com/ZahakJ/concord/internal/transport"
)

// A message queued for the QR link leaves as frames that each fit one code,
// and comes back whole on the other device whatever order the camera reads
// the codes in, and however many times it reads each one.
func TestQRSequenceCarriesAMessage(t *testing.T) {
	const topic = "guild/chan"
	var mu sync.Mutex
	var got [][]byte
	a := transport.NewManager("", nil)
	b := transport.NewManager("", func(tp string, p []byte) {
		mu.Lock()
		defer mu.Unlock()
		if tp == topic {
			got = append(got, p)
		}
	})
	for _, m := range []*transport.Manager{a, b} {
		m.Register(Link{})
		m.Carry(topic)
		m.Start(t.Context())
		if err := m.SetEnabled("qr", true); err != nil {
			t.Fatal(err)
		}
	}
	payload := make([]byte, 1500) // an MLS message with some text in it
	rand.New(rand.NewSource(1)).Read(payload)
	a.Send(topic, payload)
	frames, err := a.Export("qr")
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) < 2 {
		t.Fatalf("%d frames for 1500 bytes", len(frames))
	}
	for _, f := range frames {
		if len(f) > MTU {
			t.Fatalf("frame of %d bytes exceeds the QR MTU", len(f))
		}
	}
	rng := rand.New(rand.NewSource(2))
	order := append(rng.Perm(len(frames)), rng.Perm(len(frames))...)
	for _, i := range order {
		b.Receive("qr", frames[i])
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || !bytes.Equal(got[0], payload) {
		t.Fatalf("delivered %d messages", len(got))
	}
	if err := (Link{}).Send(t.Context(), frames[0]); err != ErrShowOnly {
		t.Fatalf("Send: %v", err)
	}
}
