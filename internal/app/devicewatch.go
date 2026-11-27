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
	s.deviceScanMu.Lock()
	defer s.deviceScanMu.Unlock()
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

	if changed {
		s.persistDeviceRoster()
		s.logContactDevices(fresh)
	}
	s.noteStrangersInDMs()
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
func (s *Service) ContactDevices(fingerprint string) (count int, added []ContactDevice) {
	s.mu.RLock()
	count = len(s.devices[fingerprint])
	s.mu.RUnlock()
	s.deviceLogMu.Lock()
	defer s.deviceLogMu.Unlock()
	log := map[string][]ContactDevice{}
	if raw, err := s.store.GetSetting(deviceLogKey); err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &log)
	}
	return count, log[fingerprint]
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
		for _, c := range creds {
			fpr := accountFingerprintOf(c)
			if fpr == "" || fpr == me || fpr == d.peer {
				continue
			}
			k := d.id + "|" + hex.EncodeToString(mailboxKeyOf(c))
			if noted[k] {
				continue
			}
			noted[k] = true
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
