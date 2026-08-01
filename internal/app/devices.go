package app

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"runtime"
	"sort"
	"time"

	p2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/zahak/concord/internal/identity"
)

// This file is the account's view of ITSELF: which devices sign in to it, how to
// reach them, and how they come to recognise each other.
//
// The problem it solves is that a linked device's libp2p PeerID is its own
// device key. Nothing about that key says which account it belongs to, and every
// question the app asks about a connection — is this a member, is this me, whose
// name goes in the voice room, should I serve this history request — is really a
// question about the ACCOUNT. The mapping lives in a certificate the account key
// signed, and until this file existed the only way to see one was to stumble
// across it in traffic that had nothing to do with the device: a group roster we
// could read, a message we could decrypt, a join handshake.
//
// Measured, on a desktop that could not read the mapping out of a roster (a phone
// linked by an older build, or one whose leaf is in no group we share): the
// device connected and stayed a stranger for the entire session — no presence
// event, no history sync (the connect tail gives up on an unplaceable peer after
// 11.5s and returns), voice presence attributed to a fingerprint that matches
// nobody, and a "unknown peer" row in the diagnostics panel. Not slow: never.
//
// The fix is to stop waiting for the accident. A peer we CAN place introduces
// itself over /concord/hello the moment it connects, the far end verifies the
// certificate against the key the connection is already authenticated to, and
// everything that was blocked on the mapping runs at once.

// deviceRegistryKey persists this account's own device registry.
//
// It supersedes ownDevicesKey ("account.devices"), which held only the
// certificates and only the ones WE issued. Both limits mattered: a phone linked
// from a different desktop, or before the issuer kept its certs at all, left this
// device with nothing on disk — so recognition depended on the roster every time,
// including on the launch where the roster had not been read yet.
const deviceRegistryKey = "account.devices.v2"

// ownDevice is one device of this account as we know it locally.
type ownDevice struct {
	// Key is the hex device pubkey. It is also the libp2p host key, so it is
	// exactly what a PeerID for this device is built from.
	Key  string `json:"key"`
	Name string `json:"name,omitempty"`
	// Cert is the account-signed certificate. Absent for the account-key device
	// (an install that was never linked: its PeerID IS the account key, so no
	// certificate is needed or issued).
	Cert     *identity.DeviceCert `json:"cert,omitempty"`
	LinkedAt int64                `json:"linked,omitempty"`
	LastSeen int64                `json:"seen,omitempty"`
	// AppVersion is the last version this device reported in a hello. Purely
	// diagnostic — it is what lets "the fix shipped" be distinguished from "the
	// fix is installed" without physical access to the device.
	AppVersion string `json:"appVersion,omitempty"`
	// Revoked is when this device was unlinked, 0 if it wasn't. A revoked device
	// stays in the registry rather than being deleted: forgetting it would make
	// it a stranger again the next time it connects, which is precisely when we
	// need to recognise it in order to tell it it's been revoked.
	Revoked int64 `json:"revokedAt,omitempty"`
}

// deviceRegistry reads the registry, migrating the older cert-only list on first
// use. Certs are re-verified against THIS account on every read — a restored or
// replaced account key must never drag another account's devices along.
func (s *Service) deviceRegistry() []ownDevice {
	raw, err := s.store.GetSetting(deviceRegistryKey)
	if err == nil && raw != "" {
		var recs []ownDevice
		if json.Unmarshal([]byte(raw), &recs) == nil {
			return s.filterOwnDevices(recs)
		}
	}
	// Migration: the old key held bare certs we had issued.
	var recs []ownDevice
	if legacy, err := s.store.GetSetting(ownDevicesKey); err == nil && legacy != "" {
		var certs []*identity.DeviceCert
		if json.Unmarshal([]byte(legacy), &certs) == nil {
			for _, c := range certs {
				if c == nil {
					continue
				}
				recs = append(recs, ownDevice{
					Key: hex.EncodeToString(c.DevicePub), Name: c.DeviceName,
					Cert: c, LinkedAt: c.IssuedAt,
				})
			}
		}
	}
	return s.filterOwnDevices(recs)
}

func (s *Service) filterOwnDevices(recs []ownDevice) []ownDevice {
	out := recs[:0]
	for _, r := range recs {
		if r.Cert != nil && (!r.Cert.Verify() || !ed25519Equal(r.Cert.AccountPub, s.id.PublicKey())) {
			continue
		}
		if r.Key == "" {
			continue
		}
		out = append(out, r)
	}
	return out
}

func (s *Service) saveDeviceRegistry(recs []ownDevice) {
	if raw, err := json.Marshal(recs); err == nil {
		_ = s.store.SetSetting(deviceRegistryKey, string(raw))
	}
}

// noteOwnDevice records (or refreshes) one of this account's devices.
//
// It is called for every own-account certificate we ever see, not only the ones
// we issued. That is the whole point: the user's phone was linked years of builds
// ago by a desktop that did not keep the cert, so the only copy left is the one
// the phone itself presents. Writing it down the first time the phone says hello
// means the recognition survives every restart afterwards, with no roster and no
// network involved.
func (s *Service) noteOwnDevice(cert *identity.DeviceCert, name, appVersion string, seen bool) {
	if cert == nil || !cert.Verify() || !ed25519Equal(cert.AccountPub, s.id.PublicKey()) {
		return
	}
	key := hex.EncodeToString(cert.DevicePub)
	s.regMu.Lock()
	defer s.regMu.Unlock()
	recs := s.deviceRegistry()
	now := time.Now().Unix()
	for i := range recs {
		if recs[i].Key != key {
			continue
		}
		changed := false
		if recs[i].Cert == nil {
			recs[i].Cert, changed = cert, true
		}
		if name != "" && recs[i].Name != name {
			recs[i].Name, changed = name, true
		}
		if appVersion != "" && recs[i].AppVersion != appVersion {
			recs[i].AppVersion, changed = appVersion, true
		}
		// Last-seen is written at most once a minute: a heartbeat that rewrote a
		// settings row on every hello would be a database write per connection.
		if seen && now-recs[i].LastSeen > 60 {
			recs[i].LastSeen, changed = now, true
		}
		if changed {
			s.saveDeviceRegistry(recs)
		}
		return
	}
	rec := ownDevice{Key: key, Name: name, Cert: cert, LinkedAt: cert.IssuedAt}
	if rec.Name == "" {
		rec.Name = cert.DeviceName
	}
	if seen {
		rec.LastSeen = now
	}
	s.saveDeviceRegistry(append(recs, rec))
}

// ownDeviceCerts is the certificate view of the registry, kept for the callers
// (and tests) that only want the certs.
func (s *Service) ownDeviceCerts() []*identity.DeviceCert {
	var out []*identity.DeviceCert
	for _, r := range s.deviceRegistry() {
		if r.Cert != nil && r.Revoked == 0 {
			out = append(out, r.Cert)
		}
	}
	return out
}

// loadOwnDevices seeds the device→account map with our own devices at startup,
// before any of them can connect and be mistaken for a stranger.
//
// It also writes OURSELVES into the registry when this install is a linked
// device. Nothing else would: we learn other devices from their certificates,
// and our own certificate arrives from the marker rather than over the wire. A
// phone that skipped this listed its desktop and not itself, which is a device
// list with the one device you are definitely holding missing from it.
func (s *Service) loadOwnDevices() {
	if cert, ok := identity.ParseDeviceCert(s.myCredential); ok {
		s.noteOwnDevice(cert, cert.DeviceName, "", true)
	}
	for _, c := range s.ownDeviceCerts() {
		s.learnDeviceCert(c.Marshal())
	}
}

// defaultDeviceName is the label a newly linked device announces to its own
// account. The platform, not the hostname: this string ends up in a certificate
// that travels with the device's MLS leaf into every shared group, so it is seen
// by people the device's owner may barely know. "Android device" tells the owner
// which row is their phone and tells everyone else nothing they couldn't guess.
func defaultDeviceName() string {
	switch runtime.GOOS {
	case "android":
		return "Android device"
	case "ios":
		return "iPhone or iPad"
	case "darwin":
		return "Mac"
	case "windows":
		return "Windows PC"
	case "linux":
		return "Linux desktop"
	}
	return "New device"
}

// devicePeerID turns a device pubkey into the libp2p PeerID that device runs
// under — they are the same key, which is what makes a targeted lookup possible.
func devicePeerID(devicePub []byte) (peer.ID, bool) {
	pub, err := p2pcrypto.UnmarshalEd25519PublicKey(ed25519.PublicKey(devicePub))
	if err != nil {
		return "", false
	}
	id, err := peer.IDFromPublicKey(pub)
	if err != nil {
		return "", false
	}
	return id, true
}

// otherDevicePeers is every device of this account except the one we are running
// on, and except any we have revoked.
//
// The account key is included as a device in its own right. An account that was
// never linked runs its libp2p host on the account key itself, so from a linked
// phone's point of view "the desktop" is a peer whose PeerID is the account key
// — a device with no certificate, and the one the phone most needs to reach.
func (s *Service) otherDevicePeers() []peer.ID {
	self := s.host.PeerID()
	seen := map[peer.ID]bool{self: true}
	var out []peer.ID
	add := func(pub []byte) {
		if id, ok := devicePeerID(pub); ok && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	add(s.id.PublicKey())
	for _, r := range s.deviceRegistry() {
		if r.Revoked != 0 {
			continue
		}
		if raw, err := hex.DecodeString(r.Key); err == nil {
			add(raw)
		}
	}
	return out
}

// ---- keeping our own devices close ----

// The reach schedule: eager while a device of ours is missing, idle when they
// are all here. The eagerness is the point — the first attempt after launch
// almost always fails because the DHT routing table is still empty, and a flat
// interval turns that guaranteed first miss into a guaranteed wait of exactly
// one interval. Measured with a flat 10s beat: a returning phone was reached at
// T+10.07s, all of it spent waiting for the second attempt.
const (
	deviceReachMin  = 2 * time.Second
	deviceReachMax  = 30 * time.Second
	deviceReachIdle = 30 * time.Second
)

// keepDevicesClose dials this account's other devices whenever one is missing.
//
// Rendezvous discovery would find them eventually — a 15s beat over a provider
// record the other device has to have successfully published — and "eventually"
// is the complaint. Our own devices are the one case where we do not have to
// search at all: we hold their certificates, a certificate names the device key,
// and the device key IS the PeerID. So we ask the DHT for that exact peer and
// dial it, which needs no advertisement from them and no luck from us.
func (s *Service) keepDevicesClose() {
	wait, wasComplete := deviceReachMin, false
	for {
		missing := s.reachOwnDevices()
		switch {
		case missing == 0:
			wait, wasComplete = deviceReachIdle, true
		case wasComplete:
			// A device just went away. Start eager again rather than inheriting
			// the idle interval — otherwise the phone you picked up half a
			// second ago waits out a full idle beat before anyone looks for it.
			wait, wasComplete = deviceReachMin, false
		case wait < deviceReachMax:
			wait *= 2
		}
		select {
		case <-s.ctx.Done():
			return
		case <-s.bgWakeCh():
			// Foregrounded: the OS just told us the user is back, which is
			// exactly when one of their devices tends to have moved. Look now.
		case <-time.After(s.bgPace(wait)):
			// Backgrounded, the eager schedule is what dialed an absent desktop
			// every 30s all night; bgPace stretches it to the background beat.
		}
	}
}

// reachOwnDevices dials every device of this account we are not connected to,
// returning how many were missing.
func (s *Service) reachOwnDevices() int {
	want := s.otherDevicePeers()
	if len(want) == 0 {
		return 0
	}
	connected := map[peer.ID]bool{}
	for _, p := range s.host.Peers() {
		connected[p] = true
	}
	missing := 0
	for _, id := range want {
		if connected[id] {
			continue
		}
		missing++
		// A lookup can outlive the beat that started it, so one attempt per
		// device at a time — otherwise an unreachable phone accumulates a dial
		// per tick forever.
		if !s.reaching.claim(id) {
			continue
		}
		go func(id peer.ID) {
			defer s.reaching.release(id)
			ctx, cancel := context.WithTimeout(s.ctx, 20*time.Second)
			defer cancel()
			// Ask the DHT where it is. A hit also seeds the peerstore, so the
			// Connect below usually has an address without dialling anything.
			pi, err := s.host.FindPeer(ctx, id)
			if err != nil {
				// No DHT, or nobody has heard of it. A bare Connect can still
				// succeed from a cached address, so it is worth the attempt.
				pi = peer.AddrInfo{ID: id}
			}
			_ = s.host.Connect(ctx, pi)
		}(id)
	}
	return missing
}

// ---- diagnostics ----

// LinkedDeviceView is one device of this account, for the diagnostics panel.
//
// This is deliberately its own list rather than a row in the peer list. A device
// of yours is not a peer in the sense that list means: it is you, it is expected
// to be there, and the questions you have about it (is it online, when was it
// last here, is it going through a relay) are not the questions you have about
// the strangers your rendezvous also introduces you to. It used to render there
// as "unknown peer", which is the worst of both.
type LinkedDeviceView struct {
	// Key is the hex device pubkey — stable, and what Unlink is addressed to.
	Key      string `json:"key"`
	PeerID   string `json:"peerId"`
	Name     string `json:"name,omitempty"`
	ThisOne  bool   `json:"thisOne,omitempty"` // the device you are reading this on
	Online   bool   `json:"online"`
	LinkedAt int64  `json:"linkedAt,omitempty"`
	LastSeen int64  `json:"lastSeen,omitempty"`
	Revoked  int64  `json:"revokedAt,omitempty"`
	// How it is reached right now — empty when it isn't connected.
	Transport string `json:"transport,omitempty"` // quic | tcp | relay | p2p
	Relayed   bool   `json:"relayed,omitempty"`
	Direction string `json:"direction,omitempty"` // inbound | outbound
	RTTms     int64  `json:"rttMs,omitempty"`
	// AppVersion the device last reported. Empty for a device that has not said
	// hello since this field existed — which is itself the diagnostic.
	AppVersion string `json:"appVersion,omitempty"`
}

// LinkedDevices reports every device signed in to this account, connected or
// not. The list always contains at least this device.
func (s *Service) LinkedDevices() []LinkedDeviceView {
	self := s.host.PeerID()
	byPeer := map[peer.ID]bool{}
	for _, p := range s.host.Peers() {
		byPeer[p] = true
	}

	out := []LinkedDeviceView{}
	add := func(pub []byte, rec ownDevice) {
		id, ok := devicePeerID(pub)
		if !ok {
			return
		}
		v := LinkedDeviceView{
			Key: hex.EncodeToString(pub), PeerID: id.String(), Name: rec.Name,
			AppVersion: rec.AppVersion,
			ThisOne:    id == self, LinkedAt: rec.LinkedAt, LastSeen: rec.LastSeen,
			Revoked: rec.Revoked,
		}
		if v.ThisOne {
			v.Online = true
			v.LastSeen = time.Now().Unix()
			v.Name = "This device" // whatever it calls itself elsewhere, here it is the one you're holding
		} else if byPeer[id] {
			v.Online = true
			v.LastSeen = time.Now().Unix()
			v.Transport, v.Relayed, v.Direction = s.connShape(id)
			v.RTTms = s.cachedRTT(id)
		}
		out = append(out, v)
	}

	// The account-key device first: it is the original install, and on an account
	// that has never been linked it is the only row there is.
	seen := map[string]bool{}
	accountKey := []byte(s.id.PublicKey())
	add(accountKey, ownDevice{Name: "Original device"})
	seen[hex.EncodeToString(accountKey)] = true
	for _, r := range s.deviceRegistry() {
		if seen[r.Key] {
			continue
		}
		seen[r.Key] = true
		if raw, err := hex.DecodeString(r.Key); err == nil {
			add(raw, r)
		}
	}
	// This device on top, then whoever is online, then most recently seen.
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.ThisOne != b.ThisOne {
			return a.ThisOne
		}
		if a.Online != b.Online {
			return a.Online
		}
		return a.LastSeen > b.LastSeen
	})
	return out
}

// connShape describes how a connected peer is currently reached.
func (s *Service) connShape(p peer.ID) (transport string, relayed bool, direction string) {
	conns := s.host.Libp2p().Network().ConnsToPeer(p)
	if len(conns) == 0 {
		return "", false, ""
	}
	return transportOf(conns[0].RemoteMultiaddr().String()), isRelayed(conns[0].RemoteMultiaddr().String()), directionOf(conns[0])
}
