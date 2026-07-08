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

// inviteRequest is sent by a joiner over the invite stream. It carries the
// joiner's profile so the owner learns their display name over this reliable
// stream — a gossip announce right after joining races mesh formation and can
// be silently lost.
type inviteRequest struct {
	GuildID    string  `json:"guildId"`
	KeyPackage []byte  `json:"keyPackage"`
	Credential []byte  `json:"credential"` // joiner's account key, for retry recovery
	Profile    Profile `json:"profile"`
}

// inviteResponse is returned by the owner over the invite stream. Profiles is
// the roster of member profiles the owner knows (including its own), keyed by
// fingerprint, so the joiner shows real names immediately.
type inviteResponse struct {
	Welcome  []byte             `json:"welcome"`
	Guild    domain.Guild       `json:"guild"`
	Profiles map[string]Profile `json:"profiles,omitempty"`
	Error    string             `json:"error,omitempty"`
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

// Messages returns stored history for a channel (oldest first). Opening a
// channel also backfills display names from message authors we don't have a
// name for yet, so old history converges the roster and chat onto one name.
func (s *Service) Messages(channelID string, limit int) ([]domain.Message, error) {
	msgs, err := s.store.Messages(channelID, limit)
	if err == nil {
		for _, m := range msgs {
			if m.Name != "" {
				s.learnNameHint(identity.FingerprintOf(m.Sender), m.Name)
			}
		}
	}
	return msgs, err
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
	if !s.hasPerm(guildID, PermManageGuild) {
		return fmt.Errorf("app: you don't have permission to manage this guild")
	}
	s.mu.Lock()
	g, ok := s.guilds[guildID]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("app: unknown guild %s", guildID)
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

// SetGuildProfile updates a guild's name/icon/banner/description and announces
// them to members. Icon/banner are data-URI images (an animated GIF banner just
// works). Requires ManageGuild.
func (s *Service) SetGuildProfile(guildID, name, icon, banner, description string) error {
	if !s.hasPerm(guildID, PermManageGuild) {
		return fmt.Errorf("app: you don't have permission to manage this guild")
	}
	name = strings.TrimSpace(name)
	if len(description) > 1000 {
		description = description[:1000]
	}
	for _, img := range []struct{ v, what string }{{icon, "icon"}, {banner, "banner"}} {
		if img.v != "" && !strings.HasPrefix(img.v, "data:image/") {
			return fmt.Errorf("app: %s must be an image", img.what)
		}
		if len(img.v) > maxGuildImageBytes {
			return fmt.Errorf("app: %s image too large", img.what)
		}
	}
	s.mu.Lock()
	g, ok := s.guilds[guildID]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("app: unknown guild %s", guildID)
	}
	if name != "" {
		g.Name = name
	}
	g.Icon, g.Banner, g.Description = icon, banner, description
	groupID := g.GroupID
	guildCopy := *g
	s.mu.Unlock()

	_ = s.store.SaveGuild(guildCopy)
	s.emitGuildUpdate()
	s.publishMeta(groupID, guildMeta{
		Type: "guild_profile", Name: guildCopy.Name,
		GuildIcon: icon, GuildBanner: banner, GuildDescription: description,
	})
	return nil
}

// maxGuildImageBytes caps a guild icon/banner data URI so it stays under the
// gossipsub frame limit (banners are downscaled client-side).
const maxGuildImageBytes = 512 << 10 // 512 KiB

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

// RemoveMember evicts a member from a guild. The caller must be the owner or
// hold the manage-members permission; the resulting MLS commit is published to
// the control topic so remaining members re-key, and honest peers accept it
// because the committer-authority gate recognizes the same authorization.
func (s *Service) RemoveMember(guildID string, memberCredential []byte) error {
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("app: unknown guild %s", guildID)
	}
	if !s.canManageMembers(guildID) {
		return fmt.Errorf("app: you don't have permission to remove members")
	}
	if bytes.Equal(g.OwnerID, memberCredential) {
		return fmt.Errorf("app: the owner cannot be removed")
	}
	if bytes.Equal(memberCredential, s.PublicKey()) {
		return fmt.Errorf("app: use Leave to remove yourself")
	}
	commit, err := s.mls.Remove(s.ctx, g.GroupID, memberCredential)
	if err != nil {
		return err
	}
	s.logCommit(g.GroupID, commit)
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
	reqBytes, _ := json.Marshal(inviteRequest{
		GuildID: ic.GuildID, KeyPackage: kp, Credential: s.PublicKey(),
		Profile: s.SelfProfile(),
	})

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
	// Adopt the member roster carried by the response so real names show
	// immediately, without waiting for gossip announces.
	for fpr, p := range resp.Profiles {
		s.learnProfile(fpr, p)
	}
	// Keep the owner connection alive so presence and gossipsub keep flowing.
	s.host.Protect(owner.ID)
	// Pull channel history from the owner right away (the peer-connect sync
	// trigger fired before we joined, so it skipped this guild).
	go s.syncGuildFromPeer(g.ID, owner.ID)
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

	// Enforce the banlist at the gate: a banned fingerprint cannot rejoin, even
	// with a fresh invite code. This is what makes a ban survive rejoin.
	if len(req.Credential) > 0 && s.isBanned(req.GuildID, identity.FingerprintOf(req.Credential)) {
		return json.Marshal(inviteResponse{Error: "you are banned from this server"})
	}

	// Serialize the add + epoch-advancing publish: concurrent joins (a group DM
	// invites several people at once) must not commit at the same epoch.
	s.inviteMu.Lock()
	defer s.inviteMu.Unlock()

	commit, welcome, err := s.mls.Invite(ctx, g.GroupID, req.KeyPackage)
	if err != nil {
		// Most common cause: this is a RETRY where the joiner is already in the
		// group (a previous attempt added them but they never finished joining,
		// e.g. the response was lost). Remove the stale entry and re-invite so
		// they get a fresh Welcome at the current epoch.
		if len(req.Credential) > 0 {
			if rmCommit, rmErr := s.mls.Remove(ctx, g.GroupID, req.Credential); rmErr == nil {
				s.logCommit(g.GroupID, rmCommit)
				_ = s.ps.Publish(ctx, domain.ControlTopicID(g.GroupID), rmCommit)
				commit, welcome, err = s.mls.Invite(ctx, g.GroupID, req.KeyPackage)
			}
		}
		if err != nil {
			return json.Marshal(inviteResponse{Error: "invite failed"})
		}
	}
	s.logCommit(g.GroupID, commit)
	// Advance existing members (if any) to the new epoch.
	if err := s.ps.Publish(ctx, domain.ControlTopicID(g.GroupID), commit); err != nil {
		return nil, err
	}
	// Learn the joiner's display name over this reliable stream (their gossip
	// announce may be lost while their mesh warms up), and hand back the member
	// roster so they show real names immediately.
	if len(req.Credential) > 0 {
		s.learnProfile(identity.FingerprintOf(req.Credential), req.Profile)
	}
	// Keep this member reachable (esp. over a relay) and refresh the roster.
	s.host.Protect(from)
	if len(req.Credential) > 0 {
		s.clearPendingDMInvite(req.GuildID, identity.FingerprintOf(req.Credential))
	}
	s.emitGuildUpdate()
	return json.Marshal(inviteResponse{Welcome: welcome, Guild: *g, Profiles: s.profileRoster()})
}

// profileRoster snapshots every profile this peer knows, plus its own, keyed by
// fingerprint. Shared over invite and sync streams so names converge without
// depending on gossip timing.
func (s *Service) profileRoster() map[string]Profile {
	s.mu.RLock()
	out := make(map[string]Profile, len(s.profiles)+1)
	for fpr, p := range s.profiles {
		out[fpr] = p
	}
	s.mu.RUnlock()
	out[s.id.Fingerprint()] = s.SelfProfile()
	return out
}

// logCommit records a commit this peer just created or applied, keyed by the
// epoch it produced (the group's current epoch, since both paths advance the
// local state in place). The log is what lets reconnecting members bridge
// missed membership changes — see sync.go.
func (s *Service) logCommit(groupID, commit []byte) {
	epoch, err := s.mls.Epoch(s.ctx, groupID)
	if err != nil {
		return
	}
	_ = s.store.SaveCommit(groupID, epoch, commit)
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
	// A muted member can't post new messages (moderation actions like delete are
	// still allowed).
	if kind == "" && s.isMuted(guildID, s.id.Fingerprint()) {
		return domain.Message{}, fmt.Errorf("app: you're muted in this guild")
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
	// Also stash a sealed copy in the mailbox of any member who is offline, so
	// they receive it on reconnect even if no peer is online to sync from.
	go s.depositForOffline(groupID, ct)
	switch msg.Kind {
	case "delete":
		s.applyDelete(msg.ReplyTo, msg.Sender, channelID)
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

// applyDelete tombstones a target message and pushes the update to the UI. The
// author may delete their own; a member with ManageMessages may delete anyone's
// (the deleter is the MLS-authenticated sender, so this is enforceable).
func (s *Service) applyDelete(targetID string, bySender []byte, channelID string) {
	force := false
	s.mu.RLock()
	guildID := s.channelToGuild[channelID]
	s.mu.RUnlock()
	if guildID != "" {
		force = s.memberHasPerm(guildID, identity.FingerprintOf(bySender), PermManageMessages)
	}
	deleted, ok, err := s.store.MarkDeleted(targetID, bySender, force)
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
	// Fold any governance ops we already hold for this guild (loaded at startup
	// or from a prior sync) now that we know its owner.
	s.rebuildGovStateLocked(g.ID)
	s.mu.Unlock()

	groupID := g.GroupID
	guildID := g.ID

	// Control topic: apply commits from the owner as membership changes, then
	// refresh the UI so new/removed members show up live. Applied commits are
	// logged so this peer can serve them to members that missed them; a failed
	// apply means WE missed one (epoch gap), so pull the gap via history sync
	// instead of silently falling out of the ratchet.
	_ = s.ps.Subscribe(s.ctx, domain.ControlTopicID(groupID), func(_ peer.ID, data []byte) {
		// Governance gate: apply a membership commit only if its MLS author is
		// authorized to change membership for this guild (foundationally, the
		// owner). An unauthorized member cannot kick/add by publishing a commit —
		// honest peers drop it here. The author is read from the signed commit
		// framing, so it is unforgeable and independent of who relayed the gossip.
		sender, err := s.mls.CommitSender(s.ctx, groupID, data)
		if err != nil {
			// We can't resolve the author — almost always because this commit is
			// for an epoch ahead of us (its sender leaf isn't in our member list
			// yet). That's a gap, not an attack: pull it via history sync, which
			// re-validates authorization commit-by-commit.
			go s.syncGuildFromAnyPeer(guildID)
			return
		}
		if !s.authorizedCommitter(guildID, sender) {
			return // author resolved but not permitted: drop silently, no sync
		}
		if err := s.mls.ApplyCommit(s.ctx, groupID, data); err == nil {
			s.logCommit(groupID, data)
			s.emitGuildUpdate()
		} else {
			go s.syncGuildFromAnyPeer(guildID)
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
		// Watch voice presence for every voice channel so the sidebar shows who's
		// in a call without us having to join it.
		if c.ChannelType() == "voice" {
			s.watchVoice(groupID, channelID)
		}
	}
}

// guildMeta is an MLS-encrypted guild metadata update sent over the guild-meta
// topic so all members converge on shared state (channels, member display
// names). Only the fields relevant to Type are populated.
type guildMeta struct {
	Type string `json:"type"` // channel_added | channel_updated | category_added | profile | nickname | guild_renamed
	Channel     domain.Channel  `json:"channel,omitempty"`
	Category    domain.Category `json:"category,omitempty"`
	Fingerprint string              `json:"fingerprint,omitempty"`
	Name        string              `json:"name,omitempty"`
	Status      string              `json:"status,omitempty"`
	Emoji       string              `json:"emoji,omitempty"`
	Color       string              `json:"color,omitempty"`
	Avatar      string              `json:"avatar,omitempty"`
	Presence    string              `json:"presence,omitempty"`
	Bio         string              `json:"bio,omitempty"`
	MailboxPub  []byte              `json:"mbx,omitempty"`
	CustomEmoji domain.CustomEmoji  `json:"customEmoji,omitempty"`
	GovOp       json.RawMessage     `json:"govOp,omitempty"` // a signed governance op (roles/bans)
	// guild_profile: icon/banner/description (Name reused from above).
	GuildIcon        string `json:"gIcon,omitempty"`
	GuildBanner      string `json:"gBanner,omitempty"`
	GuildDescription string `json:"gDesc,omitempty"`
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
		Presence: p.Presence, Bio: p.Bio, MailboxPub: p.MailboxPub,
	}
	payload, _ := json.Marshal(meta)
	ct, err := s.mls.Encrypt(s.ctx, groupID, payload)
	if err != nil {
		return
	}
	_ = s.ps.Publish(s.ctx, domain.GuildMetaTopicID(groupID), ct)

	// Piggyback our own per-guild nickname (if any) so new members learn it at
	// the same time they learn our profile.
	if nick := s.NickOf(guildID, s.id.Fingerprint()); nick != "" {
		s.publishMeta(groupID, guildMeta{Type: "nickname", Fingerprint: s.id.Fingerprint(), Name: nick})
	}
}

// SetNickname sets (or, with an empty nick, clears) this member's own display
// name inside one guild. It shadows the global profile name for that guild only.
// The change is persisted locally and announced to the other members.
func (s *Service) SetNickname(guildID, nick string) error {
	nick = strings.TrimSpace(nick)
	if len(nick) > maxNameBytes {
		nick = nick[:maxNameBytes]
	}
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var groupID []byte
	if ok {
		groupID = g.GroupID
	}
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("app: unknown guild %q", guildID)
	}
	s.rememberNick(guildID, s.id.Fingerprint(), nick)
	s.publishMeta(groupID, guildMeta{Type: "nickname", Fingerprint: s.id.Fingerprint(), Name: nick})
	s.emitGuildUpdate()
	return nil
}

// CreateChannel adds a channel to a guild and announces it (MLS-encrypted) to
// the other members so they add it too. ctype is "" (text), "voice", or
// "announcement"; category is a category ID or "".
func (s *Service) CreateChannel(guildID, name, ctype, category string) (domain.Channel, error) {
	if !s.hasPerm(guildID, PermManageChannels) {
		return domain.Channel{}, fmt.Errorf("app: you don't have permission to manage channels")
	}
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var groupID []byte
	var pos int
	if ok {
		groupID = g.GroupID
		pos = len(g.Channels)
	}
	s.mu.RUnlock()
	if !ok {
		return domain.Channel{}, fmt.Errorf("app: unknown guild %s", guildID)
	}
	if strings.TrimSpace(name) == "" {
		return domain.Channel{}, fmt.Errorf("app: channel name is empty")
	}
	switch ctype {
	case "", "text", "voice", "announcement":
	default:
		return domain.Channel{}, fmt.Errorf("app: unknown channel type %q", ctype)
	}

	ch := domain.Channel{
		ID: domain.NewID(), GuildID: guildID, Name: strings.TrimSpace(name),
		Type: ctype, Category: category, Position: pos,
	}
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
	// Note the creation in the new channel itself (text channels only — voice
	// channels have no chat feed).
	if ch.ChannelType() != "voice" {
		s.sendSystem(ch.ID, "created this channel")
	}
	return ch, nil
}

// CreateCategory adds a sidebar category and announces it to members.
func (s *Service) CreateCategory(guildID, name string) (domain.Category, error) {
	if !s.hasPerm(guildID, PermManageChannels) {
		return domain.Category{}, fmt.Errorf("app: you don't have permission to manage channels")
	}
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var groupID []byte
	s.mu.RUnlock()
	if !ok {
		return domain.Category{}, fmt.Errorf("app: unknown guild %s", guildID)
	}
	groupID = g.GroupID
	if strings.TrimSpace(name) == "" {
		return domain.Category{}, fmt.Errorf("app: category name is empty")
	}
	cat := domain.Category{ID: domain.NewID(), GuildID: guildID, Name: strings.TrimSpace(name)}
	_ = s.store.SaveCategory(cat)
	s.emitGuildUpdate()
	s.publishMeta(groupID, guildMeta{Type: "category_added", Category: cat})
	return cat, nil
}

// DeleteChannel removes a channel for everyone. Requires ManageChannels.
func (s *Service) DeleteChannel(guildID, channelID string) error {
	if !s.hasPerm(guildID, PermManageChannels) {
		return fmt.Errorf("app: you don't have permission to manage channels")
	}
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var groupID []byte
	if ok {
		groupID = g.GroupID
	}
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("app: unknown guild %s", guildID)
	}
	s.applyChannelRemoved(guildID, channelID)
	s.publishMeta(groupID, guildMeta{Type: "channel_removed", Channel: domain.Channel{ID: channelID}})
	return nil
}

// applyChannelRemoved drops a channel locally (from any source).
func (s *Service) applyChannelRemoved(guildID, channelID string) {
	s.mu.Lock()
	if g, ok := s.guilds[guildID]; ok {
		out := g.Channels[:0]
		for _, c := range g.Channels {
			if c.ID != channelID {
				out = append(out, c)
			}
		}
		g.Channels = out
		delete(s.channelToGuild, channelID)
	}
	s.mu.Unlock()
	_ = s.store.DeleteChannel(channelID)
	s.emitGuildUpdate()
}

// DeleteCategory removes a category (its channels stay, un-categorized).
// Requires ManageChannels.
func (s *Service) DeleteCategory(guildID, categoryID string) error {
	if !s.hasPerm(guildID, PermManageChannels) {
		return fmt.Errorf("app: you don't have permission to manage channels")
	}
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var groupID []byte
	if ok {
		groupID = g.GroupID
	}
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("app: unknown guild %s", guildID)
	}
	s.applyCategoryRemoved(guildID, categoryID)
	s.publishMeta(groupID, guildMeta{Type: "category_removed", Category: domain.Category{ID: categoryID}})
	return nil
}

// applyCategoryRemoved drops a category locally and un-categorizes its channels.
func (s *Service) applyCategoryRemoved(guildID, categoryID string) {
	s.mu.Lock()
	if g, ok := s.guilds[guildID]; ok {
		for i := range g.Channels {
			if g.Channels[i].Category == categoryID {
				g.Channels[i].Category = ""
			}
		}
	}
	s.mu.Unlock()
	_ = s.store.DeleteCategory(guildID, categoryID)
	s.emitGuildUpdate()
}

// SetChannelMeta changes a channel's type/category/position/topic and announces it.
func (s *Service) SetChannelMeta(guildID, channelID, ctype, category string, position int, topic string) error {
	if !s.hasPerm(guildID, PermManageChannels) {
		return fmt.Errorf("app: you don't have permission to manage channels")
	}
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var groupID []byte
	if ok {
		groupID = g.GroupID
	}
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("app: unknown guild %s", guildID)
	}
	topic = strings.TrimSpace(topic)
	_ = s.store.UpdateChannelMeta(channelID, ctype, category, position, topic)
	s.mu.Lock()
	for i := range g.Channels {
		if g.Channels[i].ID == channelID {
			g.Channels[i].Type = ctype
			g.Channels[i].Category = category
			g.Channels[i].Position = position
			g.Channels[i].Topic = topic
		}
	}
	s.mu.Unlock()
	s.emitGuildUpdate()
	s.publishMeta(groupID, guildMeta{Type: "channel_updated",
		Channel: domain.Channel{ID: channelID, GuildID: guildID, Type: ctype, Category: category, Position: position, Topic: topic}})
	return nil
}

// Categories returns a guild's sidebar categories.
func (s *Service) Categories(guildID string) ([]domain.Category, error) {
	return s.store.Categories(guildID)
}

// publishMeta MLS-encrypts and publishes a guild-meta update (best-effort).
func (s *Service) publishMeta(groupID []byte, meta guildMeta) {
	payload, _ := json.Marshal(meta)
	if ct, err := s.mls.Encrypt(s.ctx, groupID, payload); err == nil {
		_ = s.ps.Publish(s.ctx, domain.GuildMetaTopicID(groupID), ct)
	}
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
	if ch.ChannelType() == "voice" {
		s.watchVoice(groupID, channelID)
	}
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
	case "channel_updated":
		if m.Channel.ID == "" {
			return
		}
		_ = s.store.UpdateChannelMeta(m.Channel.ID, m.Channel.Type, m.Channel.Category, m.Channel.Position, m.Channel.Topic)
		s.mu.Lock()
		if g, ok := s.guilds[guildID]; ok {
			for i := range g.Channels {
				if g.Channels[i].ID == m.Channel.ID {
					g.Channels[i].Type = m.Channel.Type
					g.Channels[i].Category = m.Channel.Category
					g.Channels[i].Position = m.Channel.Position
					g.Channels[i].Topic = m.Channel.Topic
				}
			}
		}
		s.mu.Unlock()
		s.emitGuildUpdate()
	case "category_added":
		if m.Category.ID == "" {
			return
		}
		m.Category.GuildID = guildID
		_ = s.store.SaveCategory(m.Category)
		s.emitGuildUpdate()
	case "channel_removed":
		if m.Channel.ID != "" {
			s.applyChannelRemoved(guildID, m.Channel.ID)
		}
	case "category_removed":
		if m.Category.ID != "" {
			s.applyCategoryRemoved(guildID, m.Category.ID)
		}
	case "emoji_added":
		s.applyCustomEmoji(guildID, m.CustomEmoji)
		s.emitGuildUpdate()
	case "emoji_removed":
		if m.CustomEmoji.Name != "" {
			_ = s.store.DeleteCustomEmoji(guildID, m.CustomEmoji.Name)
			s.emitGuildUpdate()
		}
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
	case "guild_profile":
		// Name/icon/banner/description update. Validate the images like a local set.
		if (m.GuildIcon != "" && !strings.HasPrefix(m.GuildIcon, "data:image/")) ||
			(m.GuildBanner != "" && !strings.HasPrefix(m.GuildBanner, "data:image/")) ||
			len(m.GuildIcon) > maxGuildImageBytes || len(m.GuildBanner) > maxGuildImageBytes {
			return
		}
		s.mu.Lock()
		if g, ok := s.guilds[guildID]; ok {
			if strings.TrimSpace(m.Name) != "" {
				g.Name = m.Name
			}
			g.Icon, g.Banner, g.Description = m.GuildIcon, m.GuildBanner, m.GuildDescription
			gc := *g
			s.mu.Unlock()
			_ = s.store.SaveGuild(gc)
		} else {
			s.mu.Unlock()
		}
		s.emitGuildUpdate()
	case "profile":
		// First time we see this member: reply with our own profile so the
		// newcomer learns us too (bounded — only on genuinely new members).
		if s.learnProfile(m.Fingerprint, Profile{Name: m.Name, Status: m.Status, Emoji: m.Emoji, Color: m.Color, Avatar: m.Avatar, Presence: m.Presence, Bio: m.Bio, MailboxPub: m.MailboxPub}) {
			s.announceProfile(guildID)
		}
	case "nickname":
		// A member set their own per-guild nickname (self-asserted, same trust
		// model as profiles). Empty Name clears it back to the profile name.
		if m.Fingerprint == "" || m.Fingerprint == s.id.Fingerprint() {
			return
		}
		nick := m.Name
		if len(nick) > maxNameBytes {
			nick = nick[:maxNameBytes]
		}
		s.rememberNick(guildID, m.Fingerprint, nick)
		s.emitGuildUpdate()
	case "gov_op":
		// A governance op (role grant/revoke, ban/unban). ingestGovOp verifies the
		// signature and replay re-checks the signer's authority, so a forged or
		// unauthorized op changes nothing.
		if len(m.GovOp) > 0 {
			var o govOp
			if json.Unmarshal(m.GovOp, &o) == nil {
				s.ingestGovOp(guildID, o)
			}
		}
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
		s.applyDelete(m.ReplyTo, m.Sender, m.ChannelID)
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
	// Advisory mute: drop a normal message from a member who is currently muted.
	s.mu.RLock()
	guildID := s.channelToGuild[m.ChannelID]
	s.mu.RUnlock()
	if guildID != "" && s.isMuted(guildID, identity.FingerprintOf(m.Sender)) {
		return
	}
	// Backfill a display name from the message if we don't know this member's
	// name yet, so the roster and chat stay consistent.
	s.learnNameHint(identity.FingerprintOf(m.Sender), m.Name)
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
