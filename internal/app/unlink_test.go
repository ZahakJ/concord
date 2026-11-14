package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/identity"
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
	// Signed by us, naming THIS install's device key, but the install holds no
	// device marker (never linked, or restored): not a linked device, so not
	// erased. Without the marker requirement only this case would catch it.
	if dev := s.id.DevicePublicKey(); len(dev) == 0 {
		t.Fatal("the test install has no device key")
	} else if s.revokesUs(s.id.RevokeDevice(dev, time.Now().Unix())) {
		t.Error("an install with no device marker treated a revocation of its device key as its own")
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

// The same, when the device comes back before the desktop has noticed its old
// connection go: the desktop still holds the "already answered" claim from
// that connection when the new hello arrives. Its answer must carry the
// revocation anyway.
func TestUnlinkedDeviceErasesItselfWhenItReturnsAtOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := testRendezvous(t, ctx)
	deskDir, phoneDir := t.TempDir(), t.TempDir()
	desk, phone, _, _ := linkedPair(t, ctx, deskDir, phoneDir, boot)

	key := deviceKeyOf(t, phone)
	phoneID := phone.host.PeerID()
	_ = phone.Close()
	if err := desk.UnlinkDevice(key); err != nil {
		t.Fatalf("UnlinkDevice: %v", err)
	}
	// What the desktop looks like when the phone's old connection has not been
	// reaped yet: the claim taken when it answered the phone's last hello is
	// still held. Taken here if the desktop has already released it, and left
	// as it is if not. This used to wait for the release, which is not
	// guaranteed in any useful time: when the two devices dial each other at
	// once, the desktop can be left holding a connection the phone never
	// finished accepting before it closed, and that one ends only at the
	// transport's idle timeout, about thirty seconds, the test's whole wait.
	// That lingering connection is this test's very case, not a fault in it.
	desk.answered.claim(phoneID)

	phone2 := startServiceOn(t, ctx, phoneDir, boot)
	_ = phone2.host.Connect(ctx, desk.host.AddrInfo())
	waitUntil(t, 30*time.Second, func() bool {
		_, err := os.Stat(keystorePathIn(phoneDir))
		return os.IsNotExist(err)
	}, "the unlinked device did not erase itself after coming back online")
}

// TestAHelloAnsweredAfterItsConnectionDroppedHoldsNoClaim is the race behind
// the occasional failure of the test above. The hello claim is per connection
// and released when the connection ends; a hello still being handled when
// that release ran took the claim afterwards and kept it with no connection
// left to release it, so the redialling device only ever got the short
// answer. Here the hello comes from a peer with no connection at all.
func TestAHelloAnsweredAfterItsConnectionDroppedHoldsNoClaim(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startServiceInDir(t, ctx, t.TempDir())
	gone := startServiceInDir(t, ctx, t.TempDir()).host.PeerID()
	if s.host.HasConnection(gone) {
		t.Fatal("the two services are connected; the case under test did not arise")
	}
	if _, err := s.handleHello(ctx, gone, []byte("{}")); err != nil {
		t.Fatalf("handleHello: %v", err)
	}
	if !s.answered.claim(gone) {
		t.Fatal("a hello from a peer with no connection left its claim held")
	}
}

// A device unlinked while it was away, then linked again keeping its device
// key, holds a certificate newer than the old revocation. That revocation is
// not about this certificate and must not erase it; a revocation issued after
// the certificate still does.
func TestOldRevocationDoesNotEraseARelinkedDevice(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	s := startServiceInDir(t, ctx, dir)
	dev := s.id.DevicePublicKey()
	if len(dev) == 0 {
		t.Fatal("the test install has no device key")
	}
	now := time.Now()
	if err := saveDeviceMarker(dir, s.id.IssueDeviceCertFor(dev, "relinked", now.Add(-24*time.Hour).Unix())); err != nil {
		t.Fatal(err)
	}
	if s.revokesUs(s.id.RevokeDevice(dev, now.Add(-48*time.Hour).Unix())) {
		t.Error("a revocation older than this device's certificate would have erased it")
	}
	if !s.revokesUs(s.id.RevokeDevice(dev, now.Unix())) {
		t.Error("a revocation issued after this device's certificate no longer erases it")
	}
	if !s.revokesUs(s.id.RevokeDevice(dev, now.Add(-24*time.Hour).Unix())) {
		t.Error("a revocation issued in the same second as the certificate no longer erases it")
	}
}

// The issuer, certifying a device key it once revoked (the device came back
// and was linked again), drops that revocation: it is not handed to the device
// at its next hello, and the device is not treated as revoked here.
func TestRecertifyingADeviceDropsItsOldRevocation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startServiceInDir(t, ctx, t.TempDir())
	dev, _, _ := ed25519.GenerateKey(nil)
	sib, _, _ := ed25519.GenerateKey(nil)
	s.saveRevocations([]*identity.DeviceRevocation{
		s.id.RevokeDevice(dev, time.Now().Add(-time.Hour).Unix()),
		s.id.RevokeDevice(sib, time.Now().Add(-time.Hour).Unix()),
	})
	s.loadRevoked()
	s.markDeviceRevoked(hex.EncodeToString(dev), time.Now().Add(-time.Hour).Unix())
	if cert := s.certifyDevice(dev, "phone again"); cert == nil || !cert.Verify() {
		t.Fatal("no certificate issued")
	}
	for _, r := range s.revocations() {
		if bytes.Equal(r.DevicePub, dev) {
			t.Fatal("the old revocation is still stored and would be handed to the re-linked device")
		}
	}
	if len(s.revocations()) != 1 {
		t.Fatalf("the sibling's revocation went too: %d left", len(s.revocations()))
	}
	if s.deviceIsRevoked(dev) {
		t.Fatal("the re-linked device is still treated as revoked")
	}
	if !s.deviceIsRevoked(sib) {
		t.Fatal("the sibling is no longer treated as revoked")
	}
	for _, r := range s.deviceRegistry() {
		if r.Key == hex.EncodeToString(dev) && r.Revoked != 0 {
			t.Fatal("the registry still marks the re-linked device revoked")
		}
	}
}

// A revoke wipe that cannot remove the keystore leaves the install looking
// like what it is, a revoked linked device: the marker goes last, so the next
// start sees the revocation still applies and finishes the erase.
func TestInterruptedRevokeWipeFinishesAtTheNextStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	s := startServiceInDir(t, ctx, dir)
	dev := s.id.DevicePublicKey()
	if err := saveDeviceMarker(dir, s.id.IssueDeviceCertFor(dev, "phone", time.Now().Add(-time.Hour).Unix())); err != nil {
		t.Fatal(err)
	}
	s.saveRevocations([]*identity.DeviceRevocation{s.id.RevokeDevice(dev, time.Now().Unix())})

	orig := removeKeystore
	removeKeystore = func(string) error { return errors.New("held by another program") }
	s.wipeSelf()
	removeKeystore = orig
	if _, err := os.Stat(deviceMarkerPath(dir)); err != nil {
		t.Fatalf("the interrupted revoke wipe removed the device marker first: %v", err)
	}

	_, err := Start(ctx, Config{DataDir: dir, Passphrase: "test-pass", DisableMDNS: true})
	if !errors.Is(err, ErrDeviceUnlinked) {
		t.Fatalf("the next start did not finish the revoke wipe: %v", err)
	}
	if hasAccountData(dir) || HasIdentity(dir) {
		t.Fatal("the revoke wipe at the next start left data behind")
	}
}

// When part of the data cannot be removed, the device marker stays with it
// and is named: what is left still reads as a linked install, so a restore
// over it asks instead of keeping a linked device's group state.
func TestPartialEraseKeepsTheDeviceMarker(t *testing.T) {
	dir := t.TempDir()
	bea := seedDevice(t, dir, "pw", "Bea", false)
	if err := saveDeviceMarker(dir, bea.IssueDeviceCertFor(make([]byte, 32), "phone", time.Now().Unix())); err != nil {
		t.Fatal(err)
	}
	// a peer cache that cannot be removed: a directory with something in it
	pc := peerCachePath(dir)
	_ = os.Remove(pc)
	if err := os.MkdirAll(filepath.Join(pc, "held"), 0o700); err != nil {
		t.Fatal(err)
	}
	err := resetDataIn(dir)
	if !errors.Is(err, ErrDataNotErased) || !strings.Contains(err.Error(), "device.json") {
		t.Fatalf("partial erase: %v, want it to name the device marker as left", err)
	}
	if _, err := os.Stat(deviceMarkerPath(dir)); err != nil {
		t.Fatalf("the device marker went while other data stayed: %v", err)
	}
}
