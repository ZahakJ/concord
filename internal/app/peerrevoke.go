package app

import (
	"bytes"
	"cmp"
	"encoding/hex"
	"encoding/json"
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
//
// The keys are (account, device) pairs: a revocation is the account's word
// about its own device and nobody else's, so an account cannot shut another
// account's device out by naming its key.

// peerRevocationsKey persists the revocations other accounts have shown us.
const peerRevocationsKey = "peer.revocations"

// peerRevocationsPerAccount bounds what one account can make us store. An
// account unlinks a handful of devices in its life; a flood is someone
// minting keys to fill our settings.
const peerRevocationsPerAccount = 64

// helloRevocationsMax is the most revocations one hello carries (newest
// first): about 250 bytes each, well inside the 64 KiB frame.
const helloRevocationsMax = 32

func peerRevKey(account, device []byte) string {
	return hex.EncodeToString(account) + ":" + hex.EncodeToString(device)
}

// peerRevocations reads the stored revocations of other accounts, keeping only
// those that still verify.
func (s *Service) peerRevocations() []*identity.DeviceRevocation {
	raw, err := s.store.GetSetting(peerRevocationsKey)
	if err != nil || raw == "" {
		return nil
	}
	var recs []*identity.DeviceRevocation
	if json.Unmarshal([]byte(raw), &recs) != nil {
		return nil
	}
	out := recs[:0]
	for _, r := range recs {
		if peerRevocationValid(r) && !ed25519Equal(r.AccountPub, s.id.PublicKey()) {
			out = append(out, r)
		}
	}
	return out
}

// peerRevocationValid is a revocation that verifies and withdraws a device
// key, not the account key itself (an original device's credential is the
// bare account key; withdrawing it is a key rotation, not an unlink).
func peerRevocationValid(r *identity.DeviceRevocation) bool {
	return r != nil && r.Verify() && !bytes.Equal(r.AccountPub, r.DevicePub)
}

// loadPeerRevoked seeds the in-memory set at startup and drops any mapping a
// roster read may already have made for a revoked device.
func (s *Service) loadPeerRevoked() {
	recs := s.peerRevocations()
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
	if ed25519Equal(cert.AccountPub, s.id.PublicKey()) {
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
// revocations, newest first, at most helloRevocationsMax. Nothing to a peer we
// cannot place (a stranger learns nothing of our devices), and nothing to a
// device we unlinked, which gets its own record in Revoked instead.
func (s *Service) revocationsToShare(p peer.ID) []*identity.DeviceRevocation {
	recs := s.revocations()
	if len(recs) == 0 || !s.canPlace(p) || s.revocationFor(p) != nil {
		return nil
	}
	recs = slices.Clone(recs)
	slices.SortFunc(recs, func(a, b *identity.DeviceRevocation) int { return cmp.Compare(b.RevokedAt, a.RevokedAt) })
	if len(recs) > helloRevocationsMax {
		recs = recs[:helloRevocationsMax]
	}
	return recs
}

// adoptPeerRevocations keeps the revocations a peer's account signed, when the
// peer is placed as that account. Our own account's are not taken this way:
// they erase a device, and arrive in Revoked under applyRevocation's rules.
func (s *Service) adoptPeerRevocations(from peer.ID, recs []*identity.DeviceRevocation) {
	if len(recs) == 0 {
		return
	}
	fpr := s.presence(from).Fingerprint
	if fpr == "" || fpr == s.id.Fingerprint() {
		return
	}
	var fresh []*identity.DeviceRevocation
	for _, r := range recs {
		if len(fresh) >= helloRevocationsMax {
			break
		}
		if peerRevocationValid(r) && identity.FingerprintOf(r.AccountPub) == fpr {
			fresh = append(fresh, r)
		}
	}
	if len(fresh) == 0 {
		return
	}
	s.peerRevMu.Lock()
	defer s.peerRevMu.Unlock()
	held := s.peerRevocations()
	have := map[string]bool{}
	for _, r := range held {
		have[peerRevKey(r.AccountPub, r.DevicePub)] = true
	}
	added := false
	for _, r := range fresh {
		k := peerRevKey(r.AccountPub, r.DevicePub)
		if have[k] {
			continue
		}
		have[k] = true
		held = append(held, r)
		added = true
	}
	if !added {
		return
	}
	held = capPerAccount(held, peerRevocationsPerAccount)
	if raw, err := json.Marshal(held); err == nil {
		_ = s.store.SetSetting(peerRevocationsKey, string(raw))
	}
	s.deviceMu.Lock()
	for _, r := range held {
		s.forgetPeerDeviceLocked(r)
	}
	s.deviceMu.Unlock()
}

// capPerAccount keeps each account's newest n revocations.
func capPerAccount(recs []*identity.DeviceRevocation, n int) []*identity.DeviceRevocation {
	slices.SortStableFunc(recs, func(a, b *identity.DeviceRevocation) int { return cmp.Compare(b.RevokedAt, a.RevokedAt) })
	count := map[string]int{}
	out := recs[:0]
	for _, r := range recs {
		a := hex.EncodeToString(r.AccountPub)
		if count[a] >= n {
			continue
		}
		count[a]++
		out = append(out, r)
	}
	return out
}
