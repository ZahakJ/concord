package app

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/identity"
)

// A contact's devices, and the one change in them worth saying out loud.
//
// Verification compares ACCOUNT fingerprints, and a linked device doesn't
// change one: the account key signs a device certificate, and every peer maps
// that leaf back to the same account (device.go, identity.AccountKeyOf). A
// device reaches a contact's set here only through a certificate that
// verifies under that contact's account key, which only a device already
// holding the account can issue, and does issue only after its owner confirms
// the new device's code (link.go). That is the chain, checked
// cryptographically on every scan, and a device on it is the contact's own
// doing: it is recorded, silently, and shown as a quiet line in the contact's
// details (ContactDevices), not as a message in the chat. (Up to 0.64 each one
// wrote "X added a new device. If this wasn't X, verify." into the 1:1, which
// told every contact about every phone; those lines are removed at start.)
//
// What is NOT on the chain is said: a key in a 1:1 with someone that belongs
// to neither of you. Its credential does not verify under the contact's
// account (else it would map to them), so nothing the contact's account
// signed put it there. It is written into that 1:1 as kind "keychange", once
// per key, as a local line (a non-"" kind: never pings, never counts unread,
// and is dropped by the sync ingest, so it never travels).
//
// A contact whose account key itself changed (a reset, or an account made
// again) is a different fingerprint, so a different contact without the
// verified mark, by construction: there is no key rotation to chain, and so
// nothing for a scan to vouch for.

// deviceRosterKey persists the per-contact device sets.
const deviceRosterKey = "contacts.devices"

// loadDeviceRoster restores the known-devices map. Restoring it BEFORE the
// first scan is what stops every restart from announcing devices we had
// already seen.
func (s *Service) loadDeviceRoster() {
	raw, err := s.store.GetSetting(deviceRosterKey)
	if err != nil || raw == "" {
		return
	}
	var m map[string][]string
	if json.Unmarshal([]byte(raw), &m) != nil {
		return
	}
	s.mu.Lock()
	for fpr, keys := range m {
		set := make(map[string]bool, len(keys))
		for _, k := range keys {
			set[k] = true
		}
		if len(set) > 0 {
			s.devices[fpr] = set
		}
	}
	s.mu.Unlock()
}

// persistDeviceRoster writes the known-devices map back. mu must not be held.
func (s *Service) persistDeviceRoster() {
	s.mu.RLock()
	m := make(map[string][]string, len(s.devices))
	for fpr, set := range s.devices {
		keys := make([]string, 0, len(set))
		for k := range set {
			keys = append(keys, k)
		}
		m[fpr] = keys
	}
	s.mu.RUnlock()
	if raw, err := json.Marshal(m); err == nil {
		_ = s.store.SetSetting(deviceRosterKey, string(raw))
	}
}

// newDevice is one device we had not seen behind a contact's account.
type newDevice struct {
	fpr     string
	key     string // device key, hex
	account []byte
	name    string
	issued  int64
}

// foldDevices merges a scan (account fingerprint -> device key -> credential)
// into the known roster, in place, and returns the devices that are new. The
// first sight of an account seeds silently, and a device already in the roster
// is never returned again — that is the exactly-once guarantee, so callers must
// hold the lock that guards known.
func foldDevices(known map[string]map[string]bool, seen map[string]map[string][]byte) (fresh []newDevice, changed bool) {
	for fpr, keys := range seen {
		set, first := known[fpr], known[fpr] == nil
		if first {
			set = map[string]bool{}
			known[fpr] = set
		}
		for k, cred := range keys {
			if set[k] {
				continue
			}
			set[k] = true
			changed = true
			if first {
				continue
			}
			d := newDevice{fpr: fpr, key: k, account: accountKeyOf(cred)}
			if cert, ok := identity.ParseDeviceCert(cred); ok {
				d.name, d.issued = cert.DeviceName, cert.IssuedAt
			}
			fresh = append(fresh, d)
		}
	}
	return fresh, changed
}

// noteDeviceLeaves folds every shared group's member leaves into the per-contact
// device roster and reports the ones that are new. Runs at startup (to seed) and
// on the heal tick, which is the only cadence this needs: a device that joins an
// account shows up as a leaf in the groups we share, and it stays there.
func (s *Service) noteDeviceLeaves() {
	s.scanDeviceLeaves(func(groupID []byte) ([][]byte, error) { return s.mls.Members(s.ctx, groupID) })
}

// scanDeviceLeaves is noteDeviceLeaves reading each group's leaves through
// members (a test lays out a contact's devices there).
func (s *Service) scanDeviceLeaves(members func(groupID []byte) ([][]byte, error)) {
	s.deviceScanMu.Lock()
	defer s.deviceScanMu.Unlock()
	s.mu.RLock()
	ids := make([][]byte, 0, len(s.guilds))
	for _, g := range s.guilds {
		ids = append(ids, g.GroupID)
	}
	s.mu.RUnlock()

	s.mu.RLock()
	prevGroups := s.deviceLiveGroups
	s.mu.RUnlock()

	// account fingerprint -> device key hex -> that leaf's credential
	seen := map[string]map[string][]byte{}
	// hex group id -> account fingerprint -> device keys live in that group
	groups := make(map[string]map[string]map[string]bool, len(ids))
	for _, groupID := range ids {
		gk := hex.EncodeToString(groupID)
		creds, err := members(groupID)
		if err != nil {
			// Unreadable right now: absence here must never mean "gone", so
			// the group keeps the devices it last showed.
			if prev, ok := prevGroups[gk]; ok {
				groups[gk] = prev
			}
			continue
		}
		inGroup := map[string]map[string]bool{}
		groups[gk] = inGroup
		for _, c := range creds {
			fpr := accountFingerprintOf(c)
			if fpr == "" || fpr == s.id.Fingerprint() {
				continue
			}
			// A device its account unlinked is not theirs any more, wherever
			// a group still holds its leaf.
			if s.leafRevoked(c) {
				continue
			}
			if seen[fpr] == nil {
				seen[fpr] = map[string][]byte{}
			}
			key := hex.EncodeToString(mailboxKeyOf(c))
			seen[fpr][key] = c
			if inGroup[fpr] == nil {
				inGroup[fpr] = map[string]bool{}
			}
			inGroup[fpr][key] = true
		}
	}

	// A verified contact's devices at verification are what a new one is
	// told apart from; one verified before this was recorded starts from
	// the devices already known, so nothing is marked for the upgrade.
	s.seedVerifiedDevices()

	s.mu.Lock()
	fresh, changed := foldDevices(s.devices, seen)
	s.deviceLive = unionLive(groups)
	s.deviceLiveGroups = groups
	s.mu.Unlock()

	if changed {
		s.persistDeviceRoster()
		s.logContactDevices(fresh)
	}
	if !sameLiveGroups(prevGroups, groups) {
		s.persistDeviceLive(groups)
	}
	s.noteStrangersInDMs()
}

// deviceLiveKey persists deviceLiveGroups, so the count and the new-device
// mark are right from startup, before the first scan has read every group.
const deviceLiveKey = "contacts.devicelive"

// unionLive folds the per-group sets into one per account.
func unionLive(groups map[string]map[string]map[string]bool) map[string]map[string]bool {
	live := map[string]map[string]bool{}
	for _, byAcct := range groups {
		for fpr, keys := range byAcct {
			if live[fpr] == nil {
				live[fpr] = map[string]bool{}
			}
			for k := range keys {
				live[fpr][k] = true
			}
		}
	}
	return live
}

func sameLiveGroups(a, b map[string]map[string]map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for g, x := range a {
		y, ok := b[g]
		if !ok || len(x) != len(y) {
			return false
		}
		for fpr, kx := range x {
			ky := y[fpr]
			if len(kx) != len(ky) {
				return false
			}
			for k := range kx {
				if !ky[k] {
					return false
				}
			}
		}
	}
	return true
}

// loadDeviceLive restores the last live sets at startup.
func (s *Service) loadDeviceLive() {
	raw, err := s.store.GetSetting(deviceLiveKey)
	if err != nil || raw == "" {
		return
	}
	var m map[string]map[string][]string
	if json.Unmarshal([]byte(raw), &m) != nil {
		return
	}
	groups := make(map[string]map[string]map[string]bool, len(m))
	for g, byAcct := range m {
		groups[g] = map[string]map[string]bool{}
		for fpr, keys := range byAcct {
			set := make(map[string]bool, len(keys))
			for _, k := range keys {
				set[k] = true
			}
			groups[g][fpr] = set
		}
	}
	s.mu.Lock()
	if s.deviceLiveGroups == nil { // a scan that already ran is newer
		s.deviceLiveGroups = groups
		s.deviceLive = unionLive(groups)
	}
	s.mu.Unlock()
}

func (s *Service) persistDeviceLive(groups map[string]map[string]map[string]bool) {
	m := make(map[string]map[string][]string, len(groups))
	for g, byAcct := range groups {
		m[g] = map[string][]string{}
		for fpr, keys := range byAcct {
			for k := range keys {
				m[g][fpr] = append(m[g][fpr], k)
			}
		}
	}
	if raw, err := json.Marshal(m); err == nil {
		_ = s.store.SetSetting(deviceLiveKey, string(raw))
	}
}

// cleanDeviceLabel drops control characters and caps the label at 32 runes.
func cleanDeviceLabel(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 32 {
		s = string(r[:32])
	}
	return s
}

// ContactDevice is one device on a contact's account, as this device came to
// know it: the quiet line in their details ("3 devices · Pixel added 26 Nov").
type ContactDevice struct {
	Key    string `json:"key"`    // device public key, hex
	Name   string `json:"name"`   // the certificate's device name, cleaned
	Issued int64  `json:"issued"` // when the account signed it, unix seconds (0 unknown)
	Seen   int64  `json:"seen"`   // when this device first saw it, unix seconds
}

const deviceLogKey = "contacts.devicelog"

// logContactDevices appends devices that joined a contact's account to the
// per-contact log behind ContactDevices. Nothing is written to any chat.
func (s *Service) logContactDevices(fresh []newDevice) {
	if len(fresh) == 0 {
		return
	}
	s.deviceLogMu.Lock()
	defer s.deviceLogMu.Unlock()
	log := map[string][]ContactDevice{}
	if raw, err := s.store.GetSetting(deviceLogKey); err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &log)
	}
	now := time.Now().Unix()
	for _, d := range fresh {
		list := log[d.fpr]
		dup := false
		for _, e := range list {
			dup = dup || e.Key == d.key
		}
		if dup {
			continue
		}
		list = append(list, ContactDevice{Key: d.key, Name: cleanDeviceLabel(d.name), Issued: d.issued, Seen: now})
		// A long history is not a details line: the newest 16 are kept.
		if len(list) > 16 {
			list = list[len(list)-16:]
		}
		log[d.fpr] = list
	}
	if raw, err := json.Marshal(log); err == nil {
		_ = s.store.SetSetting(deviceLogKey, string(raw))
	}
}

// ContactDevices is what this device knows of a contact's devices: how many
// are on their account in the groups we share now, and the ones it saw added,
// oldest first. Every one of them is on the account's chain (see the top of
// this file); the list is for a details view, never a notice.
//
// Both are of the latest scan: a device that left every group we share, or
// that their account unlinked, is neither counted nor listed.
func (s *Service) ContactDevices(fingerprint string) (count int, added []ContactDevice) {
	s.mu.RLock()
	live := s.deviceLive[fingerprint]
	count = len(live)
	s.mu.RUnlock()
	s.deviceLogMu.Lock()
	defer s.deviceLogMu.Unlock()
	log := map[string][]ContactDevice{}
	if raw, err := s.store.GetSetting(deviceLogKey); err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &log)
	}
	for _, d := range log[fingerprint] {
		if live[d.Key] {
			added = append(added, d)
		}
	}
	return count, added
}

// verifiedDevicesKey persists, per verified contact, the devices on their
// account when they were verified.
const verifiedDevicesKey = "contacts.verifieddevices"

// loadVerifiedDevicesLocked reads the verified-device sets once. deviceLogMu
// must be held.
func (s *Service) loadVerifiedDevicesLocked() {
	if s.verifiedDevs != nil {
		return
	}
	s.verifiedDevs = map[string]map[string]bool{}
	raw, err := s.store.GetSetting(verifiedDevicesKey)
	if err != nil || raw == "" {
		return
	}
	var m map[string][]string
	if json.Unmarshal([]byte(raw), &m) != nil {
		return
	}
	for fpr, keys := range m {
		set := make(map[string]bool, len(keys))
		for _, k := range keys {
			set[k] = true
		}
		s.verifiedDevs[fpr] = set
	}
}

// persistVerifiedDevicesLocked writes them back. deviceLogMu must be held.
func (s *Service) persistVerifiedDevicesLocked() {
	m := make(map[string][]string, len(s.verifiedDevs))
	for fpr, set := range s.verifiedDevs {
		keys := make([]string, 0, len(set))
		for k := range set {
			keys = append(keys, k)
		}
		m[fpr] = keys
	}
	if raw, err := json.Marshal(m); err == nil {
		_ = s.store.SetSetting(verifiedDevicesKey, string(raw))
	}
}

// knownDevices copies the devices ever seen behind fpr.
func (s *Service) knownDevices(fpr string) map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]bool, len(s.devices[fpr]))
	for k := range s.devices[fpr] {
		out[k] = true
	}
	return out
}

// snapshotVerifiedDevices records fpr's devices as the ones verified. With
// none known yet, the record waits for the first scan to see some
// (seedVerifiedDevices), so a contact's first device is never "new".
func (s *Service) snapshotVerifiedDevices(fpr string) {
	known := s.knownDevices(fpr)
	s.deviceLogMu.Lock()
	defer s.deviceLogMu.Unlock()
	s.loadVerifiedDevicesLocked()
	if len(known) == 0 {
		delete(s.verifiedDevs, fpr)
	} else {
		s.verifiedDevs[fpr] = known
	}
	s.persistVerifiedDevicesLocked()
}

// seedVerifiedDevices gives every verified contact without a record one:
// the devices known for them so far.
func (s *Service) seedVerifiedDevices() {
	verified := s.VerifiedFingerprints()
	if len(verified) == 0 {
		return
	}
	s.deviceLogMu.Lock()
	defer s.deviceLogMu.Unlock()
	s.loadVerifiedDevicesLocked()
	changed := false
	for fpr, ok := range verified {
		if !ok || s.verifiedDevs[fpr] != nil {
			continue
		}
		if known := s.knownDevices(fpr); len(known) > 0 {
			s.verifiedDevs[fpr] = known
			changed = true
		}
	}
	if changed {
		s.persistVerifiedDevicesLocked()
	}
}

// NewDeviceSinceVerified reports whether a verified contact has a device on
// their account now that they did not have when they were verified. It is a
// mark on the contact and their verified badge, never a line in a chat, and
// verifying them again clears it. An unverified contact is never marked.
func (s *Service) NewDeviceSinceVerified(fpr string) bool {
	return s.NewDeviceSinceVerifiedAmong(fpr, s.VerifiedFingerprints())
}

// NewDeviceSinceVerifiedAmong is NewDeviceSinceVerified with the verified set
// already in hand (VerifiedFingerprints), for a caller marking many members.
func (s *Service) NewDeviceSinceVerifiedAmong(fpr string, verified map[string]bool) bool {
	if fpr == "" || !verified[fpr] {
		return false
	}
	s.mu.RLock()
	live := make([]string, 0, len(s.deviceLive[fpr]))
	for k := range s.deviceLive[fpr] {
		live = append(live, k)
	}
	s.mu.RUnlock()
	s.deviceLogMu.Lock()
	defer s.deviceLogMu.Unlock()
	s.loadVerifiedDevicesLocked()
	at := s.verifiedDevs[fpr]
	if at == nil {
		return false
	}
	for _, k := range live {
		if !at[k] {
			return true
		}
	}
	return false
}

// strangerNoticesPerDM caps the notices one 1:1 can hold for keys off its
// contact's chain: past it, a peer that can put keys into the conversation
// is already being told about, and more lines would only bury the chat.
const strangerNoticesPerDM = 4

// admitStranger notes key in the 1:1 dmID and reports whether it is a new
// notice to write: not noted before, and the 1:1 is under its cap.
func admitStranger(noted map[string]bool, dmID, key string) bool {
	k := dmID + "|" + key
	if noted[k] {
		return false
	}
	n := 0
	for e := range noted {
		if strings.HasPrefix(e, dmID+"|") {
			n++
		}
	}
	if n >= strangerNoticesPerDM {
		return false
	}
	noted[k] = true
	return true
}

// pruneStrangers forgets the keys noted in dmID that are no longer in it
// (present holds the stranger keys there now), so the cap counts only keys
// still present, and reports whether anything was dropped. A key that comes
// back is told about again.
func pruneStrangers(noted map[string]bool, dmID string, present map[string]bool) bool {
	prefix := dmID + "|"
	dropped := false
	for e := range noted {
		if strings.HasPrefix(e, prefix) && !present[strings.TrimPrefix(e, prefix)] {
			delete(noted, e)
			dropped = true
		}
	}
	return dropped
}

const strangerKeysKey = "contacts.dmstrangers"

// strangerNoticeText is the visible line for a key in a 1:1 that is neither
// of its two people's. who is the contact's display name.
func strangerNoticeText(who string) string {
	if who == "" {
		who = "this contact"
	}
	return fmt.Sprintf("A key that isn't linked to %s's account is in this conversation. Check with %s another way before you share anything private.", who, who)
}

// noteStrangersInDMs looks, in every 1:1, for a member whose account is
// neither ours nor the contact's, and says so once per key in that 1:1.
func (s *Service) noteStrangersInDMs() {
	type dm struct {
		id, peer, channel string
		group             []byte
	}
	s.mu.RLock()
	var dms []dm
	for id, g := range s.guilds {
		if g.Kind != "dm" || len(g.Channels) == 0 {
			continue
		}
		if p := s.dmPeers[id]; p != "" {
			dms = append(dms, dm{id, p, g.Channels[0].ID, g.GroupID})
		}
	}
	s.mu.RUnlock()
	if len(dms) == 0 {
		return
	}
	noted := map[string]bool{}
	if raw, err := s.store.GetSetting(strangerKeysKey); err == nil && raw != "" {
		var keys []string
		if json.Unmarshal([]byte(raw), &keys) == nil {
			for _, k := range keys {
				noted[k] = true
			}
		}
	}
	me := s.id.Fingerprint()
	added := false
	for _, d := range dms {
		creds, err := s.mls.Members(s.ctx, d.group)
		if err != nil {
			continue
		}
		present := map[string]bool{}
		for _, c := range creds {
			if fpr := accountFingerprintOf(c); fpr != "" && fpr != me && fpr != d.peer {
				present[hex.EncodeToString(mailboxKeyOf(c))] = true
			}
		}
		if pruneStrangers(noted, d.id, present) {
			added = true // the shorter list is written back below
		}
		for _, c := range creds {
			fpr := accountFingerprintOf(c)
			if fpr == "" || fpr == me || fpr == d.peer {
				continue
			}
			if !admitStranger(noted, d.id, hex.EncodeToString(mailboxKeyOf(c))) {
				continue
			}
			added = true
			name := s.ProfileName(d.peer)
			msg, err := domain.NewMessage(d.channel, accountKeyOf(c), strangerNoticeText(name))
			if err != nil {
				continue
			}
			msg.Kind = "keychange"
			if _, err := s.store.SaveMessage(msg); err == nil {
				s.emitMessage(msg)
			}
		}
	}
	if added {
		keys := make([]string, 0, len(noted))
		for k := range noted {
			keys = append(keys, k)
		}
		if raw, err := json.Marshal(keys); err == nil {
			_ = s.store.SetSetting(strangerKeysKey, string(raw))
		}
	}
}
