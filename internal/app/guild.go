package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"

	"github.com/zahak/concord/internal/domain"
	"github.com/zahak/concord/internal/identity"
)

// This file implements the guild lifecycle: creating a guild, generating and
// redeeming invites, sending messages, and the receive loops that turn inbound
// gossip into stored, decrypted messages.
//
// Membership model (MVP): the guild owner is the sole committer. Only the owner
// issues invites, which serializes MLS commits and sidesteps concurrent-commit
// conflict resolution. Any-member invites and staged-commit conflict handling
// are a later refinement.

// inviteCode is the out-of-band string a guild owner shares. It carries
// everything a joiner needs — the guild, the owner's dialable addresses, and
// the owner's rendezvous/bootstrap nodes — so pasting one code fully
// configures a fresh install (no separate "server address" step).
type inviteCode struct {
	GuildID   string   `json:"g"`
	GuildName string   `json:"n"`
	OwnerID   string   `json:"p"`           // owner libp2p peer ID
	OwnerAddr []string `json:"a"`           // owner dialable multiaddrs
	Bootstrap []string `json:"b,omitempty"` // rendezvous/relay multiaddrs
}

// inviteRequest is sent by a joiner over the invite stream.
type inviteRequest struct {
	GuildID    string `json:"guildId"`
	KeyPackage []byte `json:"keyPackage"`
	Credential []byte `json:"credential"` // joiner's account key, for retry recovery
}

// inviteResponse is returned by the owner over the invite stream.
type inviteResponse struct {
	Welcome []byte       `json:"welcome"`
	Guild   domain.Guild `json:"guild"`
	Error   string       `json:"error,omitempty"`
}

// CreateGuild creates a new guild (a fresh MLS group) owned by this peer,
// persists it, and subscribes to its topics.
func (s *Service) CreateGuild(name string) (domain.Guild, error) {
	gid, err := s.mls.CreateGroup(s.ctx)
	if err != nil {
		return domain.Guild{}, fmt.Errorf("app: create group: %w", err)
	}
	g := domain.NewGuild(name, gid, s.PublicKey())
	if err := s.store.SaveGuild(g); err != nil {
		return domain.Guild{}, err
	}
	s.trackGuild(&g)
	return g, nil
}

// Guilds returns the guilds this peer belongs to.
func (s *Service) Guilds() []domain.Guild {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.Guild, 0, len(s.guilds))
	for _, g := range s.guilds {
		out = append(out, *g)
	}
	return out
}

// Messages returns stored history for a channel (oldest first).
func (s *Service) Messages(channelID string, limit int) ([]domain.Message, error) {
	return s.store.Messages(channelID, limit)
}

// MemberCount returns how many members this peer currently sees in a guild's
// MLS group. It reflects the local MLS epoch, so it doubles as a readiness
// signal: once every peer reports the same count, they share an epoch and can
// exchange messages.
func (s *Service) MemberCount(guildID string) (int, error) {
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	s.mu.RUnlock()
	if !ok {
		return 0, fmt.Errorf("app: unknown guild %s", guildID)
	}
	members, err := s.mls.Members(s.ctx, g.GroupID)
	if err != nil {
		return 0, err
	}
	return len(members), nil
}

// RenameGuild changes a guild's name (owner only) and syncs it to all members.
func (s *Service) RenameGuild(guildID, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("app: guild name is empty")
	}
	s.mu.Lock()
	g, ok := s.guilds[guildID]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("app: unknown guild %s", guildID)
	}
	if !bytes.Equal(g.OwnerID, s.PublicKey()) {
		s.mu.Unlock()
		return fmt.Errorf("app: only the guild owner can rename it")
	}
	g.Name = name
	groupID := g.GroupID
	guildCopy := *g
	s.mu.Unlock()

	_ = s.store.SaveGuild(guildCopy)
	s.emitGuildUpdate()

	payload, _ := json.Marshal(guildMeta{Type: "guild_renamed", Name: name})
	if ct, err := s.mls.Encrypt(s.ctx, groupID, payload); err == nil {
		_ = s.ps.Publish(s.ctx, domain.GuildMetaTopicID(groupID), ct)
	}
	return nil
}

// LeaveGuild removes a guild from this peer locally: it stops tracking it and
// deletes its stored data. (A local action — other members keep the guild.)
func (s *Service) LeaveGuild(guildID string) error {
	s.mu.Lock()
	g, ok := s.guilds[guildID]
	if ok {
		for _, c := range g.Channels {
			delete(s.channelToGuild, c.ID)
		}
		delete(s.guilds, guildID)
	}
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("app: unknown guild %s", guildID)
	}
	_ = s.store.DeleteGuild(guildID)
	s.emitGuildUpdate()
	return nil
}

// GuildMembers returns the account public keys of a guild's current members.
func (s *Service) GuildMembers(guildID string) ([][]byte, error) {
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("app: unknown guild %s", guildID)
	}
	return s.mls.Members(s.ctx, g.GroupID)
}

// IsOwner reports whether this peer owns the guild.
func (s *Service) IsOwner(guildID string) bool {
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	s.mu.RUnlock()
	return ok && bytes.Equal(g.OwnerID, s.PublicKey())
}

// RemoveMember evicts a member from a guild. Only the owner may do so (the
// owner is the sole committer in the MVP membership model). The resulting MLS
// commit is published to the control topic so remaining members re-key.
func (s *Service) RemoveMember(guildID string, memberCredential []byte) error {
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("app: unknown guild %s", guildID)
	}
	if !bytes.Equal(g.OwnerID, s.PublicKey()) {
		return fmt.Errorf("app: only the guild owner can remove members")
	}
	if bytes.Equal(memberCredential, s.PublicKey()) {
		return fmt.Errorf("app: owner cannot remove themselves")
	}
	commit, err := s.mls.Remove(s.ctx, g.GroupID, memberCredential)
	if err != nil {
		return err
	}
	return s.ps.Publish(s.ctx, domain.ControlTopicID(g.GroupID), commit)
}

// InviteCode returns a shareable invite string for a guild this peer owns.
func (s *Service) InviteCode(guildID string) (string, error) {
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	s.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("app: unknown guild %s", guildID)
	}

	ai := s.host.AddrInfo()
	addrs := make([]string, 0, len(ai.Addrs))
	for _, a := range ai.Addrs {
		addrs = append(addrs, a.String())
	}
	// Add relay "circuit" addresses derived from our rendezvous nodes, so a
	// joiner behind a different NAT can reach us THROUGH the relay even when our
	// own (private/LAN) addresses are undialable. Format:
	//   /dns/host/tcp/4001/p2p/<relayID>/p2p-circuit
	bootstrap := LoadNetConfig(s.dataDir).Bootstrap
	for _, b := range bootstrap {
		if b = strings.TrimSpace(b); b != "" {
			addrs = append(addrs, b+"/p2p-circuit")
		}
	}
	code := inviteCode{
		GuildID:   g.ID,
		GuildName: g.Name,
		OwnerID:   ai.ID.String(),
		OwnerAddr: addrs,
		// Embed our rendezvous nodes so the joiner is configured by the code
		// alone — one paste connects them to the same network.
		Bootstrap: bootstrap,
	}
	raw, err := json.Marshal(code)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// JoinViaInvite redeems an invite code: it contacts the owner, exchanges an MLS
// KeyPackage for a Welcome, joins the group, and subscribes to guild topics.
func (s *Service) JoinViaInvite(code string) (domain.Guild, error) {
	raw, err := base64.RawURLEncoding.DecodeString(code)
	if err != nil {
		return domain.Guild{}, fmt.Errorf("app: bad invite code: %w", err)
	}
	var ic inviteCode
	if err := json.Unmarshal(raw, &ic); err != nil {
		return domain.Guild{}, fmt.Errorf("app: bad invite code: %w", err)
	}

	// Adopt any rendezvous nodes carried by the invite: persist them for future
	// restarts (DHT bootstrap) and connect to them right now so relayed dials
	// to a NAT'd owner can resolve during this session.
	if len(ic.Bootstrap) > 0 {
		s.adoptBootstrap(ic.Bootstrap)
	}

	owner, err := ownerAddrInfo(ic)
	if err != nil {
		return domain.Guild{}, err
	}

	kp, err := s.mls.KeyPackage(s.ctx)
	if err != nil {
		return domain.Guild{}, fmt.Errorf("app: build key package: %w", err)
	}
	reqBytes, _ := json.Marshal(inviteRequest{GuildID: ic.GuildID, KeyPackage: kp, Credential: s.PublicKey()})

	// Generous timeout: a relayed/hole-punched dial to a NAT'd owner can take a
	// few seconds to establish.
	dialCtx, cancel := context.WithTimeout(s.ctx, 40*time.Second)
	defer cancel()
	respBytes, err := s.host.RequestInvite(dialCtx, owner, reqBytes)
	if err != nil {
		return domain.Guild{}, err
	}
	var resp inviteResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return domain.Guild{}, fmt.Errorf("app: bad invite response: %w", err)
	}
	if resp.Error != "" {
		return domain.Guild{}, fmt.Errorf("app: invite rejected: %s", resp.Error)
	}

	if _, err := s.mls.Join(s.ctx, resp.Welcome); err != nil {
		return domain.Guild{}, fmt.Errorf("app: join group: %w", err)
	}
	if err := s.store.SaveGuild(resp.Guild); err != nil {
		return domain.Guild{}, err
	}
	g := resp.Guild
	s.trackGuild(&g)
	// Keep the owner connection alive so presence and gossipsub keep flowing.
	s.host.Protect(owner.ID)
	// Tell existing members our display name (and learn theirs in reply).
	s.announceProfile(g.ID)
	// Announce arrival with a system message in the default channel.
	if len(g.Channels) > 0 {
		s.sendSystem(g.Channels[0].ID, "joined the server")
	}
	return g, nil
}

// handleInviteRequest is the owner side of the join handshake: it adds the
// requester to the MLS group, publishes the resulting commit to existing
// members via the control topic, and returns the Welcome plus guild metadata.
func (s *Service) handleInviteRequest(ctx context.Context, from peer.ID, request []byte) ([]byte, error) {
	var req inviteRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return nil, err
	}

	s.mu.RLock()
	g, ok := s.guilds[req.GuildID]
	s.mu.RUnlock()
	if !ok {
		return json.Marshal(inviteResponse{Error: "unknown guild"})
	}

	commit, welcome, err := s.mls.Invite(ctx, g.GroupID, req.KeyPackage)
	if err != nil {
		// Most common cause: this is a RETRY where the joiner is already in the
		// group (a previous attempt added them but they never finished joining,
		// e.g. the response was lost). Remove the stale entry and re-invite so
		// they get a fresh Welcome at the current epoch.
		if len(req.Credential) > 0 {
			if rmCommit, rmErr := s.mls.Remove(ctx, g.GroupID, req.Credential); rmErr == nil {
				_ = s.ps.Publish(ctx, domain.ControlTopicID(g.GroupID), rmCommit)
				commit, welcome, err = s.mls.Invite(ctx, g.GroupID, req.KeyPackage)
			}
		}
		if err != nil {
			return json.Marshal(inviteResponse{Error: "invite failed"})
		}
	}
	// Advance existing members (if any) to the new epoch.
	if err := s.ps.Publish(ctx, domain.ControlTopicID(g.GroupID), commit); err != nil {
		return nil, err
	}
	// Keep this member reachable (esp. over a relay) and refresh the roster.
	s.host.Protect(from)
	s.emitGuildUpdate()
	return json.Marshal(inviteResponse{Welcome: welcome, Guild: *g})
}

// SendMessage encrypts and publishes a normal chat message to a channel.
// replyTo is the ID of a message being replied to, or "".
func (s *Service) SendMessage(channelID, content, replyTo string) (domain.Message, error) {
	return s.send(channelID, content, "", replyTo)
}

// sendSystem posts a system notice (join/create) to a channel. Errors are
// swallowed since these are best-effort UI sugar.
func (s *Service) sendSystem(channelID, content string) {
	_, _ = s.send(channelID, content, "system", "")
}

func (s *Service) send(channelID, content, kind, replyTo string) (domain.Message, error) {
	s.mu.RLock()
	guildID, ok := s.channelToGuild[channelID]
	var groupID []byte
	if ok {
		groupID = s.guilds[guildID].GroupID
	}
	s.mu.RUnlock()
	if !ok {
		return domain.Message{}, fmt.Errorf("app: unknown channel %s", channelID)
	}

	msg, err := domain.NewMessage(channelID, s.PublicKey(), content)
	if err != nil {
		return domain.Message{}, err
	}
	msg.Name = s.DisplayName()
	msg.Kind = kind
	msg.ReplyTo = replyTo
	payload, _ := json.Marshal(msg)
	ct, err := s.mls.Encrypt(s.ctx, groupID, payload)
	if err != nil {
		return domain.Message{}, fmt.Errorf("app: encrypt: %w", err)
	}
	if err := s.ps.Publish(s.ctx, domain.TopicID(groupID, channelID), ct); err != nil {
		return domain.Message{}, err
	}
	switch msg.Kind {
	case "delete":
		s.applyDelete(msg.ReplyTo, msg.Sender)
		return msg, nil
	case "reaction":
		s.applyReaction(msg.ReplyTo, msg.Content, msg.Sender)
		return msg, nil
	case "edit":
		s.applyEdit(msg.ReplyTo, msg.Content, msg.Sender)
		return msg, nil
	case "pin":
		s.applyPin(msg.ReplyTo)
		return msg, nil
	}
	if _, err := s.store.SaveMessage(msg); err != nil {
		return domain.Message{}, err
	}
	s.emitMessage(msg)
	return msg, nil
}

// ToggleReaction adds/removes an emoji reaction on a message.
func (s *Service) ToggleReaction(channelID, targetID, emoji string) error {
	_, err := s.send(channelID, emoji, "reaction", targetID)
	return err
}

// applyReaction toggles a reaction in the store and re-emits the target message
// (now carrying the updated reactions) so the UI refreshes.
func (s *Service) applyReaction(targetID, emoji string, bySender []byte) {
	if targetID == "" || emoji == "" {
		return
	}
	fpr := identity.FingerprintOf(bySender)
	if _, err := s.store.ToggleReaction(targetID, fpr, emoji); err != nil {
		return
	}
	if m, ok, err := s.store.MessageByID(targetID); err == nil && ok {
		s.emitMessage(m)
	}
}

// EditMessage edits one of this peer's own messages for everyone.
func (s *Service) EditMessage(channelID, targetID, newContent string) error {
	_, err := s.send(channelID, newContent, "edit", targetID)
	return err
}

// applyEdit updates a target message's content (if bySender authored it) and
// re-emits it so the UI refreshes.
func (s *Service) applyEdit(targetID, newContent string, bySender []byte) {
	if targetID == "" || newContent == "" {
		return
	}
	ok, err := s.store.UpdateContent(targetID, bySender, newContent)
	if err != nil || !ok {
		return
	}
	if m, found, err := s.store.MessageByID(targetID); err == nil && found {
		s.emitMessage(m)
	}
}

// PinMessage toggles a message's pinned state for everyone in the guild.
func (s *Service) PinMessage(channelID, targetID string) error {
	_, err := s.send(channelID, "pin", "pin", targetID)
	return err
}

// applyPin toggles the pin locally and re-emits the message so UIs refresh.
func (s *Service) applyPin(targetID string) {
	if targetID == "" {
		return
	}
	if _, err := s.store.TogglePinned(targetID); err != nil {
		return
	}
	if m, ok, err := s.store.MessageByID(targetID); err == nil && ok {
		s.emitMessage(m)
	}
}

// SearchMessages searches this peer's full local history (all guilds/channels).
func (s *Service) SearchMessages(query string, limit int) ([]domain.Message, error) {
	return s.store.SearchMessages(query, limit)
}

// DeleteMessage removes one of this peer's own messages for everyone.
func (s *Service) DeleteMessage(channelID, targetID string) error {
	_, err := s.send(channelID, "deleted", "delete", targetID)
	return err
}

// applyDelete tombstones a target message (if bySender authored it) and pushes
// the update to the UI.
func (s *Service) applyDelete(targetID string, bySender []byte) {
	deleted, ok, err := s.store.MarkDeleted(targetID, bySender)
	if err != nil || !ok {
		return
	}
	s.emitMessage(deleted)
}

// trackGuild records a guild in memory and subscribes to its control and
// channel topics so inbound commits and messages are processed.
func (s *Service) trackGuild(g *domain.Guild) {
	s.mu.Lock()
	s.guilds[g.ID] = g
	for _, c := range g.Channels {
		s.channelToGuild[c.ID] = g.ID
	}
	s.mu.Unlock()

	groupID := g.GroupID
	guildID := g.ID

	// Control topic: apply commits from the owner as membership changes, then
	// refresh the UI so new/removed members show up live.
	_ = s.ps.Subscribe(s.ctx, domain.ControlTopicID(groupID), func(_ peer.ID, data []byte) {
		if err := s.mls.ApplyCommit(s.ctx, groupID, data); err == nil {
			s.emitGuildUpdate()
		}
	})

	// Guild-meta topic: MLS-encrypted metadata updates (e.g. new channels).
	_ = s.ps.Subscribe(s.ctx, domain.GuildMetaTopicID(groupID), func(_ peer.ID, ct []byte) {
		s.receiveGuildMeta(guildID, groupID, ct)
	})

	// Channel topics: decrypt, store, and surface inbound messages.
	for _, c := range g.Channels {
		channelID := c.ID
		_ = s.ps.Subscribe(s.ctx, domain.TopicID(groupID, channelID), func(_ peer.ID, ct []byte) {
			s.receiveCiphertext(groupID, ct)
		})
		// Ephemeral typing signals (surfaced by sender fingerprint).
		_ = s.ps.Subscribe(s.ctx, domain.TypingTopicID(groupID, channelID), func(from peer.ID, _ []byte) {
			s.emitTyping(presenceFor(from).Fingerprint, channelID)
		})
	}
}

// guildMeta is an MLS-encrypted guild metadata update sent over the guild-meta
// topic so all members converge on shared state (channels, member display
// names). Only the fields relevant to Type are populated.
type guildMeta struct {
	Type        string         `json:"type"` // "channel_added" | "profile" | "guild_renamed"
	Channel     domain.Channel `json:"channel,omitempty"`
	Fingerprint string         `json:"fingerprint,omitempty"`
	Name        string         `json:"name,omitempty"`
	Status      string         `json:"status,omitempty"`
	Emoji       string         `json:"emoji,omitempty"`
	Color       string         `json:"color,omitempty"`
	Avatar      string         `json:"avatar,omitempty"`
}

// announceProfileAll broadcasts this peer's display name to every guild it is in.
func (s *Service) announceProfileAll() {
	s.mu.RLock()
	ids := make([]string, 0, len(s.guilds))
	for id := range s.guilds {
		ids = append(ids, id)
	}
	s.mu.RUnlock()
	for _, id := range ids {
		s.announceProfile(id)
	}
}

// announceProfile publishes this peer's fingerprint→name mapping to one guild.
func (s *Service) announceProfile(guildID string) {
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var groupID []byte
	if ok {
		groupID = g.GroupID
	}
	s.mu.RUnlock()
	if !ok {
		return
	}
	p := s.SelfProfile()
	meta := guildMeta{
		Type: "profile", Fingerprint: s.id.Fingerprint(),
		Name: p.Name, Status: p.Status, Emoji: p.Emoji, Color: p.Color, Avatar: p.Avatar,
	}
	payload, _ := json.Marshal(meta)
	ct, err := s.mls.Encrypt(s.ctx, groupID, payload)
	if err != nil {
		return
	}
	_ = s.ps.Publish(s.ctx, domain.GuildMetaTopicID(groupID), ct)
}

// CreateChannel adds a channel to a guild and announces it (MLS-encrypted) to
// the other members so they add it too.
func (s *Service) CreateChannel(guildID, name string) (domain.Channel, error) {
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var groupID []byte
	if ok {
		groupID = g.GroupID
	}
	s.mu.RUnlock()
	if !ok {
		return domain.Channel{}, fmt.Errorf("app: unknown guild %s", guildID)
	}
	if strings.TrimSpace(name) == "" {
		return domain.Channel{}, fmt.Errorf("app: channel name is empty")
	}

	ch := domain.Channel{ID: domain.NewID(), GuildID: guildID, Name: strings.TrimSpace(name)}
	s.addChannel(guildID, ch)

	// Announce to members (encrypted so non-members never learn the channel).
	payload, _ := json.Marshal(guildMeta{Type: "channel_added", Channel: ch})
	ct, err := s.mls.Encrypt(s.ctx, groupID, payload)
	if err != nil {
		return domain.Channel{}, fmt.Errorf("app: encrypt guild meta: %w", err)
	}
	if err := s.ps.Publish(s.ctx, domain.GuildMetaTopicID(groupID), ct); err != nil {
		return domain.Channel{}, err
	}
	// Note the creation in the new channel itself.
	s.sendSystem(ch.ID, "created this channel")
	return ch, nil
}

// addChannel records a channel in memory, persists it, subscribes to its topics,
// and notifies the UI. Safe to call for both locally-created and remotely-
// announced channels (idempotent on channel ID).
func (s *Service) addChannel(guildID string, ch domain.Channel) {
	s.mu.Lock()
	g, ok := s.guilds[guildID]
	if !ok {
		s.mu.Unlock()
		return
	}
	for _, existing := range g.Channels {
		if existing.ID == ch.ID {
			s.mu.Unlock()
			return // already known
		}
	}
	g.Channels = append(g.Channels, ch)
	s.channelToGuild[ch.ID] = guildID
	groupID := g.GroupID
	guildCopy := *g
	s.mu.Unlock()

	_ = s.store.SaveGuild(guildCopy)

	channelID := ch.ID
	_ = s.ps.Subscribe(s.ctx, domain.TopicID(groupID, channelID), func(_ peer.ID, ct []byte) {
		s.receiveCiphertext(groupID, ct)
	})
	_ = s.ps.Subscribe(s.ctx, domain.TypingTopicID(groupID, channelID), func(from peer.ID, _ []byte) {
		s.emitTyping(presenceFor(from).Fingerprint, channelID)
	})
	s.emitGuildUpdate()
}

// OnGuildUpdate fires when the guild list or its channels change.
func (s *Service) OnGuildUpdate(fn func()) {
	s.mu.Lock()
	s.onGuildUpdate = append(s.onGuildUpdate, fn)
	s.mu.Unlock()
}

func (s *Service) emitGuildUpdate() {
	s.mu.RLock()
	cbs := append([]func(){}, s.onGuildUpdate...)
	s.mu.RUnlock()
	for _, cb := range cbs {
		cb()
	}
}

// SendTyping broadcasts an ephemeral "is typing" hint for a channel.
func (s *Service) SendTyping(channelID string) error {
	groupID, err := s.groupForChannel(channelID)
	if err != nil {
		return err
	}
	return s.ps.Publish(s.ctx, domain.TypingTopicID(groupID, channelID), []byte{1})
}

// OnTyping fires when a peer signals typing in a channel.
func (s *Service) OnTyping(fn func(from, channelID string)) {
	s.mu.Lock()
	s.onTyping = append(s.onTyping, fn)
	s.mu.Unlock()
}

func (s *Service) emitTyping(from, channelID string) {
	s.mu.RLock()
	cbs := append([]func(string, string){}, s.onTyping...)
	s.mu.RUnlock()
	for _, cb := range cbs {
		cb(from, channelID)
	}
}

// receiveGuildMeta decrypts and applies an inbound guild metadata update.
func (s *Service) receiveGuildMeta(guildID string, groupID, ct []byte) {
	msg, err := s.mls.Decrypt(s.ctx, groupID, ct)
	if err != nil {
		return
	}
	var m guildMeta
	if json.Unmarshal(msg.Plaintext, &m) != nil {
		return
	}
	switch m.Type {
	case "channel_added":
		m.Channel.GuildID = guildID
		s.addChannel(guildID, m.Channel)
	case "guild_renamed":
		if strings.TrimSpace(m.Name) == "" {
			return
		}
		s.mu.Lock()
		if g, ok := s.guilds[guildID]; ok {
			g.Name = m.Name
			gc := *g
			s.mu.Unlock()
			_ = s.store.SaveGuild(gc)
		} else {
			s.mu.Unlock()
		}
		s.emitGuildUpdate()
	case "profile":
		if m.Fingerprint == "" {
			return
		}
		if len(m.Avatar) > maxAvatarBytes || (m.Avatar != "" && !strings.HasPrefix(m.Avatar, "data:image/")) {
			m.Avatar = "" // reject oversized or non-image avatars from peers
		}
		s.mu.Lock()
		_, known := s.profiles[m.Fingerprint]
		s.profiles[m.Fingerprint] = Profile{Name: m.Name, Status: m.Status, Emoji: m.Emoji, Color: m.Color, Avatar: m.Avatar}
		s.mu.Unlock()
		// First time we see this member: reply with our own profile so the
		// newcomer learns us too (bounded — only on genuinely new members).
		if !known && m.Fingerprint != s.id.Fingerprint() {
			s.announceProfile(guildID)
		}
		s.emitGuildUpdate()
	}
}

// receiveCiphertext decrypts an inbound channel message, persists it, and
// notifies the UI. Decryption failures (e.g. an epoch we haven't reached yet)
// are dropped rather than surfaced.
func (s *Service) receiveCiphertext(groupID, ct []byte) {
	msg, err := s.mls.Decrypt(s.ctx, groupID, ct)
	if err != nil {
		return
	}
	var m domain.Message
	if err := json.Unmarshal(msg.Plaintext, &m); err != nil {
		return
	}
	// Ignore messages for channels we no longer track (e.g. a guild we left but
	// whose gossipsub subscription is still live this session).
	s.mu.RLock()
	_, tracked := s.channelToGuild[m.ChannelID]
	s.mu.RUnlock()
	if !tracked {
		return
	}
	// Trust MLS's authenticated sender over the self-reported field.
	m.Sender = msg.SenderID
	switch m.Kind {
	case "delete":
		s.applyDelete(m.ReplyTo, m.Sender)
		return
	case "reaction":
		s.applyReaction(m.ReplyTo, m.Content, m.Sender)
		return
	case "edit":
		s.applyEdit(m.ReplyTo, m.Content, m.Sender)
		return
	case "pin":
		s.applyPin(m.ReplyTo)
		return
	}
	inserted, err := s.store.SaveMessage(m)
	if err != nil || !inserted {
		return // duplicate (gossip re-delivery or already synced): stay silent
	}
	s.emitMessage(m)
}

// adoptBootstrap merges invite-carried rendezvous addrs into the saved network
// config and connects to them, waiting briefly so a subsequent relayed dial to
// the (NAT'd) guild owner can succeed.
func (s *Service) adoptBootstrap(addrs []string) {
	existing := LoadNetConfig(s.dataDir).Bootstrap
	seen := map[string]bool{}
	for _, a := range existing {
		seen[a] = true
	}
	merged := existing
	for _, a := range addrs {
		if a = strings.TrimSpace(a); a != "" && !seen[a] {
			merged = append(merged, a)
			seen[a] = true
		}
	}
	_ = SaveNetConfig(s.dataDir, NetConfig{Bootstrap: merged})

	infos, err := parseBootstrapPeers(addrs)
	if err != nil {
		return
	}
	var wg sync.WaitGroup
	for _, pi := range infos {
		pi := pi
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(s.ctx, 8*time.Second)
			defer cancel()
			_ = s.host.Connect(ctx, pi)
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
}

func ownerAddrInfo(ic inviteCode) (peer.AddrInfo, error) {
	pid, err := peer.Decode(ic.OwnerID)
	if err != nil {
		return peer.AddrInfo{}, fmt.Errorf("app: bad owner peer id: %w", err)
	}
	addrs := make([]multiaddr.Multiaddr, 0, len(ic.OwnerAddr))
	for _, a := range ic.OwnerAddr {
		ma, err := multiaddr.NewMultiaddr(a)
		if err != nil {
			continue
		}
		addrs = append(addrs, ma)
	}
	return peer.AddrInfo{ID: pid, Addrs: addrs}, nil
}
