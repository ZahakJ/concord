package main

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
)

// bridge is the transport-agnostic API surface the UI drives. It wraps Concord's
// Service (internal/app) and translates domain types into JSON-friendly view
// structs. Two front ends bind to it: the Wails desktop app (main_wails.go,
// which turns onMessage/onPresence into runtime events and binds bridge's
// exported methods to JavaScript) and the browser-served web app (main_web.go,
// which exposes the same methods over HTTP and streams events via SSE).
//
// Keeping this layer free of any Wails or HTTP dependency is what lets the
// identical UI run either as a native window or in a browser.
type bridge struct {
	ctx context.Context

	mu  sync.Mutex
	svc *appsvc.Service

	// Event sinks, set by whichever transport owns the bridge.
	onMessage       func(MessageView)
	onPresence      func()
	onVoicePresence func(VoicePresence)
	onVoiceSignal   func(VoiceSignal)
	onTyping        func(TypingInfo)
	onGuildUpdate   func()
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

func newBridge(ctx context.Context) *bridge { return &bridge{ctx: ctx} }

// setContext lets the Wails OnStartup hook supply the runtime context.
func (b *bridge) setContext(ctx context.Context) { b.ctx = ctx }

func (b *bridge) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.svc != nil {
		_ = b.svc.Close()
		b.svc = nil
	}
}

var errLocked = errors.New("identity is locked; log in first")

func (b *bridge) service() (*appsvc.Service, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.svc == nil {
		return nil, errLocked
	}
	return b.svc, nil
}

// ---- View types (JSON shapes the frontend consumes) ----

type IdentityInfo struct {
	PeerID      string `json:"peerId"`
	Fingerprint string `json:"fingerprint"`
	DisplayName string `json:"displayName"`
	Status      string `json:"status"`
	Emoji       string `json:"emoji"`
	Color       string `json:"color"`
	Avatar      string `json:"avatar"`
	Presence    string `json:"presence"`
	Bio         string `json:"bio"`
}

type ChannelView struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`     // "text" | "voice" | "announcement"
	Category string `json:"category"` // category ID or ""
	Position int    `json:"position"`
	Topic    string `json:"topic"`    // channel topic/description
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
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Kind       string         `json:"kind,omitempty"`   // "" guild, "dm" direct message
	DMPeer     string         `json:"dmPeer,omitempty"` // for a peer DM: the other member's fingerprint
	IsOwner    bool           `json:"isOwner"`
	CanManage  bool           `json:"canManage"` // viewer may invite/kick/ban here
	MyPerms    uint32         `json:"myPerms"`   // viewer's effective permission bitmask
	Icon        string        `json:"icon"`        // guild logo (data URI)
	Banner      string        `json:"banner"`      // guild banner image (data URI)
	Description string        `json:"description"` // guild blurb
	Channels   []ChannelView  `json:"channels"`
	Categories []CategoryView `json:"categories"`
	Emoji      []EmojiView    `json:"emoji"`
	// OutOfSync: this member is stranded at an old MLS epoch that no reachable
	// peer could bridge; new messages can't be decrypted until re-invited.
	OutOfSync bool `json:"outOfSync,omitempty"`
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
	Fingerprint string `json:"fingerprint"`
	Name        string `json:"name"`     // effective display name (nickname if set, else profile name)
	Username    string `json:"username"` // underlying profile name, surfaced when a nickname shadows it
	Status      string `json:"status"`
	Emoji       string `json:"emoji"`
	Color       string `json:"color"`
	Avatar      string `json:"avatar"`
	Presence    string `json:"presence"` // "" | online | idle | dnd | invisible
	Bio         string `json:"bio"`
	IsSelf      bool   `json:"isSelf"`
	Online      bool   `json:"online"`
	Verified    bool   `json:"verified"`
	IsOwner     bool     `json:"isOwner"`   // guild owner (implicit full authority)
	Perms       uint32   `json:"perms"`     // effective permission bitmask
	CanManage   bool     `json:"canManage"`   // owner or manage-members holder
	RoleIDs     []string `json:"roleIds"`     // assigned role IDs (highest-first from Roles())
	MutedUntil  int64    `json:"mutedUntil"`  // unix seconds muted-until (0 = not muted)
}

type ContactView struct {
	PeerID      string `json:"peerId"`
	Fingerprint string `json:"fingerprint"`
	Verified    bool   `json:"verified"`
}

// ---- Connection settings (usable before unlock) ----

// Logout closes the running Service and locks the app again (back to the login
// screen) without deleting anything.
func (b *bridge) Logout() error {
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
func (b *bridge) HasIdentity() (bool, error) {
	dir, err := appsvc.DataDir()
	if err != nil {
		return false, err
	}
	return appsvc.HasIdentity(dir), nil
}

// RevealMnemonic returns the unlocked identity's recovery phrase, for the user
// to write down. Requires an unlocked session.
func (b *bridge) RevealMnemonic() (string, error) {
	svc, err := b.service()
	if err != nil {
		return "", err
	}
	return svc.Mnemonic()
}

// RestoreFromMnemonic reconstructs the identity from a recovery phrase, sealing
// it under a new passphrase. Refused if an identity already exists here (the
// user must "start over" first) or while unlocked.
func (b *bridge) RestoreFromMnemonic(phrase, passphrase string) error {
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

// ResetIdentity deletes the identity and all data tied to it so a new identity
// can be created (forgotten passphrase / corrupted keystore). Only allowed
// while locked.
func (b *bridge) ResetIdentity() error {
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

// Session reports whether the identity is already unlocked (a Service is
// running) — lets the UI skip the login screen after a page refresh.
func (b *bridge) Session() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.svc != nil
}

// SetBootstrapLive saves rendezvous addresses and dials them now (post-login).
func (b *bridge) SetBootstrapLive(addrs string) error {
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
func (b *bridge) GetBootstrap() ([]string, error) {
	dir, err := appsvc.DataDir()
	if err != nil {
		return nil, err
	}
	return appsvc.LoadNetConfig(dir).Bootstrap, nil
}

// SetBootstrap saves rendezvous/relay addresses (newline- or comma-separated).
// Takes effect on the next unlock.
func (b *bridge) SetBootstrap(addrs string) error {
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
func (b *bridge) Login(passphrase string) error {
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
		if b.onMessage != nil {
			b.onMessage(messageView(m))
		}
	})
	presence := func(appsvc.PeerPresence) {
		if b.onPresence != nil {
			b.onPresence()
		}
	}
	svc.OnPeerConnected(presence)
	svc.OnPeerDisconnected(presence)
	svc.OnVoicePresence(func(from, fingerprint, channelID, action string) {
		if b.onVoicePresence != nil {
			b.onVoicePresence(VoicePresence{From: from, Fingerprint: fingerprint, ChannelID: channelID, Action: action})
		}
	})
	svc.OnVoiceSignal(func(from string, data []byte) {
		if b.onVoiceSignal != nil {
			b.onVoiceSignal(VoiceSignal{From: from, Data: string(data)})
		}
	})
	svc.OnTyping(func(from, channelID string) {
		if b.onTyping != nil {
			b.onTyping(TypingInfo{From: from, Name: svc.ProfileName(from), ChannelID: channelID})
		}
	})
	svc.OnGuildUpdate(func() {
		if b.onGuildUpdate != nil {
			b.onGuildUpdate()
		}
	})
	b.svc = svc
	return nil
}

// ToggleReaction adds/removes an emoji reaction on a message.
func (b *bridge) ToggleReaction(channelID, messageID, emoji string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.ToggleReaction(channelID, messageID, emoji)
}

// EditMessage edits one of this peer's own messages.
func (b *bridge) EditMessage(channelID, messageID, newContent string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.EditMessage(channelID, messageID, newContent)
}

// DeleteMessage deletes one of this peer's own messages.
func (b *bridge) DeleteMessage(channelID, messageID string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.DeleteMessage(channelID, messageID)
}

// LeaveGuild removes a guild from this peer (local delete).
func (b *bridge) LeaveGuild(guildID string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.LeaveGuild(guildID)
}

// RenameGuild renames a guild (owner only).
func (b *bridge) RenameGuild(guildID, name string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.RenameGuild(guildID, name)
}

// CreateChannel adds a channel to a guild. ctype is ""/"text"/"voice"/
// "announcement"; category is a category ID or "".
func (b *bridge) CreateChannel(guildID, name, ctype, category string) (ChannelView, error) {
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

// CreateCategory adds a sidebar category to a guild.
func (b *bridge) CreateCategory(guildID, name string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	_, err = svc.CreateCategory(guildID, name)
	return err
}

// DeleteChannel removes a channel (ManageChannels).
func (b *bridge) DeleteChannel(guildID, channelID string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.DeleteChannel(guildID, channelID)
}

// DeleteCategory removes a category, un-categorizing its channels (ManageChannels).
func (b *bridge) DeleteCategory(guildID, categoryID string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.DeleteCategory(guildID, categoryID)
}

// SetGuildProfile updates the guild's name/icon/banner/description (ManageGuild).
func (b *bridge) SetGuildProfile(guildID, name, icon, banner, description string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.SetGuildProfile(guildID, name, icon, banner, description)
}

// SetChannelMeta changes a channel's type/category/position/topic.
func (b *bridge) SetChannelMeta(guildID, channelID, ctype, category string, position int, topic string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.SetChannelMeta(guildID, channelID, ctype, category, position, topic)
}

// SendTyping broadcasts an ephemeral typing hint for a channel.
func (b *bridge) SendTyping(channelID string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.SendTyping(channelID)
}

// JoinVoice enters a channel's voice room.
func (b *bridge) JoinVoice(channelID string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.JoinVoice(channelID)
}

// LeaveVoice leaves a channel's voice room.
func (b *bridge) LeaveVoice(channelID string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.LeaveVoice(channelID)
}

// RelaySignal forwards a WebRTC signaling blob to a peer.
func (b *bridge) RelaySignal(toPeerID, data string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.RelaySignal(toPeerID, []byte(data))
}

func (b *bridge) Identity() (IdentityInfo, error) {
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
		Presence:    p.Presence,
		Bio:         p.Bio,
	}, nil
}

// SetProfile updates this peer's profile (incl. avatar image) and re-announces.
func (b *bridge) SetProfile(name, status, emoji, color, avatar, presence, bio string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.SetProfile(appsvc.Profile{
		Name: name, Status: status, Emoji: emoji, Color: color, Avatar: avatar,
		Presence: presence, Bio: bio,
	})
}

// VerifyFingerprint marks a member's identity as verified after an out-of-band
// fingerprint comparison.
func (b *bridge) VerifyFingerprint(fingerprint string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.VerifyFingerprint(fingerprint)
}

// PinMessage toggles a message's pinned state for everyone.
func (b *bridge) PinMessage(channelID, messageID string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.PinMessage(channelID, messageID)
}

// SearchMessages searches this peer's full local history.
func (b *bridge) SearchMessages(query string) ([]MessageView, error) {
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
func (b *bridge) Guilds() ([]GuildView, error) {
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

func (b *bridge) CreateGuild(name string) (GuildView, error) {
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

func (b *bridge) InviteCode(guildID string) (string, error) {
	svc, err := b.service()
	if err != nil {
		return "", err
	}
	return svc.InviteCode(guildID)
}

func (b *bridge) JoinViaInvite(code string) (GuildView, error) {
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

func (b *bridge) Messages(channelID string) ([]MessageView, error) {
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

func (b *bridge) SendMessage(channelID, content, replyTo string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	_, err = svc.SendMessage(channelID, content, replyTo)
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
func (b *bridge) LinkPreview(url string) (PreviewView, error) {
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
func (b *bridge) SendAttachment(channelID, dataURL string, w, h int, replyTo string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	_, err = svc.SendAttachment(channelID, dataURL, w, h, replyTo)
	return err
}

// FetchAttachment resolves an attachment token to a plaintext image data URL,
// fetching the blob from guild members if it isn't cached locally.
func (b *bridge) FetchAttachment(channelID, blobID, keys, subtype string) (string, error) {
	svc, err := b.service()
	if err != nil {
		return "", err
	}
	return svc.FetchAttachment(channelID, blobID, keys, subtype)
}

// SendFile seals an arbitrary file into an encrypted blob and posts a file
// reference token (rendered as a download card, not inline).
func (b *bridge) SendFile(channelID, dataURL, filename, replyTo string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	_, err = svc.SendFile(channelID, dataURL, filename, replyTo)
	return err
}

// FetchFile resolves a file token to a plaintext data URL of the given mime,
// fetching the blob from guild members if not cached locally.
func (b *bridge) FetchFile(channelID, blobID, keys, mime string) (string, error) {
	svc, err := b.service()
	if err != nil {
		return "", err
	}
	return svc.FetchFile(channelID, blobID, keys, mime)
}

func (b *bridge) Members(guildID string) ([]MemberView, error) {
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
	for _, cred := range creds {
		fpr := identity.FingerprintOf(cred)
		isSelf := bytes.Equal(cred, self)
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
func (b *bridge) SetNickname(guildID, nick string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.SetNickname(guildID, nick)
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
func (b *bridge) Roles(guildID string) ([]RoleView, error) {
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
func (b *bridge) UpsertRole(guildID, roleID, name, color string, perms, position int) (string, error) {
	svc, err := b.service()
	if err != nil {
		return "", err
	}
	return svc.UpsertRole(guildID, roleID, name, color, appsvc.Permission(perms), position)
}

// DeleteRole removes a role.
func (b *bridge) DeleteRole(guildID, roleID string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.DeleteRole(guildID, roleID)
}

// AssignRole grants (add=true) or revokes a role from a member.
func (b *bridge) AssignRole(guildID, fingerprint, roleID string, add bool) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.AssignRole(guildID, fingerprint, roleID, add)
}

// BanMember bars a fingerprint and evicts them if present (manage-members).
func (b *bridge) BanMember(guildID, fingerprint string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.BanMember(guildID, fingerprint)
}

// UnbanMember lifts a ban (manage-members).
func (b *bridge) UnbanMember(guildID, fingerprint string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.UnbanMember(guildID, fingerprint)
}

// MuteMember times a member out for `minutes` (mute-members).
func (b *bridge) MuteMember(guildID, fingerprint string, minutes int) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.MuteMember(guildID, fingerprint, minutes)
}

// UnmuteMember lifts a mute (mute-members).
func (b *bridge) UnmuteMember(guildID, fingerprint string) error {
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
func (b *bridge) Bans(guildID string) ([]BanView, error) {
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

func (b *bridge) RemoveMember(guildID, fingerprint string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	creds, err := svc.GuildMembers(guildID)
	if err != nil {
		return err
	}
	for _, cred := range creds {
		if identity.FingerprintOf(cred) == fingerprint {
			return svc.RemoveMember(guildID, cred)
		}
	}
	return errors.New("member not found")
}

func (b *bridge) Contacts() ([]ContactView, error) {
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
		out = append(out, ContactView{PeerID: c.PeerID, Fingerprint: c.Fingerprint, Verified: c.Verified})
	}
	return out, nil
}

// ---- mapping helpers ----

func channelView(c domain.Channel) ChannelView {
	return ChannelView{ID: c.ID, Name: c.Name, Type: c.ChannelType(), Category: c.Category, Position: c.Position, Topic: c.Topic}
}

func guildView(svc *appsvc.Service, g domain.Guild) GuildView {
	channels := make([]ChannelView, 0, len(g.Channels))
	for _, c := range g.Channels {
		channels = append(channels, channelView(c))
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
	dmPeer := ""
	if g.Kind == "dm" {
		if creds, err := svc.GuildMembers(g.ID); err == nil {
			self := svc.PublicKey()
			for _, c := range creds {
				if !bytes.Equal(c, self) {
					fpr := identity.FingerprintOf(c)
					dmPeer = fpr
					if n := svc.ProfileName(fpr); n != "" {
						name = n
					}
					break
				}
			}
		}
	}
	return GuildView{
		ID: g.ID, Name: name, Kind: g.Kind, DMPeer: dmPeer, IsOwner: svc.IsOwner(g.ID),
		CanManage:   svc.CanManageMembers(g.ID),
		MyPerms:     uint32(svc.MemberPermission(g.ID, svc.Fingerprint())),
		Icon:        g.Icon, Banner: g.Banner, Description: g.Description,
		Channels: channels, Categories: cats, Emoji: emoji, OutOfSync: svc.OutOfSync(g.ID),
	}
}

// AddCustomEmoji uploads a guild custom emoji (:name: → image).
func (b *bridge) AddCustomEmoji(guildID, name, dataURI string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.AddCustomEmoji(guildID, name, dataURI)
}

// RemoveCustomEmoji deletes a guild custom emoji.
func (b *bridge) RemoveCustomEmoji(guildID, name string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.RemoveCustomEmoji(guildID, name)
}

// NotesDM returns (creating if needed) the user's personal self-DM.
func (b *bridge) NotesDM() (GuildView, error) {
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
func (b *bridge) StartDM(fingerprint string) (GuildView, error) {
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

// NewDMInvite creates a fresh DM and returns a shareable invite code (start a DM
// with someone you don't share a guild with).
func (b *bridge) NewDMInvite() (string, error) {
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
func (b *bridge) Dispatch(method string, args []json.RawMessage) (any, error) {
	switch method {
	case "GetBootstrap":
		return b.GetBootstrap()
	case "SetBootstrap":
		return nil, b.SetBootstrap(argStr(args, 0))
	case "SetBootstrapLive":
		return nil, b.SetBootstrapLive(argStr(args, 0))
	case "Session":
		return b.Session(), nil
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
	case "Login":
		return nil, b.Login(argStr(args, 0))
	case "Identity":
		return b.Identity()
	case "Guilds":
		return b.Guilds()
	case "CreateGuild":
		return b.CreateGuild(argStr(args, 0))
	case "NotesDM":
		return b.NotesDM()
	case "NewDMInvite":
		return b.NewDMInvite()
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
	case "CheckForUpdate":
		return b.CheckForUpdate()
	case "Members":
		return b.Members(argStr(args, 0))
	case "RemoveMember":
		return nil, b.RemoveMember(argStr(args, 0), argStr(args, 1))
	case "SetNickname":
		return nil, b.SetNickname(argStr(args, 0), argStr(args, 1))
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
		return nil, b.SetProfile(argStr(args, 0), argStr(args, 1), argStr(args, 2), argStr(args, 3), argStr(args, 4), argStr(args, 5), argStr(args, 6))
	case "VerifyFingerprint":
		return nil, b.VerifyFingerprint(argStr(args, 0))
	case "PinMessage":
		return nil, b.PinMessage(argStr(args, 0), argStr(args, 1))
	case "SearchMessages":
		return b.SearchMessages(argStr(args, 0))
	case "CreateChannel":
		return b.CreateChannel(argStr(args, 0), argStr(args, 1), argStr(args, 2), argStr(args, 3))
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
func (b *bridge) DispatchJSON(method, argsJSON string) string {
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

func argInt(args []json.RawMessage, i int) int {
	if i >= len(args) {
		return 0
	}
	var n int
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
