package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"

	"github.com/zahak/concord/internal/domain"
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
	return s.offerAfter(g, nil)
}

// meetingTTL is the DEFAULT lifetime of an instant meeting: how long it
// outlives its creation before every participant's client sweeps it away on
// startup. A meeting whose guest link was minted with an explicit lifetime
// records an absolute expiry instead (meetingLife); this constant is what a
// meeting created before that existed still gets, so nothing that already
// works changes its behaviour.
const meetingTTL = 24 * time.Hour

// meetingLifetimeKey persists chosen meeting expiries. Not a column on the
// guild: an expiry only exists for the handful of meeting rooms, and the
// setting travels with the same encrypted store.
const meetingLifetimeKey = "meetings.expiry"

// meetingLifetimes are the lifetimes a guest link may be minted with, in
// hours. Deliberately a short menu rather than a free-form number: the host
// picks "office hours all week", not a duration puzzle, and a fixed set is
// also what keeps a peer from talking us into keeping a room forever.
var meetingLifetimes = []int{1, 24, 24 * 7, 24 * 30}

// maxMeetingLifetime bounds any expiry we will accept — including one a peer
// announces. Without it, one guild-meta frame could pin a room (and its
// message history) on every participant's disk indefinitely.
const maxMeetingLifetime = 31 * 24 * time.Hour

// meetingLifetime resolves a requested lifetime to a duration, refusing anything
// that is not on the menu.
func meetingLifetime(h int) (time.Duration, bool) {
	for _, allowed := range meetingLifetimes {
		if h == allowed {
			return time.Duration(h) * time.Hour, true
		}
	}
	return 0, false
}

// meetingExpiry is when a meeting disappears: the lifetime chosen when its
// guest link was minted, or the legacy fixed TTL after creation when none was
// ever chosen. Zero for a guild that is not a meeting (or is gone).
func (s *Service) meetingExpiry(guildID string) time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.meetingExpiryLocked(guildID)
}

func (s *Service) meetingExpiryLocked(guildID string) time.Time {
	g, ok := s.guilds[guildID]
	if !ok || g.Kind != "meeting" {
		return time.Time{}
	}
	if at, ok := s.meetingLife[guildID]; ok && !at.IsZero() {
		return at
	}
	return g.Created.Add(meetingTTL)
}

// setMeetingExpiry records a meeting's chosen expiry and tells the other
// participants, so everyone's sweep agrees on when the room vanishes. Members
// who don't understand the announcement simply keep the old 24h default — they
// lose the room a day in while the host (and any guest link) keeps working,
// which is a stale sidebar entry rather than a broken meeting.
func (s *Service) setMeetingExpiry(guildID string, at time.Time) {
	s.mu.Lock()
	g, ok := s.guilds[guildID]
	if !ok || g.Kind != "meeting" {
		s.mu.Unlock()
		return
	}
	groupID := g.GroupID
	s.meetingLife[guildID] = at
	s.mu.Unlock()
	s.saveMeetingLife()
	s.publishMeta(groupID, guildMeta{Type: "meeting_life", At: at.UnixMilli()})
	s.emitGuildUpdate()
}

// loadMeetingLife restores chosen meeting expiries, dropping entries for
// guilds that are gone (the startup sweep has already run).
func (s *Service) loadMeetingLife() {
	raw, err := s.store.GetSetting(meetingLifetimeKey)
	if err != nil || raw == "" {
		return
	}
	var saved map[string]int64
	if json.Unmarshal([]byte(raw), &saved) != nil {
		return
	}
	s.mu.Lock()
	for id, ms := range saved {
		if g, ok := s.guilds[id]; ok && g.Kind == "meeting" && ms > 0 {
			s.meetingLife[id] = time.UnixMilli(ms)
		}
	}
	s.mu.Unlock()
}

func (s *Service) saveMeetingLife() {
	s.mu.RLock()
	out := make(map[string]int64, len(s.meetingLife))
	for id, at := range s.meetingLife {
		if _, ok := s.guilds[id]; ok {
			out[id] = at.UnixMilli()
		}
	}
	s.mu.RUnlock()
	blob, err := json.Marshal(out)
	if err != nil {
		return
	}
	_ = s.store.SetSetting(meetingLifetimeKey, string(blob))
}

// StartMeeting spins up a TEMPORARY room — the Zoom-link move: one click
// makes a disposable guild with a single channel that doubles as the call
// room (like a DM), and hands back an invite code to send to anyone. Every
// participant's client deletes expired meetings on startup, so the room
// cleans itself up.
func (s *Service) StartMeeting() (domain.Guild, string, error) {
	gid, err := s.mls.CreateGroup(s.ctx)
	if err != nil {
		return domain.Guild{}, "", fmt.Errorf("app: create meeting group: %w", err)
	}
	g := domain.NewMeetingGuild("⚡ Meeting "+time.Now().Format("Jan 2, 15:04"), gid, s.PublicKey())
	if err := s.store.SaveGuild(g); err != nil {
		return domain.Guild{}, "", err
	}
	s.trackGuild(&g)
	code, err := s.InviteCode(g.ID)
	if err != nil {
		return domain.Guild{}, "", err
	}
	return g, code, nil
}

// sweepExpiredMeetings hard-deletes instant meetings past their expiry. Called
// at startup on every participant, so nobody accumulates dead rooms. It honours
// the lifetime chosen when the guest link was minted — deleting a room whose
// link is still meant to work for another week is exactly the bug a fixed TTL
// caused.
func (s *Service) sweepExpiredMeetings() {
	now := time.Now()
	s.mu.RLock()
	var expired []string
	for id, g := range s.guilds {
		if g.Kind != "meeting" {
			continue
		}
		if now.After(s.meetingExpiryLocked(id)) {
			expired = append(expired, id)
		}
	}
	s.mu.RUnlock()
	for _, id := range expired {
		_ = s.deleteGuildLocal(id)
	}
	if len(expired) > 0 {
		s.saveMeetingLife() // prunes the entries of the rooms just deleted
	}
}

// Guilds returns the guilds this peer belongs to. Closed DMs stay tracked
// underneath (messages still arrive and reopen them) but are not listed.
func (s *Service) Guilds() []domain.Guild {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.Guild, 0, len(s.guilds))
	for id, g := range s.guilds {
		if s.hiddenDMs[id] {
			continue
		}
		out = append(out, *g)
	}
	return out
}

// GuildLastActivity returns the newest message/update time (UnixNano) across
// all of a guild's channels, or 0 for a silent guild. Drives recency ordering
// of the DM list.
func (s *Service) GuildLastActivity(guildID string) int64 {
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var chans []domain.Channel
	if ok {
		chans = append(chans, g.Channels...)
	}
	s.mu.RUnlock()
	var newest int64
	for _, c := range chans {
		if t, err := s.store.LatestTimestamp(c.ID); err == nil && t > newest {
			newest = t
		}
	}
	return newest
}

// Messages returns stored history for a channel (oldest first). Opening a
// channel also backfills display names from message authors we don't have a
// name for yet, so old history converges the roster and chat onto one name.
func (s *Service) Messages(channelID string, limit int) ([]domain.Message, error) {
	msgs, err := s.store.Messages(channelID, limit)
	if err == nil {
		for _, m := range msgs {
			if m.Name != "" {
				s.learnNameHint(accountFingerprintOf(m.Sender), m.Name)
			}
		}
	}
	return msgs, err
}

// UnreadCounts returns the per-channel count of normal messages newer than each
// channel's cursor, without decrypting any bodies.
func (s *Service) UnreadCounts(sinceNano map[string]int64) (map[string]int, error) {
	return s.store.UnreadCounts(sinceNano)
}

// MessagesBefore returns up to limit messages older than beforeNano (oldest
// first) — the older page fetched when the reader scrolls up past the initial
// window.
func (s *Service) MessagesBefore(channelID string, beforeNano int64, limit int) ([]domain.Message, error) {
	msgs, err := s.store.MessagesBefore(channelID, beforeNano, limit)
	if err == nil {
		for _, m := range msgs {
			if m.Name != "" {
				s.learnNameHint(accountFingerprintOf(m.Sender), m.Name)
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
	// A banner may also be a named preset from the shared library — the same
	// "preset:<id>" form profile banners already travel as, which is a dozen
	// bytes on the guild-meta topic instead of a quarter-megabyte image, and
	// which animates because it is drawn rather than decoded. The id charset is
	// deliberately narrow: this string reaches a CSS context in the client, so
	// nothing that could carry a quote or a url() gets through. An id the client
	// does not recognise simply renders as no banner.
	if banner != "" && strings.HasPrefix(banner, presetPrefix) {
		if !validPresetID(strings.TrimPrefix(banner, presetPrefix)) {
			return fmt.Errorf("app: banner preset id must be 1–32 chars of a-z, 0-9 or -")
		}
	} else if banner != "" && !validImageDataURI(banner, maxGuildImageBytes) {
		// A PREFIX check is not enough, and this is not hypothetical: the client
		// used to build an unquoted CSS url() out of this value, so a string
		// beginning "data:image/" and continuing ");background:url(http://…"
		// escaped the declaration and made every member who opened the guild
		// fetch a remote asset — an IP disclosure to whoever set the banner.
		// validImageDataURI demands the WHOLE string be a base64 raster URI, and
		// the renderer now vets it again. Both, deliberately.
		return fmt.Errorf("app: banner must be a png/jpeg/gif/webp data URI or a preset")
	}
	if icon != "" && !validImageDataURI(icon, maxGuildImageBytes) {
		return fmt.Errorf("app: icon must be a png/jpeg/gif/webp data URI")
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

// LeaveGuild removes a guild from this peer locally. (A local action — other
// members keep the guild.) A 1:1 DM (including Notes) is special-cased,
// Discord-style: "closing" it only hides the conversation — membership,
// subscriptions, and history stay intact, and it reopens (history and all)
// when either side messages again. Destroying the group here is what used to
// strand the other side typing into a conversation we could no longer see.
// Group DMs and guilds keep the hard behavior: stop tracking, delete local data.
func (s *Service) LeaveGuild(guildID string) error {
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	isDM := ok && g.Kind == "dm"
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("app: unknown guild %s", guildID)
	}
	if isDM && !s.isGroupDM(guildID) {
		// 1:1 (or membership unresolvable — hiding is the safe default: it never
		// destroys history, and a mistaken hide of a group merely resurfaces it
		// on the next message).
		s.hideDM(guildID)
		return nil
	}
	return s.deleteGuildLocal(guildID)
}

// isGroupDM reports whether a DM has (or is gathering, counting pending
// invitees) more than one other person — accounts, not device leaves. When
// membership can't be resolved it answers false, which steers LeaveGuild to
// the non-destructive hide path.
func (s *Service) isGroupDM(guildID string) bool {
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var groupID []byte
	if ok {
		groupID = g.GroupID
	}
	s.mu.RUnlock()
	if !ok {
		return false
	}
	others, resolved := s.dmOtherAccounts(groupID)
	if !resolved {
		return false
	}
	people := map[string]bool{}
	for _, f := range others {
		people[f] = true
	}
	for _, f := range s.PendingDMInvitees(guildID) {
		people[f] = true
	}
	return len(people) > 1
}

// deleteGuildLocal is the hard removal: stop tracking the guild, go quiet on
// its topics, and delete its stored data (and any DM bookkeeping for it).
// Membership itself is untouched — no MLS remove/commit — because a silent
// leaf harms nobody, while a leave that mutated group crypto would put this
// path inside the comms-critical zone for no user-visible gain.
func (s *Service) deleteGuildLocal(guildID string) error {
	s.mu.Lock()
	g, ok := s.guilds[guildID]
	var chIDs []string
	var groupID []byte
	if ok {
		groupID = g.GroupID
		for _, c := range g.Channels {
			delete(s.channelToGuild, c.ID)
			chIDs = append(chIDs, c.ID)
		}
		delete(s.guilds, guildID)
		delete(s.hiddenDMs, guildID)
		delete(s.dmPeers, guildID)
	}
	s.mu.Unlock()
	_ = s.store.DeleteReadState(chIDs)
	if !ok {
		return fmt.Errorf("app: unknown guild %s", guildID)
	}
	// Tombstone BEFORE deleting the data: leaving is an intent, not just a
	// cleanup, and every automatic adoption path (a linked device's hello
	// offer, a re-link handover, a contact's pushed invite, the startup load)
	// consults this row to refuse the re-add. Written first so a crash between
	// the two writes errs on the side of staying gone — the reverse order
	// would recreate the reported bug in the crash window. An explicit
	// user-driven JoinViaInvite clears it (see there).
	_ = s.store.MarkGuildLeft(guildID)
	// Go quiet: unwind the subscriptions trackGuild opened, so a left guild's
	// frames stop arriving at all instead of being decrypted and dropped.
	s.untrackGuildTopics(groupID, chIDs)
	s.dmInviteMu.Lock()
	delete(s.pendingDMInvites, guildID)
	s.dmInviteMu.Unlock()
	s.persistDMState()
	_ = s.store.DeleteGuild(guildID)
	s.emitGuildUpdate()
	return nil
}

// untrackGuildTopics unwinds, symmetrically, everything trackGuild (and
// watchVoice via trackGuild) subscribed for a guild: control, guild-meta, and
// each channel's message/typing/voice topics. Called only from
// deleteGuildLocal, after the guild has already been dropped from s.guilds.
func (s *Service) untrackGuildTopics(groupID []byte, chIDs []string) {
	s.ps.Unsubscribe(domain.ControlTopicID(groupID))
	s.ps.Unsubscribe(domain.GuildMetaTopicID(groupID))
	for _, ch := range chIDs {
		// If the user was IN this room's call, stop the heartbeat goroutine.
		// The departure announce inside LeaveVoice needs channelToGuild, which
		// is already gone — acceptable: peers age us out of the roster, and a
		// guild we are leaving is not owed a goodbye frame.
		_ = s.LeaveVoice(ch)
		s.ps.Unsubscribe(domain.TopicID(groupID, ch))
		s.ps.Unsubscribe(domain.TypingTopicID(groupID, ch))
		s.voiceMu.Lock()
		watched := s.voiceWatched[ch]
		delete(s.voiceWatched, ch) // so a re-join's watchVoice re-subscribes
		s.voiceMu.Unlock()
		if watched {
			s.ps.Unsubscribe(domain.VoiceTopicID(groupID, ch))
		}
	}
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

// IsOwner reports whether this peer is the guild's CURRENT owner — the
// founding key only until a transfer_owner op moves the crown (govstate.go).
func (s *Service) IsOwner(guildID string) bool {
	return s.IsGuildOwner(guildID, s.id.Fingerprint())
}

// GuildOwnerFingerprint is the account fingerprint of a guild's CURRENT owner.
// Used to authenticate relayed guest messages: a "guest" message is only
// genuine if the meeting's OWNER (the host running the guest gateway) signed it.
func (s *Service) GuildOwnerFingerprint(guildID string) string {
	return s.effectiveOwner(guildID)
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
	// Compare on the account fingerprint so a device leaf is recognized as its
	// account (owner protection and self-check must hold across all of an
	// account's devices) — against the EFFECTIVE owner, so a transferred crown
	// protects its new head and stops shielding the old one.
	if accountFingerprintOf(memberCredential) == s.effectiveOwner(guildID) {
		return fmt.Errorf("app: the owner cannot be removed")
	}
	if bytes.Equal(accountKeyOf(memberCredential), s.PublicKey()) {
		return fmt.Errorf("app: use Leave to remove yourself")
	}
	commit, err := s.mls.Remove(s.ctx, g.GroupID, memberCredential)
	if err != nil {
		return err
	}
	s.logCommit(g.GroupID, commit)
	return s.ps.Publish(s.ctx, domain.ControlTopicID(g.GroupID), commit)
}

// AddMember drops a VERIFIED contact straight into a guild — the thing you can
// already do with a DM, and there was never a reason servers couldn't do it too.
// It mints an invite and pushes it to them over the same channel a DM invite
// uses; their client auto-accepts only because they verified US (see
// isVerifiedGuildInvite). Both halves are required: we must be allowed to invite
// here, and they must have verified us. Anyone else still needs a code.
func (s *Service) AddMember(guildID, fingerprint string) error {
	if fingerprint == "" {
		return fmt.Errorf("app: no contact given")
	}
	if !s.canManageMembers(guildID) {
		return fmt.Errorf("app: you don't have permission to invite people here")
	}
	if !s.VerifiedFingerprints()[fingerprint] {
		return fmt.Errorf("app: verify this contact first — only verified contacts can be added directly")
	}
	if s.guildHasMember(guildID, fingerprint) {
		return fmt.Errorf("app: they're already in this server")
	}
	// Record them as PENDING right away, so they show in the roster like a DM you
	// opened — even if they're offline. The invite is pushed now if they're
	// reachable, and retried each heal tick (reconcilePendingMembers) until they
	// join; they drop out of pending the moment they actually appear as a member.
	s.addPending(guildID, fingerprint)
	if pid, ok := s.peerForFingerprint(fingerprint); ok {
		s.mu.RLock()
		name := ""
		if g, ok := s.guilds[guildID]; ok {
			name = g.Name
		}
		s.mu.RUnlock()
		if code, err := s.InviteCode(guildID); err == nil {
			s.pushGuildInvite(pid, code, name)
		}
	}
	s.emitGuildUpdate()
	return nil
}

// InviteCode returns a shareable invite string for a guild this peer owns.
func (s *Service) InviteCode(guildID string) (string, error) {
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	s.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("app: unknown guild %s", guildID)
	}
	// Only an authorized committer can actually admit a joiner: a code minted by a
	// member without ManageMembers would advance that member onto a private epoch
	// fork the moment it's redeemed (honest peers drop the unauthorized commit).
	// Refuse to hand out a code we can't honor.
	if !s.canManageMembers(guildID) {
		return "", fmt.Errorf("app: you don't have permission to invite members to this server")
	}

	ai := s.host.AddrInfo()
	// Relay "circuit" addresses let a joiner behind a different NAT reach us
	// THROUGH the relay when our own addresses are undialable; codeAddrs adds
	// one per reservation we actually hold.
	bootstrap := LoadNetConfig(s.dataDir).Bootstrap
	addrs := codeAddrs(ai.Addrs, bootstrap)
	return encodeInviteCode(inviteCode{
		GuildID:   g.ID,
		GuildName: g.Name,
		OwnerID:   ai.ID.String(),
		OwnerAddr: addrs,
		// Embed our rendezvous nodes so the joiner is configured by the code
		// alone — one paste connects them to the same network.
		Bootstrap: bootstrap,
	}), nil
}

// JoinViaInvite redeems an invite code: it contacts the owner, exchanges an MLS
// KeyPackage for a Welcome, joins the group, and subscribes to guild topics.
// offerAfter tells this account's other devices about a guild we just gained, so
// they are not stuck with whatever existed the moment they were linked.
func (s *Service) offerAfter(g domain.Guild, err error) (domain.Guild, error) {
	if err == nil {
		go s.regreetOwnDevices()
	}
	return g, err
}

// JoinViaInvite is the USER-INITIATED entry point: a human pasted this code
// (or clicked accept on a request). That is why it may lift a leave-tombstone
// where the automatic paths (joinOfferedInvite, JoinLinkedInvite, a pushed
// invite) must honour it — the human just said "I want back in", and it is the
// only voice allowed to override an earlier "get me out".
func (s *Service) JoinViaInvite(code string) (domain.Guild, error) {
	ic, err := decodeInviteCode(strings.TrimSpace(code))
	if err != nil {
		return domain.Guild{}, err
	}
	// Cleared even if the join then fails (owner offline): the intent stands,
	// and a half-lifted state would leave the user unable to be re-offered the
	// guild they are actively trying to re-enter.
	_ = s.store.ClearGuildLeft(ic.GuildID)
	// One join per guild at a time: a concurrent duplicate reads to the owner
	// as a stale retry, which REMOVES our live leaf and re-adds it — two
	// epoch-advancing commits every member must gaplessly apply for nothing.
	// An explicit call for a guild we already hold is still served (it is the
	// owner's job to judge it — refuse a ban, re-admit a kick, re-add a
	// stranded leaf); the automatic hello path uses joinOfferedInvite, which
	// declines the redundant case entirely.
	release := s.claimJoin(ic.GuildID)
	defer release()
	return s.joinViaInviteLocked(ic)
}

// joinOfferedInvite redeems a code offered by one of our own devices, joining
// only if this device is not already a healthy member. The check runs INSIDE
// the per-guild join lock: the link flow's startup redemption and the first
// hello's offer race each other with the same codes, and the loser of that
// race must become a no-op, not a duplicate join request (see JoinViaInvite
// for what a duplicate costs — and it used to be worse: each redundant join
// re-greeted our devices, which offered the codes again, a self-sustaining
// re-join loop that churned the guild epoch several times a second and
// stranded every member who dropped one commit frame).
func (s *Service) joinOfferedInvite(code string) {
	ic, err := decodeInviteCode(strings.TrimSpace(code))
	if err != nil {
		return
	}
	// Leave-tombstone veto, enforced RECEIVE-side: this path runs on nothing
	// but another device's say-so, and a guild the user deliberately left must
	// not ride back in on it. Only the user's own JoinViaInvite clears the
	// tombstone.
	if s.store.GuildIsLeft(ic.GuildID) {
		return
	}
	release := s.claimJoin(ic.GuildID)
	defer release()
	s.mu.RLock()
	_, already := s.guilds[ic.GuildID]
	s.mu.RUnlock()
	if already && s.guildHasMember(ic.GuildID, s.id.Fingerprint()) {
		return
	}
	_, _ = s.joinViaInviteLocked(ic)
}

// JoinLinkedInvite redeems one of the invite codes a device-link handover
// carries (bridge.RedeemLinkCode). It is AUTOMATIC adoption — the user linked
// a device, they did not name any guild — so unlike JoinViaInvite it honours
// the leave-tombstone: a re-link of a device that still holds a guild the user
// deleted must not resurrect it. Errors are deliberately swallowed, matching
// the handover's best-effort contract (history converges via sync anyway).
func (s *Service) JoinLinkedInvite(code string) {
	s.joinOfferedInvite(code)
}

// joinViaInviteLocked is the join proper; the caller holds the guild's join
// lock (claimJoin).
func (s *Service) joinViaInviteLocked(ic inviteCode) (domain.Guild, error) {
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
		GuildID: ic.GuildID, KeyPackage: kp, Credential: s.myCredential,
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
	// We are members now, which is what makes these peers worth caching — the
	// connection callback ran before that was true. See rememberMembers.
	s.rememberMembers()
	// Pull channel history from the owner right away (the peer-connect sync
	// trigger fired before we joined, so it skipped this guild).
	go s.syncGuildFromPeer(g.ID, owner.ID)
	// Tell existing members our display name (and learn theirs in reply).
	s.announceProfile(g.ID)
	// Announce arrival with a system message — but only for a genuine first join.
	// When this is an additional DEVICE of an account already in the guild (device
	// linking), the account has another leaf here already, so stay quiet rather
	// than posting a bogus "joined the server" for a member who never left.
	if len(g.Channels) > 0 && s.accountLeafCount(g.GroupID) <= 1 {
		s.sendSystem(g.Channels[0].ID, "joined the server")
	}
	return s.offerAfter(g, nil)
}

// claimJoin takes the per-guild join lock, returning the release. It is what
// keeps two overlapping redemptions of the same invite code (paste + hello
// offer + link redeem) from turning into duplicate join requests at the owner.
func (s *Service) claimJoin(guildID string) func() {
	s.joiningMu.Lock()
	if s.joining == nil {
		s.joining = map[string]*sync.Mutex{}
	}
	m, ok := s.joining[guildID]
	if !ok {
		m = &sync.Mutex{}
		s.joining[guildID] = m
	}
	s.joiningMu.Unlock()
	m.Lock()
	return m.Unlock
}

// accountLeafCount returns how many MLS leaves in a group belong to THIS account
// (>1 means this install's account already has another device in the group).
func (s *Service) accountLeafCount(groupID []byte) int {
	creds, err := s.mls.Members(s.ctx, groupID)
	if err != nil {
		return 0
	}
	mine := s.id.Fingerprint()
	n := 0
	for _, c := range creds {
		if accountFingerprintOf(c) == mine {
			n++
		}
	}
	return n
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
	// Serving the join advances the epoch and returns a Welcome. If WE aren't an
	// authorized committer, honest peers drop our commit and the joiner forks onto
	// a private epoch chain — silently. Refuse rather than fork.
	if !s.canManageMembers(req.GuildID) {
		return json.Marshal(inviteResponse{Error: "inviter is not authorized to admit members"})
	}
	// A device that knows its own group state is stale must not mint commits:
	// they would fork the group at a dead epoch and drag the joiner onto the
	// fork. Refusing makes the requester try another committer (heals iterate
	// candidates), which is strictly better than serving them poison.
	if s.OutOfSync(req.GuildID) {
		return json.Marshal(inviteResponse{Error: "inviter is catching up; ask another member"})
	}

	// Bind the claimed credential to the AUTHENTICATED caller. The libp2p PeerID
	// is the account key, so presenceFor(from) is who actually dialed us.
	// Without this, req.Credential is attacker-supplied and drives both the ban
	// check and the retry-time Remove below — letting anyone with an invite code
	// (a) evict any member by fingerprint (bad KeyPackage + Credential=victim →
	// owner-authored Remove commit) and (b) bypass the ban gate (real KeyPackage
	// + Credential=some non-banned fingerprint). Requiring Credential == caller
	// makes the Remove self-only and binds the ban check to the real joiner.
	// Bind the claimed credential to the dialing device: a bare credential to the
	// caller's account key, a device cert to the caller's device key (and the cert
	// must chain to an account). This works for both a legacy account-key PeerID
	// and a linked device's device-key PeerID.
	if len(req.Credential) > 0 && !credentialBoundToPeer(req.Credential, from) {
		return json.Marshal(inviteResponse{Error: "credential does not match caller"})
	}
	// Learn this joiner's device→account mapping so their PeerID resolves to the
	// account in presence/roster (a device cert; a no-op for a bare credential).
	s.learnDeviceCert(req.Credential)

	// Enforce the banlist at the gate: a banned fingerprint cannot rejoin, even
	// with a fresh invite code. This is what makes a ban survive rejoin.
	if len(req.Credential) > 0 && s.isBanned(req.GuildID, accountFingerprintOf(req.Credential)) {
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
		s.learnProfile(accountFingerprintOf(req.Credential), req.Profile)
	}
	// Keep this member reachable (esp. over a relay) and refresh the roster.
	s.host.Protect(from)
	// The commit above is what made the joiner a member, so this is the first
	// moment they are worth caching. See rememberMembers.
	s.rememberMembers()
	if len(req.Credential) > 0 {
		s.clearPendingDMInvite(req.GuildID, accountFingerprintOf(req.Credential))
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
		if p.Name == "" {
			continue // don't relay someone we only know as "unknown" — it would
			// blank the name for a peer who already learned it
		}
		out[fpr] = p
	}
	s.mu.RUnlock()
	// The STORED copy, not the presentation copy: the roster is how our own
	// devices catch up over sync, and they must never adopt a now-playing
	// substitute as a manual status. Peers lose nothing — activity is ephemeral
	// and travels over live announces only.
	out[s.id.Fingerprint()] = s.selfStoredProfile()
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

// SendCallNotice posts a lightweight call event into a channel — e.g. a
// "call-missed" line when a DM ring went unanswered. It rides the normal
// encrypted message path (both sides store and render it) but, like every
// non-"" kind, never pings or counts as unread on the client.
func (s *Service) SendCallNotice(channelID, kind, content string) (domain.Message, error) {
	if kind != "call-missed" {
		return domain.Message{}, fmt.Errorf("app: bad call notice kind %q", kind)
	}
	return s.send(channelID, content, kind, "")
}

func (s *Service) send(channelID, content, kind, replyTo string) (domain.Message, error) {
	return s.sendAs(channelID, content, kind, replyTo, "")
}

// sendAs is send() with an explicit author name — used only for relayed guest
// messages, which are signed by the host but spoken by someone else.
func (s *Service) sendAs(channelID, content, kind, replyTo, guestName string) (domain.Message, error) {
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
	// A closed forum post takes no more messages. Moderators are not exempt:
	// the point of closing a thread is that the conversation is over, and a
	// moderator who wants the last word can reopen it, which leaves a trace.
	if kind == "" && s.postIsLocked(channelID) {
		return domain.Message{}, fmt.Errorf("app: this post is closed")
	}

	msg, err := domain.NewMessage(channelID, s.PublicKey(), content)
	if err != nil {
		return domain.Message{}, err
	}
	msg.Name = s.DisplayName()
	if kind == "guest" && guestName != "" {
		// A guest has no key of their own: the message is relayed under the
		// host's signature, but it is NOT the host speaking. The guest's name
		// rides in the (self-asserted, decorative) Name field and the kind marks
		// it, so every client can render them as their own author instead of
		// nesting their words under the host's.
		msg.Name = guestName
	}
	if kind == "system" && guestName != "" {
		// A system notice can also speak for something other than the member
		// whose node posted it — today, the GUILD itself announcing that a
		// channel-located calendar event is starting (events.go). Same footing
		// as the guest case: Name is display-only and self-asserted everywhere,
		// so this re-labels the line, it does not forge authorship — the
		// signature and sender stay this node's.
		msg.Name = guestName
	}
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
	fpr := accountFingerprintOf(bySender)
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

// Saved messages (bookmarks): device-local, never on any wire.
func (s *Service) BookmarkMessage(messageID, channelID string) error {
	return s.store.BookmarkMessage(messageID, channelID)
}
func (s *Service) UnbookmarkMessage(messageID string) error {
	return s.store.UnbookmarkMessage(messageID)
}
func (s *Service) BookmarkedMessages() ([]domain.Message, error) {
	return s.store.BookmarkedMessages()
}

// DeleteMessage removes one of this peer's own messages for everyone.
// maxPurge bounds one /clear: a slip of the finger shouldn't be able to erase a
// channel, and each delete is its own signed message on the wire.
const maxPurge = 100

func (s *Service) DeleteMessage(channelID, targetID string) error {
	_, err := s.send(channelID, "deleted", "delete", targetID)
	return err
}

// PurgeMessages deletes the most recent n messages in a channel — the "/clear
// 20" moderator broom. It needs MANAGE_MESSAGES here, and every peer re-checks
// that permission when the delete arrives (see applyDelete), so a patched client
// that skips the check convinces nobody.
//
// Only actual chat is swept (normal messages and relayed guest ones): system
// notices and call records are the room's history of itself, not clutter
// someone posted.
func (s *Service) PurgeMessages(channelID string, n int) (int, error) {
	if n <= 0 {
		return 0, fmt.Errorf("app: how many messages? (e.g. /clear 10)")
	}
	if n > maxPurge {
		return 0, fmt.Errorf("app: %d at a time, max", maxPurge)
	}
	s.mu.RLock()
	guildID, ok := s.channelToGuild[channelID]
	s.mu.RUnlock()
	if !ok {
		return 0, fmt.Errorf("app: unknown channel %s", channelID)
	}
	if !s.hasPerm(guildID, PermManageMessages) {
		return 0, fmt.Errorf("app: you need the Manage messages permission to clear messages")
	}
	// Over-fetch: deleted tombstones and notices don't count toward n.
	msgs, err := s.store.Messages(channelID, n*4+40)
	if err != nil {
		return 0, err
	}
	var targets []string
	for i := len(msgs) - 1; i >= 0 && len(targets) < n; i-- {
		m := msgs[i]
		if m.Deleted || (m.Kind != "" && m.Kind != "guest") {
			continue
		}
		targets = append(targets, m.ID)
	}
	for _, id := range targets {
		if err := s.DeleteMessage(channelID, id); err != nil {
			return len(targets), err
		}
	}
	return len(targets), nil
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
		force = s.memberHasPerm(guildID, accountFingerprintOf(bySender), PermManageMessages)
	}
	deleted, ok, err := s.store.MarkDeleted(targetID, bySender, force)
	if err != nil || !ok {
		return
	}
	// DMs get a REAL delete: wipe the content at rest so neither side can recover
	// it in-app (the chosen model — unsend actually unsends). Guild deletes keep
	// the content so a moderator can reveal it (see RevealDeleted).
	s.mu.RLock()
	isDM := false
	if g, gok := s.guilds[guildID]; gok {
		isDM = g.Kind == "dm"
	}
	s.mu.RUnlock()
	if isDM {
		_ = s.store.EraseContent(targetID)
	}
	s.emitMessage(deleted)
}

// ExpireMessage erases THIS device's copy of a disappearing message whose
// embedded TTL has elapsed. It is purely local (no broadcast, no permission
// check): the expiry was set by the message's own MLS-authenticated author and
// travels in the synced content, so every device independently sweeps and
// erases at the same wall-clock time — that's what makes it vanish on all sides
// without any extra coordination. Idempotent; the content is wiped for real
// (EraseContent), so an expired message leaves no recoverable trace here.
func (s *Service) ExpireMessage(channelID, messageID string) error {
	deleted, ok, err := s.store.MarkDeleted(messageID, s.PublicKey(), true)
	if err != nil || !ok {
		return err
	}
	_ = s.store.EraseContent(messageID)
	_ = s.store.MarkExpired(messageID) // label it "disappeared", not "deleted"
	deleted.Expired = true
	s.emitMessage(deleted)
	return nil
}

// EmptyTrash permanently erases the retained bodies of soft-deleted messages so
// "Show original" has nothing left to reveal. With a guildID it scopes to that
// guild's channels; empty scopes to the whole device. Returns rows scrubbed.
func (s *Service) EmptyTrash(guildID string) (int, error) {
	var channelIDs []string
	if guildID != "" {
		s.mu.RLock()
		if g, ok := s.guilds[guildID]; ok {
			for _, c := range g.Channels {
				channelIDs = append(channelIDs, c.ID)
			}
		}
		s.mu.RUnlock()
	}
	n, err := s.store.PurgeDeletedContent(channelIDs)
	if err == nil && n > 0 {
		s.emitGuildUpdate() // reloads messages; revealed content is now gone
	}
	return n, err
}

// RevealDeleted returns the original text of a soft-deleted GUILD message, for a
// moderator. It is gated on MANAGE_MESSAGES here, but the real protection is
// that the content only EXISTS to reveal in guilds — DM deletes erase it (see
// applyDelete), so there is nothing to return for a DM even to its participant.
func (s *Service) RevealDeleted(channelID, messageID string) (string, error) {
	s.mu.RLock()
	guildID, ok := s.channelToGuild[channelID]
	isDM := false
	if g, gok := s.guilds[guildID]; ok && gok {
		isDM = g.Kind == "dm"
	}
	s.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("app: unknown channel %s", channelID)
	}
	if isDM {
		return "", fmt.Errorf("app: deleted direct messages can't be recovered")
	}
	if !s.hasPerm(guildID, PermManageMessages) {
		return "", fmt.Errorf("app: only moderators can view deleted messages")
	}
	return s.store.MessageContent(messageID)
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

	// Recover which leaves are linked devices before anyone can ask us about
	// them; without this a restart makes every quiet second device a stranger.
	s.relearnDevices(g.GroupID)

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
			// A commit is how a leaf appears, and a linked device's leaf carries
			// the certificate that says which account it belongs to. Reading it
			// only at startup (see trackGuild above) meant a device that joined
			// while we were running stayed unplaced for the whole session: its
			// PeerID resolved to its own device key, so it read as a stranger in
			// the peer list, was refused the member-only paths in recordPeer, and
			// only became itself after a restart. The roster is already in hand
			// here; re-reading it is cheap and commits are rare.
			s.relearnDevices(groupID)
			// Membership just moved, which is rememberMembers' whole trigger: the
			// peer we could not place a moment ago may be a member now. Off the
			// gossip callback's goroutine — it writes contacts and flushes the
			// peer cache, and nothing here should hold up commit delivery.
			go s.rememberMembers()
			s.emitGuildUpdate()
			// The epoch just advanced. Any message that arrived moments BEFORE
			// this commit — the two travel different gossip topics, so their
			// order is luck — is sitting in the stash, readable now. Retrying
			// here is what makes the send-during-a-membership-change case
			// deliver in milliseconds instead of waiting for a history sync.
			s.retryPendingCiphertexts(groupID)
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
		// Ephemeral typing signals, attributed to a member ACCOUNT — your own
		// included — or dropped (see receiveTyping: an unlearned device key
		// must never surface as an encoded stranger).
		_ = s.ps.Subscribe(s.ctx, domain.TypingTopicID(groupID, channelID), func(from peer.ID, _ []byte) {
			s.receiveTyping(guildID, groupID, channelID, from)
		})
		// Watch voice presence for every voice channel so the sidebar shows who's
		// in a call without us having to join it. In a DM (and an instant
		// meeting) there's no dedicated voice channel — the single channel
		// doubles as the call room — so watch it too, or nobody sees the ring.
		if c.ChannelType() == "voice" || g.Kind == "dm" || g.Kind == "meeting" {
			s.watchVoice(groupID, channelID)
		}
	}
}

// guildMeta is an MLS-encrypted guild metadata update sent over the guild-meta
// topic so all members converge on shared state (channels, member display
// names). Only the fields relevant to Type are populated.
type guildMeta struct {
	Type        string             `json:"type"` // channel_added | channel_updated | category_added | profile | nickname | guild_renamed
	Channel     domain.Channel     `json:"channel,omitempty"`
	Category    domain.Category    `json:"category,omitempty"`
	Fingerprint string             `json:"fingerprint,omitempty"`
	Name        string             `json:"name,omitempty"`
	Status      string             `json:"status,omitempty"`
	Emoji       string             `json:"emoji,omitempty"`
	Color       string             `json:"color,omitempty"`
	Avatar      string             `json:"avatar,omitempty"`
	Banner      string             `json:"banner,omitempty"` // user profile banner (not the guild banner below)
	Presence    string             `json:"presence,omitempty"`
	Bio         string             `json:"bio,omitempty"`
	MailboxPub  []byte             `json:"mbx,omitempty"`
	Activity    *Activity          `json:"activity,omitempty"`  // structured now-playing (rich presence)
	Games       []Game             `json:"games,omitempty"`     // profile: curated game collection
	Color2      string             `json:"color2,omitempty"`    // profile: gradient partner color
	Frame       string             `json:"frame,omitempty"`     // profile: avatar frame enum id
	Effect      string             `json:"effect,omitempty"`    // profile: card effect enum id
	Style       *Style             `json:"style,omitempty"`     // profile: fine-grained style dials
	UpdatedAt   int64              `json:"updatedAt,omitempty"` // profile: owner's edit stamp (Profile.UpdatedAt)
	CustomEmoji domain.CustomEmoji `json:"customEmoji,omitempty"`
	// Gif carries a guild GIF-pack record (gifs.go). Only the reference travels
	// here — the image itself is an encrypted attachment blob, fetched out of
	// band, because a GIF would not fit in a gossip frame.
	Gif   *GuildGif       `json:"gif,omitempty"`
	GovOp json.RawMessage `json:"govOp,omitempty"` // a signed governance op (roles/bans)
	// guild_profile: icon/banner/description (Name reused from above).
	GuildIcon        string `json:"gIcon,omitempty"`
	GuildBanner      string `json:"gBanner,omitempty"`
	GuildDescription string `json:"gDesc,omitempty"`
	// read_marker (sent over the Notes self-group only — own devices): the user
	// read these channels through the given times (UnixMilli) on some device.
	// ChannelID/At is the single-marker form; Markers carries a coalesced batch.
	// meeting_life reuses At as the instant the meeting room expires.
	ChannelID string           `json:"channelId,omitempty"`
	At        int64            `json:"at,omitempty"`
	Markers   map[string]int64 `json:"markers,omitempty"`

	// forum_tags: replace the tag palette of the forum named by ChannelID.
	// Its own type rather than a field on channel_updated, because that message
	// is built from a bare four-field Channel — folding the palette in would
	// make every "move this channel" announcement wipe the forum's tags.
	ForumTags []domain.ForumTag `json:"forumTags,omitempty"`
	// forum_banner: a FORUM's own artwork — a data URI or "preset:<id>". Named
	// distinctly from Banner above, which is a member's profile banner, because
	// the two ride the same struct and confusing them would put a member's
	// picture on a channel.
	ForumBanner string `json:"forumBanner,omitempty"`
	// post_meta: board state for the forum post named by ChannelID. POINTERS,
	// so "field absent" is distinguishable from "field set to false" — a nil
	// PostSolved means unchanged, not reopen. Each is authorized separately
	// (see applyPostMeta): tagging and answering are the author's or a
	// moderator's, pinning is a moderator's alone.
	PostTags   *[]string `json:"ptags,omitempty"`
	PostPinned *bool     `json:"ppin,omitempty"`
	PostSolved *bool     `json:"psolved,omitempty"`
	PostLocked *bool     `json:"plock,omitempty"`

	// event_upserted carries a full calendar event — create and edit share the
	// lane, told apart on receive by whether the id is already known (events.go
	// gates each arm separately). event_removed and event_rsvp name their
	// target via EventID; RSVP is the SENDER'S OWN answer (going|maybe|no, or
	// "" to clear). Its own lane rather than a field on event_upserted, because
	// an RSVP binds to the authenticated sender while an upsert speaks for the
	// event's author — folding them together would let an author rewrite the
	// attendee list with every edit.
	Event   *domain.Event `json:"event,omitempty"`
	EventID string        `json:"eventId,omitempty"`
	RSVP    string        `json:"rsvp,omitempty"`
}

// applyProfileMeta is the receive half of a gossiped profile announce. Its own
// method (rather than a switch arm) so tests can drive the gate directly, the
// way channel renames do.
func (s *Service) applyProfileMeta(guildID, actor string, m guildMeta) {
	// A profile only speaks for its own author. The self-reported Fingerprint
	// must equal the MLS-authenticated sender, or a member could overwrite any
	// other member's cached identity — and, via MailboxPub, silently redirect
	// their offline mail. Bind it to the authenticated actor.
	if m.Fingerprint != "" && m.Fingerprint != actor {
		return
	}
	// First time we see this member: reply with our own profile so the
	// newcomer learns us too (bounded — only on genuinely new members).
	// Our own account's announce (a linked device speaking to peers) is a
	// no-op here: learnProfile never binds our own fingerprint, and devices
	// converge over the device hello, which carries the STORED profile rather
	// than this presentation copy (see selfStoredProfile).
	if s.learnProfile(actor, Profile{Name: m.Name, Status: m.Status, Emoji: m.Emoji, Color: m.Color, Avatar: m.Avatar, Banner: m.Banner, Presence: m.Presence, Bio: m.Bio, MailboxPub: m.MailboxPub, Activity: m.Activity, Games: m.Games, Color2: m.Color2, Frame: m.Frame, Effect: m.Effect, Style: m.Style, UpdatedAt: m.UpdatedAt}) {
		s.announceProfile(guildID)
	}
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
		Banner: p.Banner, Presence: p.Presence, Bio: p.Bio, MailboxPub: p.MailboxPub,
		Activity: p.Activity, Games: p.Games,
		Color2: p.Color2, Frame: p.Frame, Effect: p.Effect, Style: p.Style,
		UpdatedAt: p.UpdatedAt,
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
// nickAllowed decides whether `actor` may set `target`'s nickname in a guild.
// You may always rename yourself. Renaming someone else needs MANAGE_MEMBERS —
// and you must outrank them, so a moderator cannot rename the owner or a peer
// of equal standing. Same shape as the kick/ban gate: authority is never
// self-asserted, it's replayed from the signed op log.
func (s *Service) nickAllowed(guildID, actor, target string) bool {
	if actor == "" || target == "" {
		return false
	}
	if actor == target {
		return true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.guilds[guildID]; !ok {
		return false
	}
	owner := s.effectiveOwnerLocked(guildID)
	st := s.govState[guildID]
	if !st.Can(owner, actor, PermManageMembers) {
		return false
	}
	if target == owner {
		return false // nobody renames the owner but the owner
	}
	return actor == owner || st.topPosition(owner, actor) > st.topPosition(owner, target)
}

// SetMemberNickname sets ANOTHER member's per-guild nickname (a moderator
// action). The same gate runs on every peer that receives it, so a client that
// skips this check convinces nobody.
func (s *Service) SetMemberNickname(guildID, fingerprint, nick string) error {
	nick = strings.TrimSpace(nick)
	if len(nick) > maxNameBytes {
		nick = nick[:maxNameBytes]
	}
	if !s.nickAllowed(guildID, s.id.Fingerprint(), fingerprint) {
		return fmt.Errorf("app: you can't change that member's nickname")
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
	s.rememberNick(guildID, fingerprint, nick)
	s.publishMeta(groupID, guildMeta{Type: "nickname", Fingerprint: fingerprint, Name: nick})
	s.emitGuildUpdate()
	return nil
}

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
	case "", "text", "voice", "announcement", "forum":
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
	// A forum POST is member content — CreateThread needs no permission, so
	// requiring one to remove it would let anyone start a thread nobody but a
	// moderator could ever take back. Its author may delete it; so may a
	// moderator holding Manage Messages, which is the bit that already governs
	// removing what members wrote. Every OTHER kind of channel still needs
	// Manage Channels.
	if _, _, isPost := s.postAndForum(guildID, channelID); isPost {
		if !s.mayCuratePost(guildID, channelID, s.id.Fingerprint()) {
			return fmt.Errorf("app: only the author or a moderator can delete this post")
		}
	} else if !s.hasPerm(guildID, PermManageChannels) {
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
// channelGuild returns the guild a channel is currently mapped to.
func (s *Service) channelGuild(channelID string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.channelToGuild[channelID]
	return g, ok
}

// channelInGuild reports whether a channel is known to belong to guildID. Used
// to scope inbound guild-meta store mutations so a member of one guild cannot
// touch another guild's channel by naming its ID.
func (s *Service) channelInGuild(channelID, guildID string) bool {
	g, ok := s.channelGuild(channelID)
	return ok && g == guildID
}

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
	_ = s.store.DeleteReadState([]string{channelID})
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
// validChannelType reports whether a channel type is one we know how to render.
// The type now travels on channel_updated whenever anyone converts a channel,
// so it's worth saying explicitly what's allowed rather than storing whatever
// string turns up: an unknown type renders as nothing in particular, and a peer
// with ManageChannels shouldn't be able to put a channel into a state the UI
// has no case for. "" is accepted and means text, as it always has.
func validChannelType(t string) bool {
	switch t {
	case "", "text", "voice", "announcement", "forum", "thread":
		return true
	}
	return false
}

func (s *Service) SetChannelMeta(guildID, channelID, ctype, category string, position int, topic string) error {
	if !s.hasPerm(guildID, PermManageChannels) {
		return fmt.Errorf("app: you don't have permission to manage channels")
	}
	if !validChannelType(ctype) {
		return fmt.Errorf("app: unknown channel type %q", ctype)
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
	if ctype == "voice" {
		s.watchVoice(groupID, channelID) // idempotent; see receiveGuildMeta
	}
	s.emitGuildUpdate()
	s.publishMeta(groupID, guildMeta{Type: "channel_updated",
		Channel: domain.Channel{ID: channelID, GuildID: guildID, Type: ctype, Category: category, Position: position, Topic: topic}})
	return nil
}

// RenameChannel renames a channel for everyone (ManageChannels). The name rides
// its own meta type — channel_updated deliberately carries a bare four-field
// Channel, so folding the name in there would erase it on every move.
func (s *Service) RenameChannel(guildID, channelID, name string) error {
	if !s.hasPerm(guildID, PermManageChannels) {
		return fmt.Errorf("app: you don't have permission to manage channels")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("app: channel name is empty")
	}
	if len(name) > 80 {
		return fmt.Errorf("app: channel name is too long (80 characters max)")
	}
	updated, groupID, ok := s.mutateChannel(guildID, channelID, func(c *domain.Channel) bool {
		if c.Name == name {
			return false
		}
		c.Name = name
		return true
	})
	if !ok {
		if updated.Name == name {
			return nil // renamed to itself: nothing to say
		}
		return fmt.Errorf("app: unknown channel %s", channelID)
	}
	_ = s.store.UpdateChannelName(channelID, name)
	s.publishMeta(groupID, guildMeta{Type: "channel_renamed", ChannelID: channelID, Name: name})
	return nil
}

// applyChannelRename is the receive half of RenameChannel, mirroring its checks.
func (s *Service) applyChannelRename(guildID, actor, channelID, name string) {
	if channelID == "" || !s.memberHasPerm(guildID, actor, PermManageChannels) {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 80 {
		return
	}
	if !s.channelInGuild(channelID, guildID) {
		return
	}
	s.mutateChannel(guildID, channelID, func(c *domain.Channel) bool {
		if c.Name == name {
			return false
		}
		c.Name = name
		return true
	})
	_ = s.store.UpdateChannelName(channelID, name)
}

// SetChannelLinks records which channels an ANNOUNCEMENT channel publishes to
// (ManageChannels). Links must be text channels of the same guild; the
// announcement channel itself and anything unknown are dropped. Synced to
// members over the channel_updated meta lane, like categories.
func (s *Service) SetChannelLinks(guildID, channelID string, links []string) error {
	if !s.hasPerm(guildID, PermManageChannels) {
		return fmt.Errorf("app: you don't have permission to manage channels")
	}
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var groupID []byte
	valid := map[string]bool{}
	if ok {
		groupID = g.GroupID
		for _, c := range g.Channels {
			if c.ID != channelID && c.ChannelType() == "text" && c.Parent == "" {
				valid[c.ID] = true
			}
		}
	}
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("app: unknown guild %s", guildID)
	}
	var clean []string
	seen := map[string]bool{}
	for _, l := range links {
		if valid[l] && !seen[l] {
			seen[l] = true
			clean = append(clean, l)
		}
	}
	s.mu.Lock()
	var updated domain.Channel
	for i := range g.Channels {
		if g.Channels[i].ID == channelID {
			g.Channels[i].Links = clean
			updated = g.Channels[i]
		}
	}
	gc := *g
	s.mu.Unlock()
	if updated.ID == "" {
		return fmt.Errorf("app: unknown channel")
	}
	_ = s.store.SaveGuild(gc)
	s.emitGuildUpdate()
	s.publishMeta(groupID, guildMeta{Type: "channel_updated", Channel: updated})
	return nil
}

// CreateThread opens a forum POST: a thread channel nested under a forum.
// Unlike CreateChannel this needs no ManageChannels — posts are member
// content, exactly like messages. The creator's first message rides along, as do
// the tags they picked (validated against the forum's own palette, which we
// certainly hold on this path).
func (s *Service) CreateThread(guildID, forumID, title, firstMessage string, tagIDs []string) (domain.Channel, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return domain.Channel{}, fmt.Errorf("app: a post needs a title")
	}
	if len(title) > maxNameBytes {
		title = title[:maxNameBytes]
	}
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var groupID []byte
	var palette []domain.ForumTag
	isForum := false
	if ok {
		groupID = g.GroupID
		for _, c := range g.Channels {
			if c.ID == forumID && c.Type == "forum" {
				isForum = true
				palette = c.ForumTags
			}
		}
	}
	s.mu.RUnlock()
	if !ok {
		return domain.Channel{}, fmt.Errorf("app: unknown guild %s", guildID)
	}
	if !isForum {
		return domain.Channel{}, fmt.Errorf("app: posts can only be created in a forum channel")
	}
	if len(tagIDs) > maxPostTags {
		return domain.Channel{}, fmt.Errorf("app: a post can carry at most %d tags", maxPostTags)
	}
	known := make(map[string]bool, len(palette))
	for _, t := range palette {
		known[t.ID] = true
	}
	for _, id := range tagIDs {
		if !known[id] {
			return domain.Channel{}, fmt.Errorf("app: this forum has no tag %q", id)
		}
	}

	ch := domain.Channel{
		ID: domain.NewID(), GuildID: guildID, Name: title, Type: "thread", Parent: forumID,
		Tags: sanitizePostTags(tagIDs),
	}
	s.addChannel(guildID, ch)
	payload, _ := json.Marshal(guildMeta{Type: "channel_added", Channel: ch})
	ct, err := s.mls.Encrypt(s.ctx, groupID, payload)
	if err != nil {
		return domain.Channel{}, fmt.Errorf("app: encrypt guild meta: %w", err)
	}
	if err := s.ps.Publish(s.ctx, domain.GuildMetaTopicID(groupID), ct); err != nil {
		return domain.Channel{}, err
	}
	if strings.TrimSpace(firstMessage) != "" {
		if _, err := s.SendMessage(ch.ID, firstMessage, ""); err != nil {
			return ch, nil // the post exists; the body can be retyped
		}
	}
	return ch, nil
}

// ChannelLastActivity returns the newest message time (UnixNano) in one
// channel — drives forum post ordering.
func (s *Service) ChannelLastActivity(channelID string) int64 {
	t, err := s.store.LatestTimestamp(channelID)
	if err != nil {
		return 0
	}
	return t
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
	// Scrub the forum metadata HERE, at the one funnel every channel record from
	// every source passes through: a peer's channel_added, a history-sync snapshot
	// (which adopts a peer-supplied domain.Channel wholesale), and our own
	// creation paths. Validating only the gossip path would leave sync free to
	// hand us a ten-thousand-entry tag palette whose colour is
	// "#fff;background:url(…)" — and sync is the call site that gets forgotten.
	sanitizeForumMeta(&ch)
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
		s.receiveTyping(guildID, groupID, channelID, from)
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

// SendTyping broadcasts an ephemeral "is typing" hint for a channel. With the
// indicator switched off (see typing.go) nothing is published at all — the
// setting is enforced by not sending, which is the only enforcement a
// serverless design can offer and the only one worth trusting.
func (s *Service) SendTyping(channelID string) error {
	if !s.TypingEnabled() {
		return nil
	}
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
	if !s.TypingEnabled() {
		return // reciprocal: not sending means not seeing
	}
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
	// MLS proves msg.SenderID is *some* group member; the account fingerprint is
	// the authenticated actor for every authorization decision below. Decryption
	// alone is NOT authorization — admin metadata (channels, emoji, rename,
	// profile) must re-check the sender's permission on the receive side, or a
	// patched client could rewrite the guild on every honest peer.
	actor := accountFingerprintOf(msg.SenderID)
	switch m.Type {
	case "read_marker":
		// Read markers only ever travel the Notes self-group, and only OUR OWN
		// account's devices may move our read cursor — a marker authored by
		// anyone else is dropped (MLS authenticates the sender, so this holds).
		if actor != s.id.Fingerprint() {
			return
		}
		if m.ChannelID != "" {
			s.applyRemoteReadMarker(m.ChannelID, m.At)
		}
		for ch, at := range m.Markers {
			if ch != "" {
				s.applyRemoteReadMarker(ch, at)
			}
		}
		return
	case "channel_added":
		if !s.mayAddChannel(guildID, actor, m.Channel) {
			return
		}
		// Refuse to bind a channel ID that already belongs to another guild — a
		// stray/hostile channel_added must not hijack an existing mapping.
		if g, ok := s.channelGuild(m.Channel.ID); ok && g != guildID {
			return
		}
		m.Channel.GuildID = guildID
		// A brand-new post arrives with the tags its author chose, but pinning is a
		// moderator act with its own permission — a member cannot open a post
		// pre-pinned to the top of the board. (Structural validation of the tags
		// themselves happens in addChannel, which every source funnels through.)
		if !s.memberHasPerm(guildID, actor, PermManageMessages) {
			m.Channel.Pinned = false
		}
		s.addChannel(guildID, m.Channel)
	case "channel_updated":
		if m.Channel.ID == "" {
			return
		}
		if !s.memberHasPerm(guildID, actor, PermManageChannels) || !s.channelInGuild(m.Channel.ID, guildID) {
			return
		}
		if !validChannelType(m.Channel.Type) {
			return
		}
		_ = s.store.UpdateChannelMeta(m.Channel.ID, m.Channel.Type, m.Channel.Category, m.Channel.Position, m.Channel.Topic)
		s.mu.Lock()
		var gc domain.Guild
		if g, ok := s.guilds[guildID]; ok {
			for i := range g.Channels {
				if g.Channels[i].ID == m.Channel.ID {
					g.Channels[i].Type = m.Channel.Type
					g.Channels[i].Category = m.Channel.Category
					g.Channels[i].Position = m.Channel.Position
					g.Channels[i].Topic = m.Channel.Topic
					g.Channels[i].Links = m.Channel.Links
				}
			}
			gc = *g
		}
		s.mu.Unlock()
		if gc.ID != "" {
			_ = s.store.SaveGuild(gc) // persists links/parent too
		}
		// A channel that just became voice needs its presence topic watched, the
		// same as one created as voice (addChannel does this). Without it a
		// converted channel showed an empty roster until the next launch, when
		// trackGuild subscribes from the stored type — and channel conversion is
		// an ordinary action now, not a curiosity. watchVoice is idempotent, so
		// the other conversions cost nothing.
		if m.Channel.Type == "voice" && gc.ID != "" {
			s.watchVoice(gc.GroupID, m.Channel.ID)
		}
		s.emitGuildUpdate()
	case "category_added":
		if m.Category.ID == "" {
			return
		}
		if !s.memberHasPerm(guildID, actor, PermManageChannels) {
			return
		}
		m.Category.GuildID = guildID
		_ = s.store.SaveCategory(m.Category)
		s.emitGuildUpdate()
	case "channel_removed":
		if m.Channel.ID != "" {
			if !s.memberHasPerm(guildID, actor, PermManageChannels) || !s.channelInGuild(m.Channel.ID, guildID) {
				return
			}
			s.applyChannelRemoved(guildID, m.Channel.ID)
		}
	case "category_removed":
		if m.Category.ID != "" {
			if !s.memberHasPerm(guildID, actor, PermManageChannels) {
				return
			}
			s.applyCategoryRemoved(guildID, m.Category.ID)
		}
	case "emoji_added":
		if !s.memberHasPerm(guildID, actor, PermManageGuild) {
			return
		}
		s.applyCustomEmoji(guildID, m.CustomEmoji)
		s.emitGuildUpdate()
	case "emoji_removed":
		if m.CustomEmoji.Name != "" {
			if !s.memberHasPerm(guildID, actor, PermManageGuild) {
				return
			}
			_ = s.store.DeleteCustomEmoji(guildID, m.CustomEmoji.Name)
			s.emitGuildUpdate()
		}
	case "forum_tags":
		// Permission gating and the strict field validation (hex colour, bounded
		// name, id charset) live in forum.go, so this path and the local one share
		// one implementation — a peer's tag colour reaches a CSS context in the
		// client and is no more trustworthy than one we typed ourselves.
		s.applyForumTags(guildID, actor, m.ChannelID, m.ForumTags)
	case "channel_renamed":
		// Same shape as every other channel mutation: gate on the ACTOR holding
		// ManageChannels in our copy of the roster, validate exactly as the
		// local path does, and ignore junk rather than clearing anything.
		s.applyChannelRename(guildID, actor, m.ChannelID, m.Name)
	case "event_upserted":
		// Creation is open to any member; edits re-check author-or-moderator on
		// the receive side. All gating lives in events.go so this path and the
		// local one share one implementation.
		if m.Event != nil {
			s.applyEventUpsert(guildID, actor, *m.Event)
		}
	case "event_removed":
		s.applyEventRemove(guildID, actor, m.EventID)
	case "event_rsvp":
		// The answer binds to the MLS-authenticated actor — the payload cannot
		// name a target, so nobody RSVPs on someone else's behalf.
		_ = s.applyEventRSVP(guildID, actor, m.EventID, m.RSVP)
	case "forum_banner":
		// Same gating and the same two-shape validation as the local path — a
		// peer's banner string reaches the same CSS context ours does.
		s.applyForumBanner(guildID, actor, m.ChannelID, m.ForumBanner)
	case "post_meta":
		s.applyPostMeta(guildID, actor, m)
	case "gif_added", "gif_removed":
		// Permission gating and strict field validation live in gifs.go, so the
		// receive path and the local add path share one implementation.
		s.applyGifMeta(guildID, actor, m.Type, m.Gif)
	case "guild_renamed":
		if strings.TrimSpace(m.Name) == "" {
			return
		}
		if !s.memberHasPerm(guildID, actor, PermManageGuild) {
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
	case "meeting_life":
		// Whoever minted the meeting's guest link chose how long the room lives;
		// this is how the other participants learn it, so their startup sweep
		// doesn't delete a room whose link is still valid for days. Bounded by
		// maxMeetingLifetime — a peer must not be able to make a room immortal on
		// our disk — and only from someone with authority over the guild (in a
		// meeting, its creator).
		if m.At <= 0 || !s.memberHasPerm(guildID, actor, PermManageGuild) {
			return
		}
		at := time.UnixMilli(m.At)
		s.mu.Lock()
		g, ok := s.guilds[guildID]
		if !ok || g.Kind != "meeting" || at.After(g.Created.Add(maxMeetingLifetime)) {
			s.mu.Unlock()
			return
		}
		s.meetingLife[guildID] = at
		s.mu.Unlock()
		s.saveMeetingLife()
		s.emitGuildUpdate()
	case "guild_profile":
		if !s.memberHasPerm(guildID, actor, PermManageGuild) {
			return
		}
		// Name/icon/banner/description update, validated exactly as a local set
		// is — a remote peer's string is no more trustworthy than our own, and
		// the banner reaches a CSS context in the client. A preset id is allowed
		// here for the same reason it is allowed locally, and bounded to the same
		// charset, so a peer cannot smuggle a quote or a url() through it.
		bannerOK := m.GuildBanner == "" ||
			validImageDataURI(m.GuildBanner, maxGuildImageBytes) ||
			(strings.HasPrefix(m.GuildBanner, presetPrefix) &&
				validPresetID(strings.TrimPrefix(m.GuildBanner, presetPrefix)))
		if (m.GuildIcon != "" && !validImageDataURI(m.GuildIcon, maxGuildImageBytes)) || !bannerOK {
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
		s.applyProfileMeta(guildID, actor, m)
	case "nickname":
		// A per-guild nickname. Two legitimate authors: the member themselves,
		// or a moderator with MANAGE_MEMBERS renaming someone (Discord-style).
		// EVERY peer checks this independently against the replayed op log —
		// the payload names its target, so without this check any member could
		// rename anyone on everyone else's screen. MLS authenticates SenderID,
		// which is what makes the check meaningful.
		if m.Fingerprint == "" {
			return
		}
		if !s.nickAllowed(guildID, actor, m.Fingerprint) {
			return
		}
		if m.Fingerprint == s.id.Fingerprint() {
			// Someone renamed US. Keep it — but it must have passed the same gate.
			nick := m.Name
			if len(nick) > maxNameBytes {
				nick = nick[:maxNameBytes]
			}
			s.rememberNick(guildID, m.Fingerprint, nick)
			s.emitGuildUpdate()
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

// flagUndecryptable marks the guild owning groupID as out of sync, so the heal
// path runs and the UI stops pretending everything is fine. Rate-limited to one
// flag per group per healRetryInterval: a busy channel can deliver many
// undecryptableRecoverWindow rate-limits how often an unreadable message may
// kick the recovery machinery for one group. A channel full of unreadable
// traffic must raise the alarm once per beat, not once per packet — but the
// beat has to be short, because between beats an arriving message is simply
// lost until anti-entropy: this window IS the floor on how long a member can
// stay silently deaf.
const undecryptableRecoverWindow = 5 * time.Second

// flagUndecryptable reacts to a message we could not read: stash aside for a
// retry (the commit it needs is usually milliseconds behind it), and kick the
// guild's recovery — history-sync first, re-add heal only if syncing every
// reachable member could not bridge us. See recoverOutOfSync for the order and
// why.
func (s *Service) flagUndecryptable(groupID []byte) {
	key := string(groupID)
	s.mu.Lock()
	if s.lastUndecryptable == nil {
		s.lastUndecryptable = map[string]time.Time{}
	}
	if t, ok := s.lastUndecryptable[key]; ok && time.Since(t) < undecryptableRecoverWindow {
		s.mu.Unlock()
		return
	}
	s.lastUndecryptable[key] = time.Now()
	var guildID string
	for id, g := range s.guilds {
		if bytes.Equal(g.GroupID, groupID) {
			guildID = id
			break
		}
	}
	s.mu.Unlock()
	if guildID != "" {
		go s.recoverOutOfSync(guildID)
	}
}

// pendingCipher is a channel ciphertext we could not decrypt yet — almost
// always a message that outran its own membership commit across two different
// gossip topics. Kept briefly and retried when the epoch advances.
type pendingCipher struct {
	ct    []byte
	added time.Time
}

// pendingCipherTTL bounds how long an unreadable ciphertext is kept for retry.
// Long enough for several recovery rounds; short enough that garbage from a
// broken or hostile peer cannot accumulate.
const pendingCipherTTL = 2 * time.Minute

// pendingCipherCap bounds the stash per group (newest win — the older a
// ciphertext, the more likely history sync already delivered its content).
const pendingCipherCap = 64

// stashCiphertext keeps an undecryptable channel message for a later retry.
func (s *Service) stashCiphertext(groupID, ct []byte) {
	key := string(groupID)
	s.pendingCTMu.Lock()
	defer s.pendingCTMu.Unlock()
	if s.pendingCT == nil {
		s.pendingCT = map[string][]pendingCipher{}
	}
	q := s.pendingCT[key]
	if len(q) >= pendingCipherCap {
		q = q[1:]
	}
	s.pendingCT[key] = append(q, pendingCipher{ct: ct, added: time.Now()})
}

// retryPendingCiphertexts re-attempts every stashed ciphertext for a group.
// Called whenever the group's epoch moves (a commit applied live, commits
// bridged by history sync, a heal's re-join) — the moments a message that
// arrived early could suddenly become readable. A gossip mesh delivers a
// message exactly once, so this retry is the only way an in-flight message
// survives racing its own membership commit.
func (s *Service) retryPendingCiphertexts(groupID []byte) {
	key := string(groupID)
	s.pendingCTMu.Lock()
	q := s.pendingCT[key]
	delete(s.pendingCT, key)
	s.pendingCTMu.Unlock()
	if len(q) == 0 {
		return
	}
	var keep []pendingCipher
	for _, p := range q {
		// deliverCiphertext, not receiveCiphertext: a still-unreadable entry
		// must be re-queued by age here, not re-stashed as brand new (which
		// would also re-kick recovery and make the stash immortal).
		if !s.deliverCiphertext(groupID, p.ct) && time.Since(p.added) < pendingCipherTTL {
			keep = append(keep, p)
		}
	}
	if len(keep) > 0 {
		s.pendingCTMu.Lock()
		if s.pendingCT == nil {
			s.pendingCT = map[string][]pendingCipher{}
		}
		s.pendingCT[key] = append(keep, s.pendingCT[key]...)
		s.pendingCTMu.Unlock()
	}
}

// pendingCiphertexts reports how many unreadable messages a group is sitting
// on — recoverOutOfSync's measure of whether recovery actually recovered.
func (s *Service) pendingCiphertexts(groupID []byte) int {
	s.pendingCTMu.Lock()
	defer s.pendingCTMu.Unlock()
	return len(s.pendingCT[string(groupID)])
}

// receiveCiphertext handles an inbound channel message: decrypt, persist,
// notify the UI. A message we cannot read yet is kept and retried — see
// deliverCiphertext for the delivery half and the stash for the retry half.
func (s *Service) receiveCiphertext(groupID, ct []byte) {
	if s.deliverCiphertext(groupID, ct) {
		return
	}
	// A message arrived, was addressed to a group we are in, and we could not
	// read it. Dropping that silently is how a conversation becomes a black
	// hole: the transport is demonstrably fine — typing indicators travel the
	// same topics UNENCRYPTED and keep arriving — so both people watch each
	// other type and neither ever receives a word, with nothing anywhere
	// saying why.
	//
	// The cause is almost always a ratchet epoch we have not caught up to —
	// often by mere milliseconds, when a message and the membership commit
	// before it travel different gossip topics and arrive out of order. So:
	// keep the ciphertext for a retry (in the racing-commit case that recovers
	// the message with no network round trip at all), and kick recovery, which
	// pulls the missing commits from a peer's log and only escalates to a
	// re-add heal if no reachable member can bridge us. The "Catching up…"
	// banner appears only when recovery finds a real gap — not for a
	// two-millisecond race.
	s.stashCiphertext(groupID, ct)
	s.flagUndecryptable(groupID)
}

// deliverCiphertext decrypts and delivers one channel message, reporting
// whether decryption succeeded. All post-decryption filtering (untracked
// channel, mutes, duplicates) still counts as delivered — those messages are
// finished, not pending.
func (s *Service) deliverCiphertext(groupID, ct []byte) bool {
	msg, err := s.mls.Decrypt(s.ctx, groupID, ct)
	if err != nil {
		return false
	}
	var m domain.Message
	if err := json.Unmarshal(msg.Plaintext, &m); err != nil {
		return true
	}
	// Ignore messages for channels we no longer track (e.g. a guild we left but
	// whose gossipsub subscription is still live this session).
	s.mu.RLock()
	_, tracked := s.channelToGuild[m.ChannelID]
	s.mu.RUnlock()
	if !tracked {
		return true
	}
	// Learn the sender's device→account mapping (if it's a device cert) so their
	// linked-device PeerID resolves to the account in presence/roster views.
	s.learnDeviceCert(msg.SenderID)
	// Trust MLS's authenticated sender over the self-reported field, and
	// normalize it to the account key: a message from any of an account's linked
	// devices (whose leaf credential is a device cert) is attributed to the one
	// account, and every stored/compared Sender is a uniform 32-byte account key.
	m.Sender = accountKeyOf(msg.SenderID)
	switch m.Kind {
	case "delete":
		s.applyDelete(m.ReplyTo, m.Sender, m.ChannelID)
		return true
	case "reaction":
		s.applyReaction(m.ReplyTo, m.Content, m.Sender)
		return true
	case "edit":
		s.applyEdit(m.ReplyTo, m.Content, m.Sender)
		return true
	case "pin":
		s.applyPin(m.ReplyTo)
		return true
	}
	// Advisory mute: drop a normal message from a member who is currently muted.
	s.mu.RLock()
	guildID := s.channelToGuild[m.ChannelID]
	s.mu.RUnlock()
	if guildID != "" && s.isMuted(guildID, accountFingerprintOf(m.Sender)) {
		return true
	}
	// Same for a closed forum post. Refusing only on the SEND side would make
	// closing a thread a polite request to the one person who was never going to
	// ignore it; dropping on receive is what makes every honest client agree the
	// conversation is over.
	if s.postIsLocked(m.ChannelID) {
		return true
	}
	// Backfill a display name from the message if we don't know this member's
	// name yet, so the roster and chat stay consistent.
	s.learnNameHint(accountFingerprintOf(m.Sender), m.Name)
	inserted, err := s.store.SaveMessage(m)
	if err != nil || !inserted {
		return true // duplicate (gossip re-delivery or already synced): stay silent
	}
	s.emitMessage(m)
	// A message landing in a closed DM reopens the conversation (Discord
	// behavior: closing hides it, new activity surfaces it again).
	if guildID != "" {
		s.unhideDM(guildID)
	}
	return true
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
	_ = SaveBootstrap(s.dataDir, merged)

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

// presetPrefix marks a banner that names an entry in the client's preset
// library rather than carrying an image. See SetGuildProfile.
const presetPrefix = "preset:"

// validPresetID bounds a preset id to a charset that cannot escape the CSS
// context the client renders it in. Validated on the way in AND wherever a
// peer's guild metadata is learned, because a remote peer's string is no more
// trustworthy than a local one.
func validPresetID(id string) bool {
	if id == "" || len(id) > 32 {
		return false
	}
	for _, r := range id {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}
