package app

// seatcheck.go decides what this device can say about who holds each leaf of a
// group, and acts on it.
//
// A leaf carries a credential (an account key, or a device certificate) and a
// signature key. Concord's own clients always pair the two the same way
// (mlsIdentity): an original install signs with a key derived from the account
// seed, and a linked device signs with the device key its certificate names.
// That pairing is what everything here checks:
//
//   - A certificate names its key, so any member can check it.
//   - An account key's signing key derives from the seed, which every device of
//     that account holds, so the account's own devices can check it exactly.
//     Every other member records the key it first saw beside each account key
//     and flags a later leaf whose key differs from that record. The record is
//     learned on first sight, in any group, so it is evidence and not proof:
//     a mismatch against it only flags (seatKeyDiffers). It never drops a
//     frame, never offers a removal and never passes over the owner guard.
//
// In this release nothing here refuses a commit: a member that refused what
// older members accepted would fall behind the group. A leaf that fails the
// check is flagged (logged and shown), carries no authority for guild
// metadata or unsigned messages, and is removed by the account's own device
// when that device may commit.

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ZahakJ/concord/internal/crypto/mls"
	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/identity"
)

// Settings keys.
const (
	accountKeysSetting = "mls.account_keys" // hex(account key) -> hex(signing key)
	seatNoticeSetting  = "mls.seat_notices" // guild -> notices still to show
	signersSetting     = "accounts.signed"  // fingerprints seen signing a live message
)

// refuseSeatChanges turns flagging into refusal. Off in this release, and only
// safe to turn on once every member of a group checks the same way.
var refuseSeatChanges atomic.Bool

// seatDefenceHold stops the automatic removal; tests use it to look at a
// group before this device acts on it. Nothing in the product sets it.
var seatDefenceHold atomic.Bool

// seatDefenceDelay is how long a device that is not its account's first leaf
// waits before removing a leaf itself, giving the first one the chance to.
var seatDefenceDelay = 20 * time.Second

// seatDefenceRetry is how soon a device that could not act yet looks again.
var seatDefenceRetry = 5 * time.Second

// Kinds of flag. All of them describe one leaf, named by its signature key
// (unique in a tree) and the index it held when last read.
const (
	seatNotTheirs = "not_theirs" // key provably not the one the credential's account uses
	seatAmbiguous = "ambiguous"  // one account key on two leaves, nothing to tell them apart
	seatSeated    = "seated"     // added while the same credential was already in the group
	seatReplaced  = "replaced"   // added under the credential of a leaf the same commit removed, with another key
	seatRepeated  = "repeated"   // two adds of one credential in one commit
	seatChanged   = "changed"    // a commit rewrote this leaf's credential or key
	// seatKeyDiffers: an account key on a key other than the one this device
	// first saw beside it. Shown only: the record proves nothing (see top).
	seatKeyDiffers = "key_differs"
)

// SeatFlag is one flagged leaf, as the UI sees it.
type SeatFlag struct {
	Index   uint32 `json:"index"`
	Key     string `json:"key"` // hex signature key
	Kind    string `json:"kind"`
	Account string `json:"account"` // fingerprint the credential names
	Adder   string `json:"adder,omitempty"`
	Own     bool   `json:"own"` // the credential names this device's account
	// Provable: this device can show the leaf is not the account's, so the
	// removal action may be offered for it.
	Provable bool `json:"provable"`
}

// SeatNotice is something already done or found that the person should be
// told once: a leaf removed for them, a removal that did not stick.
type SeatNotice struct {
	Kind    string `json:"kind"` // "removed" | "repeats" | "replaced"
	Account string `json:"account,omitempty"`
	Adder   string `json:"adder,omitempty"`
	At      int64  `json:"at"`
}

type seatState struct {
	mu sync.Mutex
	// accountKeys: the signing key first seen beside each account key.
	accountKeys map[string][]byte
	// flags: the last audit of each guild.
	flags map[string][]SeatFlag
	// committed: flags found while reading a commit, by guild and hex key.
	// They outlive the commit because the tree no longer shows them.
	committed map[string]map[string]SeatFlag
	notices   map[string][]SeatNotice
	signers   map[string]bool
	caughtUp  map[string]bool
	// defended: the epoch at which this device last removed a leaf in a
	// guild, so it removes at most one per epoch.
	defended map[string]uint64
	repeats  map[string]int // guild + adder -> removals
	pending  map[string]bool
	loaded   bool
}

func (s *Service) seatsState() *seatState {
	s.seatOnce.Do(func() {
		st := &seatState{
			accountKeys: map[string][]byte{},
			flags:       map[string][]SeatFlag{},
			committed:   map[string]map[string]SeatFlag{},
			notices:     map[string][]SeatNotice{},
			signers:     map[string]bool{},
			caughtUp:    map[string]bool{},
			defended:    map[string]uint64{},
			repeats:     map[string]int{},
			pending:     map[string]bool{},
		}
		if s.store != nil {
			if v, err := s.store.GetSetting(accountKeysSetting); err == nil && v != "" {
				var m map[string]string
				if json.Unmarshal([]byte(v), &m) == nil {
					for k, hk := range m {
						if b, err := hex.DecodeString(hk); err == nil {
							st.accountKeys[k] = b
						}
					}
				}
			}
			if v, err := s.store.GetSetting(seatNoticeSetting); err == nil && v != "" {
				_ = json.Unmarshal([]byte(v), &st.notices)
			}
			if v, err := s.store.GetSetting(signersSetting); err == nil && v != "" {
				var l []string
				if json.Unmarshal([]byte(v), &l) == nil {
					for _, f := range l {
						st.signers[f] = true
					}
				}
			}
		}
		s.seats = st
	})
	return s.seats
}

// isAccountKeyCredential: a bare 32-byte account key, as an original install
// presents. Anything else is a device certificate or unknown.
func isAccountKeyCredential(cred []byte) bool {
	if len(cred) != ed25519.PublicKeySize {
		return false
	}
	_, isCert := identity.ParseDeviceCert(cred)
	return !isCert
}

// ownAccountSigningKey is the signing key every original install of this
// account uses (deriveMLSSigningKey), which every device of the account can
// compute because each holds the account seed.
func (s *Service) ownAccountSigningKey() []byte {
	s.seatKeyOnce.Do(func() {
		s.seatOwnKey = deriveMLSSigningKey(s.id).Public().(ed25519.PublicKey)
	})
	return s.seatOwnKey
}

// recordedAccountKey is the signing key this device first saw beside another
// account's key, or nil. It is never consulted for this account's own key,
// which the seed gives exactly.
func (s *Service) recordedAccountKey(cred []byte) []byte {
	st := s.seatsState()
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.accountKeys[hex.EncodeToString(cred)]
}

// leafNotTheirs reports whether this device can prove a leaf is not a device
// of the account its credential names: a certificate that verifies and names
// another key, or this account's own key on a key its seed does not give. Only
// this verdict sets Provable, drops the leaf's frames, offers its removal and
// passes over the owner guard. A certificate that does not verify names no
// account (accountKeyOf leaves it as it is), so it is not judged here.
func (s *Service) leafNotTheirs(l mls.Leaf) bool {
	if len(l.Key) == 0 {
		return false
	}
	if cert, ok := identity.ParseDeviceCert(l.Credential); ok {
		return cert.Verify() && !bytes.Equal(cert.DevicePub, l.Key)
	}
	if isAccountKeyCredential(l.Credential) && bytes.Equal(l.Credential, s.PublicKey()) {
		return !bytes.Equal(l.Key, s.ownAccountSigningKey())
	}
	return false
}

// leafUnlikeRecord reports whether another account's key sits on a key other
// than the one this device recorded for it. A flag, never a proof: the record
// may have been learned from a leaf somebody else seated (see top).
func (s *Service) leafUnlikeRecord(l mls.Leaf) bool {
	if len(l.Key) == 0 || !isAccountKeyCredential(l.Credential) || bytes.Equal(l.Credential, s.PublicKey()) {
		return false
	}
	want := s.recordedAccountKey(l.Credential)
	return want != nil && !bytes.Equal(want, l.Key)
}

// ownAccountLeaf is THE test for "this leaf is a device of my account": the
// credential names my account and the key is the one that credential pairs
// with. It needs no list of devices: a sibling linked a minute ago, restored
// from a phrase, or never seen before passes it.
func (s *Service) ownAccountLeaf(l mls.Leaf) bool {
	if accountFingerprintOf(l.Credential) != s.id.Fingerprint() || len(l.Key) == 0 {
		return false
	}
	if cert, ok := identity.ParseDeviceCert(l.Credential); ok {
		return cert.Verify() && bytes.Equal(cert.DevicePub, l.Key)
	}
	return bytes.Equal(l.Key, s.ownAccountSigningKey())
}

// fromOwnDevice reports whether a decrypted frame comes from a current device
// of this account: its leaf passes ownAccountLeaf, and a certificate leaf's
// device has not been unlinked here. Unlinking leaves a leaf seated wherever
// this device cannot commit, and that leaf speaks for the account no more.
// Revocation stays out of ownAccountLeaf itself, so a kick that meets a
// revoked leaf still reads as a removal, not as a replacement.
func (s *Service) fromOwnDevice(m *mls.Message) bool {
	if m == nil {
		return false
	}
	l := mls.Leaf{Index: m.SenderLeaf, Credential: m.SenderLeafID, Key: m.SenderKey}
	if !bytes.Equal(m.SenderID, m.SenderLeafID) || !s.ownAccountLeaf(l) {
		return false
	}
	if cert, ok := identity.ParseDeviceCert(l.Credential); ok && s.deviceIsRevoked(cert.DevicePub) {
		return false
	}
	return true
}

// ownDeviceLeaves lists the leaves of a group that are devices of this
// account, this device's own included, by ownAccountLeaf.
func (s *Service) ownDeviceLeaves(groupID []byte) (own []mls.Leaf, mine uint32, err error) {
	leaves, err := s.mls.Leaves(s.ctx, groupID)
	if err != nil {
		return nil, 0, err
	}
	mine, err = s.mls.OwnLeaf(s.ctx, groupID)
	if err != nil {
		return nil, 0, err
	}
	for _, l := range leaves {
		if s.ownAccountLeaf(l) {
			own = append(own, l)
		}
	}
	return own, mine, nil
}

// otherOwnDevices counts the devices of this account seated in a group
// besides this one.
func (s *Service) otherOwnDevices(groupID []byte) (int, error) {
	own, mine, err := s.ownDeviceLeaves(groupID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, l := range own {
		if l.Index != mine {
			n++
		}
	}
	return n, nil
}

// frameFlagged reports whether a decrypted frame came from a leaf that must
// carry no authority: one this device can prove is not its account's, one a
// commit rewrote, or one an audit or commit flagged. It is decided here, at
// decryption, from the frame's own leaf, so it never waits for a removal.
func (s *Service) frameFlagged(guildID string, m *mls.Message) bool {
	if m == nil || len(m.SenderKey) == 0 {
		return true
	}
	if !bytes.Equal(m.SenderID, m.SenderLeafID) {
		return true
	}
	l := mls.Leaf{Index: m.SenderLeaf, Credential: m.SenderLeafID, Key: m.SenderKey}
	if s.leafNotTheirs(l) {
		return true
	}
	return s.keyFlagged(guildID, m.SenderKey)
}

// keyFlagged: an audit or a commit flagged this key in this group, for a
// reason that withdraws its authority. seatKeyDiffers rests on the record
// alone and does not.
func (s *Service) keyFlagged(guildID string, key []byte) bool {
	hk := hex.EncodeToString(key)
	st := s.seatsState()
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, f := range st.flags[guildID] {
		if f.Key == hk && f.Kind != seatKeyDiffers {
			return true
		}
	}
	c, ok := st.committed[guildID][hk]
	return ok && c.Kind != seatKeyDiffers
}

// checkSeats reads a commit before it is applied and flags what it does to
// leaves. It returns true when the commit should be refused, which in this
// release is never: refuseSeatChanges is off.
func (s *Service) checkSeats(guildID string, groupID, commit []byte) bool {
	cl, err := s.mls.ReadCommitLeaves(s.ctx, groupID, commit)
	if err != nil || cl == nil {
		return false
	}
	pre, err := s.mls.Leaves(s.ctx, groupID)
	if err != nil {
		return false
	}
	byIndex := map[uint32]mls.Leaf{}
	for _, l := range pre {
		byIndex[l.Index] = l
	}
	removed := map[uint32]bool{}
	for _, i := range cl.Removed {
		removed[i] = true
	}
	adder := ""
	if sl, ok := byIndex[cl.Sender]; ok && !cl.External {
		adder = accountFingerprintOf(sl.Credential)
	}
	var found []SeatFlag
	flag := func(l mls.Leaf, kind string) {
		found = append(found, SeatFlag{
			Index: l.Index, Key: hex.EncodeToString(l.Key), Kind: kind,
			Account: accountFingerprintOf(l.Credential), Adder: adder,
			Own:      accountFingerprintOf(l.Credential) == s.id.Fingerprint(),
			Provable: s.leafNotTheirs(l),
		})
	}
	if cl.ByReference {
		log.Printf("concord/app: guild %s: a commit carries a proposal by reference; its leaves could not be read", guildID)
	}
	if sl, ok := byIndex[cl.Sender]; ok && !cl.External && (s.leafNotTheirs(sl) || s.keyFlagged(guildID, sl.Key)) {
		log.Printf("concord/app: guild %s: a commit comes from a flagged leaf %d", guildID, sl.Index)
		flag(sl, seatNotTheirs)
	}
	seenAdd := map[string]bool{}
	for _, a := range cl.Added {
		k := string(a.Credential)
		switch {
		case s.leafNotTheirs(a):
			flag(a, seatNotTheirs)
		case s.leafUnlikeRecord(a):
			flag(a, seatKeyDiffers)
		case seenAdd[k]:
			flag(a, seatRepeated)
		default:
			for _, p := range pre {
				if !bytes.Equal(p.Credential, a.Credential) {
					continue
				}
				if !removed[p.Index] {
					flag(a, seatSeated)
					break
				}
				// Removed and re-added in one commit: the same device only
				// if the key is the same, or a certificate names the new key.
				if !bytes.Equal(p.Key, a.Key) && !certNamesKey(a) {
					flag(a, seatReplaced)
					break
				}
			}
		}
		seenAdd[k] = true
	}
	for _, r := range cl.Replaced {
		p, ok := byIndex[r.Index]
		if ok && (!bytes.Equal(p.Credential, r.Credential) || !bytes.Equal(p.Key, r.Key)) {
			flag(r, seatChanged)
		}
	}
	if len(found) == 0 {
		return false
	}
	st := s.seatsState()
	st.mu.Lock()
	if st.committed[guildID] == nil {
		st.committed[guildID] = map[string]SeatFlag{}
	}
	for _, f := range found {
		st.committed[guildID][f.Key] = f
		log.Printf("concord/app: guild %s: a commit by %s seats a leaf that does not match its credential (%s)", guildID, shortFpr(adder), f.Kind)
	}
	st.mu.Unlock()
	return refuseSeatChanges.Load()
}

func certNamesKey(l mls.Leaf) bool {
	cert, ok := identity.ParseDeviceCert(l.Credential)
	return ok && cert.Verify() && bytes.Equal(cert.DevicePub, l.Key)
}

// auditSeats reads a group's whole tree and recomputes its flags. It runs
// after every applied commit, after a Welcome, and at startup, and is
// flag-only: it changes nothing in the group itself.
func (s *Service) auditSeats(guildID string, groupID []byte) {
	leaves, err := s.mls.Leaves(s.ctx, groupID)
	if err != nil {
		return
	}
	members, err := s.mls.MemberLeaves(s.ctx, groupID)
	if err != nil {
		return
	}
	mine := s.id.Fingerprint()
	count := map[string]int{}
	for _, l := range leaves {
		if isAccountKeyCredential(l.Credential) {
			count[string(l.Credential)]++
		}
	}
	// Record first sightings before judging, so a leaf seen for the first time
	// alongside nothing else is the record, not a stranger.
	st := s.seatsState()
	recorded := false
	st.mu.Lock()
	for _, l := range leaves {
		if !isAccountKeyCredential(l.Credential) || count[string(l.Credential)] != 1 || bytes.Equal(l.Credential, s.PublicKey()) {
			continue
		}
		if mc, ok := members[l.Index]; ok && !bytes.Equal(mc, l.Credential) {
			continue
		}
		hk := hex.EncodeToString(l.Credential)
		if _, ok := st.accountKeys[hk]; !ok && len(l.Key) > 0 {
			if f, flagged := st.committed[guildID][hex.EncodeToString(l.Key)]; flagged && f.Kind != "" {
				continue
			}
			st.accountKeys[hk] = append([]byte(nil), l.Key...)
			recorded = true
		}
	}
	committed := st.committed[guildID]
	st.mu.Unlock()
	if recorded {
		s.saveAccountKeys()
	}

	present := map[string]bool{}
	var flags []SeatFlag
	for _, l := range leaves {
		hk := hex.EncodeToString(l.Key)
		present[hk] = true
		acct := accountFingerprintOf(l.Credential)
		f := SeatFlag{Index: l.Index, Key: hk, Account: acct, Own: acct == mine, Provable: s.leafNotTheirs(l)}
		switch {
		case f.Provable:
			f.Kind = seatNotTheirs
		case members[l.Index] != nil && !bytes.Equal(members[l.Index], l.Credential):
			f.Kind = seatChanged
		case isAccountKeyCredential(l.Credential) && count[string(l.Credential)] > 1 && !bytes.Equal(l.Credential, s.PublicKey()):
			// Another account's key on two leaves. This device cannot tell
			// which is genuine: its record of that account's key may itself
			// come from a leaf somebody else seated, so it does not decide.
			// (Under this account's own key the seed decides, above.)
			f.Kind = seatAmbiguous
		case s.leafUnlikeRecord(l):
			f.Kind = seatKeyDiffers
		}
		if c, ok := committed[hk]; ok {
			if f.Kind == "" {
				f.Kind = c.Kind
			}
			f.Adder = c.Adder
		}
		if f.Kind != "" {
			flags = append(flags, f)
		}
	}
	sort.Slice(flags, func(i, j int) bool { return flags[i].Index < flags[j].Index })

	st.mu.Lock()
	for hk := range st.committed[guildID] {
		if !present[hk] {
			delete(st.committed[guildID], hk)
		}
	}
	changed := !sameFlags(st.flags[guildID], flags)
	if len(flags) == 0 {
		delete(st.flags, guildID)
	} else {
		st.flags[guildID] = flags
	}
	st.mu.Unlock()
	if changed {
		for _, f := range flags {
			log.Printf("concord/app: guild %s: leaf %d is flagged (%s)", guildID, f.Index, f.Kind)
		}
		s.emitGuildUpdate()
	}
	for _, f := range flags {
		if f.Own && f.Provable {
			go s.defendSeats(guildID)
			break
		}
	}
}

func sameFlags(a, b []SeatFlag) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (s *Service) saveAccountKeys() {
	st := s.seatsState()
	st.mu.Lock()
	m := make(map[string]string, len(st.accountKeys))
	for k, v := range st.accountKeys {
		m[k] = hex.EncodeToString(v)
	}
	st.mu.Unlock()
	b, _ := json.Marshal(m)
	_ = s.store.SetSetting(accountKeysSetting, string(b))
}

// markCaughtUp records that this device has finished a catch-up round (or a
// Welcome put it at the group's epoch) since it started, which is the first
// moment a removal it mints can reach anybody.
func (s *Service) markCaughtUp(guildID string) {
	st := s.seatsState()
	st.mu.Lock()
	was := st.caughtUp[guildID]
	st.caughtUp[guildID] = true
	st.mu.Unlock()
	if !was {
		go s.defendSeats(guildID)
	}
}

// defendSeats removes a leaf that carries this account's credential under a
// key that is not this account's. Best-effort: it runs only where the removal
// can reach the group (caught up, not barred), only one device of the account
// mints it, and only one leaf per epoch.
func (s *Service) defendSeats(guildID string) {
	if seatDefenceHold.Load() {
		return
	}
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var groupID []byte
	if ok {
		groupID = g.GroupID
	}
	s.mu.RUnlock()
	if !ok {
		return
	}
	st := s.seatsState()
	if !s.mayDefendSeats(guildID) {
		// Not now (behind, not yet caught up, mid-heal). Look again shortly
		// while this account's identity is still on a leaf that is not ours;
		// nothing else would bring this device back here until the next
		// commit.
		if s.canManageMembers(guildID) && s.hasUnmatchedOwnLeaf(guildID) {
			st.mu.Lock()
			if !st.pending[guildID] {
				st.pending[guildID] = true
				time.AfterFunc(seatDefenceRetry, func() {
					st.mu.Lock()
					delete(st.pending, guildID)
					st.mu.Unlock()
					s.defendSeats(guildID)
				})
			}
			st.mu.Unlock()
		}
		return
	}
	st.mu.Lock()
	if st.pending[guildID] {
		st.mu.Unlock()
		return
	}
	st.mu.Unlock()

	target, first, err := s.defenceTarget(groupID)
	if err != nil || target == nil {
		return
	}
	if !first {
		// Another device of this account sits at a lower leaf and may be
		// online to do it; give it the chance, then do it anyway.
		st.mu.Lock()
		st.pending[guildID] = true
		st.mu.Unlock()
		time.AfterFunc(seatDefenceDelay, func() {
			st.mu.Lock()
			delete(st.pending, guildID)
			st.mu.Unlock()
			s.removeUnmatchedOwnLeaf(guildID, groupID)
		})
		return
	}
	s.removeUnmatchedOwnLeaf(guildID, groupID)
}

// hasUnmatchedOwnLeaf: the last audit of a guild flagged a leaf under this
// account that this device can prove is not ours.
func (s *Service) hasUnmatchedOwnLeaf(guildID string) bool {
	if s.ctx.Err() != nil {
		return false
	}
	for _, f := range s.SeatFlags(guildID) {
		if f.Own && f.Provable {
			return true
		}
	}
	return false
}

// defenceTarget finds the lowest leaf carrying this account's credential that
// is provably not this account's, and says whether this device is the
// account's lowest genuine leaf (the one that removes it first).
func (s *Service) defenceTarget(groupID []byte) (*mls.Leaf, bool, error) {
	leaves, err := s.mls.Leaves(s.ctx, groupID)
	if err != nil {
		return nil, false, err
	}
	mine, err := s.mls.OwnLeaf(s.ctx, groupID)
	if err != nil {
		return nil, false, err
	}
	var target *mls.Leaf
	lowest := ^uint32(0)
	for i := range leaves {
		l := leaves[i]
		if s.ownAccountLeaf(l) {
			if l.Index < lowest {
				lowest = l.Index
			}
			continue
		}
		if l.Index == mine || accountFingerprintOf(l.Credential) != s.id.Fingerprint() || !s.leafNotTheirs(l) {
			continue
		}
		if target == nil {
			target = &l
		}
	}
	return target, lowest == mine, nil
}

// mayDefendSeats: a removal this device mints now can reach the group. It
// may commit, it is not behind, it is not barred, and it has caught up at
// least once since it started (before that it cannot know it is level).
func (s *Service) mayDefendSeats(guildID string) bool {
	if !s.canManageMembers(guildID) || s.OutOfSync(guildID) || s.mintBarred(guildID) != nil || s.EvictedFrom(guildID) != "" {
		return false
	}
	st := s.seatsState()
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.caughtUp[guildID]
}

func (s *Service) removeUnmatchedOwnLeaf(guildID string, groupID []byte) {
	if seatDefenceHold.Load() || !s.mayDefendSeats(guildID) {
		return
	}
	target, _, err := s.defenceTarget(groupID)
	if err != nil || target == nil {
		return
	}
	epoch, err := s.mls.Epoch(s.ctx, groupID)
	if err != nil {
		return
	}
	st := s.seatsState()
	st.mu.Lock()
	if e, ok := st.defended[guildID]; ok && e == epoch {
		st.mu.Unlock()
		return
	}
	st.defended[guildID] = epoch
	adder := st.committed[guildID][hex.EncodeToString(target.Key)].Adder
	st.mu.Unlock()

	if err := s.publishLeafRemoval(groupID, target.Index, target.Key); err != nil {
		if !errors.Is(err, mls.ErrLeafChanged) {
			log.Printf("concord/app: guild %s: removing leaf %d failed: %v", guildID, target.Index, err)
		}
		return
	}
	log.Printf("concord/app: guild %s: removed leaf %d, which carried this account's credential under another key", guildID, target.Index)
	kind := "removed"
	st.mu.Lock()
	if adder != "" {
		st.repeats[guildID+"|"+adder]++
		if st.repeats[guildID+"|"+adder] > 1 {
			kind = "repeats"
		}
	}
	st.mu.Unlock()
	s.addSeatNotice(guildID, SeatNotice{Kind: kind, Account: s.id.Fingerprint(), Adder: adder, At: time.Now().Unix()})
	go s.rememberMembers()
	s.emitGuildUpdate()
}

// publishLeafRemoval commits and publishes the removal of one leaf, named by
// index and key so a reused index is never taken for the leaf meant.
func (s *Service) publishLeafRemoval(groupID []byte, index uint32, key []byte) error {
	commit, err := s.mls.RemoveLeaf(s.ctx, groupID, index, key)
	if err != nil {
		return err
	}
	s.logCommit(groupID, commit)
	return s.ps.Publish(s.ctx, domain.ControlTopicID(groupID), commit)
}

// commitRemovesUs reads a commit at our epoch for whether it removes THIS
// device's leaf, by index. replaced is set when it does and a leaf under this
// account that is not ours stays in the group (or arrives with it): that is
// not this account leaving, and must not settle as an eviction.
func (s *Service) commitRemovesUs(groupID, commit []byte) (removed, replaced bool) {
	cl, err := s.mls.ReadCommitLeaves(s.ctx, groupID, commit)
	if err != nil || cl == nil {
		return false, false
	}
	mine, err := s.mls.OwnLeaf(s.ctx, groupID)
	if err != nil {
		return false, false
	}
	gone := map[uint32]bool{}
	for _, i := range cl.Removed {
		gone[i] = true
	}
	if !gone[mine] {
		return false, false
	}
	me := s.id.Fingerprint()
	if leaves, err := s.mls.Leaves(s.ctx, groupID); err == nil {
		for _, l := range leaves {
			if !gone[l.Index] && accountFingerprintOf(l.Credential) == me && !s.ownAccountLeaf(l) {
				return true, true
			}
		}
	}
	for _, a := range cl.Added {
		if accountFingerprintOf(a.Credential) == me && !s.ownAccountLeaf(a) {
			return true, true
		}
	}
	return true, false
}

// noteSeatReplaced tells the person their leaf was taken out while a leaf
// under their identity that is not theirs stays.
func (s *Service) noteSeatReplaced(guildID, adder string) {
	log.Printf("concord/app: guild %s: this device's leaf was removed while another leaf under this account stays", guildID)
	s.addSeatNotice(guildID, SeatNotice{Kind: "replaced", Account: s.id.Fingerprint(), Adder: adder, At: time.Now().Unix()})
	s.emitGuildUpdate()
}

func (s *Service) addSeatNotice(guildID string, n SeatNotice) {
	st := s.seatsState()
	st.mu.Lock()
	list := st.notices[guildID]
	if len(list) > 0 && list[len(list)-1].Kind == n.Kind && list[len(list)-1].Adder == n.Adder {
		list[len(list)-1] = n
	} else {
		list = append(list, n)
	}
	if len(list) > 4 {
		list = list[len(list)-4:]
	}
	st.notices[guildID] = list
	b, _ := json.Marshal(st.notices)
	st.mu.Unlock()
	_ = s.store.SetSetting(seatNoticeSetting, string(b))
}

// SeatFlags lists a guild's flagged leaves, for the banner.
func (s *Service) SeatFlags(guildID string) []SeatFlag {
	st := s.seatsState()
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]SeatFlag(nil), st.flags[guildID]...)
}

// SeatNotices lists what a guild's banner still has to say about leaves
// already dealt with.
func (s *Service) SeatNotices(guildID string) []SeatNotice {
	st := s.seatsState()
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]SeatNotice(nil), st.notices[guildID]...)
}

// DismissSeatNotices clears a guild's notices once read.
func (s *Service) DismissSeatNotices(guildID string) {
	st := s.seatsState()
	st.mu.Lock()
	delete(st.notices, guildID)
	b, _ := json.Marshal(st.notices)
	st.mu.Unlock()
	_ = s.store.SetSetting(seatNoticeSetting, string(b))
	s.emitGuildUpdate()
}

// ErrSeatNotProvable: this device cannot show which of two leaves is genuine.
var ErrSeatNotProvable = errors.New("app: this device cannot tell which of these devices is genuine")

// RemoveSeatLeaf removes a flagged leaf, by index and key. It is offered only
// for a leaf this device can prove is not the account's, and for that leaf it
// passes over the guards that protect the owner and higher roles: they protect
// an account, and the leaf is provably not that account's device.
func (s *Service) RemoveSeatLeaf(guildID string, index uint32, keyHex string) error {
	key, err := hex.DecodeString(keyHex)
	if err != nil || len(key) == 0 {
		return errors.New("app: bad device key")
	}
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	s.mu.RUnlock()
	if !ok {
		return errors.New("app: unknown guild")
	}
	if !s.canManageMembers(guildID) {
		return errors.New("app: you don't have permission to remove members")
	}
	if err := s.mintBarred(guildID); err != nil {
		return err
	}
	l, present, err := s.mls.LeafAt(s.ctx, g.GroupID, index)
	if err != nil {
		return err
	}
	if !present || !bytes.Equal(l.Key, key) {
		return nil // already gone
	}
	if !s.leafNotTheirs(l) {
		return ErrSeatNotProvable
	}
	if err := s.publishLeafRemoval(g.GroupID, index, key); err != nil {
		if errors.Is(err, mls.ErrLeafChanged) {
			return nil
		}
		return err
	}
	go s.rememberMembers()
	s.emitGuildUpdate()
	return nil
}

// noteSigner records that an account has signed a live message, after which
// an unsigned message claiming that account is dropped rather than shown.
func (s *Service) noteSigner(fpr string) {
	st := s.seatsState()
	st.mu.Lock()
	if st.signers[fpr] {
		st.mu.Unlock()
		return
	}
	st.signers[fpr] = true
	list := make([]string, 0, len(st.signers))
	for f := range st.signers {
		list = append(list, f)
	}
	st.mu.Unlock()
	sort.Strings(list)
	b, _ := json.Marshal(list)
	_ = s.store.SetSetting(signersSetting, string(b))
}

func (s *Service) seenSigning(fpr string) bool {
	st := s.seatsState()
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.signers[fpr]
}
