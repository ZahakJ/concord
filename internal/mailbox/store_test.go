package mailbox

import (
	"bytes"
	"testing"
	"time"
)

func TestStoreRegisterDepositDrainAck(t *testing.T) {
	s := New()
	const mid = "abc123"

	// Deposit to an unregistered mailbox is rejected.
	if _, ok := s.Deposit(mid, "dep", []byte("hi"), time.Hour); ok {
		t.Fatal("deposit to unregistered mailbox should be rejected")
	}

	s.Register(mid)
	id1, ok := s.Deposit(mid, "dep", []byte("one"), time.Hour)
	if !ok {
		t.Fatal("deposit after register should succeed")
	}
	id2, _ := s.Deposit(mid, "dep", []byte("two"), time.Hour)

	got := s.Drain(mid)
	if len(got) != 2 {
		t.Fatalf("drain got %d, want 2", len(got))
	}
	// Drain doesn't delete — a second drain returns the same.
	if len(s.Drain(mid)) != 2 {
		t.Fatal("drain deleted envelopes (should wait for ack)")
	}

	s.Ack(mid, []string{id1})
	got = s.Drain(mid)
	if len(got) != 1 || got[0].ID != id2 || !bytes.Equal(got[0].Data, []byte("two")) {
		t.Fatalf("after ack: %+v", got)
	}
	s.Ack(mid, []string{id2})
	if len(s.Drain(mid)) != 0 {
		t.Fatal("mailbox should be empty after acking all")
	}
}

func TestStoreTTLExpiry(t *testing.T) {
	s := New()
	const mid = "ttl"
	s.Register(mid)
	s.Deposit(mid, "dep", []byte("fleeting"), 20*time.Millisecond)
	if len(s.Drain(mid)) != 1 {
		t.Fatal("expected 1 before expiry")
	}
	time.Sleep(40 * time.Millisecond)
	if len(s.Drain(mid)) != 0 {
		t.Fatal("expired envelope should be gone")
	}
}

func TestStoreOversizeRejected(t *testing.T) {
	s := New()
	const mid = "big"
	s.Register(mid)
	if _, ok := s.Deposit(mid, "dep", make([]byte, MaxEnvelope+1), time.Hour); ok {
		t.Fatal("oversize envelope should be rejected")
	}
	if _, ok := s.Deposit(mid, "dep", nil, time.Hour); ok {
		t.Fatal("empty envelope should be rejected")
	}
}

func TestStorePerMailboxCap(t *testing.T) {
	s := New()
	const mid = "cap"
	s.Register(mid)
	for i := 0; i < MaxPerMailbox+50; i++ {
		s.Deposit(mid, "dep", []byte{byte(i)}, time.Hour)
	}
	if n := len(s.Drain(mid)); n > MaxPerMailbox {
		t.Fatalf("per-mailbox cap exceeded: %d", n)
	}
}

// TestStoreFloodFairness pins the anti-flood property: an attacker filling a
// victim's mailbox must not evict the victim's own genuine pending mail.
func TestStoreFloodFairness(t *testing.T) {
	s := New()
	const mid = "victimbox"
	s.Register(mid)

	// A legitimate sender leaves a couple of real messages.
	realA, _ := s.Deposit(mid, "honest-sender", []byte("real-1"), time.Hour)
	realB, _ := s.Deposit(mid, "honest-sender", []byte("real-2"), time.Hour)

	// An attacker floods the same tag well past the per-mailbox cap.
	for i := 0; i < MaxPerMailbox*2; i++ {
		s.Deposit(mid, "flooder", []byte{byte(i)}, time.Hour)
	}

	got := s.Drain(mid)
	if len(got) > MaxPerMailbox {
		t.Fatalf("per-mailbox cap exceeded: %d", len(got))
	}
	haveA, haveB := false, false
	for _, e := range got {
		if e.ID == realA {
			haveA = true
		}
		if e.ID == realB {
			haveB = true
		}
	}
	if !haveA || !haveB {
		t.Fatalf("flooder evicted the victim's genuine mail (realA=%v realB=%v)", haveA, haveB)
	}
}

func TestMailboxIDStable(t *testing.T) {
	pub := []byte("account-pubkey-32-bytes-xxxxxxxx")
	if MailboxID(pub) != MailboxID(pub) {
		t.Fatal("MailboxID not deterministic")
	}
	if MailboxID(pub) == MailboxID([]byte("different")) {
		t.Fatal("distinct keys collide")
	}
	if len(MailboxID(pub)) != 32 { // 16 bytes hex
		t.Fatalf("MailboxID length = %d", len(MailboxID(pub)))
	}
}

// TestDepositRateLimit verifies the per-peer token bucket: a burst is allowed,
// then deposits are throttled, and tokens refill over time.
func TestDepositRateLimit(t *testing.T) {
	svc := NewService(New())
	const p = "peerA"

	// The full burst should be permitted up front.
	for i := 0; i < int(depositBurst); i++ {
		if !svc.allowDeposit(p) {
			t.Fatalf("deposit %d within burst was rate-limited", i)
		}
	}
	// The next one exceeds the burst and must be refused.
	if svc.allowDeposit(p) {
		t.Fatal("deposit past the burst should be rate-limited")
	}
	// A different peer has its own independent bucket.
	if !svc.allowDeposit("peerB") {
		t.Fatal("a distinct peer must not be limited by another's usage")
	}

	// Simulate elapsed time so the bucket refills, then it should allow again.
	svc.mu.Lock()
	svc.buckets[p].last = svc.buckets[p].last.Add(-2 * time.Second)
	svc.mu.Unlock()
	if !svc.allowDeposit(p) {
		t.Fatal("bucket should refill after time passes")
	}
}
