package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/identity"
	"github.com/libp2p/go-libp2p/core/peer"
)

// sessionFixture is a service and a device of its account that it has not
// placed yet: the device's first hello is what places it.
func sessionFixture(t *testing.T) (context.Context, *Service, peer.ID, []byte) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := startServiceInDir(t, ctx, t.TempDir())
	dev, err := identity.Generate()
	if err != nil {
		t.Fatalf("generate the device: %v", err)
	}
	cert := s.id.IssueDeviceCertFor(dev.PublicKey(), "phone", time.Now().UnixMilli()).Marshal()
	p := peerOf(t, dev)
	if s.ownDevice(p) {
		t.Fatal("the device was placed before it said anything")
	}
	return ctx, s, p, cert
}

func helloFromDevice(t *testing.T, ctx context.Context, s *Service, p peer.ID, cert []byte, session string) {
	t.Helper()
	req, err := json.Marshal(helloFrame{Credential: cert, Session: session})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.handleHello(ctx, p, req); err != nil {
		t.Fatal(err)
	}
}

func sessionOf(s *Service, p peer.ID) (peerSession, bool) {
	s.peerSessionsMu.Lock()
	defer s.peerSessionsMu.Unlock()
	ps, ok := s.peerSessions[p]
	return ps, ok
}

// The Session used to be noted before the certificate in the same hello was
// learnt, so a device placed by that hello had none recorded, and its next
// restart looked like a first meeting.
func TestADeviceFirstPlacedByItsHelloHasItsSessionRecorded(t *testing.T) {
	ctx, s, p, cert := sessionFixture(t)
	helloFromDevice(t, ctx, s, p, cert, "run-1")
	if !s.ownDevice(p) {
		t.Fatal("the hello did not place the device as ours")
	}
	if ps, ok := sessionOf(s, p); !ok || ps.session != "run-1" {
		t.Fatalf("Session after the placing hello = %+v (recorded %v); want run-1", ps, ok)
	}
	helloFromDevice(t, ctx, s, p, cert, "run-2")
	if ps, _ := sessionOf(s, p); ps.resetAt.IsZero() {
		t.Fatal("a new Session from the device released nothing")
	}
}

// A build that sends no Session is recorded as seen, so the first hello after
// the upgrade, the first to name one, is the restart it is. Past that, one
// device's changes of Session release what we hold at most once per
// peerSessionResetEvery.
func TestAnUpgradeToASessionIsARestartAndResetsAreRateLimited(t *testing.T) {
	ctx, s, p, cert := sessionFixture(t)
	helloFromDevice(t, ctx, s, p, cert, "")
	if ps, ok := sessionOf(s, p); !ok || ps.session != "" {
		t.Fatalf("a hello without a Session was not recorded as seen: %+v %v", ps, ok)
	}
	helloFromDevice(t, ctx, s, p, cert, "")
	if ps, _ := sessionOf(s, p); !ps.resetAt.IsZero() {
		t.Fatal("a second Session-less hello was taken for a restart")
	}
	helloFromDevice(t, ctx, s, p, cert, "upgraded")
	first, _ := sessionOf(s, p)
	if first.resetAt.IsZero() {
		t.Fatal("the first hello with a Session after one without was not taken for a restart")
	}
	helloFromDevice(t, ctx, s, p, cert, "again")
	again, _ := sessionOf(s, p)
	if again.session != "again" {
		t.Fatalf("the Session was not recorded: %q", again.session)
	}
	if !again.resetAt.Equal(first.resetAt) {
		t.Fatal("a second change of Session within peerSessionResetEvery released everything again")
	}
}

// An answer to our hello from a new run releases the claims on answering and
// soliciting it, and not the one on greeting it: our hello just reached it.
func TestAnAnswerFromANewRunKeepsTheGreeting(t *testing.T) {
	ctx, s, p, cert := sessionFixture(t)
	helloFromDevice(t, ctx, s, p, cert, "run-1")
	s.greeted.claim(p)
	s.answered.claim(p)
	s.solicited.claim(p)
	s.notePeerSession(p, "run-2", false)
	if s.greeted.claim(p) {
		t.Fatal("an answer from a new run released the greeting")
	}
	if !s.answered.claim(p) || !s.solicited.claim(p) {
		t.Fatal("an answer from a new run kept the claims on answering and soliciting it")
	}
}
