package app

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"golang.org/x/crypto/hkdf"
	"golang.org/x/sync/singleflight"

	"github.com/zahak/concord/internal/crypto/mls"
	"github.com/zahak/concord/internal/domain"
	"github.com/zahak/concord/internal/identity"
	cnet "github.com/zahak/concord/internal/net"
	"github.com/zahak/concord/internal/store"
)

// Service is the orchestration layer (layer 6): it owns the identity, network
// host, gossipsub bus, MLS group-crypto engine, and encrypted store, and
// presents one UI-agnostic API. Both the headless CLI and the Wails GUI drive
// Concord exclusively through a Service.
type Service struct {
	ctx     context.Context
	dataDir string

	id    *identity.Identity
	host  *cnet.Host
	ps    *cnet.PubSub
	mls   mls.Engine
	store *store.Store

	mu             sync.RWMutex
	guilds         map[string]*domain.Guild // by guild ID
	channelToGuild map[string]string        // channel ID -> guild ID
	onMessage      []func(domain.Message)

	voiceMu         sync.Mutex
	voiceRooms      map[string]context.CancelFunc // channel ID -> heartbeat stop
	onVoicePresence []func(from, fingerprint, channelID, action string)
	onVoiceSignal   []func(from string, data []byte)

	onTyping      []func(from, channelID string)
	onGuildUpdate []func()

	profiles map[string]Profile // fingerprint -> profile, learned from peers

	// outOfSync marks guilds whose MLS epoch gap could not be bridged by any
	// peer's commit log (see sync.go); the UI surfaces a re-invite hint.
	outOfSync map[string]bool

	// attachFlight collapses concurrent fetches of one attachment blob (e.g.
	// the same image rendered several times) into a single network request.
	attachFlight singleflight.Group

	// previews caches link-preview scrapes (see preview.go).
	previews *previewCache
}

// Profile is a member's self-asserted presentation: display name, a short
// status line, an avatar (emoji and/or a small image), and an accent color.
// All decorative — the fingerprint remains the authenticated identity.
type Profile struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Emoji  string `json:"emoji"`
	Color  string `json:"color"`
	Avatar string `json:"avatar"` // small image as a data URI ("" = none)
}

// maxAvatarBytes caps the avatar data URI so profile broadcasts stay far below
// the gossipsub frame limit (the UI downscales to ~96px JPEG, typically <10 KB).
const maxAvatarBytes = 64 * 1024

// PeerPresence is a UI-facing view of a connected peer.
type PeerPresence struct {
	PeerID      string `json:"peerId"`
	Fingerprint string `json:"fingerprint"`
}

// Config configures a Service.
type Config struct {
	// DataDir is where the keystore and database live.
	DataDir string
	// Passphrase unlocks the identity keystore.
	Passphrase string
	// DisableMDNS turns off LAN discovery; tests set it for determinism.
	DisableMDNS bool
	// BootstrapPeers are multiaddrs of rendezvous/relay nodes for internet-wide
	// discovery. When any are set, the DHT is enabled.
	BootstrapPeers []string
}

// Start loads (or creates) the identity, then brings up storage, networking
// (with LAN discovery), gossipsub, and the MLS engine, and restores any
// previously-joined guilds. The returned Service must be Closed by the caller.
func Start(ctx context.Context, cfg Config) (*Service, error) {
	id, _, err := identity.LoadOrCreate(keystorePathIn(cfg.DataDir), cfg.Passphrase)
	if err != nil {
		return nil, fmt.Errorf("app: open identity: %w", err)
	}

	st, err := store.Open(dbPathIn(cfg.DataDir), deriveStoreKey(id))
	if err != nil {
		return nil, fmt.Errorf("app: open store: %w", err)
	}

	// Bootstrap sources, in precedence order: explicit config (env) first, then
	// the saved connection settings (set in-app on the login screen).
	bootstrapAddrs := cfg.BootstrapPeers
	if len(bootstrapAddrs) == 0 {
		bootstrapAddrs = LoadNetConfig(cfg.DataDir).Bootstrap
	}
	bootstrap, err := parseBootstrapPeers(bootstrapAddrs)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	host, err := cnet.New(ctx, cnet.Config{
		Identity:       id,
		EnableMDNS:     !cfg.DisableMDNS,
		EnableDHT:      len(bootstrap) > 0,
		BootstrapPeers: bootstrap,
	})
	if err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("app: start network: %w", err)
	}

	ps, err := host.NewPubSub(ctx)
	if err != nil {
		_ = host.Close()
		_ = st.Close()
		return nil, fmt.Errorf("app: start pubsub: %w", err)
	}

	mlsDir, err := mlsDirIn(cfg.DataDir)
	if err != nil {
		_ = host.Close()
		_ = st.Close()
		return nil, fmt.Errorf("app: mls storage dir: %w", err)
	}
	// Persistent MLS storage + a deterministic signing key derived from the
	// identity give full restart recovery: group state from disk, signing key
	// reproduced, so a restarted node can both receive and send.
	engine, err := mls.NewPersistent([]byte(id.PublicKey()), deriveMLSSigningKey(id), mlsDir)
	if err != nil {
		_ = host.Close()
		_ = st.Close()
		return nil, fmt.Errorf("app: start mls: %w", err)
	}

	s := &Service{
		ctx:            ctx,
		dataDir:        cfg.DataDir,
		id:             id,
		host:           host,
		ps:             ps,
		mls:            engine,
		store:          st,
		guilds:         map[string]*domain.Guild{},
		channelToGuild: map[string]string{},
		voiceRooms:     map[string]context.CancelFunc{},
		profiles:       map[string]Profile{},
		outOfSync:      map[string]bool{},
		previews:       newPreviewCache(),
	}

	// Restore learned member profiles so names survive restarts (they are also
	// repaired by invite handshakes and history sync, but this avoids a window
	// of fingerprint-only names right after unlock).
	if rows, err := st.Profiles(); err == nil {
		for _, r := range rows {
			s.profiles[r.Fingerprint] = Profile{Name: r.Name, Status: r.Status, Emoji: r.Emoji, Color: r.Color, Avatar: r.Avatar}
		}
	}

	// Owner side of the join handshake.
	host.HandleInvites(s.handleInviteRequest)

	// Serve history catch-up requests from reconnecting peers.
	host.HandleSync(s.handleSyncRequest)

	// Serve attachment blobs to peers rendering images we hold.
	host.HandleAttachments(s.handleAttachRequest)

	// Inbound WebRTC signaling for voice/video.
	host.HandleSignals(func(from peer.ID, data []byte) {
		s.emitVoiceSignal(from.String(), data)
	})

	// Trust-on-first-use: record every peer we connect to so it can later be
	// verified out-of-band. Also re-broadcast our display name to all guilds a
	// moment after any peer connects: the gossipsub mesh needs to warm up, so a
	// one-shot announce at join time can be missed — this makes names converge
	// reliably (each side re-announces, and learning a new profile triggers a
	// reply, so both peers end up with each other's names).
	host.OnPeerConnected(func(p peer.ID) {
		pp := presenceFor(p)
		_ = st.RecordContact(pp.PeerID, pp.Fingerprint)
		go func() {
			time.Sleep(1500 * time.Millisecond)
			s.announceProfileAll()
			// Pull any history we missed while apart from this peer; one retry
			// covers a stream that failed while the connection settled.
			if !s.syncFromPeer(p) {
				time.Sleep(10 * time.Second)
				s.syncFromPeer(p)
			}
		}()
	})

	// Restore guilds we already belong to and re-subscribe to their topics.
	// MLS group state was reloaded from disk above, so a restarted member can
	// immediately decrypt and send at the epoch it left off.
	guilds, err := st.Guilds()
	if err != nil {
		s.Close()
		return nil, fmt.Errorf("app: load guilds: %w", err)
	}
	for i := range guilds {
		s.trackGuild(&guilds[i])
	}

	return s, nil
}

// Fingerprint returns this peer's safety-number fingerprint.
func (s *Service) Fingerprint() string { return s.id.Fingerprint() }

// PeerID returns this peer's libp2p peer ID as a string.
func (s *Service) PeerID() string { return s.host.PeerID().String() }

// PublicKey returns this peer's Ed25519 account public key (its MLS credential).
func (s *Service) PublicKey() []byte { return []byte(s.id.PublicKey()) }

// Peers returns the currently connected peers.
func (s *Service) Peers() []PeerPresence {
	ids := s.host.Peers()
	out := make([]PeerPresence, 0, len(ids))
	for _, p := range ids {
		out = append(out, presenceFor(p))
	}
	return out
}

// OnPeerConnected registers a presence-up callback.
func (s *Service) OnPeerConnected(fn func(PeerPresence)) {
	s.host.OnPeerConnected(func(p peer.ID) { fn(presenceFor(p)) })
}

// OnPeerDisconnected registers a presence-down callback.
func (s *Service) OnPeerDisconnected(fn func(PeerPresence)) {
	s.host.OnPeerDisconnected(func(p peer.ID) { fn(presenceFor(p)) })
}

// OnMessage registers a callback fired for every message — sent or received —
// after it has been persisted. This is the UI's live message feed.
func (s *Service) OnMessage(fn func(domain.Message)) {
	s.mu.Lock()
	s.onMessage = append(s.onMessage, fn)
	s.mu.Unlock()
}

func (s *Service) emitMessage(m domain.Message) {
	s.mu.RLock()
	cbs := append([]func(domain.Message){}, s.onMessage...)
	s.mu.RUnlock()
	for _, cb := range cbs {
		cb(m)
	}
}

// SetBootstrapLive replaces the saved rendezvous list and dials the new nodes
// immediately (best-effort), so an in-app change helps the current session too.
// Full DHT re-init still happens on next launch.
func (s *Service) SetBootstrapLive(addrs []string) error {
	if err := SaveNetConfig(s.dataDir, NetConfig{Bootstrap: addrs}); err != nil {
		return err
	}
	if infos, err := parseBootstrapPeers(addrs); err == nil {
		for _, pi := range infos {
			pi := pi
			go func() { _ = s.host.Connect(s.ctx, pi) }()
		}
	}
	return nil
}

// DisplayName returns this peer's chosen display name, defaulting to the first
// block of its fingerprint when unset.
func (s *Service) DisplayName() string {
	name, _ := s.store.GetSetting("display_name")
	if strings.TrimSpace(name) != "" {
		return name
	}
	fpr := s.id.Fingerprint()
	if i := strings.IndexByte(fpr, ' '); i > 0 {
		return fpr[:i]
	}
	return fpr
}

// SetDisplayName persists a display name (self-asserted; the fingerprint remains
// the authenticated identity) and re-announces it to every guild.
func (s *Service) SetDisplayName(name string) error {
	if err := s.store.SetSetting("display_name", strings.TrimSpace(name)); err != nil {
		return err
	}
	s.announceProfileAll()
	return nil
}

// SelfProfile returns this peer's own profile.
func (s *Service) SelfProfile() Profile {
	status, _ := s.store.GetSetting("status_text")
	emoji, _ := s.store.GetSetting("avatar_emoji")
	color, _ := s.store.GetSetting("accent_color")
	avatar, _ := s.store.GetSetting("avatar_image")
	return Profile{Name: s.DisplayName(), Status: status, Emoji: emoji, Color: color, Avatar: avatar}
}

// SetProfile persists the full self profile and re-announces it to every guild.
func (s *Service) SetProfile(p Profile) error {
	if len(p.Avatar) > maxAvatarBytes {
		return fmt.Errorf("app: avatar image too large (max %d KB)", maxAvatarBytes/1024)
	}
	if p.Avatar != "" && !strings.HasPrefix(p.Avatar, "data:image/") {
		return fmt.Errorf("app: avatar must be an image data URI")
	}
	for k, v := range map[string]string{
		"display_name": strings.TrimSpace(p.Name),
		"status_text":  strings.TrimSpace(p.Status),
		"avatar_emoji": strings.TrimSpace(p.Emoji),
		"accent_color": strings.TrimSpace(p.Color),
		"avatar_image": p.Avatar,
	} {
		if err := s.store.SetSetting(k, v); err != nil {
			return err
		}
	}
	s.announceProfileAll()
	return nil
}

// ProfileOf returns a peer's learned profile for a fingerprint (zero if unknown).
func (s *Service) ProfileOf(fingerprint string) Profile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.profiles[fingerprint]
}

// learnProfile validates, records, persists, and surfaces a peer's self-asserted
// profile. It is the single sink for every way a profile can arrive (gossip
// announce, invite handshake, history sync). Reports whether the fingerprint
// was previously unknown.
func (s *Service) learnProfile(fingerprint string, p Profile) bool {
	if fingerprint == "" || fingerprint == s.id.Fingerprint() {
		return false
	}
	if len(p.Avatar) > maxAvatarBytes || (p.Avatar != "" && !strings.HasPrefix(p.Avatar, "data:image/")) {
		p.Avatar = "" // reject oversized or non-image avatars from peers
	}
	s.mu.Lock()
	prev, known := s.profiles[fingerprint]
	s.profiles[fingerprint] = p
	s.mu.Unlock()
	if known && prev == p {
		return false // nothing new; skip the store write and UI refresh
	}
	_ = s.store.SaveProfile(store.ProfileRow{
		Fingerprint: fingerprint,
		Name:        p.Name, Status: p.Status, Emoji: p.Emoji, Color: p.Color, Avatar: p.Avatar,
	})
	s.emitGuildUpdate()
	return !known
}

// learnNameHint backfills a display name from a chat message's self-asserted
// name ONLY when we have no learned name for that member yet. This keeps the
// chat and the member list consistent: without it, the roster (learned
// profile) can show a fingerprint code while the message shows a name, or vice
// versa. It never overwrites a richer learned profile (emoji/color/avatar) and
// never replaces a name we already have.
func (s *Service) learnNameHint(fingerprint, name string) {
	name = strings.TrimSpace(name)
	if fingerprint == "" || name == "" || fingerprint == s.id.Fingerprint() {
		return
	}
	s.mu.RLock()
	cur, known := s.profiles[fingerprint]
	s.mu.RUnlock()
	if known && cur.Name != "" {
		return // already have a name; don't clobber it with a stale message's
	}
	p := cur
	p.Name = name
	s.learnProfile(fingerprint, p)
}

// OutOfSync reports whether a guild is stranded at an old MLS epoch that no
// reachable peer's commit log could bridge (the member needs a re-invite).
func (s *Service) OutOfSync(guildID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.outOfSync[guildID]
}

// setOutOfSync flips a guild's stranded flag, refreshing the UI on transitions.
func (s *Service) setOutOfSync(guildID string, stranded bool) {
	s.mu.Lock()
	was := s.outOfSync[guildID]
	if stranded {
		s.outOfSync[guildID] = true
	} else {
		delete(s.outOfSync, guildID)
	}
	s.mu.Unlock()
	if was != stranded {
		s.emitGuildUpdate()
	}
}

// ProfileName returns a peer's learned display name for a fingerprint, or "".
func (s *Service) ProfileName(fingerprint string) string {
	return s.ProfileOf(fingerprint).Name
}

// Contacts returns known peers and their verification status.
func (s *Service) Contacts() ([]domain.Contact, error) {
	return s.store.Contacts()
}

// VerifyContact marks a peer as human-verified after an out-of-band fingerprint
// check.
func (s *Service) VerifyContact(peerID string) error {
	return s.store.SetVerified(peerID)
}

// VerifyFingerprint marks a fingerprint (a member's stable identity) as
// human-verified after an out-of-band comparison.
func (s *Service) VerifyFingerprint(fingerprint string) error {
	return s.store.SetVerifiedByFingerprint(fingerprint)
}

// VerifiedFingerprints returns which fingerprints the user has verified.
func (s *Service) VerifiedFingerprints() map[string]bool {
	m, err := s.store.VerifiedFingerprints()
	if err != nil {
		return map[string]bool{}
	}
	return m
}

// Close shuts everything down.
func (s *Service) Close() error {
	if s.host != nil {
		_ = s.host.Close()
	}
	if s.mls != nil {
		_ = s.mls.Close()
	}
	if s.store != nil {
		_ = s.store.Close()
	}
	return nil
}

// VerifyPassphrase reports whether passphrase decrypts the keystore in dataDir.
// Used to re-check the passphrase when a session is already unlocked, so a
// second unlock attempt can't silently succeed with the wrong passphrase.
func VerifyPassphrase(dataDir, passphrase string) bool {
	_, err := identity.LoadKeystore(keystorePathIn(dataDir), passphrase)
	return err == nil
}

// parseBootstrapPeers converts multiaddr strings (each ending in /p2p/<id>) to
// AddrInfos for DHT bootstrapping and relay.
func parseBootstrapPeers(addrs []string) ([]peer.AddrInfo, error) {
	var out []peer.AddrInfo
	for _, a := range addrs {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		ma, err := multiaddr.NewMultiaddr(a)
		if err != nil {
			return nil, fmt.Errorf("app: bad bootstrap addr %q: %w", a, err)
		}
		pi, err := peer.AddrInfoFromP2pAddr(ma)
		if err != nil {
			return nil, fmt.Errorf("app: bad bootstrap addr %q: %w", a, err)
		}
		out = append(out, *pi)
	}
	return out, nil
}

// deriveStoreKey derives the 32-byte at-rest message key from the identity seed
// via HKDF, so message-body encryption is bound to the passphrase-protected
// identity without a second secret.
func deriveStoreKey(id *identity.Identity) []byte {
	r := hkdf.New(sha256.New, id.Seed(), nil, []byte("concord-store-v1"))
	key := make([]byte, 32)
	_, _ = io.ReadFull(r, key)
	return key
}

// deriveMLSSigningKey derives a dedicated, deterministic Ed25519 key for signing
// MLS messages. It is HKDF-separated from the identity's libp2p key so the two
// protocols never share signing material, yet it is reproducible on every start
// (no storage needed) — which is what makes send-after-restart work.
func deriveMLSSigningKey(id *identity.Identity) ed25519.PrivateKey {
	r := hkdf.New(sha256.New, id.Seed(), nil, []byte("concord-mls-sig-v1"))
	seed := make([]byte, ed25519.SeedSize)
	_, _ = io.ReadFull(r, seed)
	return ed25519.NewKeyFromSeed(seed)
}

// presenceFor derives the UI view for a peer, recovering the fingerprint from
// the Ed25519 key embedded in the libp2p peer ID.
func presenceFor(p peer.ID) PeerPresence {
	pp := PeerPresence{PeerID: p.String()}
	if pub, err := p.ExtractPublicKey(); err == nil {
		if raw, err := pub.Raw(); err == nil {
			pp.Fingerprint = identity.FingerprintOf(raw)
		}
	}
	return pp
}
