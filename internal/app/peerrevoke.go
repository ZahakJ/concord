package app

import (
	"bytes"
	"cmp"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"slices"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ZahakJ/concord/internal/identity"
)

// Other accounts' revocations.
//
// An account that unlinks a device signs a DeviceRevocation (unlink.go). Until
// 0.63 only that account's own devices held it, so a device the account had
// unlinked was still recognised as the account by everyone else: a moderator's
// heal door admitted it (handleInviteRequest), and a moderator served it the
// owner's commit chain when the account owned the guild (serveOwnerChain
// resolves the requester through the device mapping). See
// docs/dev/owner-rejoin.md, open item 1.
//
// Now each device hands the revocations its account signed to every peer it
// greets or answers (helloFrame.AccountRevoked). The receiver keeps one only
// when it verifies under the account key AND the peer that carried it is
// placed as that same account, so a peer can deliver only its own account's
// word; nobody relays anybody else's. A kept revocation means:
//
//   - learnDeviceCert never maps that device key to the account, and drops a
//     mapping it already had, so presence() no longer places the device, and
//     everything gated on placing it (the owner chain, catch-up, the hello's
//     own answer) treats it as a stranger;
//   - the heal door refuses its credential (leafRevoked).
//
// What it does not change: the revoked device, if it is in hostile hands,
// holds the account seed (unlink.go), so it can certify a fresh device key and
// be admitted under that one. Only rotating the account key ends that. And the
// seed holder can equally sign revocations for the account's other devices,
// which other accounts would now honour at their doors; that is no new power,
// since the same seed can already remove those devices' leaves wherever the
// account commits.
// Both last until account rotation exists. Records are keyed by the signing
// account key (unlink.go drops non-current signers), so a rotation must carry
// the old key's revocations over, or unlinked devices come back.
//
// Who may hand us revocations at all: only an account that shares a guild
// with us (sharesGuild). Doors, the owner chain and catch-up are about guild
// members, so an account we share no guild with has nothing to withdraw here,
// and a stranger who mints identities cannot make us store anything.
//
// The store holds what we kept, verified once on the way in; a start verifies
// it once more, and nothing after that re-verifies a record we already hold.
// Caps refuse, never evict: a kept revocation stays kept, so an account cannot
// push its own older (or another account's) record out by sending new ones,
// and what memory holds is what the store holds. A failed read never turns
// into an empty set that the next write would make permanent.
//
// The keys are (account, device) pairs: a revocation is the account's word
// about its own device and nobody else's, so an account cannot shut another
// account's device out by naming its key.

// peerRevocationsKey persists the revocations other accounts have shown us.
const peerRevocationsKey = "peer.revocations"

// peerRevocationsPerAccount bounds what one account can make us store. An
// account unlinks a handful of devices in its life; a flood is someone
// minting keys to fill our settings. At the cap a record newer (by its
// RevokedAt) than the account's oldest replaces it: the account's own word
// about its own devices, so only it can push its records out.
const peerRevocationsPerAccount = 16

// peerRevocationsTotal bounds the whole set (about 250 bytes a record). Only
// accounts we share a guild with hold places in it: at the cap, records of
// accounts we no longer share one with go first, and they are dropped at
// start (prunePeerRevocations). Throwaway accounts that join and leave cannot
// keep the set full.
const peerRevocationsTotal = 2048

// helloRevocationsMax is the most revocations one hello carries: about 250
// bytes each, well inside the 64 KiB frame. With more than that, the newest
// half always go and the other half walk through the rest hello by hello.
const helloRevocationsMax = 32

func peerRevKey(account, device []byte) string {
	return hex.EncodeToString(account) + ":" + hex.EncodeToString(device)
}

// readPeerRevocations reads the stored revocations of other accounts, keeping
// those that still verify. An error (a failed read, a value that does not
// parse) is returned as such, never as an empty set.
func (s *Service) readPeerRevocations() ([]*identity.DeviceRevocation, error) {
	raw, err := s.store.GetSetting(peerRevocationsKey)
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return nil, nil
	}
	var recs []*identity.DeviceRevocation
	if err := json.Unmarshal([]byte(raw), &recs); err != nil {
		return nil, fmt.Errorf("parse %s: %w", peerRevocationsKey, err)
	}
	out := recs[:0]
	for _, r := range recs {
		if peerRevocationValid(r) && (s.id == nil || !ed25519Equal(r.AccountPub, s.id.PublicKey())) {
			out = append(out, r)
		}
	}
	return out, nil
}

// peerRevocationValid is a revocation that verifies and withdraws a device
// key, not the account key itself (an original device's credential is the
// bare account key; withdrawing it is a key rotation, not an unlink).
func peerRevocationValid(r *identity.DeviceRevocation) bool {
	return r != nil && r.Verify() && !bytes.Equal(r.AccountPub, r.DevicePub)
}

// loadPeerRevoked reads the kept set at startup, seeds memory, and drops any
// mapping a roster read may already have made for a revoked device. When the
// read fails the set stays unloaded: new records are honoured in memory but
// nothing is written, so the stored set is not overwritten by a partial one.
func (s *Service) loadPeerRevoked() {
	recs, err := s.readPeerRevocations()
	s.peerRevMu.Lock()
	defer s.peerRevMu.Unlock()
	if err != nil {
		log.Printf("concord/app: other accounts' unlinked devices: %v; keeping them in memory only until a restart reads them", err)
		s.peerRevLoaded = false
	} else {
		s.peerRevLoaded = true
	}
	s.peerRevRecs = recs
	s.deviceMu.Lock()
	s.peerRevoked = map[string]bool{}
	for _, r := range recs {
		s.forgetPeerDeviceLocked(r)
	}
	s.deviceMu.Unlock()
}

// forgetPeerDeviceLocked records a revocation in memory and drops the device's
// mapping to that account. deviceMu must be held.
func (s *Service) forgetPeerDeviceLocked(r *identity.DeviceRevocation) {
	if s.peerRevoked == nil {
		s.peerRevoked = map[string]bool{}
	}
	s.peerRevoked[peerRevKey(r.AccountPub, r.DevicePub)] = true
	key := hex.EncodeToString(r.DevicePub)
	if s.deviceAccounts[key] == identity.FingerprintOf(r.AccountPub) {
		delete(s.deviceAccounts, key)
	}
}

// certRevoked reports whether a verified device certificate names a device its
// account has unlinked: ours from our own record, another account's from what
// that account showed us.
func (s *Service) certRevoked(cert *identity.DeviceCert) bool {
	if s.id != nil && ed25519Equal(cert.AccountPub, s.id.PublicKey()) {
		return s.deviceIsRevoked(cert.DevicePub)
	}
	s.deviceMu.RLock()
	defer s.deviceMu.RUnlock()
	return s.peerRevoked[peerRevKey(cert.AccountPub, cert.DevicePub)]
}

// leafRevoked is leafRevokedHere for any account: a credential that is a
// device certificate whose account has unlinked that device, as far as this
// device knows.
func (s *Service) leafRevoked(cred []byte) bool {
	cert, ok := identity.ParseDeviceCert(cred)
	if !ok || !cert.Verify() {
		return false
	}
	return s.certRevoked(cert)
}

// revocationsToShare is what we hand a peer we can place: our account's own
// revocations, at most helloRevocationsMax. Nothing to a peer we cannot place
// (a stranger learns nothing of our devices), and nothing to a device we
// unlinked, which gets its own record in Revoked instead.
func (s *Service) revocationsToShare(p peer.ID) []*identity.DeviceRevocation {
	recs := s.revocations()
	if len(recs) == 0 || !s.canPlace(p) || s.revocationFor(p) != nil {
		return nil
	}
	return s.shareWindow(recs)
}

// shareWindow picks which of our revocations one hello carries: all of them
// when they fit; otherwise the newest half of a hello's worth, and a window
// over the older ones that moves on with every hello, so an old unlink still
// reaches a member who has not met us since.
func (s *Service) shareWindow(recs []*identity.DeviceRevocation) []*identity.DeviceRevocation {
	recs = slices.Clone(recs)
	slices.SortStableFunc(recs, func(a, b *identity.DeviceRevocation) int { return cmp.Compare(b.RevokedAt, a.RevokedAt) })
	if len(recs) <= helloRevocationsMax {
		return recs
	}
	newest := helloRevocationsMax / 2
	older := recs[newest:]
	n := helloRevocationsMax - newest
	start := int(s.peerRevShareAt.Add(uint64(n))-uint64(n)) % len(older)
	out := slices.Clone(recs[:newest])
	for i := 0; i < n && i < len(older); i++ {
		out = append(out, older[(start+i)%len(older)])
	}
	return out
}

// adoptPeerRevocations keeps the revocations a peer's account signed, when the
// peer is placed as that account and that account shares a guild with us.
// Our own account's are not taken this way: they erase a device, and arrive
// in Revoked under applyRevocation's rules.
func (s *Service) adoptPeerRevocations(from peer.ID, recs []*identity.DeviceRevocation) {
	if len(recs) == 0 {
		return
	}
	fpr := s.presence(from).Fingerprint
	if !s.revocationSource(fpr) {
		return
	}
	if err := s.keepPeerRevocations(fpr, recs); err != nil {
		log.Printf("concord/app: keeping %s's unlinked devices: %v", fpr, err)
	}
}

// revocationSource: whose word about their own devices we take. Not our own
// account (that path erases), not a stranger (presence gives anyone a
// fingerprint of their own key), only an account we share a guild with.
func (s *Service) revocationSource(fpr string) bool {
	if fpr == "" || s.id == nil || fpr == s.id.Fingerprint() {
		return false
	}
	return s.sharesGuild(fpr)
}

// keepPeerRevocations adds the records signed by account fpr that we do not
// hold yet. Records we hold are matched by key and never verified again; only
// new ones are. Caps refuse. A store write that fails is returned; memory
// keeps the records (refusing an unlinked device is the safe side) and the
// next write carries them.
func (s *Service) keepPeerRevocations(fpr string, recs []*identity.DeviceRevocation) error {
	return s.keepPeerRevocationsSharing(fpr, recs, s.sharesGuild)
}

// keepPeerRevocationsSharing is keepPeerRevocations with the "do we share a
// guild with this account" question passed in.
func (s *Service) keepPeerRevocationsSharing(fpr string, recs []*identity.DeviceRevocation, shares func(string) bool) error {
	s.peerRevMu.Lock()
	defer s.peerRevMu.Unlock()
	s.deviceMu.RLock()
	held := func(r *identity.DeviceRevocation) bool { return s.peerRevoked[peerRevKey(r.AccountPub, r.DevicePub)] }
	var cand []*identity.DeviceRevocation
	seen := map[string]bool{}
	for _, r := range recs {
		if len(cand) >= helloRevocationsMax {
			break
		}
		if r == nil || held(r) || identity.FingerprintOf(r.AccountPub) != fpr {
			continue
		}
		k := peerRevKey(r.AccountPub, r.DevicePub)
		if seen[k] {
			continue
		}
		seen[k] = true
		cand = append(cand, r)
	}
	s.deviceMu.RUnlock()
	if len(cand) == 0 {
		return nil
	}
	recsNow := s.peerRevRecs
	changed, removed := false, false
	for _, r := range cand {
		if !peerRevocationValid(r) {
			continue
		}
		// This account's records, oldest first by the account's own clock.
		mine, oldest := 0, -1
		for i, h := range recsNow {
			if identity.FingerprintOf(h.AccountPub) != fpr {
				continue
			}
			mine++
			if oldest < 0 || h.RevokedAt < recsNow[oldest].RevokedAt {
				oldest = i
			}
		}
		if mine >= peerRevocationsPerAccount {
			if r.RevokedAt <= recsNow[oldest].RevokedAt {
				continue
			}
			recsNow = append(recsNow[:oldest:oldest], recsNow[oldest+1:]...)
			removed = true
		} else if len(recsNow) >= peerRevocationsTotal {
			kept := keepSharing(recsNow, shares)
			if len(kept) < len(recsNow) {
				removed = true
			}
			recsNow = kept
			if len(recsNow) >= peerRevocationsTotal {
				break
			}
		}
		recsNow = append(recsNow, r)
		changed = true
	}
	if !changed && !removed {
		return nil
	}
	s.peerRevRecs = recsNow
	s.deviceMu.Lock()
	if removed {
		s.peerRevoked = map[string]bool{}
	}
	for _, r := range recsNow {
		s.forgetPeerDeviceLocked(r)
	}
	s.deviceMu.Unlock()
	return s.writePeerRevocationsLocked()
}

// keepSharing is recs without those of accounts we share no guild with. The
// answer is asked once per account.
func keepSharing(recs []*identity.DeviceRevocation, shares func(string) bool) []*identity.DeviceRevocation {
	verdict := map[string]bool{}
	var out []*identity.DeviceRevocation
	for _, r := range recs {
		f := identity.FingerprintOf(r.AccountPub)
		v, ok := verdict[f]
		if !ok {
			v = shares(f)
			verdict[f] = v
		}
		if v {
			out = append(out, r)
		}
	}
	return out
}

// writePeerRevocationsLocked stores the kept set. peerRevMu must be held.
func (s *Service) writePeerRevocationsLocked() error {
	if !s.peerRevLoaded {
		return fmt.Errorf("the stored set could not be read at start; changes kept in memory only")
	}
	raw, err := json.Marshal(s.peerRevRecs)
	if err != nil {
		return err
	}
	return s.store.SetSetting(peerRevocationsKey, string(raw))
}

// prunePeerRevocations drops the records of accounts we no longer share a
// guild with. Run once the guilds are loaded at start.
func (s *Service) prunePeerRevocations() {
	s.prunePeerRevocationsSharing(s.sharesGuild)
}

func (s *Service) prunePeerRevocationsSharing(shares func(string) bool) {
	s.peerRevMu.Lock()
	defer s.peerRevMu.Unlock()
	kept := keepSharing(s.peerRevRecs, shares)
	if len(kept) == len(s.peerRevRecs) {
		return
	}
	s.peerRevRecs = kept
	s.deviceMu.Lock()
	s.peerRevoked = map[string]bool{}
	for _, r := range kept {
		s.forgetPeerDeviceLocked(r)
	}
	s.deviceMu.Unlock()
	if err := s.writePeerRevocationsLocked(); err != nil {
		log.Printf("concord/app: other accounts' unlinked devices: %v", err)
	}
}
