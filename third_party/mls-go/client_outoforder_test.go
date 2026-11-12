package mls

import (
	"context"
	"fmt"
	"testing"

	"github.com/thomas-vilte/mls-go/ciphersuite"
)

// A client that reloads its group from storage for every operation (the
// default) could decrypt nothing that arrived after a later message from the
// same sender: the stored state carries no past generations, so each reload
// started with an empty out-of-order window ("generation N already advanced
// past"). Gossip reorders, and a sender with several sends in flight
// publishes out of order, so under a burst most of a channel failed this way.
func TestReloadingClientDecryptsOutOfOrder(t *testing.T) {
	cs := ciphersuite.MLS128DHKEMP256
	ctx := context.Background()
	alice, err := NewClient([]byte("alice"), cs)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := NewClient([]byte("bob"), cs)
	if err != nil {
		t.Fatal(err)
	}
	bobKP, err := bob.FreshKeyPackageBytes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	gid, err := alice.CreateGroup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, welcome, err := alice.InviteMember(ctx, gid, bobKP)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bob.JoinGroup(ctx, welcome); err != nil {
		t.Fatal(err)
	}

	var cts [][]byte
	for i := 0; i < 6; i++ {
		ct, err := alice.SendMessage(ctx, gid, []byte(fmt.Sprintf("m%d", i)))
		if err != nil {
			t.Fatal(err)
		}
		cts = append(cts, ct)
	}
	for _, i := range []int{5, 0, 3, 1, 4, 2} {
		got, err := bob.ReceiveMessage(ctx, gid, cts[i])
		if err != nil {
			t.Fatalf("m%d, arriving out of order: %v", i, err)
		}
		if string(got.Plaintext) != fmt.Sprintf("m%d", i) {
			t.Fatalf("m%d decrypted to %q", i, got.Plaintext)
		}
	}
	// Still exactly once each.
	if _, err := bob.ReceiveMessage(ctx, gid, cts[0]); err == nil {
		t.Fatal("a replayed message was accepted a second time")
	}
}

// The carried window stays usable however long the epoch runs: more messages
// than the library's own past-generation cache holds, then one reordered pair.
func TestOutOfOrderWindowSlidesPastItsCap(t *testing.T) {
	cs := ciphersuite.MLS128DHKEMP256
	ctx := context.Background()
	alice, _ := NewClient([]byte("alice"), cs)
	bob, _ := NewClient([]byte("bob"), cs)
	bobKP, _ := bob.FreshKeyPackageBytes(ctx)
	gid, _ := alice.CreateGroup(ctx)
	_, welcome, err := alice.InviteMember(ctx, gid, bobKP)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bob.JoinGroup(ctx, welcome); err != nil {
		t.Fatal(err)
	}
	const n = 2100 // past the 2048-generation cap
	for i := 0; i < n; i++ {
		ct, err := alice.SendMessage(ctx, gid, []byte("x"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := bob.ReceiveMessage(ctx, gid, ct); err != nil {
			t.Fatalf("in-order message %d: %v", i, err)
		}
	}
	late, _ := alice.SendMessage(ctx, gid, []byte("late"))
	early, _ := alice.SendMessage(ctx, gid, []byte("early"))
	if _, err := bob.ReceiveMessage(ctx, gid, early); err != nil {
		t.Fatal(err)
	}
	if _, err := bob.ReceiveMessage(ctx, gid, late); err != nil {
		t.Fatalf("out of order after %d messages: %v", n, err)
	}
}
