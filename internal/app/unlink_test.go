package app

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Unlink, tested for exactly what it claims (see the threat model at the top of
// unlink.go) — the leaf really goes, the honest device really erases itself, and
// neither of those is confused for the other.

// TestUnlinkRemovesTheLeaf: the half that holds. After unlinking, the device's
// MLS leaf is gone from the shared group, so nothing sent afterwards is
// decryptable by it — whatever the device chooses to do about the revocation.
func TestUnlinkRemovesTheLeaf(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := testRendezvous(t, ctx)
	deskDir, phoneDir := t.TempDir(), t.TempDir()
	desk, phone, _, _ := linkedPair(t, ctx, deskDir, phoneDir, boot)

	guildID := ""
	for _, g := range desk.Guilds() {
		if g.Name == "Shared" {
			guildID = g.ID
		}
	}
	if guildID == "" {
		t.Fatal("lost the shared guild")
	}
	if n, _ := desk.MemberCount(guildID); n != 2 {
		t.Fatalf("expected 2 leaves before unlinking, got %d", n)
	}

	key := deviceKeyOf(t, phone)
	if err := desk.UnlinkDevice(key); err != nil {
		t.Fatalf("UnlinkDevice: %v", err)
	}
	waitUntil(t, 20*time.Second, func() bool {
		n, _ := desk.MemberCount(guildID)
		return n == 1
	}, "the unlinked device's leaf is still in the group")

	// And it stops being treated as this account locally.
	if fpr := desk.presence(phone.host.PeerID()).Fingerprint; fpr == desk.Fingerprint() {
		t.Error("an unlinked device still resolves to this account")
	}
	// …while staying in the device list, marked, so the panel can say what
	// happened rather than silently dropping a row.
	found := false
	for _, d := range desk.LinkedDevices() {
		if d.Key == key {
			found = true
			if d.Revoked == 0 {
				t.Error("unlinked device is not marked as unlinked")
			}
		}
	}
	if !found {
		t.Error("unlinked device vanished from the device list instead of being marked")
	}
}

// TestUnlinkedDeviceErasesItself: the advisory half, and its exact conditions —
// the device has to come back online and reach a peer holding the revocation.
// The erase is checked by its consequence: the keystore is gone, so the install
// cannot be started again.
func TestUnlinkedDeviceErasesItself(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := testRendezvous(t, ctx)
	deskDir, phoneDir := t.TempDir(), t.TempDir()
	desk, phone, _, _ := linkedPair(t, ctx, deskDir, phoneDir, boot)

	key := deviceKeyOf(t, phone)
	_ = phone.Close() // the phone is away when the decision is made
	if err := desk.UnlinkDevice(key); err != nil {
		t.Fatalf("UnlinkDevice: %v", err)
	}
	if _, err := os.Stat(keystorePathIn(phoneDir)); err != nil {
		t.Fatal("the phone's keystore vanished before it ever came back — nothing can reach an offline device")
	}

	// It comes back and meets the desktop, which tells it.
	phone2 := startServiceOn(t, ctx, phoneDir, boot)
	_ = phone2.host.Connect(ctx, desk.host.AddrInfo())
	waitUntil(t, 30*time.Second, func() bool {
		_, err := os.Stat(keystorePathIn(phoneDir))
		return os.IsNotExist(err)
	}, "the unlinked device did not erase itself after coming back online")

	if _, err := os.Stat(filepath.Join(phoneDir, deviceMarkerName)); !os.IsNotExist(err) {
		t.Error("the device marker survived the erase — a restart would present the revoked certificate again")
	}
}

// TestUnlinkRefusesTheDeviceYouAreHolding and the account key itself: both are
// ways of asking the app to erase the thing you are typing on, by a route with
// no confirmation attached to it.
func TestUnlinkRefusesSelf(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startServiceInDir(t, ctx, t.TempDir())
	if err := s.UnlinkDevice(hex.EncodeToString(s.id.PublicKey())); err == nil {
		t.Error("unlinking the account key was allowed")
	}
	if err := s.UnlinkDevice("not-hex"); err == nil {
		t.Error("unlinking a malformed key was allowed")
	}
}

// TestForeignRevocationIsIgnored: the self-erase must fire for a revocation this
// account signed naming this device, and for nothing else. A revocation signed
// by somebody else, or naming a different device, must pass straight through —
// this is the check standing between a stray frame and a wiped install.
func TestForeignRevocationIsIgnored(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	s := startServiceInDir(t, ctx, dir)
	other := startServiceInDir(t, ctx, t.TempDir())

	// Signed by another account, naming our key.
	if s.revokesUs(other.id.RevokeDevice(s.id.PublicKey(), time.Now().Unix())) {
		t.Error("a revocation from another account would have erased this device")
	}
	// Signed by us, naming somebody else's key.
	if s.revokesUs(s.id.RevokeDevice(other.id.PublicKey(), time.Now().Unix())) {
		t.Error("a revocation for a different device would have erased this one")
	}
	// Signed by us, naming our own ACCOUNT key on an install that was never
	// linked: there is no device certificate here to revoke, and the account key
	// is not a linked device.
	if s.revokesUs(s.id.RevokeDevice(s.id.PublicKey(), time.Now().Unix())) {
		t.Error("an unlinked original device treated an account-key revocation as its own")
	}
	if _, err := os.Stat(keystorePathIn(dir)); err != nil {
		t.Fatalf("keystore disappeared during a test that erases nothing: %v", err)
	}
}

// deviceKeyOf is a linked device's own key, hex — what Unlink is addressed to.
func deviceKeyOf(t *testing.T, s *Service) string {
	t.Helper()
	pub, err := s.host.PeerID().ExtractPublicKey()
	if err != nil {
		t.Fatalf("extract device key: %v", err)
	}
	raw, err := pub.Raw()
	if err != nil {
		t.Fatalf("raw device key: %v", err)
	}
	return hex.EncodeToString(raw)
}
