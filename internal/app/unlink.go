package app

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
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

func (s *Service) saveRevocations(recs []*identity.DeviceRevocation) {
	if raw, err := json.Marshal(recs); err == nil {
		_ = s.store.SetSetting(deviceRevocationsKey, string(raw))
	}
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
	s.deviceMu.RLock()
	defer s.deviceMu.RUnlock()
	return s.revokedDevices[hex.EncodeToString(devicePub)]
}

// loadRevoked seeds the in-memory revoked set at startup, and drops anything a
// roster read may already have adopted. Order-independent on purpose: startup
// touches learnDeviceCert from several places and pinning this to being first
// would be one refactor away from silently un-revoking a device.
func (s *Service) loadRevoked() {
	set := map[string]bool{}
	for _, r := range s.revocations() {
		set[hex.EncodeToString(r.DevicePub)] = true
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

	rev := s.id.RevokeDevice(raw, time.Now().Unix())
	s.saveRevocations(append(s.revocations(), rev))

	// Stop treating the key as this account, everywhere local: rosters, voice,
	// diagnostics, and the reconnect loop that would otherwise keep dialling it.
	s.deviceMu.Lock()
	delete(s.deviceAccounts, hex.EncodeToString(raw))
	if s.revokedDevices == nil {
		s.revokedDevices = map[string]bool{}
	}
	s.revokedDevices[hex.EncodeToString(raw)] = true
	s.deviceMu.Unlock()
	s.markDeviceRevoked(hex.EncodeToString(raw), rev.RevokedAt)

	// The half that holds: drop its leaf wherever we can commit.
	failed, pending := s.removeDeviceLeaves(raw)

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
	if len(failed) > 0 || len(pending) > 0 {
		return &UnlinkIncompleteError{Failed: failed, Pending: pending}
	}
	return nil
}

// UnlinkIncompleteError: the device is unlinked (revoked, told when it next
// connects, no longer treated as this account), but some guilds still hold
// its leaf, so it keeps receiving their traffic until the leaf goes.
//
// Pending are guilds and DMs where this device may not remove a member: only
// their owner or a manage-members holder can, and honest members refuse a
// removal commit from anyone else. Committing it anyway advanced only this
// device's own copy of the group and cut it out of the conversation, both
// ways and silently; so it is not attempted. Failed are guilds where this
// device could have committed but the removal failed.
type UnlinkIncompleteError struct {
	Failed  []string // guild names
	Pending []string // guild names
}

func (e *UnlinkIncompleteError) Error() string {
	var parts []string
	if len(e.Pending) > 0 {
		parts = append(parts, fmt.Sprintf("%s still list that device until an owner or moderator there removes it (%s)",
			plural(len(e.Pending), "guild", "guilds"), strings.Join(e.Pending, ", ")))
	}
	if len(e.Failed) > 0 {
		parts = append(parts, fmt.Sprintf("removing it from %s failed (%s); unlink it again to retry",
			plural(len(e.Failed), "guild", "guilds"), strings.Join(e.Failed, ", ")))
	}
	return "app: unlinked, but " + strings.Join(parts, "; ")
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

// removeDeviceLeaves drops a device's MLS leaf from every guild it is in where
// this device may commit membership changes, returning the guilds where that
// failed and those where it may not (pending). In a pending guild the removal
// is not attempted: honest members apply a membership commit only from the
// owner or a manage-members holder (authorizedCommitter), so a commit from
// anyone else advances only the committer's own copy of the group, and it can
// then neither read nor be read by the rest. The device keeps that guild's
// traffic until somebody who can commit removes it, which the caller must say
// out loud rather than report success.
func (s *Service) removeDeviceLeaves(devicePub []byte) (failed, pending []string) {
	s.mu.RLock()
	type target struct {
		id, name string
		groupID  []byte
	}
	guilds := make([]target, 0, len(s.guilds))
	for _, g := range s.guilds {
		guilds = append(guilds, target{g.ID, g.Name, g.GroupID})
	}
	s.mu.RUnlock()

	for _, g := range guilds {
		creds, err := s.mls.Members(s.ctx, g.groupID)
		if err != nil {
			failed = append(failed, g.name)
			continue
		}
		for _, c := range creds {
			cert, ok := identity.ParseDeviceCert(c)
			if !ok || !bytes.Equal(cert.DevicePub, devicePub) {
				continue
			}
			// The same rule the other members apply to our commit.
			if !s.authorizedCommitter(g.id, s.myCredential) {
				pending = append(pending, g.name)
				break
			}
			commit, err := s.mls.Remove(s.ctx, g.groupID, c)
			if err != nil {
				failed = append(failed, g.name)
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
	return failed, pending
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
	s.saveRevocations(append(s.revocations(), rev))
	go func() { _ = s.wipeSelf() }()
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
	if _, linked := loadDeviceMarker(s.dataDir, s.id.PublicKey(), dev); !linked {
		return false
	}
	return bytes.Equal(rev.DevicePub, dev)
}

// selfRevoked reports (at startup) that this device is already known to be
// unlinked — because a previous session was told and the erase did not finish,
// or because the record survived. Cheap and local; the network path is
// applyRevocation.
func (s *Service) selfRevoked() bool {
	for _, r := range s.revocations() {
		if s.revokesUs(r) || s.revokedWithoutMarker(r) {
			return true
		}
	}
	return false
}

// revokedWithoutMarker reports a revocation this account signed for this
// install's own device key, stored here, on an install with no device marker
// at all: a linked device whose erase removed the marker and stopped before
// the rest. It is that device, and the start erases it.
func (s *Service) revokedWithoutMarker(rev *identity.DeviceRevocation) bool {
	if rev == nil || !rev.Verify() || !ed25519Equal(rev.AccountPub, s.id.PublicKey()) {
		return false
	}
	dev := s.id.DevicePublicKey()
	if len(dev) == 0 || !bytes.Equal(rev.DevicePub, dev) {
		return false
	}
	_, err := os.Lstat(deviceMarkerPath(s.dataDir))
	return os.IsNotExist(err)
}

// wipeSelf erases everything this install holds and stops it.
//
// The erase is recorded before anything goes (resetIdentity), so one stopped
// part-way is finished by the next start before anything is opened. It runs
// in the session's erase gate: Close waits for an erase in flight, and once
// Close has run no erase of this session starts (the stored revocation has
// the next start erase the device). The shell is told only once the erase
// is done, and the log says erased only when it was.
func (s *Service) wipeSelf() error {
	dir := s.dataDir
	s.mu.RLock()
	cbs := append([]func(){}, s.onUnlinked...)
	s.mu.RUnlock()

	g := s.erase
	if !g.enter() {
		log.Printf("concord/app: %v", errClosedBeforeErase)
		return errClosedBeforeErase
	}
	s.closeSession()
	err := resetIdentity(dir, true)
	g.leave()
	if err != nil {
		err = fmt.Errorf("%w: %w. %s", ErrDeviceUnlinkedNotErased, err, eraseHeldAdvice)
		log.Printf("concord/app: %v", err)
	} else {
		log.Printf("concord/app: this device was unlinked from its account; local data erased")
	}
	for _, cb := range cbs {
		cb()
	}
	return err
}

// ErrDeviceUnlinkedNotErased: this device was unlinked from its account and
// erasing it did not finish; the error names what is still here, and the
// next start tries again before it opens anything.
var ErrDeviceUnlinkedNotErased = errors.New("app: this device has been unlinked from the account, but erasing it did not finish")

// errClosedBeforeErase: the session was closed before its self-erase began.
// Nothing was erased now; the revocation is stored, and the next start
// erases the device before it opens anything.
var errClosedBeforeErase = fmt.Errorf("%w; the app closed first, and the next start erases it", ErrDeviceUnlinkedNotErased)

// eraseGate ties a session's self-erase to its Close. An erase attempt holds
// the gate while it removes anything; Close shuts it, so no attempt starts
// after, and waits (at most closeEraseWait) for an attempt in flight. One
// still running then is logged and left: its record has the next start
// finish it. A nil gate (a Service not made by Start) never shuts.
type eraseGate struct {
	mu   sync.Mutex
	shut bool          // under mu
	done chan struct{} // closed when the gate shuts
	busy chan struct{} // holds one token while an attempt runs
}

// closeEraseWait bounds how long Close waits for an erase in flight. A
// variable for tests.
var closeEraseWait = 30 * time.Second

func newEraseGate() *eraseGate {
	return &eraseGate{done: make(chan struct{}), busy: make(chan struct{}, 1)}
}

func (g *eraseGate) isShut() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.shut
}

// enter takes the gate for one erase attempt; false once it is shut.
func (g *eraseGate) enter() bool {
	if g == nil {
		return true
	}
	if g.isShut() {
		return false
	}
	select {
	case g.busy <- struct{}{}:
	case <-g.done:
		return false
	}
	if g.isShut() {
		<-g.busy
		return false
	}
	return true
}

func (g *eraseGate) leave() {
	if g != nil {
		<-g.busy
	}
}

// shutAfterErase shuts the gate, then waits for an attempt in flight, at
// most closeEraseWait.
func (g *eraseGate) shutAfterErase() {
	if g == nil {
		return
	}
	g.mu.Lock()
	if !g.shut {
		g.shut = true
		close(g.done)
	}
	g.mu.Unlock()
	t := time.NewTimer(closeEraseWait)
	defer t.Stop()
	select {
	case g.busy <- struct{}{}:
		<-g.busy
	case <-t.C:
		log.Printf("concord/app: closing while this device's erase is still running; the next start finishes it")
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
