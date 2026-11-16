package app

import (
	"bytes"
	"cmp"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"slices"
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
// connects, no longer treated as this account), but some guilds or direct
// messages still hold its leaf, so it keeps receiving their new messages
// until the leaf goes.
//
// Pending are conversations where this device may not remove a member: only
// their owner or a manage-members holder can (for a direct message, its
// owner: the person who started it), and honest members refuse a removal
// commit from anyone else. Committing it anyway advanced only this device's
// own copy of the group and cut it out of the conversation, both ways and
// silently; so it is not attempted. Failed are conversations where this
// device could have committed but the removal failed.
type UnlinkIncompleteError struct {
	Failed  []UnlinkSpot
	Pending []UnlinkSpot
}

// UnlinkSpot names one guild or direct message an unlink could not finish.
type UnlinkSpot struct {
	Kind string `json:"kind"` // "guild" or "dm"
	ID   string `json:"id"`
	// Name is the guild's name, or for a direct message the other people in
	// it, by display name.
	Name string `json:"name"`
	// RemovableBy is who can remove the leaf there: for a direct message the
	// display name of the person who started it; empty for a guild (its owner
	// or a moderator).
	RemovableBy string `json:"removableBy,omitempty"`
}

func spotNames(spots []UnlinkSpot) string {
	names := make([]string, len(spots))
	for i, sp := range spots {
		if sp.Kind == "dm" {
			names[i] = "direct message with " + sp.Name
		} else {
			names[i] = sp.Name
		}
	}
	return strings.Join(names, ", ")
}

func (e *UnlinkIncompleteError) Error() string {
	var parts []string
	if len(e.Pending) > 0 {
		parts = append(parts, fmt.Sprintf("that device stays listed in %s until someone there who can remove members does so (%s)",
			plural(len(e.Pending), "conversation", "conversations"), spotNames(e.Pending)))
	}
	if len(e.Failed) > 0 {
		parts = append(parts, fmt.Sprintf("removing it from %s failed (%s); unlink it again to retry",
			plural(len(e.Failed), "conversation", "conversations"), spotNames(e.Failed)))
	}
	return "app: unlinked, but " + strings.Join(parts, "; ")
}

// unlinkSpot describes a guild or direct message for an unlink report.
func (s *Service) unlinkSpot(id, name, kind string, groupID []byte) UnlinkSpot {
	sp := UnlinkSpot{Kind: "guild", ID: id, Name: name}
	if kind != "dm" {
		return sp
	}
	sp.Kind = "dm"
	nameOf := func(fpr string) string {
		if n := strings.TrimSpace(s.ProfileOf(fpr).Name); n != "" {
			return n
		}
		if len(fpr) > 8 {
			return fpr[:8]
		}
		return fpr
	}
	if others, ok := s.dmOtherAccounts(groupID); ok && len(others) > 0 {
		names := make([]string, len(others))
		for i, f := range others {
			names[i] = nameOf(f)
		}
		sp.Name = strings.Join(names, ", ")
	}
	if owner := s.effectiveOwner(id); owner != "" {
		sp.RemovableBy = nameOf(owner)
	}
	return sp
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
func (s *Service) removeDeviceLeaves(devicePub []byte) (failed, pending []UnlinkSpot) {
	s.mu.RLock()
	type target struct {
		id, name, kind string
		groupID        []byte
	}
	guilds := make([]target, 0, len(s.guilds))
	for _, g := range s.guilds {
		guilds = append(guilds, target{g.ID, g.Name, g.Kind, g.GroupID})
	}
	s.mu.RUnlock()

	for _, g := range guilds {
		creds, err := s.mls.Members(s.ctx, g.groupID)
		if err != nil {
			failed = append(failed, s.unlinkSpot(g.id, g.name, g.kind, g.groupID))
			continue
		}
		for _, c := range creds {
			cert, ok := identity.ParseDeviceCert(c)
			if !ok || !bytes.Equal(cert.DevicePub, devicePub) {
				continue
			}
			// The same rule the other members apply to our commit.
			if !s.authorizedCommitter(g.id, s.myCredential) || s.mintBarred(g.id) != nil {
				pending = append(pending, s.unlinkSpot(g.id, g.name, g.kind, g.groupID))
				break
			}
			commit, err := s.mls.Remove(s.ctx, g.groupID, c)
			if err != nil {
				failed = append(failed, s.unlinkSpot(g.id, g.name, g.kind, g.groupID))
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
	sortUnlinkSpots(failed)
	sortUnlinkSpots(pending)
	return failed, pending
}

// sortUnlinkSpots puts a report in the same order every time: guilds before
// direct messages, then by name, then by id.
func sortUnlinkSpots(spots []UnlinkSpot) {
	slices.SortFunc(spots, func(a, b UnlinkSpot) int {
		return cmp.Or(
			cmp.Compare(b.Kind, a.Kind), // "guild" before "dm"
			cmp.Compare(a.Name, b.Name),
			cmp.Compare(a.ID, b.ID),
		)
	})
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
// unlinked and has tried to erase itself. The shell uses it to drop the session
// and say so — without it the app keeps a closed service around and every
// action fails with a database error, which is a confusing way to learn you
// were unlinked. err is nil when the erase completed, and otherwise
// ErrDeviceUnlinkedNotErased naming what is still here (with
// ErrDeviceUnlinkedReplaced when another account is here now): the shell must
// not say the device was erased when it was not. An erase that did not finish
// is retried in the background, and fn is called again with how that ended.
func (s *Service) OnUnlinked(fn func(err error)) {
	s.mu.Lock()
	s.onUnlinked = append(s.onUnlinked, fn)
	s.mu.Unlock()
}

// applyRevocation acts on a revocation somebody handed us. It erases this
// install if — and only if — the revocation is signed by OUR account key and
// names OUR device key. Both halves are required: the first means only the
// account holder can trigger it, the second means a revocation for a sibling
// device passes straight through.
//
// A revocation of this device key, signed by this account, on an install
// whose device marker cannot be read now is acted on too: whether it is for
// this install cannot be told, and the erase checks again, under the lock,
// before anything goes (thisDeviceHere).
func (s *Service) applyRevocation(rev *identity.DeviceRevocation) {
	if ok, err := s.revocationApplies(rev); !ok && err == nil {
		return
	}
	s.saveRevocations(append(s.revocations(), rev))
	go func() { _ = s.wipeSelf() }()
}

// revokesUs is the whole safety check for the self-erase, kept in one place and
// deliberately strict.
func (s *Service) revokesUs(rev *identity.DeviceRevocation) bool {
	ok, err := s.revocationApplies(rev)
	return ok && err == nil
}

// revocationApplies is revokesUs that also says when it cannot tell: the
// revocation is this account's, for this device key, and the device marker
// that says whether this install is that linked device cannot be read (err).
// That is not "not linked", and a caller must not treat this install as
// unrevoked on the strength of it.
func (s *Service) revocationApplies(rev *identity.DeviceRevocation) (bool, error) {
	if rev == nil || !rev.Verify() {
		return false, nil
	}
	if !ed25519Equal(rev.AccountPub, s.id.PublicKey()) {
		return false, nil
	}
	// Our device key is the key our libp2p host runs on. An install that was
	// never linked has no device key of its own — its host key IS the account
	// key — and revoking the account key is not something this path will ever do.
	dev := s.id.DevicePublicKey()
	if len(dev) == 0 || !bytes.Equal(rev.DevicePub, dev) {
		return false, nil
	}
	_, linked, err := readDeviceMarker(s.dataDir, s.id.PublicKey(), dev)
	if err != nil {
		return false, err
	}
	return linked, nil
}

// selfRevoked reports (at startup) that this device is already known to be
// unlinked — because a previous session was told and the erase did not finish,
// or because the record survived. Cheap and local; the network path is
// applyRevocation. err: a revocation of this device key is stored, and the
// device marker cannot be read to tell whether it applies.
func (s *Service) selfRevoked() (bool, error) {
	var cannotTell error
	for _, r := range s.revocations() {
		ok, err := s.revocationApplies(r)
		if ok {
			return true, nil
		}
		if err != nil {
			cannotTell = err
		}
	}
	return false, cannotTell
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
//
// The revocation was for one account's device, and the erase is checked
// against that under the keystore lock, with it (thisDeviceHere): another
// window or process may have restored or linked an account here since, and
// that is not erased (ErrDeviceUnlinkedReplaced). An erase that could not be
// done or checked now (the lock busy, the marker unreadable, a file held) is
// tried again in the background, checked again each time, and the shell is
// told when it finishes; the next start, which runs this before anything is
// served, is the fallback.
//
// The erase runs in the session's erase gate: Close waits for an erase in
// flight, and once Close has run no erase of this session starts or is tried
// again (errClosedBeforeErase); the stored revocation, or the erase record of
// one that began, has the next start finish it.
func (s *Service) wipeSelf() error {
	dir := s.dataDir
	account, device := s.id.PublicKey(), s.id.DevicePublicKey()
	s.mu.RLock()
	cbs := append([]func(error){}, s.onUnlinked...)
	s.mu.RUnlock()

	g := s.erase
	var err error
	if !g.enter() {
		err = errClosedBeforeErase
	} else {
		s.closeSession()
		err = eraseRevokedDevice(dir, account, device)
		g.leave()
	}
	tell(cbs, err)
	if err != nil && !errors.Is(err, ErrDeviceUnlinkedReplaced) && !errors.Is(err, errClosedBeforeErase) {
		retryEraseRevokedDevice(g, dir, account, device, cbs)
	}
	return err
}

// errClosedBeforeErase: the session was closed before its self-erase began.
// Nothing was erased now; the revocation is stored, and the next start
// erases the device before it opens anything.
var errClosedBeforeErase = fmt.Errorf("%w; the app closed first, and the next start erases it", ErrDeviceUnlinkedNotErased)

// eraseGate ties a session's self-erase to its Close. An erase attempt holds
// the gate while it removes anything; Close shuts it, so no attempt starts
// after, and waits (at most closeEraseWait) for an attempt in flight. One
// still running then is logged and left: its record has the next start
// finish it. Kept apart from the Service so a retry loop does not keep the
// session (and the keys it holds) in memory. A nil gate (a Service not made
// by Start) never shuts.
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

// wait sleeps for d, or until the gate shuts; false when it shut.
func (g *eraseGate) wait(d time.Duration) bool {
	if g == nil {
		time.Sleep(d)
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-g.done:
		return false
	}
}

// tell logs how a self-erase went and tells the shell.
func tell(cbs []func(error), err error) {
	if err != nil {
		log.Printf("concord/app: %v", err)
	} else {
		log.Printf("concord/app: this device was unlinked from its account; local data erased")
	}
	for _, cb := range cbs {
		cb(err)
	}
}

// eraseRevokedDevice is one attempt at the self-erase of the device whose
// account and device keys are given, checked under the keystore lock.
func eraseRevokedDevice(dir string, account, device ed25519.PublicKey) error {
	var why error
	err := resetIdentityIf(dir, true, func() bool {
		why = thisDeviceHere(dir, account, device)
		return why == nil
	}, nil)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrDataNotErased):
		// Names what is still here.
		return fmt.Errorf("%w: %w. %s", ErrDeviceUnlinkedNotErased, err, eraseHeldAdvice)
	case errors.Is(err, identity.ErrKeystoreChanged) && why != nil:
		// Another account, or a device linked again (ErrDeviceUnlinkedReplaced),
		// or a marker that cannot be read now.
		return fmt.Errorf("%w; %w", ErrDeviceUnlinkedNotErased, why)
	case errors.Is(err, identity.ErrKeystoreBusy):
		return fmt.Errorf("%w; nothing was erased yet, and it is tried again: %w", ErrDeviceUnlinkedNotErased, err)
	default:
		// The erase stops at the keystore: everything is still here.
		return fmt.Errorf("%w; nothing was erased yet: %w", ErrDeviceUnlinkedNotErased, err)
	}
}

// thisDeviceHere reports, for the self-erase and under the keystore lock,
// whether dataDir still holds the device the revocation named: nil when the
// device marker certifies this account and this device key (revokesUs
// checked the same) and the keystore names no other account.
//
// ErrDeviceUnlinkedReplaced when it does not: every restore or link onto a
// linked install replaces or removes the marker, so one that landed since is
// seen, and a keystore naming another account is another account's. Any
// other error when it cannot be told now (the marker or the keystore cannot
// be read): that is worth trying again, and is not a refusal.
//
// A keystore that names no account in the clear (written before builds
// recorded it), one that cannot be parsed (it names no one), and none at all
// (an erase that stopped part-way) leave the marker to decide. No keystore
// and no account data at all is a device already erased (by a start that
// ran the erase first, say): nil, and the erase has nothing left to do.
func thisDeviceHere(dataDir string, account, device ed25519.PublicKey) error {
	pub, err := identity.ReadAccountPub(keystorePathIn(dataDir))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if !hasAccountData(dataDir) {
			return nil
		}
	case errors.Is(err, identity.ErrDamagedKeystore):
	case err != nil:
		return fmt.Errorf("nothing was erased yet: the keystore cannot be read, so whose it is cannot be told: %w", err)
	case pub != nil && !ed25519Equal(pub, account):
		return ErrDeviceUnlinkedReplaced
	}
	_, linked, err := readDeviceMarker(dataDir, account, device)
	if err != nil {
		return fmt.Errorf("nothing was erased yet: the device marker cannot be read, so whether this is still the unlinked device cannot be told: %w", err)
	}
	if !linked {
		return ErrDeviceUnlinkedReplaced
	}
	return nil
}

// retryEraseRevokedDevice keeps trying a self-erase that did not finish, in
// the background (see selfEraseRetryPause), one loop per data directory. It
// stops when an attempt erases the device, when the device turns out to hold
// another account, or when it finds the device already erased (by the next
// start, say); the shell is told each of those. Giving up is safe: the
// revocation is stored, and the next start erases before it serves anything.
//
// The loop belongs to the session's erase gate g: an attempt holds the gate,
// and the loop ends without another attempt once the session is closed.
func retryEraseRevokedDevice(g *eraseGate, dir string, account, device ed25519.PublicKey, cbs []func(error)) {
	fast, pause, slow, slowPause := selfEraseRetryAttempts, selfEraseRetryPause, selfEraseSlowAttempts, selfEraseSlowPause
	if fast+slow == 0 {
		return
	}
	if _, running := selfErasing.LoadOrStore(dir, struct{}{}); running {
		return
	}
	selfErasers.Add(1)
	go func() {
		defer selfErasers.Done()
		defer selfErasing.Delete(dir)
		for i := 0; i < fast+slow; i++ {
			d := pause
			if i >= fast {
				d = slowPause
			}
			if !g.wait(d) || !g.enter() {
				return // closed: the next start finishes the erase
			}
			if !HasIdentity(dir) && !hasAccountData(dir) && !erasePendingIn(dir) {
				g.leave()
				tell(cbs, nil) // erased meanwhile
				return
			}
			err := eraseRevokedDevice(dir, account, device)
			g.leave()
			if err == nil || errors.Is(err, ErrDeviceUnlinkedReplaced) {
				tell(cbs, err)
				return
			}
		}
	}()
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

// ErrDeviceUnlinkedNotErased is returned by Start when this install finds a
// valid revocation for its own device key and could not finish erasing
// itself. The error names what is still here; the next start tries again.
var ErrDeviceUnlinkedNotErased = errors.New("app: this device has been unlinked from the account, but erasing it did not finish")

// ErrDeviceUnlinkedReplaced: this device was unlinked, but by the time it
// came to erase itself another account had been restored or linked here (or
// it had been linked again with a new device key). That is not what the
// revocation named, so nothing was erased, and nothing will be on its
// account. Reported together with ErrDeviceUnlinkedNotErased.
var ErrDeviceUnlinkedReplaced = errors.New("app: nothing was erased: this device now holds another account, or was linked again, since the unlink arrived")

// Retrying a self-erase that could not finish (the keystore busy, the device
// marker unreadable, a file held): every selfEraseRetryPause for
// selfEraseRetryAttempts tries, then every selfEraseSlowPause for
// selfEraseSlowAttempts more, each try checked again under the keystore
// lock. The next start remains the fallback. Variables for tests.
var (
	selfEraseRetryPause    = 5 * time.Second
	selfEraseRetryAttempts = 60 // five minutes
	selfEraseSlowPause     = 5 * time.Minute
	selfEraseSlowAttempts  = 288          // a day
	selfErasing            sync.Map       // data dir -> struct{}: one retry loop each
	selfErasers            sync.WaitGroup // the running loops, so a test can wait for them
)
