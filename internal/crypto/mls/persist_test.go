package mls

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/zahak/concord/internal/identity"
)

// TestPersistentRestartCapabilities documents empirically what an MLS member
// recovers after a process restart when backed by on-disk storage. It creates a
// 2-party group, restarts Alice from her storage directory, and probes which
// operations still work. The assertions encode the *observed* behaviour so a
// future upstream change (e.g. signature-key persistence) is caught as a change
// here.
func TestPersistentRestartCapabilities(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	aliceDir := filepath.Join(dir, "alice")

	aliceID, _ := identity.Generate()
	aliceCred := []byte(aliceID.PublicKey())
	aliceSigning := aliceID.PrivateKey() // deterministic across the "restart"

	alice, err := NewPersistent(aliceCred, aliceSigning, aliceDir)
	if err != nil {
		t.Fatalf("NewPersistent alice: %v", err)
	}
	bob, _ := engineForNewMember(t)

	// Form a 2-party group and exchange one message each way.
	gid, err := alice.CreateGroup(ctx)
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	bobKP, _ := bob.KeyPackage(ctx)
	_, welcome, err := alice.Invite(ctx, gid, bobKP)
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if _, err := bob.Join(ctx, welcome); err != nil {
		t.Fatalf("Join: %v", err)
	}
	ctFromBob, _ := bob.Encrypt(ctx, gid, []byte("before-restart"))
	if _, err := alice.Decrypt(ctx, gid, ctFromBob); err != nil {
		t.Fatalf("pre-restart decrypt: %v", err)
	}

	// "Restart" Alice: close her engine and reopen from the same directory.
	if err := alice.Close(); err != nil {
		t.Fatalf("close alice: %v", err)
	}
	alice2, err := NewPersistent(aliceCred, aliceSigning, aliceDir)
	if err != nil {
		t.Fatalf("reopen alice: %v", err)
	}
	t.Cleanup(func() { _ = alice2.Close() })

	// Probe 1: can restarted Alice still see the group and its members?
	members, err := alice2.Members(ctx, gid)
	if err != nil {
		t.Fatalf("RESTART REGRESSION: group state not recovered: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("recovered group has %d members, want 2", len(members))
	}
	t.Logf("recovered: group state + %d members", len(members))

	// Probe 2: can restarted Alice decrypt a NEW message from Bob?
	ctFromBob2, _ := bob.Encrypt(ctx, gid, []byte("after-restart"))
	if _, err := alice2.Decrypt(ctx, gid, ctFromBob2); err != nil {
		t.Fatalf("RESTART REGRESSION: cannot decrypt after restart: %v", err)
	}
	t.Log("recovered: can decrypt new inbound messages")

	// Probe 3: restarted Alice must also be able to SEND. Our vendored
	// signature-key persistence makes this work: she recovers her signing key,
	// so Bob accepts and decrypts her post-restart message.
	ct, err := alice2.Encrypt(ctx, gid, []byte("send-after-restart"))
	if err != nil {
		t.Fatalf("send after restart failed: %v", err)
	}
	msg, err := bob.Decrypt(ctx, gid, ct)
	if err != nil {
		t.Fatalf("bob could not decrypt Alice's post-restart message: %v", err)
	}
	if string(msg.Plaintext) != "send-after-restart" {
		t.Fatalf("post-restart message garbled: %q", msg.Plaintext)
	}
	t.Log("recovered: can also SEND after restart (signature key persisted)")
}
