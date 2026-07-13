package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	appsvc "github.com/zahak/concord/internal/app"
	"github.com/zahak/concord/internal/domain"
	"github.com/zahak/concord/internal/identity"
	"github.com/zahak/concord/internal/version"
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

	// Event sinks, set by whichever transport owns the bridge.
	OnMessage       func(MessageView)
	OnPresence      func()
	OnVoicePresence func(VoicePresence)
	OnVoiceSignal   func(VoiceSignal)
	OnTyping        func(TypingInfo)
	OnGuildUpdate   func()
	OnGuildInvite   func(appsvc.GuildInvite)
	OnReadState     func(ReadStateView)
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
	Action      string `json:"action"`
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
	OwnerFingerprint string         `json:"ownerFingerprint,omitempty"`
	CanManage        bool           `json:"canManage"`   // viewer may invite/kick/ban here
	MyPerms          uint32         `json:"myPerms"`     // viewer's effective permission bitmask
	Icon             string         `json:"icon"`        // guild logo (data URI)
	Banner           string         `json:"banner"`      // guild banner image (data URI)
	Description      string         `json:"description"` // guild blurb
	Channels         []ChannelView  `json:"channels"`
	Categories       []CategoryView `json:"categories"`
	Emoji            []EmojiView    `json:"emoji"`
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
	ID         string              `json:"id"`
	ChannelID  string              `json:"channelId"`
	Sender     string              `json:"sender"`     // authenticated fingerprint
	SenderName string              `json:"senderName"` // self-asserted display name
	Kind       string              `json:"kind"`       // "" normal, "system" join/create notice
	ReplyTo    string              `json:"replyTo"`    // ID of the replied-to message, or ""
	Content    string              `json:"content"`
	Deleted    bool                `json:"deleted"`
	Edited     bool                `json:"edited"`
	Pinned     bool                `json:"pinned"`
	Reactions  map[string][]string `json:"reactions"` // emoji -> fingerprints
	Sent       string              `json:"sent"`
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
	Perms       uint32           `json:"perms"`      // effective permission bitmask
	CanManage   bool             `json:"canManage"`  // owner or manage-members holder
	RoleIDs     []string         `json:"roleIds"`    // assigned role IDs (highest-first from Roles())
	MutedUntil  int64            `json:"mutedUntil"` // unix seconds muted-until (0 = not muted)
}

type ContactView struct {
	PeerID      string `json:"peerId"`
	Fingerprint string `json:"fingerprint"`
	Name        string `json:"name"` // profile display name (may be "" if unknown)
	Verified    bool   `json:"verified"`
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
	return appsvc.SaveNetConfig(dir, appsvc.NetConfig{Bootstrap: list})
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
	svc.OnVoicePresence(func(from, fingerprint, channelID, action string) {
		if b.OnVoicePresence != nil {
			b.OnVoicePresence(VoicePresence{From: from, Fingerprint: fingerprint, ChannelID: channelID, Action: action})
		}
	})
	svc.OnVoiceSignal(func(from string, data []byte) {
		if b.OnVoiceSignal != nil {
			b.OnVoiceSignal(VoiceSignal{From: from, Data: string(data)})
		}
	})
	svc.OnTyping(func(from, channelID string) {
		if b.OnTyping != nil {
			b.OnTyping(TypingInfo{From: from, Name: svc.ProfileName(from), ChannelID: channelID})
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
	b.svc = svc
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

// CreateThread opens a forum post (thread) — any member may.
func (b *Bridge) CreateThread(guildID, forumID, title, firstMessage string) (ChannelView, error) {
	svc, err := b.service()
	if err != nil {
		return ChannelView{}, err
	}
	ch, err := svc.CreateThread(guildID, forumID, title, firstMessage)
	if err != nil {
		return ChannelView{}, err
	}
	return channelView(ch), nil
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
// re-announces.
func (b *Bridge) SetProfile(name, status, emoji, color, avatar, banner, presence, bio, color2, frame, effect, styleJSON string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.SetProfile(appsvc.Profile{
		Name: name, Status: status, Emoji: emoji, Color: color, Avatar: avatar,
		Banner: banner, Presence: presence, Bio: bio,
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
	out := make([]MessageView, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, messageView(m))
	}
	return out, nil
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

func (b *Bridge) Messages(channelID string) ([]MessageView, error) {
	svc, err := b.service()
	if err != nil {
		return nil, err
	}
	msgs, err := svc.Messages(channelID, 200)
	if err != nil {
		return nil, err
	}
	out := make([]MessageView, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, messageView(m))
	}
	return out, nil
}

func (b *Bridge) SendMessage(channelID, content, replyTo string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	_, err = svc.SendMessage(channelID, content, replyTo)
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
func (b *Bridge) SendAttachment(channelID, dataURL string, w, h int, replyTo string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	_, err = svc.SendAttachment(channelID, dataURL, w, h, replyTo)
	return err
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
			IsSelf:      isSelf,
			Online:      isSelf || online[fpr],
			Verified:    isSelf || verified[fpr],
			IsOwner:     isOwner,
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
		out = append(out, ContactView{PeerID: c.PeerID, Fingerprint: c.Fingerprint, Name: svc.ProfileName(c.Fingerprint), Verified: c.Verified})
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
func (b *Bridge) RedeemLinkCode(code, passphrase string) error {
	dataDir, err := appsvc.DataDir()
	if err != nil {
		return err
	}
	b.mu.Lock()
	hasSvc := b.svc != nil
	b.mu.Unlock()
	if hasSvc {
		return errors.New("log out before linking this device to an account")
	}
	res, err := appsvc.RedeemLink(b.ctx, dataDir, code, passphrase)
	if err != nil {
		return err
	}
	// Start the service in linked mode (the marker written by RedeemLink flips it).
	if err := b.Login(passphrase); err != nil {
		return err
	}
	svc, err := b.service()
	if err != nil {
		return err
	}
	// Adopt the account's profile (name/avatar/…) so the linked device presents as
	// the same person, not a blank fingerprint. Only if the issuer had one set.
	if p := res.Profile; p.Name != "" || p.Avatar != "" || p.Emoji != "" {
		_ = svc.SetProfile(p)
	}
	// Verifications are the account's knowledge ("I compared safety numbers"),
	// not the device's — carry them over so contacts verified on the old device
	// stay verified here.
	svc.ImportVerifiedFingerprints(res.Verified)
	// Join each shared guild so the new device sees existing groups. Best-effort:
	// history also converges via sync, so a transient failure isn't fatal.
	for _, ic := range res.GuildInvites {
		_, _ = svc.JoinViaInvite(ic)
	}
	return nil
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
	return ChannelView{ID: c.ID, Name: c.Name, Type: c.ChannelType(), Category: c.Category, Position: c.Position, Topic: c.Topic, Parent: c.Parent, Links: c.Links}
}

func guildView(svc *appsvc.Service, g domain.Guild) GuildView {
	channels := make([]ChannelView, 0, len(g.Channels))
	lastActivity := svc.GuildLastActivity(g.ID)
	for _, c := range g.Channels {
		cv := channelView(c)
		if c.Parent != "" {
			cv.LastActivity = svc.ChannelLastActivity(c.ID) // forum post ordering
		}
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
// needed on the other end).
func (b *Bridge) CreateGuestLink(guildID string) (string, error) {
	svc, err := b.service()
	if err != nil {
		return "", err
	}
	return svc.CreateGuestLink(guildID)
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

func messageView(m domain.Message) MessageView {
	return MessageView{
		ID:         m.ID,
		ChannelID:  m.ChannelID,
		Sender:     identity.FingerprintOf(m.Sender),
		SenderName: m.Name,
		Kind:       m.Kind,
		ReplyTo:    m.ReplyTo,
		Content:    m.Content,
		Deleted:    m.Deleted,
		Edited:     m.Edited,
		Pinned:     m.Pinned,
		Reactions:  m.Reactions,
		Sent:       m.Sent.Format("2006-01-02T15:04:05Z07:00"),
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
	case "Nudge":
		return nil, b.Nudge()
	case "RegisterPush":
		return nil, b.RegisterPush(argStr(args, 0), argStr(args, 1))
	case "LinkOffer":
		return b.LinkOffer()
	case "CancelLinkOffer":
		return nil, b.CancelLinkOffer()
	case "RedeemLinkCode":
		return nil, b.RedeemLinkCode(argStr(args, 0), argStr(args, 1))
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
		return b.CreateGuestLink(argStr(args, 0))
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
	case "SendMessage":
		return nil, b.SendMessage(argStr(args, 0), argStr(args, 1), argStr(args, 2))
	case "SendCallNotice":
		return nil, b.SendCallNotice(argStr(args, 0), argStr(args, 1), argStr(args, 2))
	case "SendAttachment":
		return nil, b.SendAttachment(argStr(args, 0), argStr(args, 1), argInt(args, 2), argInt(args, 3), argStr(args, 4))
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
	case "BanMember":
		return nil, b.BanMember(argStr(args, 0), argStr(args, 1))
	case "UnbanMember":
		return nil, b.UnbanMember(argStr(args, 0), argStr(args, 1))
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
	case "RelaySignal":
		return nil, b.RelaySignal(argStr(args, 0), argStr(args, 1))
	case "SendTyping":
		return nil, b.SendTyping(argStr(args, 0))
	case "SetProfile":
		return nil, b.SetProfile(argStr(args, 0), argStr(args, 1), argStr(args, 2), argStr(args, 3), argStr(args, 4), argStr(args, 5), argStr(args, 6), argStr(args, 7), argStr(args, 8), argStr(args, 9), argStr(args, 10), argStr(args, 11))
	case "VerifyFingerprint":
		return nil, b.VerifyFingerprint(argStr(args, 0))
	case "PinMessage":
		return nil, b.PinMessage(argStr(args, 0), argStr(args, 1))
	case "SearchMessages":
		return b.SearchMessages(argStr(args, 0))
	case "CreateChannel":
		return b.CreateChannel(argStr(args, 0), argStr(args, 1), argStr(args, 2), argStr(args, 3))
	case "SetChannelLinks":
		return nil, b.SetChannelLinks(argStr(args, 0), argStr(args, 1), argStrs(args, 2))
	case "CreateThread":
		return b.CreateThread(argStr(args, 0), argStr(args, 1), argStr(args, 2), argStr(args, 3))
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
	case "SetChannelMeta":
		return nil, b.SetChannelMeta(argStr(args, 0), argStr(args, 1), argStr(args, 2), argStr(args, 3), argInt(args, 4), argStr(args, 5))
	case "RenameGuild":
		return nil, b.RenameGuild(argStr(args, 0), argStr(args, 1))
	case "LeaveGuild":
		return nil, b.LeaveGuild(argStr(args, 0))
	case "DeleteMessage":
		return nil, b.DeleteMessage(argStr(args, 0), argStr(args, 1))
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

func argStrs(args []json.RawMessage, i int) []string {
	if i >= len(args) {
		return nil
	}
	var s []string
	_ = json.Unmarshal(args[i], &s)
	return s
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
