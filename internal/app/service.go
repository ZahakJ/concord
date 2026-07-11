package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

	// inviteMu serializes the add-and-publish critical section of
	// handleInviteRequest. The owner is the sole committer, but libp2p runs one
	// handler goroutine per inbound stream, so two joiners dialing at once (e.g.
	// a group DM fanning invites to several people) would otherwise produce two
	// commits at the same epoch. Serializing keeps each add on its own epoch.
	inviteMu sync.Mutex

	// pendingDMInvites tracks group-DM invitees we couldn't reach at creation
	// time (guild ID -> set of fingerprints). When such a peer later connects we
	// push them the invite, so a group DM eventually gathers everyone even if
	// some were offline when it was made. Best-effort, in-memory.
	dmInviteMu       sync.Mutex
	pendingDMInvites map[string]map[string]bool

	// Rich presence: an auto-detected "now playing" line that overlays the manual
	// status while something is playing. activity is the current overlay (empty =
	// use the manual status); richPresenceStop cancels the poller when disabled.
	activityMu       sync.Mutex
	activity         string
	activityInfo     *Activity
	richPresenceStop context.CancelFunc

	voiceMu    sync.Mutex
	voiceRooms map[string]context.CancelFunc // channel ID -> heartbeat stop (rooms we're IN)
	// voiceWatched marks voice channels whose presence topic we passively listen
	// to (for every voice channel in every guild), so the sidebar can show who's
	// in a call without us having to join it — Discord-style guild-wide presence.
	voiceWatched    map[string]bool
	onVoicePresence []func(from, fingerprint, channelID, action string)
	onVoiceSignal   []func(from string, data []byte)

	onTyping      []func(from, channelID string)
	onGuildUpdate []func()

	profiles map[string]Profile // fingerprint -> profile, learned from peers

	// nicks holds per-guild display-name overrides: guildID -> fingerprint ->
	// nickname. A nickname shadows the global profile name inside that guild only
	// (Discord-style server nicknames). Members set their own; propagated over
	// the guild-meta topic like profiles.
	nicks map[string]map[string]string

	// Governance (roles/permissions/bans). govOps is the per-guild signed op log;
	// govState is the folded result (rebuilt on every change) that the committer
	// gate and invite gate consult. Both are guarded by mu.
	govOps   map[string][]govOp
	govState map[string]GuildState

	// outOfSync marks guilds whose MLS epoch gap could not be bridged by any
	// peer's commit log (see sync.go); the UI surfaces a re-invite hint.
	outOfSync map[string]bool

	// attachFlight collapses concurrent fetches of one attachment blob (e.g.
	// the same image rendered several times) into a single network request.
	attachFlight singleflight.Group

	// previews caches link-preview scrapes (see preview.go).
	previews *previewCache

	// Mailbox: X25519 keypair for sealing offline envelopes, and the parsed
	// rendezvous nodes that host our mailbox (see mailbox.go).
	mbxPriv   [32]byte
	mbxPub    [32]byte
	bootstrap []peer.AddrInfo

	// Device linking (issuer side): the secret of the currently-displayed link
	// offer, consumed once a joiner proves it. nil = no active offer. See link.go.
	linkMu     sync.Mutex
	linkSecret []byte

	// myCredential is this install's MLS leaf credential — the bare account key
	// (single-device) or this device's cert (linked). It's what we present when
	// joining a guild, so it matches our KeyPackage's leaf.
	myCredential []byte

	// deviceAccounts maps a linked device's public key (hex) to its account
	// fingerprint, learned from device certs seen in rosters/messages. It lets
	// presence() recover the ACCOUNT a device-keyed PeerID belongs to, so a
	// linked phone doesn't surface as a separate "cryptic" peer. Keyed by the
	// raw pubkey embedded in the PeerID.
	deviceMu       sync.RWMutex
	deviceAccounts map[string]string
}

// Profile is a member's self-asserted presentation: display name, a short
// status line, an avatar (emoji and/or a small image), and an accent color.
// All decorative — the fingerprint remains the authenticated identity.
type Profile struct {
	Name   string `json:"name"`
	Status string `json:"status"` // short custom status line
	Emoji  string `json:"emoji"`
	Color  string `json:"color"`
	Avatar string `json:"avatar"` // small image as a data URI ("" = none)
	// Banner is a wide profile-header image (data URI, "" = none), like a guild
	// banner but for a person; shown on the profile card.
	Banner string `json:"banner,omitempty"`
	// Presence is the user-chosen availability: "" / "online" (default), "idle",
	// "dnd", or "invisible" (appear offline). It shades the avatar dot.
	Presence string `json:"presence,omitempty"`
	// Bio is a longer "about me" shown on the profile card.
	Bio string `json:"bio,omitempty"`
	// MailboxPub is this member's X25519 mailbox key, shared so others can seal
	// offline envelopes to them (see mailbox.go). Not user-facing.
	MailboxPub []byte `json:"mbx,omitempty"`
	// Activity is the structured rich-presence payload (now playing: art,
	// duration, position snapshot). Ephemeral — never persisted; old clients
	// ignore it and fall back to the 🎵 status string.
	Activity *Activity `json:"activity,omitempty"`
}

// maxAvatarBytes caps the avatar data URI so profile broadcasts stay far below
// the gossipsub frame limit (the UI downscales to ~96px JPEG, typically <10 KB).
const maxAvatarBytes = 64 * 1024

// maxProfileBannerBytes caps a user's profile banner. Larger than the avatar
// (it's a wide header) but still bounded so profile broadcasts stay reasonable;
// the UI downscales before sending.
const maxProfileBannerBytes = 256 * 1024

// maxNameBytes bounds self-asserted display names and per-guild nicknames so a
// peer can't publish a pathologically long string over the meta topic.
const maxNameBytes = 64

// maxBioBytes bounds the profile "about me" so profile broadcasts stay small.
const maxBioBytes = 600

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
	// Linked-device mode: if a device marker is present and verifies, this
	// install uses its own device key for the libp2p PeerID and its device cert
	// as the MLS credential. Absent → default single-device behavior (account-key
	// PeerID, bare account credential), unchanged.
	marker, linked := loadDeviceMarker(cfg.DataDir, id.PublicKey())
	var hostKey ed25519.PrivateKey
	if linked {
		hostKey = id.DeviceKey()
	}
	host, err := cnet.New(ctx, cnet.Config{
		Identity:       id,
		HostKey:        hostKey,
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
	// Persistent MLS storage + a deterministic signing key give full restart
	// recovery: group state from disk, signing key reproduced, so a restarted
	// node can both receive and send. mlsIdentity selects the account-key
	// credential (default) or the device cert + device signing key (linked).
	mlsCred, mlsSigning := mlsIdentity(id, marker)
	engine, err := mls.NewPersistent(mlsCred, mlsSigning, mlsDir)
	if err != nil {
		_ = host.Close()
		_ = st.Close()
		return nil, fmt.Errorf("app: start mls: %w", err)
	}

	s := &Service{
		ctx:              ctx,
		dataDir:          cfg.DataDir,
		id:               id,
		host:             host,
		ps:               ps,
		mls:              engine,
		myCredential:     mlsCred,
		store:            st,
		guilds:           map[string]*domain.Guild{},
		channelToGuild:   map[string]string{},
		pendingDMInvites: map[string]map[string]bool{},
		voiceRooms:       map[string]context.CancelFunc{},
		voiceWatched:     map[string]bool{},
		profiles:         map[string]Profile{},
		nicks:            map[string]map[string]string{},
		govOps:           map[string][]govOp{},
		govState:         map[string]GuildState{},
		outOfSync:        map[string]bool{},
		previews:         newPreviewCache(),
		bootstrap:        bootstrap,
	}
	s.mbxPriv, s.mbxPub = deriveMailboxKeys(id)

	// Restore learned member profiles so names survive restarts (they are also
	// repaired by invite handshakes and history sync, but this avoids a window
	// of fingerprint-only names right after unlock).
	if rows, err := st.Profiles(); err == nil {
		for _, r := range rows {
			s.profiles[r.Fingerprint] = Profile{Name: r.Name, Status: r.Status, Emoji: r.Emoji, Color: r.Color, Avatar: r.Avatar, Banner: r.Banner, Presence: r.Presence, Bio: r.Bio, MailboxPub: r.MailboxPub}
		}
	}

	// Restore per-guild nicknames so server-scoped names survive restarts.
	if nicks, err := st.Nicknames(); err == nil {
		s.nicks = nicks
	}

	// Restore governance op logs and fold them into per-guild state (roles/bans).
	if all, err := st.AllGuildOps(); err == nil {
		for gid, raw := range all {
			for _, b := range raw {
				var o govOp
				if json.Unmarshal(b, &o) == nil {
					s.govOps[gid] = append(s.govOps[gid], o)
				}
			}
		}
	}

	// Owner side of the join handshake.
	host.HandleInvites(s.handleInviteRequest)

	// Issuer side of the device-linking handshake.
	host.HandleLink(s.handleLinkRequest)

	// Serve history catch-up requests from reconnecting peers.
	host.HandleSync(s.handleSyncRequest)

	// Serve attachment blobs to peers rendering images we hold.
	host.HandleAttachments(s.handleAttachRequest)

	// Auto-redeem direct-message invitations pushed by a peer.
	host.HandleDMInvites(s.handleDMInvite)

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
		pp := s.presence(p)
		_ = st.RecordContact(pp.PeerID, pp.Fingerprint)
		// When a rendezvous node connects, register our mailbox with it and
		// drain anything deposited while we were offline.
		if s.isMailboxNode(p) {
			go func() {
				s.registerMailbox(p)
				s.drainMailbox(p)
			}()
		}
		// If this peer is an outstanding group-DM invitee we couldn't reach
		// earlier, invite them now that they're online.
		go s.deliverPendingDMInvites(p)
		go func() {
			time.Sleep(1500 * time.Millisecond)
			s.announceProfileAll()
			// Pull any history we missed while apart from this peer; one retry
			// covers a stream that failed while the connection settled.
			if !s.syncFromPeer(p) {
				time.Sleep(10 * time.Second)
				s.syncFromPeer(p)
			}
			// If sync couldn't bridge a gap and this peer can commit, this
			// newly-reachable committer is exactly who can re-add us.
			s.healStrandedGuilds()
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

	// Background recovery: periodically re-attempt re-add for any stranded guild.
	go s.runHealLoop()

	// Resume rich presence if the user had it on.
	if s.RichPresenceEnabled() {
		s.startRichPresence()
	}

	return s, nil
}

// Fingerprint returns this peer's safety-number fingerprint.
func (s *Service) Fingerprint() string { return s.id.Fingerprint() }

// Mnemonic returns this identity's BIP39 recovery phrase (secret material).
func (s *Service) Mnemonic() (string, error) { return s.id.Mnemonic() }

// PeerID returns this peer's libp2p peer ID as a string.
func (s *Service) PeerID() string { return s.host.PeerID().String() }

// PublicKey returns this peer's Ed25519 account public key (its MLS credential).
func (s *Service) PublicKey() []byte { return []byte(s.id.PublicKey()) }

// Peers returns the currently connected peers.
func (s *Service) Peers() []PeerPresence {
	ids := s.host.Peers()
	out := make([]PeerPresence, 0, len(ids))
	for _, p := range ids {
		out = append(out, s.presence(p))
	}
	return out
}

// NetStatus is an aggregate view of connectivity for the UI's connection
// indicator. It's cheap to compute and safe to poll.
type NetStatus struct {
	Peers            int  `json:"peers"`            // total connected peers
	BootstrapReached bool `json:"bootstrapReached"` // at least one rendezvous node connected
	HasBootstrap     bool `json:"hasBootstrap"`     // any rendezvous node is configured
	OutOfSyncGuilds  int  `json:"outOfSyncGuilds"`  // guilds currently stranded (healing)
}

// NetworkStatus reports current connectivity for the UI banner: how many peers
// are connected, whether a rendezvous/relay node is reachable, and how many
// guilds are mid-heal. Mobile surfaces this as a connecting/online/offline pill.
func (s *Service) NetworkStatus() NetStatus {
	ns := NetStatus{
		Peers:        len(s.host.Peers()),
		HasBootstrap: len(s.bootstrap) > 0,
	}
	ns.BootstrapReached = len(s.mailboxNodes()) > 0
	s.mu.RLock()
	for _, g := range s.guilds {
		if s.outOfSync[g.ID] {
			ns.OutOfSyncGuilds++
		}
	}
	s.mu.RUnlock()
	return ns
}

// Nudge forces a fast reconnect + catch-up, called when the OS resumes the app
// (mobile) or the user hits "reconnect". It re-dials bootstrap nodes, re-drains
// the mailbox on any that are up, re-syncs history from every connected peer,
// and retries heal for stranded guilds. Safe to call repeatedly; each step is
// idempotent. Runs in the background so callers don't block on the network.
func (s *Service) Nudge() {
	go func() {
		// Re-dial configured rendezvous nodes we've lost.
		connected := map[peer.ID]bool{}
		for _, p := range s.host.Peers() {
			connected[p] = true
		}
		for _, pi := range s.bootstrap {
			if !connected[pi.ID] {
				ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
				_ = s.host.Connect(ctx, pi)
				cancel()
			}
		}
		// Drain the mailbox on every reachable rendezvous node.
		for _, node := range s.mailboxNodes() {
			s.registerMailbox(node)
			s.drainMailbox(node)
		}
		// Catch up history from every connected peer, then heal stragglers.
		for _, p := range s.host.Peers() {
			s.syncFromPeer(p)
		}
		s.healStrandedGuilds()
	}()
}

// OnPeerConnected registers a presence-up callback.
func (s *Service) OnPeerConnected(fn func(PeerPresence)) {
	s.host.OnPeerConnected(func(p peer.ID) { fn(s.presence(p)) })
}

// OnPeerDisconnected registers a presence-down callback.
func (s *Service) OnPeerDisconnected(fn func(PeerPresence)) {
	s.host.OnPeerDisconnected(func(p peer.ID) { fn(s.presence(p)) })
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
	banner, _ := s.store.GetSetting("banner_image")
	presence, _ := s.store.GetSetting("presence")
	bio, _ := s.store.GetSetting("bio")
	// Rich-presence overlay: while something is playing, it stands in for the
	// manual status; when it clears, the manual status returns.
	s.activityMu.Lock()
	var act *Activity
	if s.activity != "" {
		status = s.activity
		act = s.activityInfo
	}
	s.activityMu.Unlock()
	return Profile{
		Name: s.DisplayName(), Status: status, Emoji: emoji, Color: color, Avatar: avatar,
		Banner: banner, Presence: presence, Bio: bio, MailboxPub: s.mbxPub[:], Activity: act,
	}
}

// SetProfile persists the full self profile and re-announces it to every guild.
func (s *Service) SetProfile(p Profile) error {
	if len(p.Avatar) > maxAvatarBytes {
		return fmt.Errorf("app: avatar image too large (max %d KB)", maxAvatarBytes/1024)
	}
	if p.Avatar != "" && !strings.HasPrefix(p.Avatar, "data:image/") {
		return fmt.Errorf("app: avatar must be an image data URI")
	}
	if len(p.Banner) > maxProfileBannerBytes {
		return fmt.Errorf("app: banner image too large (max %d KB)", maxProfileBannerBytes/1024)
	}
	if p.Banner != "" && !strings.HasPrefix(p.Banner, "data:image/") {
		return fmt.Errorf("app: banner must be an image data URI")
	}
	if len(p.Bio) > maxBioBytes {
		p.Bio = p.Bio[:maxBioBytes]
	}
	for k, v := range map[string]string{
		"display_name": strings.TrimSpace(p.Name),
		"status_text":  strings.TrimSpace(p.Status),
		"avatar_emoji": strings.TrimSpace(p.Emoji),
		"accent_color": strings.TrimSpace(p.Color),
		"avatar_image": p.Avatar,
		"banner_image": p.Banner,
		"presence":     strings.TrimSpace(p.Presence),
		"bio":          strings.TrimSpace(p.Bio),
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

// NickOf returns the per-guild nickname for a member, or "" if none is set.
func (s *Service) NickOf(guildID, fingerprint string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if g := s.nicks[guildID]; g != nil {
		return g[fingerprint]
	}
	return ""
}

// rememberNick records (or clears, on empty nick) a per-guild nickname in the
// in-memory cache and persists it. Caller holds no lock.
func (s *Service) rememberNick(guildID, fingerprint, nick string) {
	s.mu.Lock()
	if nick == "" {
		if g := s.nicks[guildID]; g != nil {
			delete(g, fingerprint)
		}
	} else {
		if s.nicks[guildID] == nil {
			s.nicks[guildID] = map[string]string{}
		}
		s.nicks[guildID][fingerprint] = nick
	}
	s.mu.Unlock()
	_ = s.store.SaveNickname(guildID, fingerprint, nick)
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
	if len(p.Banner) > maxProfileBannerBytes || (p.Banner != "" && !strings.HasPrefix(p.Banner, "data:image/")) {
		p.Banner = "" // reject oversized or non-image banners from peers
	}
	if a := p.Activity; a != nil {
		// Peers only get to broadcast plausible activity: web art URLs (no
		// file:///javascript: junk that a client might render), bounded sizes.
		if len(a.Title) > maxActivityBytes || len(a.Artist) > maxActivityBytes {
			p.Activity = nil
		} else if a.ArtURL != "" && (len(a.ArtURL) > maxArtURLBytes ||
			!(strings.HasPrefix(a.ArtURL, "https://") || strings.HasPrefix(a.ArtURL, "http://"))) {
			a.ArtURL = ""
		}
	}
	// Don't let a partial update wipe fields we already learned. Peers relay each
	// other's profiles over the sync roster, and a peer that only knows someone
	// as "unknown" (empty name) would otherwise blank a good name — which the UI
	// then shows as a fingerprint stub, causing the name to flicker. An empty
	// name is never an intentional clear (users always have a display name), so
	// keep the previous one; likewise keep a known mailbox key.
	s.mu.Lock()
	prev, known := s.profiles[fingerprint]
	if known {
		if p.Name == "" && prev.Name != "" {
			p.Name = prev.Name
		}
		if len(p.MailboxPub) == 0 && len(prev.MailboxPub) > 0 {
			p.MailboxPub = prev.MailboxPub
		}
	}
	s.profiles[fingerprint] = p
	s.mu.Unlock()
	if known && profilesEqual(prev, p) {
		return false // nothing new; skip the store write and UI refresh
	}
	_ = s.store.SaveProfile(store.ProfileRow{
		Fingerprint: fingerprint,
		Name:        p.Name, Status: p.Status, Emoji: p.Emoji, Color: p.Color, Avatar: p.Avatar,
		Banner: p.Banner, Presence: p.Presence, Bio: p.Bio, MailboxPub: p.MailboxPub,
	})
	s.emitGuildUpdate()
	return !known
}

func profilesEqual(a, b Profile) bool {
	return a.Name == b.Name && a.Status == b.Status && a.Emoji == b.Emoji &&
		a.Color == b.Color && a.Avatar == b.Avatar && a.Banner == b.Banner &&
		a.Presence == b.Presence && a.Bio == b.Bio && bytes.Equal(a.MailboxPub, b.MailboxPub) &&
		activityEqual(a.Activity, b.Activity) && activityPosEqual(a.Activity, b.Activity)
}

// activityPosEqual detects re-announced position snapshots (seeks) so they
// reach the UI even when the track itself didn't change.
func activityPosEqual(a, b *Activity) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.PositionMs == b.PositionMs && a.AtMs == b.AtMs
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
	// Newly stranded: try to auto-recover immediately (re-add from an online
	// authorized committer). The heal loop retries if none is reachable yet.
	if stranded && !was {
		go s.healOutOfSync(guildID)
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

// ImportVerifiedFingerprints seeds verifications carried over from another
// device (device linking). Unlike VerifyFingerprint it doesn't require the
// peers to have been sighted on this device yet. Best-effort per entry.
func (s *Service) ImportVerifiedFingerprints(fprs []string) {
	for _, f := range fprs {
		_ = s.store.ImportVerifiedFingerprint(f)
	}
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

// accountKeyOf returns the account public key that a member credential
// authenticates under — the single identity that ownership, bans, roles, and
// message attribution are all keyed on, regardless of which device produced the
// leaf. A legacy bare 32-byte credential IS the account key (single-device, and
// every pre-multi-device client), so it passes straight through — this keeps the
// whole authorization model unchanged for existing guilds. A device-cert
// credential (multi-device) carries its accountPub and is only honored when the
// account's signature over the device key verifies; an unverified/garbage cert
// returns the raw bytes so a forged cert can't silently masquerade as some other
// account (it just fails to match any real member).
func accountKeyOf(cred []byte) []byte {
	if cert, ok := identity.ParseDeviceCert(cred); ok {
		if cert.Verify() {
			return cert.AccountPub
		}
		return cred // invalid cert: don't let it resolve to a claimed account
	}
	return cred
}

// accountFingerprintOf is the safety-number fingerprint of the account behind a
// credential — use this instead of FingerprintOf(cred) everywhere a credential
// is turned into a member identity, so device leaves map to their account.
func accountFingerprintOf(cred []byte) string {
	return identity.FingerprintOf(accountKeyOf(cred))
}

// AccountKeyOf / AccountFingerprintOf expose credential normalization to the
// bridge, so a member view collapses all of an account's device leaves onto the
// single account identity (one row, the account's name, not a per-device cert).
func (s *Service) AccountKeyOf(cred []byte) []byte         { return accountKeyOf(cred) }
func (s *Service) AccountFingerprintOf(cred []byte) string { return accountFingerprintOf(cred) }

// presenceFor derives the UI view for a peer from the key embedded in its
// PeerID. This is correct for a legacy account-key PeerID (PeerID == account);
// a linked device's PeerID is its device key, which this can't map to an
// account — use Service.presence for that.
func presenceFor(p peer.ID) PeerPresence {
	pp := PeerPresence{PeerID: p.String()}
	if pub, err := p.ExtractPublicKey(); err == nil {
		if raw, err := pub.Raw(); err == nil {
			pp.Fingerprint = identity.FingerprintOf(raw)
		}
	}
	return pp
}

// learnDeviceCert records the device→account mapping from a credential, so a
// later presence() lookup for that device's PeerID resolves to the account.
// A no-op for a legacy bare credential (PeerID already == account).
func (s *Service) learnDeviceCert(cred []byte) {
	cert, ok := identity.ParseDeviceCert(cred)
	if !ok || !cert.Verify() {
		return
	}
	key := hex.EncodeToString(cert.DevicePub)
	fpr := identity.FingerprintOf(cert.AccountPub)
	s.deviceMu.Lock()
	if s.deviceAccounts == nil {
		s.deviceAccounts = map[string]string{}
	}
	s.deviceAccounts[key] = fpr
	s.deviceMu.Unlock()
}

// presence is the account-aware presenceFor: it resolves a linked device's
// PeerID (its device key) to the account fingerprint via the learned map,
// falling back to the raw key's fingerprint for a legacy account-key PeerID.
func (s *Service) presence(p peer.ID) PeerPresence {
	pp := PeerPresence{PeerID: p.String()}
	pub, err := p.ExtractPublicKey()
	if err != nil {
		return pp
	}
	raw, err := pub.Raw()
	if err != nil {
		return pp
	}
	s.deviceMu.RLock()
	fpr, isDevice := s.deviceAccounts[hex.EncodeToString(raw)]
	s.deviceMu.RUnlock()
	if isDevice {
		pp.Fingerprint = fpr
	} else {
		pp.Fingerprint = identity.FingerprintOf(raw)
	}
	return pp
}
