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
// So we notice, and we say so, but we do NOT drop verification: nothing about
// their identity changed, and un-verifying every contact each time a friend
// pairs a phone would train people to click through the one alert that
// matters. The notice says what actually happened, offers to verify again, and
// leaves the checkmark alone.
//
// There is no "safety number changed" counterpart to this, because an account's
// fingerprint is the hash of its account key (identity.FingerprintOf) and the
// verified mark is keyed by that fingerprint: a different key is a different
// fingerprint, i.e. a different contact with no verified mark, not a changed
// one. Nothing in the design rotates an account key or links an old one to a
// new one, so there is no event to detect.
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

// newDevice is one device we had not seen behind a contact's account.
type newDevice struct {
	fpr     string
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
			d := newDevice{fpr: fpr, account: accountKeyOf(cred)}
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
	s.mu.RLock()
	ids := make([][]byte, 0, len(s.guilds))
	for _, g := range s.guilds {
		ids = append(ids, g.GroupID)
	}
	s.mu.RUnlock()

	// account fingerprint -> device key hex -> that leaf's credential
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
			seen[fpr][hex.EncodeToString(mailboxKeyOf(c))] = c
		}
	}

	s.mu.Lock()
	fresh, changed := foldDevices(s.devices, seen)
	s.mu.Unlock()

	if !changed {
		return
	}
	s.persistDeviceRoster()
	for _, d := range fresh {
		s.announceNewDevice(d.fpr, d.account, d.name, d.issued)
	}
}

// deviceNoticeText is the calm one-liner for a contact's new device:
// "Ann added a new device (iPhone, 11-14). If this wasn't Ann, verify."
// who is the contact's display name ("" falls back to "They"); device is the
// cert's DeviceName and issued its IssuedAt (unix seconds, 0 = unknown). The
// device label is attacker-supplied text, so it is flattened and capped.
func deviceNoticeText(who, device string, issued int64) string {
	subject := who
	if subject == "" {
		subject = "They"
	}
	device = cleanDeviceLabel(device)
	var parts []string
	if device != "" {
		parts = append(parts, device)
	}
	if issued > 0 {
		parts = append(parts, time.Unix(issued, 0).Local().Format("01-02"))
	}
	detail := ""
	if len(parts) > 0 {
		detail = " (" + strings.Join(parts, ", ") + ")"
	}
	ref := who
	if ref == "" {
		ref = "them"
	}
	return fmt.Sprintf("%s added a new device%s. If this wasn't %s, verify.", subject, detail, ref)
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

// announceNewDevice writes the local notice into the 1:1 with that person —
// the one place the growth actually matters, and the place they'd look. A
// contact we only share a server with gets no notice: it would be a line in a
// channel everyone else can see, about someone else's phone.
func (s *Service) announceNewDevice(fingerprint string, accountPub []byte, device string, issued int64) {
	dm := s.findPeerDM(fingerprint)
	if dm == nil || len(dm.Channels) == 0 {
		return
	}
	name := s.ProfileName(fingerprint)
	msg, err := domain.NewMessage(dm.Channels[0].ID, accountPub, deviceNoticeText(name, device, issued))
	if err != nil {
		return
	}
	msg.Kind = "device"
	msg.Name = name
	if _, err := s.store.SaveMessage(msg); err != nil {
		return
	}
	s.emitMessage(msg)
}
