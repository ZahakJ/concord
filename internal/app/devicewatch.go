package app

import (
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/zahak/concord/internal/domain"
)

// "A device was added to this account" — the half of safety numbers Concord was
// missing.
//
// Verification compares ACCOUNT fingerprints, and a linked device doesn't
// change one: the account key signs a device certificate, and every peer maps
// that leaf back to the same account (see device.go). That is the right design,
// and it is exactly why the growth needs saying out loud — the set of devices
// that can read your DMs with someone can grow silently while their verified
// checkmark stays green, which is precisely the move safety numbers exist to
// catch. A coerced or borrowed link is the adversary here.
//
// So we notice, and we say so, but we do NOT drop verification the way Signal
// does on a safety-number change: nothing about their identity changed, and
// un-verifying every contact each time a friend pairs a phone would train
// people to click through the one alert that matters. The notice says what
// actually happened and leaves the checkmark alone.
//
// It is written locally, into the 1:1 with that person, as kind "device" —
// a non-"" kind, so it never pings, never counts unread, and (like
// "call-missed") is dropped by the sync ingest, which means our observation
// stays ours and never travels back to them.

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

// noteDeviceLeaves folds every shared group's member leaves into the per-contact
// device roster and reports the ones that are new. Runs at startup (to seed) and
// on the heal tick, which is the only cadence this needs: a device that joins an
// account shows up as a leaf in the groups we share, and it stays there.
func (s *Service) noteDeviceLeaves() {
	s.mu.RLock()
	ids := make([][]byte, 0, len(s.guilds))
	for _, g := range s.guilds {
		ids = append(ids, g.GroupID)
	}
	s.mu.RUnlock()

	// account fingerprint -> device key hex -> account public key
	seen := map[string]map[string][]byte{}
	for _, groupID := range ids {
		creds, err := s.mls.Members(s.ctx, groupID)
		if err != nil {
			continue // unreadable right now: absence here must never mean "gone"
		}
		for _, c := range creds {
			fpr := accountFingerprintOf(c)
			if fpr == "" || fpr == s.id.Fingerprint() {
				continue
			}
			if seen[fpr] == nil {
				seen[fpr] = map[string][]byte{}
			}
			seen[fpr][hex.EncodeToString(mailboxKeyOf(c))] = accountKeyOf(c)
		}
	}

	type newDevice struct {
		fpr     string
		account []byte
		total   int
	}
	var fresh []newDevice
	changed := false
	s.mu.Lock()
	for fpr, keys := range seen {
		known, first := s.devices[fpr], s.devices[fpr] == nil
		if first {
			known = map[string]bool{}
			s.devices[fpr] = known
		}
		var addedFor []byte
		for k, accountPub := range keys {
			if known[k] {
				continue
			}
			known[k] = true
			changed = true
			addedFor = accountPub
		}
		// First sight of an account seeds silently — we have no idea which of
		// their devices are old, and "Alice has 2 devices" is not news the first
		// time you meet Alice. Otherwise one notice per account per scan, with
		// the count taken after the whole account is folded in so two devices
		// arriving together don't report a stale total.
		if !first && addedFor != nil {
			fresh = append(fresh, newDevice{fpr: fpr, account: addedFor, total: len(known)})
		}
	}
	s.mu.Unlock()

	if !changed {
		return
	}
	s.persistDeviceRoster()
	for _, d := range fresh {
		s.announceNewDevice(d.fpr, d.account, d.total)
	}
}

// announceNewDevice writes the local notice into the 1:1 with that person —
// the one place the growth actually matters, and the place they'd look. A
// contact we only share a server with gets no notice: it would be a line in a
// channel everyone else can see, about someone else's phone.
func (s *Service) announceNewDevice(fingerprint string, accountPub []byte, total int) {
	dm := s.findPeerDM(fingerprint)
	if dm == nil || len(dm.Channels) == 0 {
		return
	}
	msg, err := domain.NewMessage(dm.Channels[0].ID, accountPub, fmt.Sprintf(
		"linked another device — %d now sign in to this account. Their safety number hasn't changed, so this is only worth asking about if it wasn't them.", total))
	if err != nil {
		return
	}
	msg.Kind = "device"
	msg.Name = s.ProfileName(fingerprint)
	if _, err := s.store.SaveMessage(msg); err != nil {
		return
	}
	s.emitMessage(msg)
}
