package app

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/identity"
)

// Unlinking a device — what it actually does, and what it cannot do.
//
// THE HONEST THREAT MODEL. Concord has no server, so there is nothing that can
// reach into a device you no longer hold and take anything away from it. Unlink
// is therefore three separate things with three very different strengths, and
// they must not be presented as one:
//
//  1. CRYPTOGRAPHIC, and real: removing the device's leaf from the MLS groups.
//     After that commit every group key ratchets forward without it, so it can
//     decrypt nothing sent afterwards, and it cannot send anything the others
//     will accept. This works whether the device is honest, hostile, or off. It
//     is the only part that holds against an attacker, and it only covers
//     traffic from the commit onward.
//
//  2. ADVISORY, and conditional: the signed revocation record. An honest client
//     that sees a revocation naming its own device key erases its local state.
//     That requires the device to be running Concord, to come online, to reach a
//     peer that holds the revocation, and to be running a client that has not
//     been modified to ignore it. Any one of those failing means nothing
//     happens. A device that never comes back keeps everything it already had,
//     forever — the messages are on its disk and we cannot reach them.
//
//  3. COSMETIC, and local: we stop treating that device key as this account, so
//     it stops appearing as you in rosters, voice rooms and diagnostics.
//
// WHAT UNLINK EXPLICITLY DOES NOT DO. It does not un-read anything already read.
// It does not delete anything from a device that stays offline, or from a device
// running a patched client. And — this is the one that matters most and is
// easiest to gloss over — a linked device holds the ACCOUNT SEED. RedeemLink
// hands it over; that is what makes it the same account rather than a second
// one. So a hostile holder of a revoked device can still sign as your account:
// mint itself a fresh device certificate, prove ownership of your fingerprint,
// and be re-admitted by anyone who admits you. Removing its leaf costs it the
// group keys it had; it does not cost it your identity.
//
// The only remedy for a device in genuinely hostile hands is to rotate the
// account key, which changes your fingerprint and invalidates every safety
// number your contacts have verified. Unlink is not that, and this file will not
// pretend otherwise. Unlink is for the phone you sold, the laptop you retired,
// the tablet you lost — for tidying an account you still control, and for
// limiting what a lost device keeps receiving.

// deviceRevocationsKey persists the revocations this account has issued.
const deviceRevocationsKey = "account.revocations"

// ErrDeviceUnlinked is returned by Start when this install finds a valid
// revocation for its own device key: it has been unlinked and has erased itself.
var ErrDeviceUnlinked = errors.New("app: this device has been unlinked from the account and its local data was erased")

// revocations reads the revocations we hold, keeping only the ones this account
// really signed.
func (s *Service) revocations() []*identity.DeviceRevocation {
	raw, err := s.store.GetSetting(deviceRevocationsKey)
	if err != nil || raw == "" {
		return nil
	}
	var recs []*identity.DeviceRevocation
	if json.Unmarshal([]byte(raw), &recs) != nil {
		return nil
	}
	out := recs[:0]
	for _, r := range recs {
		if r != nil && r.Verify() && ed25519Equal(r.AccountPub, s.id.PublicKey()) {
			out = append(out, r)
		}
	}
	return out
}

func (s *Service) saveRevocations(recs []*identity.DeviceRevocation) error {
	raw, err := json.Marshal(recs)
	if err != nil {
		return err
	}
	return s.store.SetSetting(deviceRevocationsKey, string(raw))
}

// revocationFor returns the revocation to hand a peer, if the peer is a device
// of ours that we have unlinked. Delivered in the hello reply because that is
// the first thing a returning device does — "wipes itself when it next comes
// online" is exactly a hello away.
func (s *Service) revocationFor(p peer.ID) *identity.DeviceRevocation {
	pub, err := p.ExtractPublicKey()
	if err != nil {
		return nil
	}
	raw, err := pub.Raw()
	if err != nil {
		return nil
	}
	for _, r := range s.revocations() {
		if bytes.Equal(r.DevicePub, raw) {
			return r
		}
	}
	return nil
}

// deviceIsRevoked reports whether we have unlinked this device key.
//
// Answered from memory, not the store: every inbound message and every roster
// read passes through learnDeviceCert, which asks this, and a settings query per
// message is not a thing to put on that path.
func (s *Service) deviceIsRevoked(devicePub []byte) bool {
	_, revoked := s.revokedAt(devicePub)
	return revoked
}

// revokedAt is the newest RevokedAt this device holds against a device key.
func (s *Service) revokedAt(devicePub []byte) (int64, bool) {
	s.deviceMu.RLock()
	defer s.deviceMu.RUnlock()
	at, ok := s.revokedDevices[hex.EncodeToString(devicePub)]
	return at, ok
}

// stillRevoked reports whether cert's device key is revoked here. A
// certificate of this account for that key issued after the revocation
// supersedes it: the device was linked again (by this device or another one
// of the account) and is using the new certificate, so the revocation, which
// withdrew an earlier one, is forgotten. One issued no later is still revoked.
// A revoked device holding the account seed could sign itself such a
// certificate, but it could equally certify a fresh device key; see the
// threat model above.
func (s *Service) stillRevoked(cert *identity.DeviceCert) bool {
	at, revoked := s.revokedAt(cert.DevicePub)
	if !revoked {
		return false
	}
	if cert.IssuedAt > at && cert.Verify() && ed25519Equal(cert.AccountPub, s.id.PublicKey()) {
		return !s.forgetRevocation(cert.DevicePub, cert.IssuedAt)
	}
	return true
}

// loadRevoked seeds the in-memory revoked set at startup, and drops anything a
// roster read may already have adopted. Order-independent on purpose: startup
// touches learnDeviceCert from several places and pinning this to being first
// would be one refactor away from silently un-revoking a device.
func (s *Service) loadRevoked() {
	set := map[string]int64{}
	for _, r := range s.revocations() {
		k := hex.EncodeToString(r.DevicePub)
		if at, ok := set[k]; !ok || r.RevokedAt > at {
			set[k] = r.RevokedAt
		}
	}
	s.deviceMu.Lock()
	s.revokedDevices = set
	for k := range set {
		delete(s.deviceAccounts, k)
	}
	s.deviceMu.Unlock()
}

// UnlinkDevice revokes one of this account's devices by its hex device key.
//
// Order matters: the leaf removal is the part that actually holds, so it is
// attempted for every group before anything else, and a failure there is
// reported rather than swallowed. The record and the local forgetting happen
// regardless, since they are what make the revocation reach the device later.
func (s *Service) UnlinkDevice(deviceKeyHex string) error {
	raw, err := hex.DecodeString(deviceKeyHex)
	if err != nil || len(raw) != 32 {
		return fmt.Errorf("app: not a device key")
	}
	if bytes.Equal(raw, []byte(s.id.PublicKey())) {
		return fmt.Errorf("app: this is the account's own key, not a linked device")
	}
	if id, ok := devicePeerID(raw); ok && id == s.host.PeerID() {
		return fmt.Errorf("app: use the account reset to erase the device you are holding")
	}

	// Stamped no earlier than the newest certificate known for the key, so the
	// revocation is never older than the certificate it withdraws, whatever
	// the clocks of the device that issued it and this one said (revokesUs
	// compares the two).
	at := time.Now().Unix()
	if issued := s.newestCertIssuedAt(raw); issued > at {
		at = issued
	}
	rev := s.id.RevokeDevice(raw, at)
	s.revMu.Lock()
	err = s.saveRevocations(append(s.revocations(), rev))
	s.revMu.Unlock()
	if err != nil {
		return fmt.Errorf("app: could not record the unlink: %w", err)
	}

	// Stop treating the key as this account, everywhere local: rosters, voice,
	// diagnostics, and the reconnect loop that would otherwise keep dialling it.
	s.deviceMu.Lock()
	delete(s.deviceAccounts, hex.EncodeToString(raw))
	if s.revokedDevices == nil {
		s.revokedDevices = map[string]int64{}
	}
	if prev, ok := s.revokedDevices[hex.EncodeToString(raw)]; !ok || rev.RevokedAt > prev {
		s.revokedDevices[hex.EncodeToString(raw)] = rev.RevokedAt
	}
	s.deviceMu.Unlock()
	s.markDeviceRevoked(hex.EncodeToString(raw), rev.RevokedAt)

	// The half that holds: drop its leaf wherever we can commit.
	failed := s.removeDeviceLeaves(raw)

	// And tell it, if it happens to be here right now. If it isn't, the hello
	// reply catches it whenever it next connects.
	if id, ok := devicePeerID(raw); ok {
		// It is no longer ours, so it stops being exempt from the trim. Left
		// protected it would outlive a real peer on a phone at its high water
		// mark — a device the user just revoked holding a connection open
		// against the friend they revoked it to protect.
		s.host.UnprotectDevice(id)
		go s.pushRevocation(id, rev)
	}
	if len(failed) > 0 {
		return fmt.Errorf("app: unlinked, but %s still list that device — reopen them when you can commit there", plural(len(failed), "guild", "guilds"))
	}
	return nil
}

// markDeviceRevoked stamps the registry so the device stops being dialled and
// the diagnostics panel can say what happened to it.
func (s *Service) markDeviceRevoked(key string, at int64) {
	s.regMu.Lock()
	defer s.regMu.Unlock()
	recs := s.deviceRegistry()
	for i := range recs {
		if recs[i].Key == key {
			recs[i].Revoked = at
			s.saveDeviceRegistry(recs)
			return
		}
	}
	s.saveDeviceRegistry(append(recs, ownDevice{Key: key, Revoked: at}))
}

// removeDeviceLeaves drops a device's MLS leaf from every guild it is in,
// returning the guilds where we could not. A guild we do not have commit
// authority in is one of those: the device keeps receiving that guild's traffic
// until somebody who can commit removes it, which the caller must say out loud
// rather than report success.
func (s *Service) removeDeviceLeaves(devicePub []byte) []string {
	s.mu.RLock()
	type target struct {
		id      string
		groupID []byte
	}
	guilds := make([]target, 0, len(s.guilds))
	for _, g := range s.guilds {
		guilds = append(guilds, target{g.ID, g.GroupID})
	}
	s.mu.RUnlock()

	var failed []string
	for _, g := range guilds {
		creds, err := s.mls.Members(s.ctx, g.groupID)
		if err != nil {
			failed = append(failed, g.id)
			continue
		}
		for _, c := range creds {
			cert, ok := identity.ParseDeviceCert(c)
			if !ok || !bytes.Equal(cert.DevicePub, devicePub) {
				continue
			}
			commit, err := s.mls.Remove(s.ctx, g.groupID, c)
			if err != nil {
				failed = append(failed, g.id)
				break
			}
			s.logCommit(g.groupID, commit)
			_ = s.ps.Publish(s.ctx, domain.ControlTopicID(g.groupID), commit)
			s.relearnDevices(g.groupID)
			break
		}
	}
	if len(failed) > 0 {
		s.emitGuildUpdate()
	}
	return failed
}

// pushRevocation hands a revocation straight to the device, by greeting it. Our
// hello reply carries the revocation (revocationFor), so this is just "start the
// conversation that ends with it wiping itself".
func (s *Service) pushRevocation(p peer.ID, rev *identity.DeviceRevocation) {
	req, err := json.Marshal(helloFrame{Credential: s.myCredential, Revoked: rev})
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, helloTimeout)
	defer cancel()
	_, _ = s.host.SayHello(ctx, p, req)
}

// OnUnlinked registers a callback fired when THIS device is told it has been
// unlinked and has erased itself. The shell uses it to drop the session and say
// so — without it the app keeps a closed service around and every action fails
// with a database error, which is a confusing way to learn you were unlinked.
func (s *Service) OnUnlinked(fn func()) {
	s.mu.Lock()
	s.onUnlinked = append(s.onUnlinked, fn)
	s.mu.Unlock()
}

// applyRevocation acts on a revocation somebody handed us. It erases this
// install if — and only if — the revocation is signed by OUR account key and
// names OUR device key. Both halves are required: the first means only the
// account holder can trigger it, the second means a revocation for a sibling
// device passes straight through.
func (s *Service) applyRevocation(rev *identity.DeviceRevocation) {
	if !s.revokesUs(rev) {
		return
	}
	// Kept so a wipe that cannot finish is resumed at the next start
	// (selfRevoked). If it cannot be kept, the wipe still runs now; it is only
	// the resumption that is lost, so say so.
	s.revMu.Lock()
	err := s.saveRevocations(append(s.revocations(), rev))
	s.revMu.Unlock()
	if err != nil {
		log.Printf("concord/app: this device was unlinked; the record of it could not be saved (%v), so an erase that does not finish now is not resumed at the next start", err)
	}
	go s.wipeSelf()
}

// revokesUs is the whole safety check for the self-erase, kept in one place and
// deliberately strict.
func (s *Service) revokesUs(rev *identity.DeviceRevocation) bool {
	if rev == nil || !rev.Verify() {
		return false
	}
	if !ed25519Equal(rev.AccountPub, s.id.PublicKey()) {
		return false
	}
	// Our device key is the key our libp2p host runs on. An install that was
	// never linked has no device key of its own — its host key IS the account
	// key — and revoking the account key is not something this path will ever do.
	dev := s.id.DevicePublicKey()
	if len(dev) == 0 {
		return false
	}
	m, linked := loadDeviceMarker(s.dataDir, s.id.PublicKey(), dev)
	if !linked {
		return false
	}
	// A revocation is about the certificate the device held when it was
	// issued. A device unlinked while it was away and then linked again keeping
	// its device key holds a newer certificate, which that old revocation does
	// not withdraw. Both times are stamped by devices of this account; in
	// doubt, this errs toward not erasing.
	if rev.RevokedAt < m.Cert.IssuedAt {
		return false
	}
	return bytes.Equal(rev.DevicePub, dev)
}

// forgetRevocation drops what this device holds against one device key
// from before a certificate issued at `issued`: the stored revocations (so
// none is handed to that device at its hello any more), the in-memory revoked
// mark and the registry's revoked stamp. Called only when the device shows a
// certificate newer than the revocation (stillRevoked). It reports whether
// the key is no longer revoked; if the stored list cannot be saved nothing is
// forgotten, so a restart cannot bring back a revocation the memory dropped.
func (s *Service) forgetRevocation(devicePub []byte, issued int64) bool {
	s.revMu.Lock()
	recs := s.revocations()
	kept := make([]*identity.DeviceRevocation, 0, len(recs))
	newest, still := int64(0), false
	for _, r := range recs {
		if bytes.Equal(r.DevicePub, devicePub) {
			if r.RevokedAt < issued {
				continue
			}
			if !still || r.RevokedAt > newest {
				newest, still = r.RevokedAt, true
			}
		}
		kept = append(kept, r)
	}
	if len(kept) != len(recs) {
		if err := s.saveRevocations(kept); err != nil {
			s.revMu.Unlock()
			log.Printf("concord/app: a re-linked device's old unlink could not be cleared: %v", err)
			return false
		}
	}
	key := hex.EncodeToString(devicePub)
	s.deviceMu.Lock()
	if still {
		s.revokedDevices[key] = newest
	} else {
		delete(s.revokedDevices, key)
	}
	s.deviceMu.Unlock()
	s.revMu.Unlock()
	if still {
		return false
	}
	s.regMu.Lock()
	defer s.regMu.Unlock()
	recsReg := s.deviceRegistry()
	for i := range recsReg {
		if recsReg[i].Key == key && recsReg[i].Revoked != 0 {
			recsReg[i].Revoked = 0
			s.saveDeviceRegistry(recsReg)
			break
		}
	}
	return true
}

// newestCertIssuedAt is the IssuedAt of the newest certificate this device
// knows for a device key (0 if none).
func (s *Service) newestCertIssuedAt(devicePub []byte) int64 {
	key := hex.EncodeToString(devicePub)
	s.regMu.Lock()
	defer s.regMu.Unlock()
	for _, r := range s.deviceRegistry() {
		if r.Key == key && r.Cert != nil {
			return r.Cert.IssuedAt
		}
	}
	return 0
}

// selfRevoked reports (at startup) that this device is already known to be
// unlinked — because a previous session was told and the erase did not finish,
// or because the record survived. Cheap and local; the network path is
// applyRevocation.
func (s *Service) selfRevoked() bool {
	for _, r := range s.revocations() {
		if s.revokesUs(r) {
			return true
		}
	}
	return false
}

// wipeSelf erases everything this install holds and stops it.
//
// It removes the device marker along with the keystore and database, and the
// marker last (resetDataIn): an erase that stops part-way leaves an install
// that still reads as linked, so the next start finds the revocation still
// applies (selfRevoked, before anything is served) and finishes the erase,
// instead of opening a revoked device as an ordinary install. The network config
// is deliberately KEPT — it is not account data, and leaving it means the person
// holding the device can re-link it (a fresh, freshly-authorized device) without
// having to find their rendezvous address again.
func (s *Service) wipeSelf() {
	dir := s.dataDir
	s.mu.RLock()
	cbs := append([]func(){}, s.onUnlinked...)
	s.mu.RUnlock()

	s.Close()
	if err := ResetIdentity(dir); errors.Is(err, identity.ErrEraseDeferred) {
		log.Printf("concord/app: this device was unlinked from its account; local data erased, and the keystore's removal completes at the next unlock")
	} else if err != nil {
		log.Printf("concord/app: this device was unlinked from its account, but erasing it failed: %v", err)
	} else {
		log.Printf("concord/app: this device was unlinked from its account; local data erased")
	}
	for _, cb := range cbs {
		cb()
	}
}

// plural counts a thing in a user-facing sentence. It exists because the one
// place that needed it wrote "%d server(s)" instead — the only "(s)" in the
// product, and a shape that reads like a form letter rather than a sentence
// somebody wrote.
func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
