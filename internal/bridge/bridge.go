package bridge

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	appsvc "github.com/ZahakJ/concord/internal/app"
	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/identity"
	"github.com/ZahakJ/concord/internal/version"
)

// bridge is the transport-agnostic API surface the UI drives. It wraps Concord's
// Service (internal/app) and translates domain types into JSON-friendly view
// structs. Two front ends bind to it: the Wails desktop app (main_wails.go,
// which turns OnMessage/OnPresence into runtime events and binds bridge's
// exported methods to JavaScript) and the browser-served web app (main_web.go,
// which exposes the same methods over HTTP and streams events via SSE).
//
// Keeping this layer free of any Wails or HTTP dependency is what lets the
// identical UI run either as a native window or in a browser.
type Bridge struct {
	ctx context.Context

	mu  sync.Mutex
	svc *appsvc.Service
	// The visibility vote. See visibility.go: shellBackground is the native
	// mobile lifecycle's answer, clientVisible is what each attached UI says
	// about itself, and the two are ANDed. All three are remembered across a
	// locked service so an identity unlocked while nobody is looking begins
	// life already throttled.
	shellBackground bool
	heardClient     bool
	clientVisible   map[string]bool
	// wantMetered remembers the shell's last SetMetered call for the same
	// reason: the network callback fires as soon as the process starts, long
	// before anybody types a passphrase, and a service born after that must not
	// spend its first minutes walking the DHT on a data plan.
	wantMetered bool

	// Event sinks, set by whichever transport owns the bridge.
	OnMessage       func(MessageView)
	OnPresence      func()
	OnVoicePresence func(VoicePresence)
	OnVoiceSignal   func(VoiceSignal)
	OnTyping        func(TypingInfo)
	OnGuildUpdate   func()
	OnGuildInvite   func(appsvc.GuildInvite)
	OnReadState     func(ReadStateView)
	// OnStory fires as the "story" event when a guild's stories change
	// (GuildID "" = the expiry sweep; recheck every guild).
	OnStory func(StoryUpdate)
}

// StoryUpdate names the guild whose stories changed ("" = several may have).
type StoryUpdate struct {
	GuildID string `json:"guildId"`
}

// ReadStateView reports a channel's read cursor advancing (locally, in another
// session of this backend, or on another linked device). At is UnixMilli.
type ReadStateView struct {
	ChannelID string `json:"channelId"`
	At        int64  `json:"at"`
}

// TypingInfo reports that a peer is typing in a channel.
type TypingInfo struct {
	From      string `json:"from"` // fingerprint
	Name      string `json:"name"` // display name if known
	ChannelID string `json:"channelId"`
}

// VoicePresence reports a peer joining or leaving a channel's voice room.
type VoicePresence struct {
	From        string `json:"from"` // peer ID (for signaling)
	Fingerprint string `json:"fingerprint"`
	ChannelID   string `json:"channelId"`
	Action      string `json:"action"`           // join|leave|lock|unlock|knock|admit|move|disconnect
	Target      string `json:"target,omitempty"` // the fingerprint being admitted/moved/disconnected
	Dest        string `json:"dest,omitempty"`   // move: the voice channel to send them to
}

// VoiceSignal carries an opaque WebRTC signaling blob from a peer.
type VoiceSignal struct {
	From string `json:"from"`
	Data string `json:"data"`
}

func New(ctx context.Context) *Bridge { return &Bridge{ctx: ctx} }

// setContext lets the Wails OnStartup hook supply the runtime context.
func (b *Bridge) SetContext(ctx context.Context) { b.ctx = ctx }

func (b *Bridge) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.svc != nil {
		_ = b.svc.Close()
		b.svc = nil
	}
}

var errLocked = errors.New("identity is locked; log in first")

func (b *Bridge) service() (*appsvc.Service, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.svc == nil {
		return nil, errLocked
	}
	return b.svc, nil
}

// ---- View types (JSON shapes the frontend consumes) ----

type IdentityInfo struct {
	PeerID      string           `json:"peerId"`
	Fingerprint string           `json:"fingerprint"`
	DisplayName string           `json:"displayName"`
	Status      string           `json:"status"`
	Emoji       string           `json:"emoji"`
	Color       string           `json:"color"`
	Avatar      string           `json:"avatar"`
	Banner      string           `json:"banner"`
	Presence    string           `json:"presence"`
	Bio         string           `json:"bio"`
	Birthday    string           `json:"birthday,omitempty"` // "MM-DD" only — never a year
	Activity    *appsvc.Activity `json:"activity,omitempty"` // structured now-playing
	Games       []appsvc.Game    `json:"games,omitempty"`    // curated game collection
	Color2      string           `json:"color2,omitempty"`   // gradient partner color
	Frame       string           `json:"frame,omitempty"`    // avatar frame enum id
	Effect      string           `json:"effect,omitempty"`   // card effect enum id
	Style       *appsvc.Style    `json:"style,omitempty"`    // fine-grained style dials
}

type ChannelView struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Type     string   `json:"type"`     // "text" | "voice" | "announcement" | "forum" | "thread"
	Category string   `json:"category"` // category ID or ""
	Position int      `json:"position"`
	Topic    string   `json:"topic"`            // channel topic/description
	Parent   string   `json:"parent,omitempty"` // forum this thread (post) lives under
	Links    []string `json:"links,omitempty"`  // announcement: consumer channel IDs
	// LastActivity is the newest message time (UnixNano) — forum post ordering.
	LastActivity int64 `json:"lastActivity,omitempty"`
	// SlowMode is the governed posting interval in seconds (0 = off); the
	// composer paces itself with it and managers see it in channel settings.
	SlowMode int64 `json:"slowMode,omitempty"`
	// Retention is how long messages are kept in this channel, in seconds
	// (0 = forever) — the channel's own override if set, otherwise the guild's
	// policy. Enforced locally by each client; see Service.SetRetention.
	Retention int64 `json:"retention,omitempty"`
	// Forum metadata, carried here because it IS channel state — the sidebar and
	// the forum settings modal read it from the guild snapshot they already have.
	// A forum board should read ForumBoard instead: it adds the derived author,
	// reply count and excerpt, and both are built from this same in-memory
	// channel, so the two can never disagree.
	ForumTags []domain.ForumTag `json:"forumTags,omitempty"` // forum: the tag palette
	Tags      []string          `json:"tags,omitempty"`      // post: tag IDs into that palette
	Pinned    bool              `json:"pinned,omitempty"`    // post: floated to the top of its board
	Solved    bool              `json:"solved,omitempty"`    // post: marked answered
	Locked    bool              `json:"locked,omitempty"`    // post: closed to new messages
	Banner    string            `json:"banner,omitempty"`    // forum: its own artwork
}

type CategoryView struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Position int    `json:"position"`
}

type EmojiView struct {
	Name  string `json:"name"`
	Image string `json:"image"`
}

type GuildView struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind,omitempty"`   // "" guild, "dm" direct message
	DMPeer string `json:"dmPeer,omitempty"` // for a peer DM: the other member's fingerprint
	// For a 2-person peer DM, the other member's availability so the rail can
	// show a status dot on the DM bubble (empty for group DMs / non-DMs).
	DMPeerPresence string `json:"dmPeerPresence,omitempty"` // "" | online | idle | dnd | invisible
	DMPeerOnline   bool   `json:"dmPeerOnline,omitempty"`
	DMPeerAvatar   string `json:"dmPeerAvatar,omitempty"` // the other member's profile picture (data URI)
	DMMembers      int    `json:"dmMembers,omitempty"`    // total members in a DM (incl. self); lets the UI hide empty pending DMs
	// DMFaces is the other members (excluding self) of a DM, for the bubble: a
	// single face for a peer DM, several for a group DM (rendered as a collage).
	DMFaces []DMFace `json:"dmFaces,omitempty"`
	DMNamed bool     `json:"dmNamed,omitempty"` // group DM has a user-set custom name
	DMNotes bool     `json:"dmNotes,omitempty"` // the self-notes DM (stored name, immune to a peer named "Notes")
	IsOwner bool     `json:"isOwner"`
	// OwnerFingerprint authenticates relayed guest messages: kind:"guest" is only
	// honoured in a meeting when the owner (the host) signed it. Without it a
	// member could forge an unaccountable "guest" author.
	OwnerFingerprint string `json:"ownerFingerprint,omitempty"`
	// Heir is the member the current owner pre-authorized to claim ownership
	// ("" = none). Drives the heir badge, the owner's revoke affordance, and
	// the heir's own "take ownership" affordance.
	Heir        string         `json:"heir,omitempty"`
	CanManage   bool           `json:"canManage"`   // viewer may invite/kick/ban here
	MyPerms     uint32         `json:"myPerms"`     // viewer's effective permission bitmask
	Icon        string         `json:"icon"`        // guild logo (data URI)
	Banner      string         `json:"banner"`      // guild banner image (data URI)
	Description string         `json:"description"` // guild blurb
	Channels    []ChannelView  `json:"channels"`
	Categories  []CategoryView `json:"categories"`
	Emoji       []EmojiView    `json:"emoji"`
	// OutOfSync: this member is stranded at an old MLS epoch that no reachable
	// peer could bridge; new messages can't be decrypted until re-invited.
	OutOfSync bool `json:"outOfSync,omitempty"`
	// LastActivity is the newest message time (UnixNano) across the guild's
	// channels — the UI sorts DM conversations by it, most recent first.
	LastActivity int64 `json:"lastActivity,omitempty"`
}

// DMFace is one member's avatar data for a DM bubble (used to build the group
// DM collage). Mirrors the fields Avatar needs to render an image or initials.
type DMFace struct {
	Name    string `json:"name"`
	Avatar  string `json:"avatar,omitempty"`
	Color   string `json:"color,omitempty"`
	Emoji   string `json:"emoji,omitempty"`
	Pending bool   `json:"pending,omitempty"` // invited but hasn't joined yet
}

type MessageView struct {
	ID         string `json:"id"`
	ChannelID  string `json:"channelId"`
	Sender     string `json:"sender"`     // authenticated fingerprint
	SenderName string `json:"senderName"` // self-asserted display name
	Kind       string `json:"kind"`       // "" normal chat, "system" join/create notice, "app" machine payload (never rendered as chat)
	ReplyTo    string `json:"replyTo"`    // ID of the replied-to message, or ""
	Content    string `json:"content"`
	// Dir is the base direction the AUTHOR laid the message out in: "rtl",
	// "ltr", or "" for the per-line heuristic. It has to reach the view layer
	// because the reader cannot derive it — the heuristic is precisely what
	// the author overrode. Omitted when empty so the overwhelming majority of
	// messages, which never set one, cost nothing on this wire either.
	Dir       string              `json:"dir,omitempty"`
	Deleted   bool                `json:"deleted"`
	Expired   bool                `json:"expired"` // disappeared via a timer (not a manual delete)
	Edited    bool                `json:"edited"`
	Pinned    bool                `json:"pinned"`
	Reactions map[string][]string `json:"reactions"` // emoji -> fingerprints
	Sent      string              `json:"sent"`
}

type MemberView struct {
	Fingerprint string           `json:"fingerprint"`
	Name        string           `json:"name"`     // effective display name (nickname if set, else profile name)
	Username    string           `json:"username"` // underlying profile name, surfaced when a nickname shadows it
	Status      string           `json:"status"`
	Emoji       string           `json:"emoji"`
	Color       string           `json:"color"`
	Avatar      string           `json:"avatar"`
	Banner      string           `json:"banner"`
	Presence    string           `json:"presence"` // "" | online | idle | dnd | invisible
	Bio         string           `json:"bio"`
	Birthday    string           `json:"birthday,omitempty"` // "MM-DD" only — never a year; 🎂 is a per-VIEWER render off their local clock
	Activity    *appsvc.Activity `json:"activity,omitempty"` // structured now-playing
	Games       []appsvc.Game    `json:"games,omitempty"`    // curated game collection
	Color2      string           `json:"color2,omitempty"`   // gradient partner color
	Frame       string           `json:"frame,omitempty"`    // avatar frame enum id
	Effect      string           `json:"effect,omitempty"`   // card effect enum id
	Style       *appsvc.Style    `json:"style,omitempty"`    // fine-grained style dials
	IsSelf      bool             `json:"isSelf"`
	Online      bool             `json:"online"`
	Verified    bool             `json:"verified"`
	IsOwner     bool             `json:"isOwner"`    // guild owner (implicit full authority)
	IsHeir      bool             `json:"isHeir"`     // named heir (may claim ownership at any time)
	Perms       uint32           `json:"perms"`      // effective permission bitmask
	CanManage   bool             `json:"canManage"`  // owner or manage-members holder
	RoleIDs     []string         `json:"roleIds"`    // assigned role IDs (highest-first from Roles())
	MutedUntil  int64            `json:"mutedUntil"` // unix seconds muted-until (0 = not muted)
	Pending     bool             `json:"pending"`    // added but not yet joined (shown greyed)
}

type ContactView struct {
	PeerID      string `json:"peerId"`
	Fingerprint string `json:"fingerprint"`
	Name        string `json:"name"` // profile display name (may be "" if unknown)
	Verified    bool   `json:"verified"`
	// The learned profile's face, so people pickers (New message, Add members,
	// Invite) show the same avatar as every other surface instead of an
	// initials disc. Empty when we've never learned a profile for them.
	Avatar string `json:"avatar,omitempty"`
	Emoji  string `json:"emoji,omitempty"`
	Color  string `json:"color,omitempty"`
}

// ---- Connection settings (usable before unlock) ----

// Logout closes the running Service and locks the app again (back to the login
// screen) without deleting anything.
func (b *Bridge) Logout() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.svc != nil {
		_ = b.svc.Close()
		b.svc = nil
	}
	return nil
}

// HasIdentity reports whether an identity keystore already exists, so the UI
// can show "Unlock" vs "Create a passphrase".
func (b *Bridge) HasIdentity() (bool, error) {
	dir, err := appsvc.DataDir()
	if err != nil {
		return false, err
	}
	return appsvc.HasIdentity(dir), nil
}

// RevealMnemonic returns the unlocked identity's recovery phrase, for the user
// to write down. Requires an unlocked session.
func (b *Bridge) RevealMnemonic() (string, error) {
	svc, err := b.service()
	if err != nil {
		return "", err
	}
	return svc.Mnemonic()
}

// RestoreFromMnemonic reconstructs the identity from a recovery phrase, sealing
// it under a new passphrase. Refused if an identity already exists here (the
// user must "start over" first) or while unlocked.
func (b *Bridge) RestoreFromMnemonic(phrase, passphrase string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.svc != nil {
		return errors.New("already unlocked; restart the app to restore")
	}
	dir, err := appsvc.DataDir()
	if err != nil {
		return err
	}
	if appsvc.HasIdentity(dir) {
		return errors.New("an identity already exists on this device — choose \"start over\" first")
	}
	if strings.TrimSpace(passphrase) == "" {
		return errors.New("choose a passphrase to protect the restored identity")
	}
	return appsvc.RestoreIdentity(dir, phrase, passphrase)
}

// RestoreOverExisting is the forgot-passphrase recovery path: a (locked,
// inaccessible) identity already exists on this device, and the user is
// recovering the SAME account from its recovery phrase under a new passphrase.
// The phrase is validated FIRST — a typo can never destroy data — and only then
// is the old local keystore/data replaced. Account, guilds, and history re-sync
// from peers on the next login.
func (b *Bridge) RestoreOverExisting(phrase, passphrase string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.svc != nil {
		return errors.New("already unlocked; restart the app to restore")
	}
	if strings.TrimSpace(passphrase) == "" {
		return errors.New("choose a new passphrase to protect the restored identity")
	}
	// Validate the recovery phrase before touching anything on disk.
	if _, err := identity.SeedFromMnemonic(phrase); err != nil {
		return errors.New("that doesn't look like a valid 24-word recovery phrase — check the words and their order")
	}
	dir, err := appsvc.DataDir()
	if err != nil {
		return err
	}
	if appsvc.HasIdentity(dir) {
		if err := appsvc.ResetIdentity(dir); err != nil {
			return err
		}
	}
	return appsvc.RestoreIdentity(dir, phrase, passphrase)
}

// ResetIdentity deletes the identity and all data tied to it so a new identity
// can be created (forgotten passphrase / corrupted keystore). Only allowed
// while locked.
func (b *Bridge) ResetIdentity() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.svc != nil {
		return errors.New("already unlocked; restart the app to reset")
	}
	dir, err := appsvc.DataDir()
	if err != nil {
		return err
	}
	return appsvc.ResetIdentity(dir)
}

// AppVersion returns the release tag stamped into this binary ("dev" for
// unstamped local builds). Needs no session — the login screen can show it too.
func (b *Bridge) AppVersion() string { return version.Version }

// Session reports whether the identity is already unlocked (a Service is
// running) — lets the UI skip the login screen after a page refresh.
func (b *Bridge) Session() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.svc != nil
}

// SetBootstrapLive saves rendezvous addresses and dials them now (post-login).
func (b *Bridge) SetBootstrapLive(addrs string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	var list []string
	for _, a := range strings.FieldsFunc(addrs, func(r rune) bool { return r == '\n' || r == ',' }) {
		if a = strings.TrimSpace(a); a != "" {
			list = append(list, a)
		}
	}
	return svc.SetBootstrapLive(list)
}

// GetBootstrap returns the saved rendezvous/relay addresses.
func (b *Bridge) GetBootstrap() ([]string, error) {
	dir, err := appsvc.DataDir()
	if err != nil {
		return nil, err
	}
	return appsvc.LoadNetConfig(dir).Bootstrap, nil
}

// SetBootstrap saves rendezvous/relay addresses (newline- or comma-separated).
// Takes effect on the next unlock.
func (b *Bridge) SetBootstrap(addrs string) error {
	dir, err := appsvc.DataDir()
	if err != nil {
		return err
	}
	var list []string
	for _, a := range strings.FieldsFunc(addrs, func(r rune) bool { return r == '\n' || r == ',' }) {
		if a = strings.TrimSpace(a); a != "" {
			list = append(list, a)
		}
	}
	return appsvc.SaveBootstrap(dir, list)
}

// GetPublicDHT reports whether the public-IPFS-DHT fallback is enabled.
func (b *Bridge) GetPublicDHT() (bool, error) {
	dir, err := appsvc.DataDir()
	if err != nil {
		return false, err
	}
	return appsvc.LoadNetConfig(dir).PublicDHT, nil
}

// SetPublicDHT turns the public-DHT fallback on or off. Takes effect on the
// next launch, since the DHT's bootstrap set is fixed when the host starts.
func (b *Bridge) SetPublicDHT(on bool) error {
	dir, err := appsvc.DataDir()
	if err != nil {
		return err
	}
	return appsvc.SavePublicDHT(dir, on)
}

// GetListenPort returns the pinned listen port, 0 when the node takes an
// ephemeral one.
func (b *Bridge) GetListenPort() (int, error) {
	dir, err := appsvc.DataDir()
	if err != nil {
		return 0, err
	}
	return appsvc.LoadNetConfig(dir).ListenPort, nil
}

// SetListenPort pins the listen port so it can be forwarded on the router (0 =
// automatic). Takes effect on the next launch, since the sockets are bound when
// the host starts.
func (b *Bridge) SetListenPort(port int) error {
	dir, err := appsvc.DataDir()
	if err != nil {
		return err
	}
	return appsvc.SaveListenPort(dir, port)
}

// ---- API methods ----

// Login unlocks (or creates) the identity and starts the Service, wiring live
// events into the configured sinks. A second successful call is a no-op.
func (b *Bridge) Login(passphrase string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	dataDir, err := appsvc.DataDir()
	if err != nil {
		return err
	}
	// Already unlocked (e.g. a second tab): still require the correct passphrase
	// rather than silently succeeding.
	if b.svc != nil {
		if !appsvc.VerifyPassphrase(dataDir, passphrase) {
			return errors.New("wrong passphrase")
		}
		return nil
	}
	cfg := appsvc.Config{DataDir: dataDir, Passphrase: passphrase}
	if bs := os.Getenv("CONCORD_BOOTSTRAP"); bs != "" {
		cfg.BootstrapPeers = strings.Split(bs, ",")
	}
	if os.Getenv("CONCORD_DISABLE_MDNS") == "1" {
		cfg.DisableMDNS = true
	}
	svc, err := appsvc.Start(b.ctx, cfg)
	if err != nil {
		return err
	}
	svc.OnMessage(func(m domain.Message) {
		// A blocked sender's message is stored and synced like any other, but
		// it never reaches the UI — so it raises no chime, no unread badge and
		// no OS notification. Silence is most of what blocking is for; letting
		// the message through and hiding it later would still buzz the phone.
		if svc.SenderBlocked(m.Sender) {
			return
		}
		if b.OnMessage != nil {
			b.OnMessage(messageView(m))
		}
	})
	presence := func(appsvc.PeerPresence) {
		if b.OnPresence != nil {
			b.OnPresence()
		}
	}
	svc.OnPeerConnected(presence)
	svc.OnPeerDisconnected(presence)
	// This device was unlinked from its account and has just erased itself. Drop
	// the session rather than leaving the UI talking to a closed store, and poke
	// the presence feed so the shell notices it needs to go back to the lock
	// screen — where HasIdentity is now false, which is the truth.
	svc.OnUnlinked(func() {
		b.mu.Lock()
		if b.svc == svc {
			b.svc = nil
		}
		b.mu.Unlock()
		if b.OnPresence != nil {
			b.OnPresence()
		}
	})
	svc.OnVoicePresence(func(from, fingerprint, channelID, action, target, dest string) {
		if b.OnVoicePresence != nil {
			b.OnVoicePresence(VoicePresence{From: from, Fingerprint: fingerprint, ChannelID: channelID, Action: action, Target: target, Dest: dest})
		}
	})
	svc.OnVoiceSignal(func(from string, data []byte) {
		if b.OnVoiceSignal != nil {
			b.OnVoiceSignal(VoiceSignal{From: from, Data: string(data)})
		}
	})
	svc.OnTyping(func(from, channelID string) {
		if b.OnTyping != nil {
			name := svc.ProfileName(from)
			// Your own account: typing relayed from your other device. The
			// profile cache deliberately holds no self row (a peer echoing a
			// stale profile must not rename you), so resolve your own name
			// here or the strip would fall back to a truncated fingerprint.
			if name == "" && from == svc.Fingerprint() {
				name = svc.DisplayName()
			}
			b.OnTyping(TypingInfo{From: from, Name: name, ChannelID: channelID})
		}
	})
	svc.OnGuildUpdate(func() {
		if b.OnGuildUpdate != nil {
			b.OnGuildUpdate()
		}
	})
	svc.OnGuildInvite(func(inv appsvc.GuildInvite) {
		if b.OnGuildInvite != nil {
			b.OnGuildInvite(inv)
		}
	})
	svc.OnReadState(func(channelID string, at int64) {
		if b.OnReadState != nil {
			b.OnReadState(ReadStateView{ChannelID: channelID, At: at})
		}
	})
	svc.OnStory(func(guildID string) {
		if b.OnStory != nil {
			b.OnStory(StoryUpdate{GuildID: guildID})
		}
	})
	b.svc = svc
	// The shell may have declared the app backgrounded before the unlock
	// happened; a service born after that call must not start on the eager
	// foreground cadence.
	if backgroundNow(b.shellBackground, b.heardClient, b.clientVisible) {
		svc.SetBackground(true)
	}
	// Likewise the connection the shell told us about before the unlock.
	if b.wantMetered {
		svc.SetMetered(true)
	}
	// Learn (once per version) that our own binary is a published release, so
	// peers running an older one can pull it from us. Off the critical path:
	// it hashes the executable and may touch the network.
	go adoptOwnRelease()
	return nil
}

// MarkRead records the user read a channel through at (UnixMilli) and fans the
// new cursor out to every session/device.
func (b *Bridge) MarkRead(channelID string, at int64) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.MarkRead(channelID, at)
}

// ReadState returns every channel's read-through time (UnixMilli).
func (b *Bridge) ReadState() (map[string]int64, error) {
	svc, err := b.service()
	if err != nil {
		return nil, err
	}
	return svc.ReadState()
}

// ToggleReaction adds/removes an emoji reaction on a message.
func (b *Bridge) ToggleReaction(channelID, messageID, emoji string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.ToggleReaction(channelID, messageID, emoji)
}

// EditMessage edits one of this peer's own messages.
func (b *Bridge) EditMessage(channelID, messageID, newContent string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.EditMessage(channelID, messageID, newContent)
}

// DeleteMessage deletes one of this peer's own messages.
func (b *Bridge) DeleteMessage(channelID, messageID string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.DeleteMessage(channelID, messageID)
}

// ExpireMessage erases this device's copy of an expired disappearing message.
func (b *Bridge) ExpireMessage(channelID, messageID string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.ExpireMessage(channelID, messageID)
}

// EmptyTrash permanently erases retained bodies of deleted messages (guildID
// scopes to that guild; "" is the whole device). Returns rows scrubbed.
func (b *Bridge) EmptyTrash(guildID string) (int, error) {
	svc, err := b.service()
	if err != nil {
		return 0, err
	}
	return svc.EmptyTrash(guildID)
}

// CancelPendingMember cancels a not-yet-joined member you added to a guild.
func (b *Bridge) CancelPendingMember(guildID, fingerprint string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.CancelPendingMember(guildID, fingerprint)
}

// BlockUser blocks an account fingerprint (drops its DM/guild invites).
func (b *Bridge) BlockUser(fingerprint string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.BlockUser(fingerprint)
}

// UnblockUser removes an account fingerprint from the block list.
func (b *Bridge) UnblockUser(fingerprint string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.UnblockUser(fingerprint)
}

// BlockedUsers lists blocked account fingerprints.
func (b *Bridge) BlockedUsers() ([]string, error) {
	svc, err := b.service()
	if err != nil {
		return nil, err
	}
	return svc.BlockedUsers(), nil
}

// GuildStats returns storage + membership + sync stats for one guild/DM.
func (b *Bridge) GuildStats(guildID string) (appsvc.GuildStatsView, error) {
	svc, err := b.service()
	if err != nil {
		return appsvc.GuildStatsView{}, err
	}
	return svc.GuildStats(guildID)
}

// PropsTally returns a guild's received-props counts keyed by member
// fingerprint — computed from the local reaction history, so it costs no
// network and answers instantly.
func (b *Bridge) PropsTally(guildID string) (map[string]int, error) {
	svc, err := b.service()
	if err != nil {
		return nil, err
	}
	return svc.PropsTally(guildID)
}

// NetworkStats returns a whole-device network + storage snapshot.
func (b *Bridge) NetworkStats() (appsvc.NetworkStatsView, error) {
	svc, err := b.service()
	if err != nil {
		return appsvc.NetworkStatsView{}, err
	}
	return svc.NetworkStats(), nil
}

// LinkedDevices lists the devices signed in to THIS account (see
// app.LinkedDeviceView). Separate from NetworkStats' peer list on purpose: your
// own phone is not one of the strangers your rendezvous introduced you to.
func (b *Bridge) LinkedDevices() ([]appsvc.LinkedDeviceView, error) {
	svc, err := b.service()
	if err != nil {
		return nil, err
	}
	return svc.LinkedDevices(), nil
}

// UnlinkDevice revokes one of this account's devices. Read the threat model in
// app/unlink.go before describing this to anyone: the leaf removal is real, the
// self-erase is advisory, and a device in hostile hands still holds the account
// seed.
func (b *Bridge) UnlinkDevice(deviceKey string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.UnlinkDevice(deviceKey)
}

// LeaveGuild removes a guild from this peer (local delete).
func (b *Bridge) LeaveGuild(guildID string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.LeaveGuild(guildID)
}

// RenameGuild renames a guild (owner only).
func (b *Bridge) RenameGuild(guildID, name string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.RenameGuild(guildID, name)
}

// CreateChannel adds a channel to a guild. ctype is ""/"text"/"voice"/
// "announcement"; category is a category ID or "".
func (b *Bridge) CreateChannel(guildID, name, ctype, category string) (ChannelView, error) {
	svc, err := b.service()
	if err != nil {
		return ChannelView{}, err
	}
	ch, err := svc.CreateChannel(guildID, name, ctype, category)
	if err != nil {
		return ChannelView{}, err
	}
	return channelView(ch), nil
}

// SetChannelLinks records an announcement channel's consumer channels.
func (b *Bridge) SetChannelLinks(guildID, channelID string, links []string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.SetChannelLinks(guildID, channelID, links)
}

// CreateThread opens a forum post (thread) — any member may. tagIDs are entries
// of the forum's own palette (at most maxPostTags); an unknown id is an error,
// not a silent drop.
func (b *Bridge) CreateThread(guildID, forumID, title, firstMessage string, tagIDs []string) (ChannelView, error) {
	svc, err := b.service()
	if err != nil {
		return ChannelView{}, err
	}
	ch, err := svc.CreateThread(guildID, forumID, title, firstMessage, tagIDs)
	if err != nil {
		return ChannelView{}, err
	}
	return channelView(ch), nil
}

// ForumBoard returns a forum's tag palette plus its posts, each with the
// metadata a card needs (author, reply count, excerpt) derived from the post's
// own messages. One call: a board that fetched the palette separately could
// render a chip whose tag it cannot name yet.
func (b *Bridge) ForumBoard(guildID, forumID string) (appsvc.ForumBoard, error) {
	svc, err := b.service()
	if err != nil {
		return appsvc.ForumBoard{}, err
	}
	return svc.ForumBoard(guildID, forumID)
}

// SetForumTags replaces a forum's tag palette (ManageChannels). A tag with no id
// gets one minted; the returned palette carries the ids to send back on a post.
func (b *Bridge) SetForumTags(guildID, forumID string, tags []domain.ForumTag) ([]domain.ForumTag, error) {
	svc, err := b.service()
	if err != nil {
		return nil, err
	}
	return svc.SetForumTags(guildID, forumID, tags)
}

// SetPostTags sets which of its forum's tags a post carries (author or
// ManageMessages).
func (b *Bridge) SetPostTags(guildID, postID string, tagIDs []string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.SetPostTags(guildID, postID, tagIDs)
}

// SetPostPinned floats a post to the top of its board (ManageMessages).
func (b *Bridge) SetPostPinned(guildID, postID string, pinned bool) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.SetPostPinned(guildID, postID, pinned)
}

// SetPostSolved marks a post answered or reopens it (author or ManageMessages).
func (b *Bridge) SetPostSolved(guildID, postID string, solved bool) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.SetPostSolved(guildID, postID, solved)
}

// SetPostLocked closes a forum post to new messages, or reopens it.
func (b *Bridge) SetPostLocked(guildID, postID string, locked bool) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.SetPostLocked(guildID, postID, locked)
}

// SetForumBanner sets a forum's own artwork: a data URI, "preset:<id>", or ""
// to clear it.
func (b *Bridge) SetForumBanner(guildID, forumID, banner string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.SetForumBanner(guildID, forumID, banner)
}

// CreateEvent adds a calendar event to a guild (any member). Times are UTC
// Unix seconds; endUnix zero means "no stated end". locationChannelID ties the
// event to one of the guild's own channels ("" = free-text location only).
func (b *Bridge) CreateEvent(guildID, title, details string, startUnix, endUnix int64, location, locationChannelID string) (domain.Event, error) {
	svc, err := b.service()
	if err != nil {
		return domain.Event{}, err
	}
	return svc.CreateEvent(guildID, title, details, startUnix, endUnix, location, locationChannelID)
}

// UpdateEvent edits an event (author or ManageMessages).
func (b *Bridge) UpdateEvent(guildID, eventID, title, details string, startUnix, endUnix int64, location, locationChannelID string) (domain.Event, error) {
	svc, err := b.service()
	if err != nil {
		return domain.Event{}, err
	}
	return svc.UpdateEvent(guildID, eventID, title, details, startUnix, endUnix, location, locationChannelID)
}

// DeleteEvent removes an event (author or ManageMessages).
func (b *Bridge) DeleteEvent(guildID, eventID string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.DeleteEvent(guildID, eventID)
}

// Events returns a guild's calendar, ordered by start time.
func (b *Bridge) Events(guildID string) ([]domain.Event, error) {
	svc, err := b.service()
	if err != nil {
		return nil, err
	}
	return svc.Events(guildID)
}

// StoryView is one Moments story as the frontend consumes it: the signed
// record's display fields plus the resolved author name and this device's
// local seen flag. The signature stays in the core — the UI has no use for it.
type StoryView struct {
	ID         string `json:"id"`
	GuildID    string `json:"guildId"`
	Author     string `json:"author"` // account fingerprint
	AuthorName string `json:"authorName"`
	Preset     string `json:"preset"`
	Caption    string `json:"caption"`
	Color1     string `json:"color1"`
	Color2     string `json:"color2"`
	PostedAt   int64  `json:"postedAt"`  // unix seconds
	ExpiresAt  int64  `json:"expiresAt"` // unix seconds
	Seen       bool   `json:"seen"`
}

// PostStory publishes a text-on-banner story to each of the given guilds.
func (b *Bridge) PostStory(guildIDs []string, preset, caption string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.PostStory(guildIDs, preset, caption)
}

// GuildStories returns a guild's unexpired stories, newest first.
func (b *Bridge) GuildStories(guildID string) ([]StoryView, error) {
	svc, err := b.service()
	if err != nil {
		return nil, err
	}
	recs, err := svc.GuildStories(guildID)
	if err != nil {
		return nil, err
	}
	self := svc.Fingerprint()
	out := make([]StoryView, 0, len(recs))
	for _, r := range recs {
		// Resolve the author's display name the way the member list does: own
		// profile for ourselves (the cache holds no self row), a per-guild
		// nickname shadowing the profile name for everyone.
		name := svc.ProfileOf(r.Author).Name
		if r.Author == self {
			name = svc.SelfProfile().Name
		}
		if nick := svc.NickOf(guildID, r.Author); nick != "" {
			name = nick
		}
		out = append(out, StoryView{
			ID: r.StoryID, GuildID: r.GuildID, Author: r.Author, AuthorName: name,
			Preset: r.Preset, Caption: r.Caption, Color1: r.Color1, Color2: r.Color2,
			PostedAt: r.PostedAt, ExpiresAt: r.ExpiresAt, Seen: svc.StoryIsSeen(r.StoryID),
		})
	}
	return out, nil
}

// MarkStorySeen records locally that the user opened a story (no view
// receipts — nothing leaves this device).
func (b *Bridge) MarkStorySeen(storyID string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.MarkStorySeen(storyID)
}

// DeleteStory retracts one of this account's own stories everywhere.
func (b *Bridge) DeleteStory(storyID string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.DeleteStory(storyID)
}

// RSVPEvent records this account's answer to an event: going|maybe|no, or ""
// to clear it.
func (b *Bridge) RSVPEvent(guildID, eventID, state string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.RSVP(guildID, eventID, state)
}

// EventICS exports one event as RFC 5545 text (a file for the user's own
// calendar app — the format, not a vendor).
func (b *Bridge) EventICS(guildID, eventID string) (string, error) {
	svc, err := b.service()
	if err != nil {
		return "", err
	}
	return svc.EventICS(guildID, eventID)
}

// EventsICS exports a guild's whole calendar as RFC 5545 text.
func (b *Bridge) EventsICS(guildID string) (string, error) {
	svc, err := b.service()
	if err != nil {
		return "", err
	}
	return svc.EventsICS(guildID)
}

// OpenEventGuests opens a calendar event to browser guests: mints (or
// returns) the event's disposable meeting room and its shareable link, which
// lands on the event record itself. autoAdmit false = arrivals knock.
func (b *Bridge) OpenEventGuests(guildID, eventID string, autoAdmit bool) (domain.Event, error) {
	svc, err := b.service()
	if err != nil {
		return domain.Event{}, err
	}
	return svc.OpenEventGuests(guildID, eventID, autoAdmit)
}

// RevokeEventGuests closes an event to guests: link dead, room gone.
func (b *Bridge) RevokeEventGuests(guildID, eventID string) (domain.Event, error) {
	svc, err := b.service()
	if err != nil {
		return domain.Event{}, err
	}
	return svc.RevokeEventGuests(guildID, eventID)
}

// JoinEventRoom joins an event's meeting room as a full member (one-tap Join
// for people already in the event's guild/DM; guests keep the browser link).
func (b *Bridge) JoinEventRoom(guildID, eventID string) (domain.Guild, error) {
	svc, err := b.service()
	if err != nil {
		return domain.Guild{}, err
	}
	return svc.JoinEventRoom(guildID, eventID)
}

// BookingSettings returns the public-booking config, the page URL (when
// live) and the upcoming bookings, for the Settings → Bookings panel.
func (b *Bridge) BookingSettings() (appsvc.BookingView, error) {
	svc, err := b.service()
	if err != nil {
		return appsvc.BookingView{}, err
	}
	return svc.BookingSettings(), nil
}

// SetBookingConfig saves office-hours availability and the page toggle.
func (b *Bridge) SetBookingConfig(in appsvc.BookingConfigInput) (appsvc.BookingView, error) {
	svc, err := b.service()
	if err != nil {
		return appsvc.BookingView{}, err
	}
	return svc.SetBookingConfig(in)
}

// CancelBooking frees a booked slot: calendar event and meeting room go, and
// the visitor's link stops answering.
func (b *Bridge) CancelBooking(eventID string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.CancelBooking(eventID)
}

// CreateCategory adds a sidebar category to a guild.
func (b *Bridge) CreateCategory(guildID, name string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	_, err = svc.CreateCategory(guildID, name)
	return err
}

// DeleteChannel removes a channel (ManageChannels).
func (b *Bridge) DeleteChannel(guildID, channelID string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.DeleteChannel(guildID, channelID)
}

// DeleteCategory removes a category, un-categorizing its channels (ManageChannels).
func (b *Bridge) DeleteCategory(guildID, categoryID string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.DeleteCategory(guildID, categoryID)
}

// SetGuildProfile updates the guild's name/icon/banner/description (ManageGuild).
func (b *Bridge) SetGuildProfile(guildID, name, icon, banner, description string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.SetGuildProfile(guildID, name, icon, banner, description)
}

// SetChannelMeta changes a channel's type/category/position/topic.
func (b *Bridge) SetChannelMeta(guildID, channelID, ctype, category string, position int, topic string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.SetChannelMeta(guildID, channelID, ctype, category, position, topic)
}

// RenameChannel renames a channel for every member (ManageChannels).
func (b *Bridge) RenameChannel(guildID, channelID, name string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.RenameChannel(guildID, channelID, name)
}

// SendTyping broadcasts an ephemeral typing hint for a channel.
func (b *Bridge) SendTyping(channelID string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.SendTyping(channelID)
}

// JoinVoice enters a channel's voice room.
func (b *Bridge) JoinVoice(channelID string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.JoinVoice(channelID)
}

// LeaveVoice leaves a channel's voice room.
func (b *Bridge) LeaveVoice(channelID string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.LeaveVoice(channelID)
}

// SignalCall broadcasts a soft-lock control action
// (lock/unlock/knock/admit/refuse/move/disconnect). A target of
// "guest:<name>#<session>" acts on a browser guest and stays on this node.
func (b *Bridge) SignalCall(channelID, action, target, dest string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.PublishCallControl(channelID, action, target, dest)
}

// RelaySignal forwards a WebRTC signaling blob to a peer.
func (b *Bridge) RelaySignal(toPeerID, data string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.RelaySignal(toPeerID, []byte(data))
}

func (b *Bridge) Identity() (IdentityInfo, error) {
	svc, err := b.service()
	if err != nil {
		return IdentityInfo{}, err
	}
	p := svc.SelfProfile()
	return IdentityInfo{
		PeerID:      svc.PeerID(),
		Fingerprint: svc.Fingerprint(),
		DisplayName: p.Name,
		Status:      p.Status,
		Emoji:       p.Emoji,
		Color:       p.Color,
		Avatar:      p.Avatar,
		Banner:      p.Banner,
		Presence:    p.Presence,
		Bio:         p.Bio,
		Birthday:    p.Birthday,
		Activity:    p.Activity,
		Games:       p.Games,
		Color2:      p.Color2,
		Frame:       p.Frame,
		Effect:      p.Effect,
		Style:       p.Style,
	}, nil
}

// SetGames replaces this peer's game collection (profile card section).
func (b *Bridge) SetGames(games []appsvc.Game) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.SetGames(games)
}

// SearchGames suggests real games (name + box art) for the collection
// editor's autocomplete. Best-effort; empty on network trouble.
func (b *Bridge) SearchGames(query string) ([]appsvc.GameSearchResult, error) {
	svc, err := b.service()
	if err != nil {
		return nil, err
	}
	return svc.SearchGames(query), nil
}

// SetProfile updates this peer's profile (incl. avatar + banner images) and
// re-announces. Args are positional over the RPC, so a new field
// birthday — "MM-DD", never a year) go LAST: older callers just stop early.
func (b *Bridge) SetProfile(name, status, emoji, color, avatar, banner, presence, bio, color2, frame, effect, styleJSON, birthday string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.SetProfile(appsvc.Profile{
		Name: name, Status: status, Emoji: emoji, Color: color, Avatar: avatar,
		Banner: banner, Presence: presence, Bio: bio, Birthday: birthday,
		Color2: color2, Frame: frame, Effect: effect, Style: parseStyle(styleJSON),
	})
}

// parseStyle turns the UI's style JSON into the app struct ("" = defaults).
func parseStyle(raw string) *appsvc.Style {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var st appsvc.Style
	if json.Unmarshal([]byte(raw), &st) != nil {
		return nil
	}
	return &st
}

// VerifyFingerprint marks a member's identity as verified after an out-of-band
// fingerprint comparison.
func (b *Bridge) VerifyFingerprint(fingerprint string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.VerifyFingerprint(fingerprint)
}

// PinMessage toggles a message's pinned state for everyone.
func (b *Bridge) PinMessage(channelID, messageID string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.PinMessage(channelID, messageID)
}

// SearchMessages searches this peer's full local history.
func (b *Bridge) SearchMessages(query string) ([]MessageView, error) {
	svc, err := b.service()
	if err != nil {
		return nil, err
	}
	msgs, err := svc.SearchMessages(query, 50)
	if err != nil {
		return nil, err
	}
	return visibleViews(svc, msgs), nil
}

// Saved messages (bookmarks) — device-local; the panel reuses the same
// MessageView shape the feed renders, so jump-to-message just works.
func (b *Bridge) BookmarkMessage(messageID, channelID string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.BookmarkMessage(messageID, channelID)
}

func (b *Bridge) UnbookmarkMessage(messageID string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.UnbookmarkMessage(messageID)
}

func (b *Bridge) SavedMessages() ([]MessageView, error) {
	svc, err := b.service()
	if err != nil {
		return nil, err
	}
	msgs, err := svc.BookmarkedMessages()
	if err != nil {
		return nil, err
	}
	return visibleViews(svc, msgs), nil
}

func (b *Bridge) SavedMessageIDs() ([]string, error) {
	svc, err := b.service()
	if err != nil {
		return nil, err
	}
	msgs, err := svc.BookmarkedMessages()
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(msgs))
	for _, m := range msgs {
		ids = append(ids, m.ID)
	}
	return ids, nil
}

// SetDisplayName updates this peer's display name.
func (b *Bridge) Guilds() ([]GuildView, error) {
	svc, err := b.service()
	if err != nil {
		return nil, err
	}
	guilds := svc.Guilds()
	out := make([]GuildView, 0, len(guilds))
	for _, g := range guilds {
		out = append(out, guildView(svc, g))
	}
	return out, nil
}

func (b *Bridge) CreateGuild(name string) (GuildView, error) {
	svc, err := b.service()
	if err != nil {
		return GuildView{}, err
	}
	g, err := svc.CreateGuild(name)
	if err != nil {
		return GuildView{}, err
	}
	return guildView(svc, g), nil
}

func (b *Bridge) InviteCode(guildID string) (string, error) {
	svc, err := b.service()
	if err != nil {
		return "", err
	}
	return svc.InviteCode(guildID)
}

func (b *Bridge) JoinViaInvite(code string) (GuildView, error) {
	svc, err := b.service()
	if err != nil {
		return GuildView{}, err
	}
	g, err := svc.JoinViaInvite(code)
	if err != nil {
		return GuildView{}, err
	}
	return guildView(svc, g), nil
}

// MessageRequests lists DM invites from strangers that are waiting for a yes.
// Nothing in the list has been redeemed — see internal/app/request.go.
func (b *Bridge) MessageRequests() ([]appsvc.MessageRequest, error) {
	svc, err := b.service()
	if err != nil {
		return nil, err
	}
	return svc.MessageRequests(), nil
}

// AcceptMessageRequest redeems a held invite and opens the conversation.
func (b *Bridge) AcceptMessageRequest(fingerprint string) (GuildView, error) {
	svc, err := b.service()
	if err != nil {
		return GuildView{}, err
	}
	g, err := svc.AcceptMessageRequest(fingerprint)
	if err != nil {
		return GuildView{}, err
	}
	return guildView(svc, g), nil
}

// DeclineMessageRequest drops a request, optionally blocking the sender. The
// sender is never told.
func (b *Bridge) DeclineMessageRequest(fingerprint string, block bool) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.DeclineMessageRequest(fingerprint, block)
}

// TypingEnabled reports whether typing indicators are exchanged (reciprocal).
func (b *Bridge) TypingEnabled() (bool, error) {
	svc, err := b.service()
	if err != nil {
		return true, nil
	}
	return svc.TypingEnabled(), nil
}

// SetTypingEnabled turns typing indicators on or off, in both directions.
func (b *Bridge) SetTypingEnabled(on bool) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.SetTypingEnabled(on)
}

func (b *Bridge) Messages(channelID string) ([]MessageView, error) {
	svc, err := b.service()
	if err != nil {
		return nil, err
	}
	msgs, err := svc.Messages(channelID, 200)
	if err != nil {
		return nil, err
	}
	return visibleViews(svc, msgs), nil
}

// MessagesBefore returns the page of messages older than the RFC3339 cursor
// (the sent time of the oldest row the client currently holds), oldest first.
// An empty/unparseable cursor returns nothing. This is the scroll-up pagination
// that surfaces history beyond the initial 200-row load.
func (b *Bridge) MessagesBefore(channelID, beforeISO string, limit int) ([]MessageView, error) {
	svc, err := b.service()
	if err != nil {
		return nil, err
	}
	t, err := time.Parse("2006-01-02T15:04:05Z07:00", beforeISO)
	if err != nil {
		return []MessageView{}, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	msgs, err := svc.MessagesBefore(channelID, t.UnixNano(), limit)
	if err != nil {
		return nil, err
	}
	return visibleViews(svc, msgs), nil
}

// UnreadCounts returns the per-channel unread message count. sinceISO maps a
// channel ID to the RFC3339 read cursor ("" = from the beginning). Counting
// happens in SQL with no decryption — this replaces a full-history decrypt of
// every channel on login and on cross-device read-state events.
func (b *Bridge) UnreadCounts(sinceISO map[string]string) (map[string]int, error) {
	svc, err := b.service()
	if err != nil {
		return nil, err
	}
	sinceNano := make(map[string]int64, len(sinceISO))
	for ch, iso := range sinceISO {
		if iso == "" {
			sinceNano[ch] = 0
			continue
		}
		if t, err := time.Parse("2006-01-02T15:04:05Z07:00", iso); err == nil {
			sinceNano[ch] = t.UnixNano()
		}
	}
	return svc.UnreadCounts(sinceNano)
}

// dir is the author's explicit base direction — "rtl", "ltr", or "" for the
// per-line heuristic. Absent from an older caller's argument list it arrives as
// "", which is exactly the behaviour every message had before the field
// existed, so the extra argument is additive rather than breaking.
func (b *Bridge) SendMessage(channelID, content, replyTo, dir string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	_, err = svc.SendMessage(channelID, content, replyTo, dir)
	return err
}

// SendCallNotice posts a call event line (e.g. "call-missed") into a channel.
// The caller's client emits it when a DM ring goes unanswered.
func (b *Bridge) SendCallNotice(channelID, kind, content string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	_, err = svc.SendCallNotice(channelID, kind, content)
	return err
}

// ScheduledSendView is one queued send-later message for the manager UI.
// fireAt is unix seconds.
type ScheduledSendView struct {
	ID        string `json:"id"`
	ChannelID string `json:"channelId"`
	Content   string `json:"content"`
	FireAt    int64  `json:"fireAt"`
}

// ScheduleSend queues a message to be sent later by the Go service, so it
// fires as long as this device's Concord is running — the window that queued
// it can close. fireAtUnix is unix seconds. Returns the queue row id.
func (b *Bridge) ScheduleSend(channelID, content, replyTo string, fireAtUnix int64) (string, error) {
	svc, err := b.service()
	if err != nil {
		return "", err
	}
	return svc.ScheduleSend(channelID, content, replyTo, fireAtUnix)
}

// CancelScheduledSend removes a queued send before it fires.
func (b *Bridge) CancelScheduledSend(id string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.CancelScheduledSend(id)
}

// ScheduledSends returns the send-later queue, soonest first.
func (b *Bridge) ScheduledSends() ([]ScheduledSendView, error) {
	svc, err := b.service()
	if err != nil {
		return nil, err
	}
	list, err := svc.ScheduledSends()
	if err != nil {
		return nil, err
	}
	out := make([]ScheduledSendView, 0, len(list))
	for _, ss := range list {
		out = append(out, ScheduledSendView{ID: ss.ID, ChannelID: ss.ChannelID, Content: ss.Content, FireAt: ss.FireAt})
	}
	return out, nil
}

// PreviewView is a scraped link summary for embeds (see internal/app/preview.go).
type PreviewView struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
	ImageURL    string `json:"imageUrl"`
	SiteName    string `json:"siteName"`
}

// LinkPreview fetches a link's OpenGraph metadata (SSRF-guarded, cached).
func (b *Bridge) LinkPreview(url string) (PreviewView, error) {
	svc, err := b.service()
	if err != nil {
		return PreviewView{}, err
	}
	p, err := svc.LinkPreview(url)
	if err != nil {
		return PreviewView{}, err
	}
	return PreviewView{URL: p.URL, Title: p.Title, Description: p.Description, ImageURL: p.ImageURL, SiteName: p.SiteName}, nil
}

// SendAttachment seals an image into a local encrypted blob and posts the
// reference token as a chat message (see internal/app/attach.go).
//
// It returns the new blob's id. Callers that only post an image ignore it; the
// meme editor keys the recipe it saves for "edit this meme again" by exactly
// this id, and the id is minted inside the seal — there is nothing the frontend
// could compute it from.
func (b *Bridge) SendAttachment(channelID, dataURL string, w, h int, replyTo string, spoiler bool, name, desc string) (string, error) {
	svc, err := b.service()
	if err != nil {
		return "", err
	}
	msg, err := svc.SendAttachment(channelID, dataURL, w, h, replyTo, spoiler, name, desc)
	if err != nil {
		return "", err
	}
	return appsvc.AttachBlobID(msg.Content), nil
}

// EditAttachment re-points one of this peer's own image messages at a newly
// sealed picture, and returns the new blob's id. One message in, one message
// out — see app.EditAttachment for why this isn't "send then delete".
func (b *Bridge) EditAttachment(channelID, messageID, dataURL string, w, h int) (string, error) {
	svc, err := b.service()
	if err != nil {
		return "", err
	}
	return svc.EditAttachment(channelID, messageID, dataURL, w, h)
}

// FetchAttachment resolves an attachment token to a plaintext image data URL,
// fetching the blob from guild members if it isn't cached locally.
func (b *Bridge) FetchAttachment(channelID, blobID, keys, subtype string) (string, error) {
	svc, err := b.service()
	if err != nil {
		return "", err
	}
	return svc.FetchAttachment(channelID, blobID, keys, subtype)
}

// SendFile seals an arbitrary file into an encrypted blob and posts a file
// reference token (rendered as a download card, not inline).
func (b *Bridge) SendFile(channelID, dataURL, filename, replyTo string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	_, err = svc.SendFile(channelID, dataURL, filename, replyTo)
	return err
}

// FetchFile resolves a file token to a plaintext data URL of the given mime,
// fetching the blob from guild members if not cached locally.
func (b *Bridge) FetchFile(channelID, blobID, keys, mime string) (string, error) {
	svc, err := b.service()
	if err != nil {
		return "", err
	}
	return svc.FetchFile(channelID, blobID, keys, mime)
}

func (b *Bridge) Members(guildID string) ([]MemberView, error) {
	svc, err := b.service()
	if err != nil {
		return nil, err
	}
	creds, err := svc.GuildMembers(guildID)
	if err != nil {
		return nil, err
	}
	self := svc.PublicKey()

	// A member is "online" if we currently hold a connection to the peer whose
	// key yields that fingerprint (ourselves always count).
	online := map[string]bool{}
	for _, p := range svc.Peers() {
		online[p.Fingerprint] = true
	}

	verified := svc.VerifiedFingerprints()
	heir := svc.GuildHeir(guildID)
	out := make([]MemberView, 0, len(creds))
	// One row per ACCOUNT: an account's linked devices are separate MLS leaves,
	// but they're the same person — collapse them by account fingerprint so a
	// phone+desktop shows as one member, not a phantom "cryptic" second user.
	seenAccount := map[string]bool{}
	for _, cred := range creds {
		fpr := svc.AccountFingerprintOf(cred)
		if seenAccount[fpr] {
			continue
		}
		seenAccount[fpr] = true
		isSelf := bytes.Equal(svc.AccountKeyOf(cred), self)
		p := svc.ProfileOf(fpr)
		if isSelf {
			p = svc.SelfProfile()
		}
		// A per-guild nickname shadows the profile name inside this guild; keep
		// the profile name in Username so the UI can show "nick (username)".
		name, username := p.Name, ""
		if nick := svc.NickOf(guildID, fpr); nick != "" {
			name, username = nick, p.Name
		}
		isOwner := svc.IsGuildOwner(guildID, fpr)
		perms := uint32(svc.MemberPermission(guildID, fpr))
		out = append(out, MemberView{
			Fingerprint: fpr,
			Name:        name,
			Username:    username,
			Status:      p.Status,
			Emoji:       p.Emoji,
			Color:       p.Color,
			Avatar:      p.Avatar,
			Activity:    p.Activity,
			Games:       p.Games,
			Color2:      p.Color2,
			Frame:       p.Frame,
			Effect:      p.Effect,
			Style:       p.Style,
			Banner:      p.Banner,
			Presence:    p.Presence,
			Bio:         p.Bio,
			Birthday:    p.Birthday,
			IsSelf:      isSelf,
			Online:      isSelf || online[fpr],
			Verified:    isSelf || verified[fpr],
			IsOwner:     isOwner,
			IsHeir:      heir != "" && fpr == heir,
			Perms:       perms,
			CanManage:   isOwner || perms&uint32(appsvc.PermManageMembers) != 0,
			RoleIDs:     svc.MemberRoleIDs(guildID, fpr),
			MutedUntil:  svc.MutedUntil(guildID, fpr),
		})
	}
	// The MLS library yields members in map order (random per call), which made
	// the roster reshuffle on every refresh. Sort deterministically: online
	// first, then by display name, then by fingerprint as the tiebreaker.
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Online != b.Online {
			return a.Online
		}
		an, bn := strings.ToLower(a.Name), strings.ToLower(b.Name)
		if an != bn {
			if an == "" || bn == "" {
				return bn == "" // named members before unnamed ones
			}
			return an < bn
		}
		return a.Fingerprint < b.Fingerprint
	})
	// Append people you've added who haven't joined yet (see pending.go) AFTER the
	// sort, so they sit at the bottom as greyed "pending" rows — the roster shows
	// them immediately, like an opened DM, instead of nothing until they sync.
	for _, fpr := range svc.PendingMembersFor(guildID) {
		if seenAccount[fpr] {
			continue
		}
		seenAccount[fpr] = true
		p := svc.ProfileOf(fpr)
		name := p.Name
		if nick := svc.NickOf(guildID, fpr); nick != "" {
			name = nick
		}
		out = append(out, MemberView{
			Fingerprint: fpr,
			Name:        name,
			Status:      p.Status,
			Emoji:       p.Emoji,
			Color:       p.Color,
			Avatar:      p.Avatar,
			Presence:    p.Presence,
			Online:      online[fpr],
			Verified:    true,
			Pending:     true,
		})
	}
	return out, nil
}

// SetNickname sets this member's own per-guild display name (empty clears it).
func (b *Bridge) SetNickname(guildID, nick string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.SetNickname(guildID, nick)
}

// SetMemberNickname sets ANOTHER member's per-guild nickname. Requires
// MANAGE_MEMBERS and outranking them — enforced again on every peer that
// receives the change, not just here.
// RevealDeleted returns a moderator the original text of a soft-deleted guild
// message (see Service.RevealDeleted). DM deletes are unrecoverable.
func (b *Bridge) RevealDeleted(channelID, messageID string) (string, error) {
	svc, err := b.service()
	if err != nil {
		return "", err
	}
	return svc.RevealDeleted(channelID, messageID)
}

// CallIceServers returns ICE config (STUN + optional TURN with fresh creds) for
// starting a call. See internal/app/ice.go.
func (b *Bridge) CallIceServers() (appsvc.IceConfig, error) {
	svc, err := b.service()
	if err != nil {
		return appsvc.IceConfig{}, err
	}
	return svc.CallIceServers(), nil
}

// PurgeMessages clears the last n messages in a channel (needs MANAGE_MESSAGES).
func (b *Bridge) PurgeMessages(channelID string, n int) (int, error) {
	svc, err := b.service()
	if err != nil {
		return 0, err
	}
	return svc.PurgeMessages(channelID, n)
}

// AddMember adds a verified contact to a guild directly (see Service.AddMember).
func (b *Bridge) AddMember(guildID, fingerprint string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.AddMember(guildID, fingerprint)
}

func (b *Bridge) SetMemberNickname(guildID, fingerprint, nick string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.SetMemberNickname(guildID, fingerprint, nick)
}

// RoleView is a role definition for the UI.
type RoleView struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Color    string `json:"color"`
	Perms    uint32 `json:"perms"`
	Position int    `json:"position"`
}

// Roles lists a guild's roles (highest position first).
func (b *Bridge) Roles(guildID string) ([]RoleView, error) {
	svc, err := b.service()
	if err != nil {
		return nil, err
	}
	roles := svc.Roles(guildID)
	sort.Slice(roles, func(i, j int) bool { return roles[i].Position > roles[j].Position })
	out := make([]RoleView, 0, len(roles))
	for _, r := range roles {
		out = append(out, RoleView{ID: r.ID, Name: r.Name, Color: r.Color, Perms: uint32(r.Perms), Position: r.Position})
	}
	return out, nil
}

// UpsertRole creates (empty id) or edits a role; returns the role id.
func (b *Bridge) UpsertRole(guildID, roleID, name, color string, perms, position int) (string, error) {
	svc, err := b.service()
	if err != nil {
		return "", err
	}
	return svc.UpsertRole(guildID, roleID, name, color, appsvc.Permission(perms), position)
}

// DeleteRole removes a role.
func (b *Bridge) DeleteRole(guildID, roleID string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.DeleteRole(guildID, roleID)
}

// AssignRole grants (add=true) or revokes a role from a member.
func (b *Bridge) AssignRole(guildID, fingerprint, roleID string, add bool) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.AssignRole(guildID, fingerprint, roleID, add)
}

// TransferOwnership hands the guild to another member (current owner only).
// One signed governance op — the MLS group and epoch are untouched.
func (b *Bridge) TransferOwnership(guildID, fingerprint string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.TransferOwnership(guildID, fingerprint)
}

// SetHeir pre-authorizes a member to claim guild ownership (owner only).
// The heir can use it AT ANY TIME — it is a standing, revocable authorization,
// not a dead-man switch, and the UI words it that way.
func (b *Bridge) SetHeir(guildID, fingerprint string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.SetHeir(guildID, fingerprint)
}

// ClearHeir revokes the guild's heir designation (owner only).
func (b *Bridge) ClearHeir(guildID string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.ClearHeir(guildID)
}

// ClaimOwnership makes THIS peer the guild owner, if it is the named heir.
// One signed governance op — the MLS group and epoch are untouched.
func (b *Bridge) ClaimOwnership(guildID string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.ClaimOwnership(guildID)
}

// BanMember bars a fingerprint and evicts them if present (manage-members).
func (b *Bridge) BanMember(guildID, fingerprint string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.BanMember(guildID, fingerprint)
}

// UnbanMember lifts a ban (manage-members).
func (b *Bridge) UnbanMember(guildID, fingerprint string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.UnbanMember(guildID, fingerprint)
}

// MuteMember times a member out for `minutes` (mute-members).
func (b *Bridge) MuteMember(guildID, fingerprint string, minutes int) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.MuteMember(guildID, fingerprint, minutes)
}

// ExportMarkdown renders a readable transcript of one channel, or of the whole
// guild when channelID is empty. Reads the store, so it covers the entire
// history rather than the page the UI has loaded.
func (b *Bridge) ExportMarkdown(guildID, channelID string) (string, error) {
	svc, err := b.service()
	if err != nil {
		return "", err
	}
	return svc.ExportMarkdown(guildID, channelID)
}

// ExportArchive returns a sealed history archive as base64, plus a count of
// what went in. Base64 because the RPC surface is JSON — the caller writes it
// to a file. withAttachments carries the cached blobs too (large, and complete
// only as far as the cache still holds them).
func (b *Bridge) ExportArchive(passphrase string, withAttachments bool) (map[string]any, error) {
	svc, err := b.service()
	if err != nil {
		return nil, err
	}
	sealed, st, err := svc.ExportArchive(passphrase, withAttachments)
	if err != nil {
		return nil, err
	}
	return map[string]any{"data": base64.StdEncoding.EncodeToString(sealed), "stats": st}, nil
}

// ImportArchive merges a base64 sealed archive into this device. Additive: it
// never overwrites or removes anything already here.
func (b *Bridge) ImportArchive(dataB64, passphrase string) (appsvc.ArchiveStats, error) {
	svc, err := b.service()
	if err != nil {
		return appsvc.ArchiveStats{}, err
	}
	raw, err := base64.StdEncoding.DecodeString(dataB64)
	if err != nil {
		return appsvc.ArchiveStats{}, fmt.Errorf("bridge: archive is not valid base64: %w", err)
	}
	return svc.ImportArchive(raw, passphrase)
}

// ChronicleMessagesView is a page of a guild's history archive, plus the one
// thing a page of it can be instead of messages: a refusal to spend a data plan
// on a megabyte nobody urgently needs. Metered is a field rather than an error
// because the frontend's response to it is an offer ("fetch anyway?"), not a
// failure, and matching on an error string to tell the two apart is exactly the
// kind of coupling a view type exists to avoid.
type ChronicleMessagesView struct {
	Messages []appsvc.ChronicleMessageView `json:"messages"`
	Metered  bool                          `json:"metered,omitempty"`
}

// AttachChronicle records a signed history archive against a guild. Owner-only.
// The manifest and every page arrive base64-encoded because the RPC surface is
// JSON; the pages are keyed by the content address the manifest names them by,
// and a mismatch fails the whole call rather than storing a partial archive.
func (b *Bridge) AttachChronicle(guildID, manifestB64 string, chunksB64 map[string]string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	manifest, err := base64.StdEncoding.DecodeString(manifestB64)
	if err != nil {
		return fmt.Errorf("bridge: the archive index is not valid base64: %w", err)
	}
	chunks := make(map[string][]byte, len(chunksB64))
	for id, b64 := range chunksB64 {
		raw, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return fmt.Errorf("bridge: archive page %s is not valid base64: %w", id, err)
		}
		chunks[id] = raw
	}
	return svc.AttachChronicle(guildID, manifest, chunks)
}

// ChronicleInfo describes a guild's archive: what it is, how big, and how much
// of it this device holds. The zero view means the guild has no archive.
func (b *Bridge) ChronicleInfo(guildID string) (appsvc.ChronicleView, error) {
	svc, err := b.service()
	if err != nil {
		return appsvc.ChronicleView{}, err
	}
	return svc.ChronicleInfo(guildID)
}

// ChronicleMessages reads one page of the archive, newest-backwards from
// beforeNano (0 = the newest), returned oldest-first.
func (b *Bridge) ChronicleMessages(guildID, channelID string, beforeNano int64, limit int, allowMetered bool) (ChronicleMessagesView, error) {
	svc, err := b.service()
	if err != nil {
		return ChronicleMessagesView{}, err
	}
	msgs, err := svc.ChronicleMessages(guildID, channelID, beforeNano, limit, allowMetered)
	if errors.Is(err, appsvc.ErrChronicleMetered) {
		return ChronicleMessagesView{Metered: true}, nil
	}
	if err != nil {
		return ChronicleMessagesView{}, err
	}
	return ChronicleMessagesView{Messages: msgs}, nil
}

// SetChroniclePinned keeps a guild's archive on this device permanently, or
// returns its pages to the evictable cache.
func (b *Bridge) SetChroniclePinned(guildID string, pinned bool) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.SetChroniclePinned(guildID, pinned)
}

// SetRetention sets how long messages are kept, in seconds (0 = forever).
// An empty channelID sets the guild-wide policy; a channel id overrides it for
// that channel. Needs manage-guild — it deletes other members' copies too.
func (b *Bridge) SetRetention(guildID, channelID string, seconds int) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.SetRetention(guildID, channelID, int64(seconds))
}

// GuildRetention reads the guild-wide policy in seconds (0 = forever).
func (b *Bridge) GuildRetention(guildID string) (int64, error) {
	svc, err := b.service()
	if err != nil {
		return 0, err
	}
	return svc.RetentionSeconds(guildID, ""), nil
}

// SetSlowMode sets a channel's governed posting interval (manage-channels;
// 0 turns it off).
func (b *Bridge) SetSlowMode(guildID, channelID string, seconds int) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.SetSlowMode(guildID, channelID, int64(seconds))
}

// UnmuteMember lifts a mute (mute-members).
func (b *Bridge) UnmuteMember(guildID, fingerprint string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.UnmuteMember(guildID, fingerprint)
}

// BanView is a banned member surfaced for the moderation UI.
type BanView struct {
	Fingerprint string `json:"fingerprint"`
	Name        string `json:"name"`
}

// Bans lists a guild's banned members (fingerprint + best-known name).
func (b *Bridge) Bans(guildID string) ([]BanView, error) {
	svc, err := b.service()
	if err != nil {
		return nil, err
	}
	out := []BanView{}
	for _, fpr := range svc.BannedFingerprints(guildID) {
		out = append(out, BanView{Fingerprint: fpr, Name: svc.ProfileName(fpr)})
	}
	return out, nil
}

func (b *Bridge) RemoveMember(guildID, fingerprint string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	creds, err := svc.GuildMembers(guildID)
	if err != nil {
		return err
	}
	// Match by ACCOUNT fingerprint and remove every leaf that account has in the
	// group — kicking a person removes all of their linked devices, not just one.
	removed := false
	for _, cred := range creds {
		if svc.AccountFingerprintOf(cred) == fingerprint {
			if err := svc.RemoveMember(guildID, cred); err == nil {
				removed = true
			}
		}
	}
	if removed {
		return nil
	}
	return errors.New("member not found")
}

func (b *Bridge) Contacts() ([]ContactView, error) {
	svc, err := b.service()
	if err != nil {
		return nil, err
	}
	contacts, err := svc.Contacts()
	if err != nil {
		return nil, err
	}
	out := make([]ContactView, 0, len(contacts))
	for _, c := range contacts {
		p := svc.ProfileOf(c.Fingerprint)
		out = append(out, ContactView{
			PeerID: c.PeerID, Fingerprint: c.Fingerprint, Name: p.Name, Verified: c.Verified,
			Avatar: p.Avatar, Emoji: p.Emoji, Color: p.Color,
		})
	}
	return out, nil
}

// NetworkStatus reports connectivity for the UI's connection indicator. Returns
// a zeroed (offline) status when the identity is locked rather than erroring, so
// the login screen can render the pill without special-casing.
func (b *Bridge) NetworkStatus() appsvc.NetStatus {
	svc, err := b.service()
	if err != nil {
		return appsvc.NetStatus{}
	}
	return svc.NetworkStatus()
}

// ReachabilityStatus reports whether other people can reach this node, and by
// which route, for the "Can people reach me?" panel. Returns a zeroed status
// when the identity is locked — the same convention as NetworkStatus, and the
// honest answer there anyway: a locked client is running no node at all.
func (b *Bridge) ReachabilityStatus() appsvc.ReachStatus {
	svc, err := b.service()
	if err != nil {
		return appsvc.ReachStatus{}
	}
	return svc.Reachability()
}

// Nudge forces a fast reconnect + mailbox drain + resync. The mobile shell calls
// it when the OS resumes the app. No-op (nil error) when locked.
func (b *Bridge) Nudge() error {
	svc, err := b.service()
	if err != nil {
		return nil
	}
	svc.Nudge()
	return nil
}

// SetMetered is the shell reporting whether the OS considers this connection
// metered — cellular, or a hotspot the user has flagged as billed. On a metered
// link the periodic DHT loops (advertise, bootstrap redial, peer discovery) are
// held to a gentler floor even with the app on screen; message delivery, mailbox
// drains and sync are untouched, because a data plan is not a reason to be
// late or to be wrong. Safe to call while locked — the answer is remembered and
// applied when the service starts.
func (b *Bridge) SetMetered(metered bool) error {
	b.mu.Lock()
	b.wantMetered = metered
	svc := b.svc
	b.mu.Unlock()
	if svc != nil {
		svc.SetMetered(metered)
	}
	return nil
}

// RegisterPush binds a device push token (platform "apns"/"fcm") to our mailbox
// on the rendezvous nodes, so offline deposits trigger a wake. The mobile shell
// calls it with the token from APNs/FCM after login. No-op when locked.
func (b *Bridge) RegisterPush(platform, token string) error {
	svc, err := b.service()
	if err != nil {
		return nil
	}
	svc.RegisterPush(platform, token)
	return nil
}

// LinkOffer starts a device-linking session on this (unlocked) device and
// returns the code to render as a QR for the new device to scan.
func (b *Bridge) LinkOffer() (string, error) {
	svc, err := b.service()
	if err != nil {
		return "", err
	}
	return svc.LinkOffer()
}

// CancelLinkOffer clears the active linking offer (user closed the QR).
func (b *Bridge) CancelLinkOffer() error {
	svc, err := b.service()
	if err != nil {
		return nil
	}
	svc.CancelLinkOffer()
	return nil
}

// RedeemLinkCode is the new device's side of linking: from the LOCKED state, it
// dials the issuer, adopts the account, then logs in (now in linked mode) and
// joins every guild the issuer shared. The passphrase protects the new local
// keystore.
// The returned string is a non-fatal warning to show the user ("" = none).
// It is NOT an error: by the time it can be produced the device is linked and
// logged in, and reporting that as a failure would be a lie that also leaves
// the UI on the login screen in front of a working session.
func (b *Bridge) RedeemLinkCode(code, passphrase string) (string, error) {
	dataDir, err := appsvc.DataDir()
	if err != nil {
		return "", err
	}
	b.mu.Lock()
	hasSvc := b.svc != nil
	b.mu.Unlock()
	if hasSvc {
		return "", errors.New("log out before linking this device to an account")
	}
	res, err := appsvc.RedeemLink(b.ctx, dataDir, code, passphrase)
	if err != nil {
		return "", err
	}
	// Start the service in linked mode (the marker written by RedeemLink flips it).
	if err := b.Login(passphrase); err != nil {
		return "", err
	}
	svc, err := b.service()
	if err != nil {
		return "", err
	}
	// Adopt the account's profile (name/avatar/…) so the linked device presents
	// as the same person, not a blank fingerprint. AdoptLinkedProfile rather
	// than SetProfile: it carries the game collection too, and it keeps the
	// ISSUER's edit stamp — re-stamping here would make this blank device look
	// like the newest editor and push its empty look back at the account. A
	// no-profile issuer offers stamp 0, which adopts nothing.
	svc.AdoptLinkedProfile(res.Profile)
	// Verifications are the account's knowledge ("I compared safety numbers"),
	// not the device's — carry them over so contacts verified on the old device
	// stay verified here.
	svc.ImportVerifiedFingerprints(res.Verified)
	// Join each shared guild so the new device sees existing groups. Best-effort:
	// history also converges via sync, so a transient failure isn't fatal.
	// JoinLinkedInvite, not JoinViaInvite: this loop is machine-driven adoption,
	// and it must honour a leave-tombstone on THIS device (a re-link handing
	// over a guild the user deleted here must not resurrect it), where
	// JoinViaInvite is the human's own paste and clears that tombstone.
	for _, ic := range res.GuildInvites {
		svc.JoinLinkedInvite(ic)
	}
	// Guilds the issuer belongs to but does not administer can't be handed over
	// from here — it has no authority to invite anyone to them. Saying so is the
	// whole point of tracking them: a device that looks linked and is quietly
	// missing guilds is the failure this replaces, and the fix is for a human
	// to accept the new device from a machine that can.
	if n := len(res.MissingGuilds); n > 0 {
		noun, they := "guild", "it"
		if n != 1 {
			noun, they = "guilds", "each"
		}
		return fmt.Sprintf("Linked — but %d %s couldn't be handed over from that device (%s). "+
			"Ask an admin of %s to invite you, or link again from a device that administers them.",
			n, noun, strings.Join(res.MissingGuilds, ", "), they), nil
	}
	return "", nil
}

// ---- mapping helpers ----

// isCustomDMName reports whether a DM guild's stored name is a user-chosen group
// name (vs. the auto-defaults), so guildView keeps it instead of recomputing the
// member-list name.
func isCustomDMName(n string) bool {
	switch n {
	case "", "Direct message", "Group message", "Notes":
		return false
	}
	return true
}

func channelView(c domain.Channel) ChannelView {
	return ChannelView{ID: c.ID, Name: c.Name, Type: c.ChannelType(), Category: c.Category,
		Position: c.Position, Topic: c.Topic, Parent: c.Parent, Links: c.Links,
		ForumTags: c.ForumTags, Tags: c.Tags, Pinned: c.Pinned, Solved: c.Solved,
		Locked: c.Locked, Banner: c.Banner}
}

func guildView(svc *appsvc.Service, g domain.Guild) GuildView {
	channels := make([]ChannelView, 0, len(g.Channels))
	lastActivity := svc.GuildLastActivity(g.ID)
	for _, c := range g.Channels {
		cv := channelView(c)
		if c.Parent != "" {
			cv.LastActivity = svc.ChannelLastActivity(c.ID) // forum post ordering
		}
		cv.SlowMode = svc.SlowModeSeconds(g.ID, c.ID)
		cv.Retention = svc.RetentionSeconds(g.ID, c.ID)
		channels = append(channels, cv)
	}
	cats := []CategoryView{}
	if cc, err := svc.Categories(g.ID); err == nil {
		for _, c := range cc {
			cats = append(cats, CategoryView{ID: c.ID, Name: c.Name, Position: c.Position})
		}
	}
	emoji := []EmojiView{}
	if ee, err := svc.CustomEmoji(g.ID); err == nil {
		for _, e := range ee {
			emoji = append(emoji, EmojiView{Name: e.Name, Image: e.Image})
		}
	}
	name := g.Name
	// A peer DM shows the OTHER member (name + avatar handled UI-side via the
	// fingerprint); a self-DM stays "Notes".
	dmPeer, dmPeerPresence, dmPeerAvatar, dmPeerOnline, dmMembers := "", "", "", false, 0
	var dmFaces []DMFace
	if g.Kind == "dm" {
		if creds, err := svc.GuildMembers(g.ID); err == nil {
			self := svc.PublicKey()
			joined := map[string]bool{}
			var others []string
			for _, c := range creds {
				// Any of OUR OWN devices (their leaf is a device cert of our
				// account) is "self", never an other-member face. And resolve to
				// the ACCOUNT fingerprint, deduping device leaves — so a linked
				// device never shows up as a phantom cryptic person in the DM.
				if bytes.Equal(svc.AccountKeyOf(c), self) {
					continue
				}
				f := svc.AccountFingerprintOf(c)
				if joined[f] {
					continue
				}
				others = append(others, f)
				joined[f] = true
			}
			// Fold in people we've invited who haven't joined yet, so the group
			// shows everyone you picked (Discord-style) even while some are away.
			var pending []string
			for _, f := range svc.PendingDMInvitees(g.ID) {
				if !joined[f] {
					pending = append(pending, f)
				}
			}
			intended := append(append([]string(nil), others...), pending...)
			dmMembers = len(intended) + 1 // + self

			// One face per other member (joined + pending), for the bubble.
			face := func(f string, isPending bool) (DMFace, string) {
				prof := svc.ProfileOf(f)
				dn := prof.Name
				if dn == "" {
					dn = f[:min(9, len(f))]
				}
				return DMFace{Name: dn, Avatar: prof.Avatar, Color: prof.Color, Emoji: prof.Emoji, Pending: isPending}, dn
			}
			names := make([]string, 0, len(intended))
			for _, f := range others {
				fc, dn := face(f, false)
				dmFaces = append(dmFaces, fc)
				names = append(names, dn)
			}
			for _, f := range pending {
				fc, dn := face(f, true)
				dmFaces = append(dmFaces, fc)
				names = append(names, dn)
			}

			// A genuine 2-person DM (one other, nobody pending) shows that member's
			// name + avatar + status dot. Otherwise it's a group: name it after its
			// members, unless the user gave it a custom name.
			if len(others) == 1 && len(pending) == 0 {
				dmPeer = others[0]
				prof := svc.ProfileOf(dmPeer)
				if prof.Name != "" {
					name = prof.Name
				} else {
					// The peer's profile hasn't synced yet — show a friendly
					// placeholder instead of a raw fingerprint stub.
					name = "New conversation"
				}
				dmPeerPresence = prof.Presence
				dmPeerAvatar = prof.Avatar
				for _, p := range svc.Peers() {
					if p.Fingerprint == dmPeer {
						dmPeerOnline = true
						break
					}
				}
			} else if len(intended) > 1 {
				if isCustomDMName(g.Name) {
					name = g.Name // user renamed the group; keep it
				} else {
					sorted := append([]string(nil), names...)
					sort.Strings(sorted)
					name = strings.Join(sorted, ", ")
				}
			}
		}
	}
	return GuildView{
		ID: g.ID, Name: name, Kind: g.Kind, DMPeer: dmPeer, IsOwner: svc.IsOwner(g.ID),
		OwnerFingerprint: svc.GuildOwnerFingerprint(g.ID),
		Heir:             svc.GuildHeir(g.ID),
		DMPeerPresence:   dmPeerPresence, DMPeerOnline: dmPeerOnline,
		DMPeerAvatar: dmPeerAvatar, DMMembers: dmMembers, DMFaces: dmFaces,
		DMNamed:   g.Kind == "dm" && isCustomDMName(g.Name),
		DMNotes:   g.Kind == "dm" && g.Name == "Notes",
		CanManage: svc.CanManageMembers(g.ID),
		MyPerms:   uint32(svc.MemberPermission(g.ID, svc.Fingerprint())),
		Icon:      g.Icon, Banner: g.Banner, Description: g.Description,
		Channels: channels, Categories: cats, Emoji: emoji, OutOfSync: svc.OutOfSync(g.ID),
		LastActivity: lastActivity,
	}
}

// AddCustomEmoji uploads a guild custom emoji (:name: → image).
func (b *Bridge) AddCustomEmoji(guildID, name, dataURI string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.AddCustomEmoji(guildID, name, dataURI)
}

// RemoveCustomEmoji deletes a guild custom emoji.
func (b *Bridge) RemoveCustomEmoji(guildID, name string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.RemoveCustomEmoji(guildID, name)
}

// GifView is one entry of a guild's GIF pack. It carries the attachment
// reference (blob id + key + subtype), not the image — the picker resolves the
// bytes through the same FetchAttachment path a message image uses, so a GIF is
// downloaded once and cached, however many people post it.
type GifView struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Tags    []string `json:"tags,omitempty"`
	Keys    string   `json:"keys"`
	Subtype string   `json:"subtype"`
	Width   int      `json:"w,omitempty"`
	Height  int      `json:"h,omitempty"`
}

// GuildGifs lists a guild's GIF pack (newest first). Searching it is a local
// filter over name and tags in the client — no query ever leaves the device,
// which is the entire reason this exists instead of a Tenor/Giphy picker.
func (b *Bridge) GuildGifs(guildID string) ([]GifView, error) {
	svc, err := b.service()
	if err != nil {
		return nil, err
	}
	gifs, err := svc.GuildGifs(guildID)
	if err != nil {
		return nil, err
	}
	out := []GifView{}
	for _, g := range gifs {
		out = append(out, GifView{
			ID: g.ID, Name: g.Name, Tags: g.Tags, Keys: g.Keys,
			Subtype: g.Subtype, Width: g.Width, Height: g.Height,
		})
	}
	return out, nil
}

// AddGuildGif adds an image to the guild's pack (a guild-management action).
func (b *Bridge) AddGuildGif(guildID, name string, tags []string, dataURL string, w, h int) (GifView, error) {
	svc, err := b.service()
	if err != nil {
		return GifView{}, err
	}
	g, err := svc.AddGuildGif(guildID, name, tags, dataURL, w, h)
	if err != nil {
		return GifView{}, err
	}
	return GifView{
		ID: g.ID, Name: g.Name, Tags: g.Tags, Keys: g.Keys,
		Subtype: g.Subtype, Width: g.Width, Height: g.Height,
	}, nil
}

// RemoveGuildGif deletes an entry from the guild's pack.
func (b *Bridge) RemoveGuildGif(guildID, id string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.RemoveGuildGif(guildID, id)
}

// SendGuildGif posts a pack GIF as an ordinary image attachment message.
func (b *Bridge) SendGuildGif(channelID, gifID, replyTo string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	_, err = svc.SendGuildGif(channelID, gifID, replyTo)
	return err
}

// GifHitView is one search result. It carries opaque handles, never a URL:
// the frontend has nothing it could fetch directly even if it wanted to, which
// is what stops a member's browser ever connecting to Google. Both the preview
// and the full image are pulled with GifSearchMedia, through the rendezvous.
type GifHitView struct {
	ID      string `json:"id"`
	Title   string `json:"title,omitempty"`
	Preview string `json:"preview"`
	Full    string `json:"full"`
	Width   int    `json:"w,omitempty"`
	Height  int    `json:"h,omitempty"`
}

// GifSearchView is a page of search results or, far more interestingly, an
// explained absence of them. Status is one of ok | unavailable | rate_limited |
// expired | upstream | bad_request (the node's answers) or no_rendezvous |
// unreachable (what the client concluded on its own). Detail is a sentence the
// picker shows verbatim — the whole point being that the tab never presents an
// empty grid without saying why.
type GifSearchView struct {
	Status  string       `json:"status"`
	Detail  string       `json:"detail,omitempty"`
	Source  string       `json:"source,omitempty"`
	Via     string       `json:"via,omitempty"`
	Results []GifHitView `json:"results"`
	Next    string       `json:"next,omitempty"`
}

func gifSearchView(r appsvc.GifSearchResult) GifSearchView {
	out := GifSearchView{
		Status: r.Status, Detail: r.Detail, Source: r.Source,
		Via: r.Via, Next: r.Next, Results: []GifHitView{},
	}
	for _, h := range r.Results {
		out.Results = append(out.Results, GifHitView{
			ID: h.ID, Title: h.Title, Preview: h.Preview, Full: h.Full,
			Width: h.Width, Height: h.Height,
		})
	}
	return out
}

// GifSearchStatus reports whether the Search tab can work, without running a
// search. The picker calls it on open so an unusable tab explains itself before
// the user types, instead of after.
func (b *Bridge) GifSearchStatus() (GifSearchView, error) {
	svc, err := b.service()
	if err != nil {
		return GifSearchView{}, err
	}
	return gifSearchView(svc.GifSearchAvailable(b.ctx)), nil
}

// SearchGifs runs one search through the user's own rendezvous. It returns no
// error for a failed search: every failure mode is a status the UI has to put
// into words, so they all ride in the view.
func (b *Bridge) SearchGifs(query, pos string) (GifSearchView, error) {
	svc, err := b.service()
	if err != nil {
		return GifSearchView{}, err
	}
	return gifSearchView(svc.SearchGifs(b.ctx, query, pos)), nil
}

// GifSearchMedia resolves a result handle to an inline data URL, fetched by the
// rendezvous. The frontend puts the returned string straight into an <img src>,
// so it issues no request of its own.
func (b *Bridge) GifSearchMedia(ref string, full bool) (string, error) {
	svc, err := b.service()
	if err != nil {
		return "", err
	}
	return svc.GifSearchMedia(b.ctx, ref, full)
}

// SendSearchedGif posts a searched GIF as an ordinary encrypted attachment.
func (b *Bridge) SendSearchedGif(channelID, ref, replyTo string, w, h int) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	_, err = svc.SendSearchedGif(b.ctx, channelID, ref, replyTo, w, h)
	return err
}

// SaveSearchedGif adds a searched GIF to the guild's own pack, so it stops
// needing the proxy at all.
func (b *Bridge) SaveSearchedGif(guildID, name string, tags []string, ref string, w, h int) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	_, err = svc.SaveSearchedGif(b.ctx, guildID, name, tags, ref, w, h)
	return err
}

// MeetingView is what StartMeeting hands the UI: the room plus its invite.
type MeetingView struct {
	Guild GuildView `json:"guild"`
	Code  string    `json:"code"`
}

// StartMeeting creates a disposable meeting room and returns it with its
// shareable invite code.
func (b *Bridge) StartMeeting() (MeetingView, error) {
	svc, err := b.service()
	if err != nil {
		return MeetingView{}, err
	}
	g, code, err := svc.StartMeeting()
	if err != nil {
		return MeetingView{}, err
	}
	return MeetingView{Guild: guildView(svc, g), Code: code}, nil
}

// CreateGuestLink issues a browser-guest URL for a meeting (no install
// needed on the other end). lifetimeHours picks how long the link lives from
// the menu in app.meetingLifetimes (1h / 24h / 7d / 30d); 0 leaves the
// meeting's lifetime as it is, which is what a caller from before link
// lifetimes existed sends.
func (b *Bridge) CreateGuestLink(guildID string, lifetimeHours int) (string, error) {
	svc, err := b.service()
	if err != nil {
		return "", err
	}
	return svc.CreateGuestLink(guildID, lifetimeHours)
}

// MeetingExpiry is when a meeting and its guest link stop working (Unix
// milliseconds, 0 if the guild is not a meeting) — so the UI that hands out the
// link can say so instead of leaving the host guessing.
func (b *Bridge) MeetingExpiry(guildID string) (int64, error) {
	svc, err := b.service()
	if err != nil {
		return 0, err
	}
	return svc.MeetingExpiry(guildID), nil
}

// NotesDM returns (creating if needed) the user's personal self-DM.
func (b *Bridge) NotesDM() (GuildView, error) {
	svc, err := b.service()
	if err != nil {
		return GuildView{}, err
	}
	g, err := svc.NotesDM()
	if err != nil {
		return GuildView{}, err
	}
	return guildView(svc, g), nil
}

// StartDM opens (creating if needed) a direct message with a member by
// fingerprint. Returns the DM conversation for the UI to navigate to.
func (b *Bridge) StartDM(fingerprint string) (GuildView, error) {
	svc, err := b.service()
	if err != nil {
		return GuildView{}, err
	}
	g, err := svc.StartDM(fingerprint)
	if err != nil {
		return GuildView{}, err
	}
	return guildView(svc, g), nil
}

// CreateGroupDM opens a group DM with the given (verified) contacts and returns
// the new conversation for the UI to navigate to.
func (b *Bridge) CreateGroupDM(fingerprints []string) (GuildView, error) {
	svc, err := b.service()
	if err != nil {
		return GuildView{}, err
	}
	g, err := svc.CreateGroupDM(fingerprints)
	if err != nil {
		return GuildView{}, err
	}
	return guildView(svc, g), nil
}

// SetRichPresence toggles now-playing rich presence (auto status).
func (b *Bridge) SetRichPresence(enabled bool) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.SetRichPresence(enabled)
}

// RichPresenceEnabled reports whether rich presence is on.
func (b *Bridge) RichPresenceEnabled() (bool, error) {
	svc, err := b.service()
	if err != nil {
		return false, nil
	}
	return svc.RichPresenceEnabled(), nil
}

// RenameDM sets (or, with an empty name, resets) a group DM's name.
func (b *Bridge) RenameDM(guildID, name string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.RenameDM(guildID, name)
}

// NewDMInvite creates a fresh DM and returns a shareable invite code (start a DM
// with someone you don't share a guild with).
func (b *Bridge) NewDMInvite() (string, error) {
	svc, err := b.service()
	if err != nil {
		return "", err
	}
	return svc.NewDMInvite()
}

// senderBlocker is the one thing visibleViews needs from the service, named as
// an interface so the filter can be tested without booting a node.
type senderBlocker interface {
	SenderBlocked(sender []byte) bool
}

// visibleViews converts a run of stored messages into views, dropping the ones
// whose sender this device has blocked.
//
// The filter lives here, at the bridge, because this is the one boundary every
// front end crosses: the desktop window, the web build and the phone all reach
// the service through these methods, so a message hidden here is hidden in the
// feed, in search, in bookmarks and in the live push, without four separate
// filters that can drift apart. Doing it in SQL would be faster and wrong —
// the store is the shared, converging record, and blocking is a local viewing
// preference that must not be able to alter it.
func visibleViews(svc senderBlocker, msgs []domain.Message) []MessageView {
	out := make([]MessageView, 0, len(msgs))
	for _, m := range msgs {
		if svc.SenderBlocked(m.Sender) {
			continue
		}
		out = append(out, messageView(m))
	}
	return out
}

func messageView(m domain.Message) MessageView {
	return MessageView{
		ID:         m.ID,
		ChannelID:  m.ChannelID,
		Sender:     identity.FingerprintOf(m.Sender),
		SenderName: m.Name,
		Kind:       m.Kind,
		ReplyTo:    m.ReplyTo,
		Content:    m.Content,
		Dir:        domain.ValidDir(m.Dir),
		Deleted:    m.Deleted,
		Expired:    m.Expired,
		Edited:     m.Edited,
		Pinned:     m.Pinned,
		Reactions:  m.Reactions,
		// Full nanosecond precision, fixed width. This string is ALSO the
		// scroll-up pagination cursor (MessagesBefore parses it back to
		// UnixNano and the store compares `sent < cursor` exactly): truncating
		// it to whole seconds silently dropped every message that was older
		// than the page boundary but inside its same wall-clock second — a
		// permanent hole at every 200-row page edge. Fixed width (not
		// .999999999) so the strings stay lexicographically ordered, which
		// the frontend's sent/readAnchor comparisons rely on.
		Sent: m.Sent.Format("2006-01-02T15:04:05.000000000Z07:00"),
	}
}

// ---- Unified dispatch (shared by the web shell and the mobile shim) ----
//
// Both the browser (/rpc) and the future mobile binding call the same method
// surface. Keeping the name→method mapping here — instead of in main_web.go —
// means one place to add a method for every transport. Explicit, not
// reflective, for clarity and safety.

// Dispatch routes a method name + JSON-encoded positional args to a bridge
// call. Used by the web shell's /rpc handler.
func (b *Bridge) Dispatch(method string, args []json.RawMessage) (any, error) {
	switch method {
	case "GetBootstrap":
		return b.GetBootstrap()
	case "SetBootstrap":
		return nil, b.SetBootstrap(argStr(args, 0))
	case "SetBootstrapLive":
		return nil, b.SetBootstrapLive(argStr(args, 0))
	case "GetPublicDHT":
		return b.GetPublicDHT()
	case "SetPublicDHT":
		return nil, b.SetPublicDHT(argBool(args, 0))
	case "GetListenPort":
		return b.GetListenPort()
	case "SetListenPort":
		return nil, b.SetListenPort(argInt(args, 0))
	case "Session":
		return b.Session(), nil
	case "AppVersion":
		return b.AppVersion(), nil
	case "SetGames":
		var games []appsvc.Game
		if len(args) > 0 {
			_ = json.Unmarshal(args[0], &games)
		}
		return nil, b.SetGames(games)
	case "SearchGames":
		return b.SearchGames(argStr(args, 0))
	case "NetworkStatus":
		return b.NetworkStatus(), nil
	case "ReachabilityStatus":
		return b.ReachabilityStatus(), nil
	case "Nudge":
		return nil, b.Nudge()
	case "SetForeground":
		return nil, b.SetForeground(argBool(args, 0))
	case "SetClientVisible":
		return nil, b.SetClientVisible(argStr(args, 0), argBool(args, 1))
	case "SetMetered":
		return nil, b.SetMetered(argBool(args, 0))
	case "RegisterPush":
		return nil, b.RegisterPush(argStr(args, 0), argStr(args, 1))
	case "LinkOffer":
		return b.LinkOffer()
	case "CancelLinkOffer":
		return nil, b.CancelLinkOffer()
	case "RedeemLinkCode":
		return b.RedeemLinkCode(argStr(args, 0), argStr(args, 1))
	case "Logout":
		return nil, b.Logout()
	case "HasIdentity":
		return b.HasIdentity()
	case "ResetIdentity":
		return nil, b.ResetIdentity()
	case "RevealMnemonic":
		return b.RevealMnemonic()
	case "RestoreFromMnemonic":
		return nil, b.RestoreFromMnemonic(argStr(args, 0), argStr(args, 1))
	case "RestoreOverExisting":
		return nil, b.RestoreOverExisting(argStr(args, 0), argStr(args, 1))
	case "Login":
		return nil, b.Login(argStr(args, 0))
	case "Identity":
		return b.Identity()
	case "Guilds":
		return b.Guilds()
	case "MarkRead":
		return nil, b.MarkRead(argStr(args, 0), argInt64(args, 1))
	case "ReadState":
		return b.ReadState()
	case "CreateGuild":
		return b.CreateGuild(argStr(args, 0))
	case "CreateGuestLink":
		return b.CreateGuestLink(argStr(args, 0), argInt(args, 1))
	case "MeetingExpiry":
		return b.MeetingExpiry(argStr(args, 0))
	case "StartMeeting":
		return b.StartMeeting()
	case "NotesDM":
		return b.NotesDM()
	case "NewDMInvite":
		return b.NewDMInvite()
	case "CreateGroupDM":
		return b.CreateGroupDM(argStrs(args, 0))
	case "RenameDM":
		return nil, b.RenameDM(argStr(args, 0), argStr(args, 1))
	case "SetRichPresence":
		return nil, b.SetRichPresence(argBool(args, 0))
	case "RichPresenceEnabled":
		return b.RichPresenceEnabled()
	case "StartDM":
		return b.StartDM(argStr(args, 0))
	case "InviteCode":
		return b.InviteCode(argStr(args, 0))
	case "JoinViaInvite":
		return b.JoinViaInvite(argStr(args, 0))
	case "Messages":
		return b.Messages(argStr(args, 0))
	case "MessagesBefore":
		return b.MessagesBefore(argStr(args, 0), argStr(args, 1), int(argInt64(args, 2)))
	case "UnreadCounts":
		var since map[string]string
		if len(args) > 0 {
			_ = json.Unmarshal(args[0], &since)
		}
		return b.UnreadCounts(since)
	case "SendMessage":
		return nil, b.SendMessage(argStr(args, 0), argStr(args, 1), argStr(args, 2), argStr(args, 3))
	case "SendCallNotice":
		return nil, b.SendCallNotice(argStr(args, 0), argStr(args, 1), argStr(args, 2))
	case "ScheduleSend":
		return b.ScheduleSend(argStr(args, 0), argStr(args, 1), argStr(args, 2), argInt64(args, 3))
	case "CancelScheduledSend":
		return nil, b.CancelScheduledSend(argStr(args, 0))
	case "ScheduledSends":
		return b.ScheduledSends()
	case "SendAttachment":
		return b.SendAttachment(argStr(args, 0), argStr(args, 1), argInt(args, 2), argInt(args, 3), argStr(args, 4),
			argBool(args, 5), argStr(args, 6), argStr(args, 7))
	case "EditAttachment":
		return b.EditAttachment(argStr(args, 0), argStr(args, 1), argStr(args, 2), argInt(args, 3), argInt(args, 4))
	case "FetchAttachment":
		return b.FetchAttachment(argStr(args, 0), argStr(args, 1), argStr(args, 2), argStr(args, 3))
	case "SendFile":
		return nil, b.SendFile(argStr(args, 0), argStr(args, 1), argStr(args, 2), argStr(args, 3))
	case "FetchFile":
		return b.FetchFile(argStr(args, 0), argStr(args, 1), argStr(args, 2), argStr(args, 3))
	case "LinkPreview":
		return b.LinkPreview(argStr(args, 0))
	case "UpdateState":
		return b.UpdateState(), nil
	case "ApplyUpdate":
		return nil, b.ApplyUpdate()
	case "CanSelfUpdate":
		return b.CanSelfUpdate(), nil
	case "RestartApp":
		return nil, b.RestartApp()
	case "CheckForUpdate":
		return b.CheckForUpdate()
	case "CheckPeerUpdate":
		return b.CheckPeerUpdate()
	case "ApplyPeerUpdate":
		return nil, b.ApplyPeerUpdate()
	case "Members":
		return b.Members(argStr(args, 0))
	case "RemoveMember":
		return nil, b.RemoveMember(argStr(args, 0), argStr(args, 1))
	case "SetNickname":
		return nil, b.SetNickname(argStr(args, 0), argStr(args, 1))
	case "CallIceServers":
		return b.CallIceServers()
	case "RevealDeleted":
		return b.RevealDeleted(argStr(args, 0), argStr(args, 1))
	case "PurgeMessages":
		return b.PurgeMessages(argStr(args, 0), argInt(args, 1))
	case "AddMember":
		return nil, b.AddMember(argStr(args, 0), argStr(args, 1))
	case "SetMemberNickname":
		return nil, b.SetMemberNickname(argStr(args, 0), argStr(args, 1), argStr(args, 2))
	case "Roles":
		return b.Roles(argStr(args, 0))
	case "UpsertRole":
		return b.UpsertRole(argStr(args, 0), argStr(args, 1), argStr(args, 2), argStr(args, 3), argInt(args, 4), argInt(args, 5))
	case "DeleteRole":
		return nil, b.DeleteRole(argStr(args, 0), argStr(args, 1))
	case "AssignRole":
		return nil, b.AssignRole(argStr(args, 0), argStr(args, 1), argStr(args, 2), argBool(args, 3))
	case "TransferOwnership":
		return nil, b.TransferOwnership(argStr(args, 0), argStr(args, 1))
	case "SetHeir":
		return nil, b.SetHeir(argStr(args, 0), argStr(args, 1))
	case "ClearHeir":
		return nil, b.ClearHeir(argStr(args, 0))
	case "ClaimOwnership":
		return nil, b.ClaimOwnership(argStr(args, 0))
	case "BanMember":
		return nil, b.BanMember(argStr(args, 0), argStr(args, 1))
	case "UnbanMember":
		return nil, b.UnbanMember(argStr(args, 0), argStr(args, 1))
	case "ExportMarkdown":
		return b.ExportMarkdown(argStr(args, 0), argStr(args, 1))
	case "ExportArchive":
		return b.ExportArchive(argStr(args, 0), argBool(args, 1))
	case "ImportArchive":
		return b.ImportArchive(argStr(args, 0), argStr(args, 1))
	case "AttachChronicle":
		return nil, b.AttachChronicle(argStr(args, 0), argStr(args, 1), argStrMap(args, 2))
	case "ChronicleInfo":
		return b.ChronicleInfo(argStr(args, 0))
	case "ChronicleMessages":
		return b.ChronicleMessages(argStr(args, 0), argStr(args, 1), argInt64(args, 2), argInt(args, 3), argBool(args, 4))
	case "SetChroniclePinned":
		return nil, b.SetChroniclePinned(argStr(args, 0), argBool(args, 1))
	case "SetRetention":
		return nil, b.SetRetention(argStr(args, 0), argStr(args, 1), argInt(args, 2))
	case "GuildRetention":
		return b.GuildRetention(argStr(args, 0))
	case "SetSlowMode":
		return nil, b.SetSlowMode(argStr(args, 0), argStr(args, 1), argInt(args, 2))
	case "MuteMember":
		return nil, b.MuteMember(argStr(args, 0), argStr(args, 1), argInt(args, 2))
	case "UnmuteMember":
		return nil, b.UnmuteMember(argStr(args, 0), argStr(args, 1))
	case "Bans":
		return b.Bans(argStr(args, 0))
	case "Contacts":
		return b.Contacts()
	case "JoinVoice":
		return nil, b.JoinVoice(argStr(args, 0))
	case "LeaveVoice":
		return nil, b.LeaveVoice(argStr(args, 0))
	case "SignalCall":
		return nil, b.SignalCall(argStr(args, 0), argStr(args, 1), argStr(args, 2), argStr(args, 3))
	case "RelaySignal":
		return nil, b.RelaySignal(argStr(args, 0), argStr(args, 1))
	case "SendTyping":
		return nil, b.SendTyping(argStr(args, 0))
	case "SetProfile":
		// birthday rides at the END of the positional list. A caller built
		// before it existed stops at style (12 args) — for it the field is
		// ABSENT, not cleared, so carry the stored value through instead of
		// letting argStr's "" erase it. Same lesson as the games wipe: silence
		// on the wire must never read as deletion. Clearing is still possible;
		// a new-arity caller passes it explicitly as "".
		birthday := argStr(args, 12)
		if len(args) <= 12 {
			if svc, err := b.service(); err == nil {
				birthday = svc.SelfProfile().Birthday
			}
		}
		return nil, b.SetProfile(argStr(args, 0), argStr(args, 1), argStr(args, 2), argStr(args, 3), argStr(args, 4), argStr(args, 5), argStr(args, 6), argStr(args, 7), argStr(args, 8), argStr(args, 9), argStr(args, 10), argStr(args, 11), birthday)
	case "VerifyFingerprint":
		return nil, b.VerifyFingerprint(argStr(args, 0))
	case "PinMessage":
		return nil, b.PinMessage(argStr(args, 0), argStr(args, 1))
	case "BookmarkMessage":
		return nil, b.BookmarkMessage(argStr(args, 0), argStr(args, 1))
	case "UnbookmarkMessage":
		return nil, b.UnbookmarkMessage(argStr(args, 0))
	case "SavedMessages":
		return b.SavedMessages()
	case "SavedMessageIDs":
		return b.SavedMessageIDs()
	case "SearchMessages":
		return b.SearchMessages(argStr(args, 0))
	case "CreateChannel":
		return b.CreateChannel(argStr(args, 0), argStr(args, 1), argStr(args, 2), argStr(args, 3))
	case "SetChannelLinks":
		return nil, b.SetChannelLinks(argStr(args, 0), argStr(args, 1), argStrs(args, 2))
	case "CreateThread":
		return b.CreateThread(argStr(args, 0), argStr(args, 1), argStr(args, 2), argStr(args, 3), argStrs(args, 4))
	case "ForumBoard":
		return b.ForumBoard(argStr(args, 0), argStr(args, 1))
	case "SetForumTags":
		return b.SetForumTags(argStr(args, 0), argStr(args, 1), argForumTags(args, 2))
	case "SetPostTags":
		return nil, b.SetPostTags(argStr(args, 0), argStr(args, 1), argStrs(args, 2))
	case "SetPostPinned":
		return nil, b.SetPostPinned(argStr(args, 0), argStr(args, 1), argBool(args, 2))
	case "SetPostSolved":
		return nil, b.SetPostSolved(argStr(args, 0), argStr(args, 1), argBool(args, 2))
	case "SetPostLocked":
		return nil, b.SetPostLocked(argStr(args, 0), argStr(args, 1), argBool(args, 2))
	case "SetForumBanner":
		return nil, b.SetForumBanner(argStr(args, 0), argStr(args, 1), argStr(args, 2))
	case "CreateEvent":
		return b.CreateEvent(argStr(args, 0), argStr(args, 1), argStr(args, 2), argInt64(args, 3), argInt64(args, 4), argStr(args, 5), argStr(args, 6))
	case "UpdateEvent":
		return b.UpdateEvent(argStr(args, 0), argStr(args, 1), argStr(args, 2), argStr(args, 3), argInt64(args, 4), argInt64(args, 5), argStr(args, 6), argStr(args, 7))
	case "DeleteEvent":
		return nil, b.DeleteEvent(argStr(args, 0), argStr(args, 1))
	case "Events":
		return b.Events(argStr(args, 0))
	case "PostStory":
		return nil, b.PostStory(argStrs(args, 0), argStr(args, 1), argStr(args, 2))
	case "GuildStories":
		return b.GuildStories(argStr(args, 0))
	case "DeleteStory":
		return nil, b.DeleteStory(argStr(args, 0))
	case "MarkStorySeen":
		return nil, b.MarkStorySeen(argStr(args, 0))
	case "RSVPEvent":
		return nil, b.RSVPEvent(argStr(args, 0), argStr(args, 1), argStr(args, 2))
	case "EventICS":
		return b.EventICS(argStr(args, 0), argStr(args, 1))
	case "EventsICS":
		return b.EventsICS(argStr(args, 0))
	case "OpenEventGuests":
		return b.OpenEventGuests(argStr(args, 0), argStr(args, 1), argBool(args, 2))
	case "RevokeEventGuests":
		return b.RevokeEventGuests(argStr(args, 0), argStr(args, 1))
	case "JoinEventRoom":
		return b.JoinEventRoom(argStr(args, 0), argStr(args, 1))
	case "BookingSettings":
		return b.BookingSettings()
	case "SetBookingConfig":
		return b.SetBookingConfig(argBookingConfig(args, 0))
	case "CancelBooking":
		return nil, b.CancelBooking(argStr(args, 0))
	case "CreateCategory":
		return nil, b.CreateCategory(argStr(args, 0), argStr(args, 1))
	case "DeleteChannel":
		return nil, b.DeleteChannel(argStr(args, 0), argStr(args, 1))
	case "DeleteCategory":
		return nil, b.DeleteCategory(argStr(args, 0), argStr(args, 1))
	case "SetGuildProfile":
		return nil, b.SetGuildProfile(argStr(args, 0), argStr(args, 1), argStr(args, 2), argStr(args, 3), argStr(args, 4))
	case "AddCustomEmoji":
		return nil, b.AddCustomEmoji(argStr(args, 0), argStr(args, 1), argStr(args, 2))
	case "RemoveCustomEmoji":
		return nil, b.RemoveCustomEmoji(argStr(args, 0), argStr(args, 1))
	case "GuildGifs":
		return b.GuildGifs(argStr(args, 0))
	case "AddGuildGif":
		return b.AddGuildGif(argStr(args, 0), argStr(args, 1), argStrs(args, 2), argStr(args, 3), argInt(args, 4), argInt(args, 5))
	case "RemoveGuildGif":
		return nil, b.RemoveGuildGif(argStr(args, 0), argStr(args, 1))
	case "SendGuildGif":
		return nil, b.SendGuildGif(argStr(args, 0), argStr(args, 1), argStr(args, 2))
	case "GifSearchStatus":
		return b.GifSearchStatus()
	case "SearchGifs":
		return b.SearchGifs(argStr(args, 0), argStr(args, 1))
	case "GifSearchMedia":
		return b.GifSearchMedia(argStr(args, 0), argBool(args, 1))
	case "SendSearchedGif":
		return nil, b.SendSearchedGif(argStr(args, 0), argStr(args, 1), argStr(args, 2), argInt(args, 3), argInt(args, 4))
	case "SaveSearchedGif":
		return nil, b.SaveSearchedGif(argStr(args, 0), argStr(args, 1), argStrs(args, 2), argStr(args, 3), argInt(args, 4), argInt(args, 5))
	case "RenameChannel":
		return nil, b.RenameChannel(argStr(args, 0), argStr(args, 1), argStr(args, 2))
	case "SetChannelMeta":
		return nil, b.SetChannelMeta(argStr(args, 0), argStr(args, 1), argStr(args, 2), argStr(args, 3), argInt(args, 4), argStr(args, 5))
	case "RenameGuild":
		return nil, b.RenameGuild(argStr(args, 0), argStr(args, 1))
	case "LeaveGuild":
		return nil, b.LeaveGuild(argStr(args, 0))
	case "DeleteMessage":
		return nil, b.DeleteMessage(argStr(args, 0), argStr(args, 1))
	case "ExpireMessage":
		return nil, b.ExpireMessage(argStr(args, 0), argStr(args, 1))
	case "CancelPendingMember":
		return nil, b.CancelPendingMember(argStr(args, 0), argStr(args, 1))
	case "EmptyTrash":
		return b.EmptyTrash(argStr(args, 0))
	case "BlockUser":
		return nil, b.BlockUser(argStr(args, 0))
	case "UnblockUser":
		return nil, b.UnblockUser(argStr(args, 0))
	case "BlockedUsers":
		return b.BlockedUsers()
	case "MessageRequests":
		return b.MessageRequests()
	case "AcceptMessageRequest":
		return b.AcceptMessageRequest(argStr(args, 0))
	case "DeclineMessageRequest":
		return nil, b.DeclineMessageRequest(argStr(args, 0), argBool(args, 1))
	case "TypingEnabled":
		return b.TypingEnabled()
	case "SetTypingEnabled":
		return nil, b.SetTypingEnabled(argBool(args, 0))
	case "GuildStats":
		return b.GuildStats(argStr(args, 0))
	case "PropsTally":
		return b.PropsTally(argStr(args, 0))
	case "NetworkStats":
		return b.NetworkStats()
	case "LinkedDevices":
		return b.LinkedDevices()
	case "UnlinkDevice":
		return nil, b.UnlinkDevice(argStr(args, 0))
	case "EditMessage":
		return nil, b.EditMessage(argStr(args, 0), argStr(args, 1), argStr(args, 2))
	case "ToggleReaction":
		return nil, b.ToggleReaction(argStr(args, 0), argStr(args, 1), argStr(args, 2))
	default:
		return nil, fmt.Errorf("unknown method %q", method)
	}
}

// DispatchJSON is the string-in/string-out form for gomobile (whose bindings
// only support a restricted type set). argsJSON is a JSON array of positional
// args; the result is a JSON object {result?, error?}. Never returns a Go
// error (mobile reads the error from the JSON), so the binding stays simple.
func (b *Bridge) DispatchJSON(method, argsJSON string) string {
	var args []json.RawMessage
	if strings.TrimSpace(argsJSON) != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return `{"error":"bad args json"}`
		}
	}
	result, err := b.Dispatch(method, args)
	out := map[string]any{}
	if err != nil {
		out["error"] = err.Error()
	} else if result != nil {
		out["result"] = result
	}
	raw, _ := json.Marshal(out)
	return string(raw)
}

func argStr(args []json.RawMessage, i int) string {
	if i >= len(args) {
		return ""
	}
	var s string
	_ = json.Unmarshal(args[i], &s)
	return s
}

// argStrMap decodes a string->string argument — the archive-page bundle, keyed
// by content address and base64-encoded because the envelope is JSON.
func argStrMap(args []json.RawMessage, i int) map[string]string {
	if i >= len(args) {
		return nil
	}
	var m map[string]string
	_ = json.Unmarshal(args[i], &m)
	return m
}

func argStrs(args []json.RawMessage, i int) []string {
	if i >= len(args) {
		return nil
	}
	var s []string
	_ = json.Unmarshal(args[i], &s)
	return s
}

// argForumTags decodes a forum tag palette. A malformed entry decodes to its
// zero value and is then refused by SetForumTags' validation, so a bad argument
// produces an error the user can read rather than a silently empty palette.
func argBookingConfig(args []json.RawMessage, i int) appsvc.BookingConfigInput {
	if i >= len(args) {
		return appsvc.BookingConfigInput{}
	}
	var c appsvc.BookingConfigInput
	_ = json.Unmarshal(args[i], &c)
	return c
}

func argForumTags(args []json.RawMessage, i int) []domain.ForumTag {
	if i >= len(args) {
		return nil
	}
	var t []domain.ForumTag
	_ = json.Unmarshal(args[i], &t)
	return t
}

func argInt(args []json.RawMessage, i int) int {
	if i >= len(args) {
		return 0
	}
	var n int
	_ = json.Unmarshal(args[i], &n)
	return n
}

func argInt64(args []json.RawMessage, i int) int64 {
	if i >= len(args) {
		return 0
	}
	var n int64
	_ = json.Unmarshal(args[i], &n)
	return n
}

func argBool(args []json.RawMessage, i int) bool {
	if i >= len(args) {
		return false
	}
	var b bool
	_ = json.Unmarshal(args[i], &b)
	return b
}
