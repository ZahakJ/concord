package mailbox

import (
	"bytes"
	"strconv"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
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

// TestStoreEvictionSparesSmallMailboxes pins the GLOBAL eviction policy, which
// is a different attack from TestStoreFloodFairness above: there the flooder
// aims at one victim's tag, here it registers mailboxes of its own and fills
// the store until the byte cap forces eviction on everyone.
//
// With oldest-first eviction the flooder wins outright — the mail waiting
// longest belongs to the people who have been offline longest, which is who the
// store is for, while the flooder's junk is the newest thing in it. Eviction
// must come out of the fattest mailboxes instead.
func TestStoreEvictionSparesSmallMailboxes(t *testing.T) {
	s := New()

	// Someone who has been offline a while: two real envelopes, deposited first
	// so they are also the OLDEST bytes in the store — the ones the previous
	// policy reached for first.
	const victim = "quiet-recipient"
	s.Register(victim)
	realA, _ := s.Deposit(victim, "honest-sender", []byte("real-1"), time.Hour)
	realB, _ := s.Deposit(victim, "honest-sender", []byte("real-2"), time.Hour)

	// The flooder registers its own mailboxes (a tag costs a keypair) and stuffs
	// each to the per-mailbox cap until the store is over its byte ceiling.
	env := make([]byte, MaxEnvelope)
	boxes := MaxTotalBytes/(MaxPerMailbox*MaxEnvelope) + 2
	for b := 0; b < boxes; b++ {
		mid := "flood-" + strconv.Itoa(b)
		s.Register(mid)
		for i := 0; i < MaxPerMailbox; i++ {
			s.Deposit(mid, "flooder", env, time.Hour)
		}
	}

	if _, _, total := s.Stats(); total > MaxTotalBytes {
		t.Fatalf("store is over its byte cap after eviction: %d > %d", total, MaxTotalBytes)
	}
	got := s.Drain(victim)
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
		t.Fatalf("a store-wide flood purged an offline recipient's genuine mail "+
			"(realA=%v realB=%v, %d left) — eviction is aimed at the wrong bytes",
			haveA, haveB, len(got))
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
		if !svc.allowDeposit(p, 1) {
			t.Fatalf("deposit %d within burst was rate-limited", i)
		}
	}
	// The next one exceeds the burst and must be refused.
	if svc.allowDeposit(p, 1) {
		t.Fatal("deposit past the burst should be rate-limited")
	}
	// A different peer has its own independent bucket.
	if !svc.allowDeposit("peerB", 1) {
		t.Fatal("a distinct peer must not be limited by another's usage")
	}

	// Simulate elapsed time so the bucket refills, then it should allow again.
	svc.mu.Lock()
	svc.buckets[p].last = svc.buckets[p].last.Add(-2 * time.Second)
	svc.mu.Unlock()
	if !svc.allowDeposit(p, 1) {
		t.Fatal("bucket should refill after time passes")
	}
}

// TestDepositGlobalByteCeiling is the point of the second dimension: rotating
// peer ids is free, so a limit keyed on identity is a limit an attacker opts
// out of. Every fresh identity here passes its own per-peer bucket and is still
// stopped, because the ceiling it runs into is keyed on nothing.
func TestDepositGlobalByteCeiling(t *testing.T) {
	svc := NewService(New())
	const env = MaxEnvelope // the biggest envelope the store accepts

	// One deposit each from a never-repeated identity — the cheapest possible
	// evasion of the per-peer bucket.
	perIdentity := 0
	stopped := false
	for i := 0; i < int(depositBytesBurst/env)+20; i++ {
		if !svc.allowDeposit(peer.ID("flooder-"+strconv.Itoa(i)), env) {
			stopped = true
			break
		}
		perIdentity++
	}
	if !stopped {
		t.Fatalf("%d deposits from %d distinct identities were all allowed — "+
			"the ceiling is still keyed on identity", perIdentity, perIdentity)
	}
	if want := int(depositBytesBurst / env); perIdentity < want/2 {
		t.Fatalf("only %d deposits got through before the ceiling; the burst (%d) "+
			"should cover an honest fan-out", perIdentity, want)
	}

	// An honest depositor is refused too while the ceiling is spent. That is
	// what a node-wide ceiling MEANS: the alternative — exempting anyone — is
	// a hole shaped exactly like the flood it is meant to stop.
	if svc.allowDeposit("honest", env) {
		t.Fatal("the global ceiling did not apply once spent")
	}

	// It refills: an hour of quiet, and the node is open for business again.
	svc.mu.Lock()
	svc.globalBytes.last = svc.globalBytes.last.Add(-time.Hour)
	svc.mu.Unlock()
	if !svc.allowDeposit("honest", env) {
		t.Fatal("the ceiling should refill over time, not latch shut")
	}
}
