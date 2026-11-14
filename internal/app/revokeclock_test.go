package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/identity"
)

// samePhone starts another install of desk's account (a phone) whose device
// marker carries cert, as a linked device's does. The marker is written
// after the phone starts, since its device key is minted at its first unlock.
func samePhone(t *testing.T, ctx context.Context, desk *Service, issuedAt int64) (*Service, *identity.DeviceCert) {
	t.Helper()
	dir := t.TempDir()
	if err := identity.RestoreKeystore(keystorePathIn(dir), phraseOf(t, desk.id), "test-pass", nil); err != nil {
		t.Fatal(err)
	}
	phone := startServiceInDir(t, ctx, dir)
	dev := phone.id.DevicePublicKey()
	if len(dev) == 0 {
		t.Fatal("the phone has no device key")
	}
	cert := desk.id.IssueDeviceCertFor(dev, "phone", issuedAt)
	if err := saveDeviceMarker(dir, cert); err != nil {
		t.Fatal(err)
	}
	return phone, cert
}

// revocationOf is the revocation desk holds for dev, nil if none.
func revocationOf(desk *Service, dev []byte) *identity.DeviceRevocation {
	for _, r := range desk.revocations() {
		if bytes.Equal(r.DevicePub, dev) {
			return r
		}
	}
	return nil
}

// A genuine unlink always erases the device, whatever the clocks of the
// devices that linked and unlinked it said. The revocation is stamped no
// earlier than the newest certificate known for the key, so a certificate
// stamped ahead (an issuer clock ahead, a revoker clock behind, a date far
// in the future) cannot make it inert. The registry keeps the newest
// certificate for a key, not the first one seen.
func TestARevocationNeverLosesToTheClocks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	desk := startServiceInDir(t, ctx, t.TempDir())
	now := time.Now()
	for _, c := range []struct {
		name  string
		ahead time.Duration
		older time.Duration // a first certificate for the key, seen before (0: none)
	}{
		{"issuer 2 h ahead at link time, unlink 10 min later", 2*time.Hour - 10*time.Minute, 0},
		{"revoker 90 s behind the issuer, unlink 60 s after the link", 30 * time.Second, 0},
		{"certificate stamped a year ahead", 365 * 24 * time.Hour, 0},
		{"re-certified ahead after an older certificate was seen", time.Hour, -24 * time.Hour},
	} {
		phone, cert := samePhone(t, ctx, desk, now.Add(c.ahead).Unix())
		dev := phone.id.DevicePublicKey()
		if c.older != 0 {
			desk.noteOwnDevice(desk.id.IssueDeviceCertFor(dev, "phone", now.Add(c.older).Unix()), "phone", "", true)
		}
		desk.noteOwnDevice(cert, "phone", "", true) // its hello
		if err := desk.UnlinkDevice(hex.EncodeToString(dev)); err != nil {
			t.Fatalf("%s: unlink: %v", c.name, err)
		}
		rev := revocationOf(desk, dev)
		if rev == nil {
			t.Fatalf("%s: no revocation stored", c.name)
		}
		if !phone.revokesUs(rev) {
			t.Errorf("%s: the unlinked device would not erase itself (revoked %d, certificate %d)", c.name, rev.RevokedAt, cert.IssuedAt)
		}
	}
}

// The issuer keeps a revocation when it answers a link request for that
// key: the joiner may still cancel ("erase this device's copy and link?"),
// or its answer may be lost. It is dropped only when the device shows, at
// its hello, a certificate newer than the revocation.
func TestACancelledRelinkLeavesTheDeviceRevoked(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	desk := startServiceInDir(t, ctx, t.TempDir())
	phone, old := samePhone(t, ctx, desk, time.Now().Add(-time.Hour).Unix())
	dev := phone.id.DevicePublicKey()
	desk.noteOwnDevice(old, "phone", "", true)
	sib, _, _ := ed25519.GenerateKey(nil)
	desk.saveRevocations([]*identity.DeviceRevocation{desk.id.RevokeDevice(sib, time.Now().Unix())})
	desk.loadRevoked()
	if err := desk.UnlinkDevice(hex.EncodeToString(dev)); err != nil {
		t.Fatal(err)
	}

	fresh := desk.certifyDevice(dev, "phone again") // answered; the joiner cancels
	if fresh == nil || !fresh.Verify() {
		t.Fatal("no certificate issued")
	}
	pid, ok := devicePeerID(dev)
	if !ok {
		t.Fatal("no peer id for the device key")
	}
	rev := desk.revocationFor(pid)
	if rev == nil || !desk.deviceIsRevoked(dev) {
		t.Fatal("answering a link request undid the unlink before the device accepted it")
	}
	if !phone.revokesUs(rev) {
		t.Fatal("the device that cancelled its re-link would no longer erase itself")
	}

	hello := func(cert *identity.DeviceCert) {
		raw, _ := json.Marshal(helloFrame{Credential: cert.Marshal()})
		desk.ingestHello(pid, raw)
	}
	hello(old)
	if !desk.deviceIsRevoked(dev) || revocationOf(desk, dev) == nil {
		t.Fatal("the old certificate at a hello undid the unlink")
	}
	hello(fresh)
	if desk.deviceIsRevoked(dev) || revocationOf(desk, dev) != nil {
		t.Fatal("the device presented its new certificate and is still treated as revoked")
	}
	if !desk.deviceIsRevoked(sib) || revocationOf(desk, sib) == nil {
		t.Fatal("a sibling's revocation went with it")
	}
}

// A device of the account that did not do the re-link (it unlinked the
// phone; another device linked it again) stops treating the key as revoked
// once it sees a newer certificate for it, wherever that certificate comes
// from. One no newer than the revocation changes nothing.
func TestAThirdDeviceSeesTheRelink(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := startServiceInDir(t, ctx, t.TempDir())
	dev, _, _ := ed25519.GenerateKey(nil)
	at := time.Now().Add(-time.Hour).Unix()
	c.saveRevocations([]*identity.DeviceRevocation{c.id.RevokeDevice(dev, at)})
	c.loadRevoked()
	c.learnDeviceCert(c.id.IssueDeviceCertFor(dev, "phone", at).Marshal())
	if !c.deviceIsRevoked(dev) {
		t.Fatal("a certificate no newer than the revocation undid it")
	}
	c.learnDeviceCert(c.id.IssueDeviceCertFor(dev, "phone", at+60).Marshal())
	if c.deviceIsRevoked(dev) || revocationOf(c, dev) != nil {
		t.Fatal("a device that saw the newer certificate still treats the key as revoked")
	}
}

// A certificate the issuer signs for a key it holds a revocation for is
// stamped after that revocation, whatever its own clock says, so the device
// that accepts it is not erased by the revocation it replaces.
func TestARecertificateIsNewerThanTheRevocationItReplaces(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	desk := startServiceInDir(t, ctx, t.TempDir())
	dev, _, _ := ed25519.GenerateKey(nil)
	ahead := time.Now().Add(365 * 24 * time.Hour).Unix()
	desk.saveRevocations([]*identity.DeviceRevocation{desk.id.RevokeDevice(dev, ahead)})
	desk.loadRevoked()
	if cert := desk.certifyDevice(dev, "phone again"); cert.IssuedAt <= ahead {
		t.Fatalf("certificate stamped %d, not after the revocation it replaces (%d)", cert.IssuedAt, ahead)
	}
}
