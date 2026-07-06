package main

import (
	"bytes"
	"context"
	"errors"
	"os"
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
	From      string `json:"from"`
	ChannelID string `json:"channelId"`
	Action    string `json:"action"`
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
}

type ChannelView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type GuildView struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	IsOwner  bool          `json:"isOwner"`
	Channels []ChannelView `json:"channels"`
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
	Reactions  map[string][]string `json:"reactions"` // emoji -> fingerprints
	Sent       string              `json:"sent"`
}

type MemberView struct {
	Fingerprint string `json:"fingerprint"`
	Name        string `json:"name"`
	IsSelf      bool   `json:"isSelf"`
	Online      bool   `json:"online"`
}

type ContactView struct {
	PeerID      string `json:"peerId"`
	Fingerprint string `json:"fingerprint"`
	Verified    bool   `json:"verified"`
}

// ---- API methods ----

// Login unlocks (or creates) the identity and starts the Service, wiring live
// events into the configured sinks. A second successful call is a no-op.
func (b *bridge) Login(passphrase string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.svc != nil {
		return nil
	}
	dataDir, err := appsvc.DataDir()
	if err != nil {
		return err
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
	svc.OnVoicePresence(func(from, channelID, action string) {
		if b.onVoicePresence != nil {
			b.onVoicePresence(VoicePresence{From: from, ChannelID: channelID, Action: action})
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

// DeleteMessage deletes one of this peer's own messages.
func (b *bridge) DeleteMessage(channelID, messageID string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.DeleteMessage(channelID, messageID)
}

// CreateChannel adds a channel to a guild.
func (b *bridge) CreateChannel(guildID, name string) (ChannelView, error) {
	svc, err := b.service()
	if err != nil {
		return ChannelView{}, err
	}
	ch, err := svc.CreateChannel(guildID, name)
	if err != nil {
		return ChannelView{}, err
	}
	return ChannelView{ID: ch.ID, Name: ch.Name}, nil
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
	return IdentityInfo{
		PeerID:      svc.PeerID(),
		Fingerprint: svc.Fingerprint(),
		DisplayName: svc.DisplayName(),
	}, nil
}

// SetDisplayName updates this peer's display name.
func (b *bridge) SetDisplayName(name string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.SetDisplayName(name)
}

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

	out := make([]MemberView, 0, len(creds))
	for _, cred := range creds {
		fpr := identity.FingerprintOf(cred)
		isSelf := bytes.Equal(cred, self)
		name := svc.ProfileName(fpr)
		if isSelf {
			name = svc.DisplayName()
		}
		out = append(out, MemberView{
			Fingerprint: fpr,
			Name:        name,
			IsSelf:      isSelf,
			Online:      isSelf || online[fpr],
		})
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

func (b *bridge) Verify(peerID string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.VerifyContact(peerID)
}

// ---- mapping helpers ----

func guildView(svc *appsvc.Service, g domain.Guild) GuildView {
	channels := make([]ChannelView, 0, len(g.Channels))
	for _, c := range g.Channels {
		channels = append(channels, ChannelView{ID: c.ID, Name: c.Name})
	}
	return GuildView{ID: g.ID, Name: g.Name, IsOwner: svc.IsOwner(g.ID), Channels: channels}
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
		Reactions:  m.Reactions,
		Sent:       m.Sent.Format("2006-01-02T15:04:05Z07:00"),
	}
}
