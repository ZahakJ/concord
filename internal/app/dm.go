package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/zahak/concord/internal/domain"
)

// Direct messages are ordinary MLS groups tagged kind="dm", rendered without
// server chrome. The simplest is the self-DM ("Notes") — a dm group with a
// single member (you) — a private, end-to-end-encrypted scratchpad that syncs
// across your own devices once device-linking lands. Peer DMs (a 2-person dm
// group set up via the friend handshake) build on this same shape.

const notesGuildName = "Notes"

// dmStateKey is the settings-table key holding persisted DM lifecycle state.
const dmStateKey = "dm.state"

// dmState is the persisted shape of the DM bookkeeping that must survive a
// restart: which conversations are closed (hidden), who each 1:1 DM is FOR,
// and which invitees are still pending delivery.
type dmState struct {
	Hidden  []string            `json:"hidden,omitempty"`
	Peers   map[string]string   `json:"peers,omitempty"`
	Pending map[string][]string `json:"pending,omitempty"`
}

// persistDMState snapshots hiddenDMs/dmPeers/pendingDMInvites into the settings
// table. Called after every mutation; cheap (one small JSON row).
func (s *Service) persistDMState() {
	var st dmState
	s.mu.RLock()
	for id := range s.hiddenDMs {
		st.Hidden = append(st.Hidden, id)
	}
	if len(s.dmPeers) > 0 {
		st.Peers = make(map[string]string, len(s.dmPeers))
		for id, f := range s.dmPeers {
			st.Peers[id] = f
		}
	}
	s.mu.RUnlock()
	s.dmInviteMu.Lock()
	if len(s.pendingDMInvites) > 0 {
		st.Pending = make(map[string][]string, len(s.pendingDMInvites))
		for id, set := range s.pendingDMInvites {
			for f := range set {
				st.Pending[id] = append(st.Pending[id], f)
			}
		}
	}
	s.dmInviteMu.Unlock()
	if raw, err := json.Marshal(st); err == nil {
		_ = s.store.SetSetting(dmStateKey, string(raw))
	}
}

// loadDMState restores persisted DM lifecycle state at startup, dropping
// entries for guilds that no longer exist locally.
func (s *Service) loadDMState() {
	raw, err := s.store.GetSetting(dmStateKey)
	if err != nil || raw == "" {
		return
	}
	var st dmState
	if json.Unmarshal([]byte(raw), &st) != nil {
		return
	}
	s.mu.Lock()
	for _, id := range st.Hidden {
		if _, ok := s.guilds[id]; ok {
			s.hiddenDMs[id] = true
		}
	}
	for id, f := range st.Peers {
		if _, ok := s.guilds[id]; ok {
			s.dmPeers[id] = f
		}
	}
	known := make(map[string]bool, len(s.guilds))
	for id := range s.guilds {
		known[id] = true
	}
	s.mu.Unlock()
	s.dmInviteMu.Lock()
	for id, fprs := range st.Pending {
		if !known[id] {
			continue
		}
		set := map[string]bool{}
		for _, f := range fprs {
			if f != "" {
				set[f] = true
			}
		}
		if len(set) > 0 {
			s.pendingDMInvites[id] = set
		}
	}
	s.dmInviteMu.Unlock()
}

// hideDM closes a DM conversation: it disappears from the UI but stays fully
// alive underneath, so history is kept and a new message reopens it.
func (s *Service) hideDM(guildID string) {
	s.mu.Lock()
	s.hiddenDMs[guildID] = true
	s.mu.Unlock()
	s.persistDMState()
	s.emitGuildUpdate()
}

// unhideDM reopens a closed DM. Reports whether it was hidden (so callers can
// skip redundant UI refreshes).
func (s *Service) unhideDM(guildID string) bool {
	s.mu.Lock()
	was := s.hiddenDMs[guildID]
	if was {
		delete(s.hiddenDMs, guildID)
	}
	s.mu.Unlock()
	if was {
		s.persistDMState()
		s.emitGuildUpdate()
	}
	return was
}

// dmOtherAccounts returns the deduped ACCOUNT fingerprints of a group's members
// besides ourselves. Linked devices collapse into their account, so a peer with
// a phone and a desktop counts as ONE other person — every "how many people are
// in this DM" decision must use this, never the raw leaf count. ok=false means
// membership couldn't be resolved (transient MLS trouble) — callers must treat
// that as "unknown", never as "empty".
func (s *Service) dmOtherAccounts(groupID []byte) (others []string, ok bool) {
	creds, err := s.mls.Members(s.ctx, groupID)
	if err != nil {
		return nil, false
	}
	self := s.id.Fingerprint()
	seen := map[string]bool{}
	for _, c := range creds {
		if f := accountFingerprintOf(c); f != self && !seen[f] {
			seen[f] = true
			others = append(others, f)
		}
	}
	return others, true
}

// NotesDM returns your personal self-DM, creating it on first use. It is a
// one-member MLS group, so nothing leaves your device until you have linked a
// second one.
func (s *Service) NotesDM() (domain.Guild, error) {
	s.mu.RLock()
	for _, g := range s.guilds {
		if g.Kind == "dm" && len(g.OwnerID) > 0 && string(g.OwnerID) == string(s.PublicKey()) && g.Name == notesGuildName {
			gc := *g
			s.mu.RUnlock()
			s.unhideDM(gc.ID) // opening Notes always surfaces it
			return gc, nil
		}
	}
	s.mu.RUnlock()

	gid, err := s.mls.CreateGroup(s.ctx)
	if err != nil {
		return domain.Guild{}, fmt.Errorf("app: create notes group: %w", err)
	}
	g := domain.NewGuild(notesGuildName, gid, s.PublicKey())
	g.Kind = "dm"
	g.Channels[0].Name = "notes"
	if err := s.store.SaveGuild(g); err != nil {
		return domain.Guild{}, err
	}
	s.trackGuild(&g)
	return g, nil
}

type dmInvite struct {
	Code string `json:"code"`
	// Kind "" = a DM invite (auto-redeemed, historic behaviour). Kind "guild" =
	// someone adding you to a SERVER: that is never auto-redeemed. Redeeming an
	// invite is what puts you in the MLS group, so auto-joining and then quietly
	// discarding it would leave a GHOST member in the inviter's roster — they'd
	// see you in the server you never joined. We offer it instead, and only the
	// invitee's "yes" redeems the code.
	Kind  string `json:"kind,omitempty"`
	Guild string `json:"guild,omitempty"` // server name, for the prompt
}

// NewDMInvite creates a fresh 2-person DM group owned by this peer and returns
// a shareable invite code for it — so you can start a direct conversation with
// someone you DON'T already share a guild with (they paste the code to join, no
// guild required). The first person to redeem it becomes the other party; more
// redeemers make it a group DM.
func (s *Service) NewDMInvite() (string, error) {
	gid, err := s.mls.CreateGroup(s.ctx)
	if err != nil {
		return "", fmt.Errorf("app: create dm group: %w", err)
	}
	g := domain.NewGuild("Direct message", gid, s.PublicKey())
	g.Kind = "dm"
	g.Channels[0].Name = "dm"
	if err := s.store.SaveGuild(g); err != nil {
		return "", err
	}
	s.trackGuild(&g)
	return s.InviteCode(g.ID)
}

// groupDMMax bounds a group DM so it stays a small conversation, not a guild.
const groupDMMax = 10

// CreateGroupDM opens a group direct message with the given fingerprints. Every
// invitee must be a VERIFIED contact — verification is the trust gate for group
// DMs (you pull people you've confirmed into a private group, Discord-style but
// stricter). The group is an ordinary kind="dm" MLS group owned by this peer;
// each reachable invitee is pushed the standard DM invite and auto-redeems it.
// Unreachable invitees simply don't join yet (they can be re-invited later).
func (s *Service) CreateGroupDM(fingerprints []string) (domain.Guild, error) {
	verified, err := s.store.VerifiedFingerprints()
	if err != nil {
		verified = map[string]bool{}
	}
	self := s.id.Fingerprint()
	seen := map[string]bool{}
	var targets []string
	for _, f := range fingerprints {
		if f == "" || f == self || seen[f] {
			continue
		}
		seen[f] = true
		if !verified[f] {
			return domain.Guild{}, fmt.Errorf("app: everyone in a group DM must be a verified contact — verify %s first", shortFpr(f))
		}
		targets = append(targets, f)
	}
	if len(targets) < 2 {
		return domain.Guild{}, fmt.Errorf("app: a group DM needs at least two other people")
	}
	if len(targets) > groupDMMax {
		return domain.Guild{}, fmt.Errorf("app: a group DM tops out at %d people", groupDMMax)
	}

	// If we already have a DM with exactly this set of people, reuse it instead
	// of spawning a duplicate conversation (reopening it if it was closed).
	if g := s.findDMByMembers(targets); g != nil {
		s.unhideDM(g.ID)
		return *g, nil
	}

	gid, err := s.mls.CreateGroup(s.ctx)
	if err != nil {
		return domain.Guild{}, fmt.Errorf("app: create group dm: %w", err)
	}
	g := domain.NewGuild("Group message", gid, s.PublicKey())
	g.Kind = "dm"
	g.Channels[0].Name = "dm"
	if err := s.store.SaveGuild(g); err != nil {
		return domain.Guild{}, err
	}
	s.trackGuild(&g)

	// Remember everyone we intend to add, then push to whoever's reachable now.
	// Unreachable invitees stay pending (persisted) and are invited when they
	// next connect (see deliverPendingDMInvites) or on the heal tick, so the
	// group eventually gathers everyone.
	for _, f := range targets {
		s.queueDMInvite(g.ID, f)
	}
	return g, nil
}

// retryPendingDMInvites re-pushes group-DM invites to any pending invitee who
// is reachable right now but hasn't joined yet — covering the case where the
// initial push failed while they stayed connected (so no reconnect fires the
// redelivery). Runs on the heal-loop tick.
func (s *Service) retryPendingDMInvites() {
	s.dmInviteMu.Lock()
	type todo struct{ gid, fpr string }
	var pending []todo
	for gid, set := range s.pendingDMInvites {
		for fpr := range set {
			pending = append(pending, todo{gid, fpr})
		}
	}
	s.dmInviteMu.Unlock()

	for _, t := range pending {
		if s.guildHasMember(t.gid, t.fpr) {
			s.clearPendingDMInvite(t.gid, t.fpr)
			continue
		}
		pid, ok := s.peerForFingerprint(t.fpr)
		if !ok {
			continue // still offline
		}
		if code, err := s.InviteCode(t.gid); err == nil {
			s.pushDMInvite(pid, code)
		}
	}
}

// RenameDM sets a custom name for a group DM and syncs it to the other members
// (any member may rename it, like Discord). An empty name resets to the auto
// member-list name. Reuses the guild_renamed meta lane (advisory, same trust
// model as a channel rename).
func (s *Service) RenameDM(guildID, name string) error {
	name = strings.TrimSpace(name)
	if len(name) > maxNameBytes {
		name = name[:maxNameBytes]
	}
	s.mu.Lock()
	g, ok := s.guilds[guildID]
	if !ok || g.Kind != "dm" {
		s.mu.Unlock()
		return fmt.Errorf("app: not a group DM")
	}
	if name == "" {
		name = "Group message" // reset sentinel → guildView recomputes from members
	}
	g.Name = name
	groupID := g.GroupID
	gc := *g
	s.mu.Unlock()

	_ = s.store.SaveGuild(gc)
	s.emitGuildUpdate()
	payload, _ := json.Marshal(guildMeta{Type: "guild_renamed", Name: name})
	if ct, err := s.mls.Encrypt(s.ctx, groupID, payload); err == nil {
		_ = s.ps.Publish(s.ctx, domain.GuildMetaTopicID(groupID), ct)
	}
	return nil
}

// PendingDMInvitees returns the fingerprints we've invited to a group DM who
// haven't joined yet, so the UI can show the full intended group (everyone you
// picked) even while some are still offline.
func (s *Service) PendingDMInvitees(guildID string) []string {
	s.dmInviteMu.Lock()
	defer s.dmInviteMu.Unlock()
	set := s.pendingDMInvites[guildID]
	out := make([]string, 0, len(set))
	for fpr := range set {
		out = append(out, fpr)
	}
	return out
}

// clearPendingDMInvite drops a fingerprint from a DM's pending set once
// they've joined (called from the add path). Safe for non-DM guilds (no-op).
func (s *Service) clearPendingDMInvite(guildID, fpr string) {
	s.dmInviteMu.Lock()
	changed := false
	if set, ok := s.pendingDMInvites[guildID]; ok {
		if set[fpr] {
			delete(set, fpr)
			changed = true
		}
		if len(set) == 0 {
			delete(s.pendingDMInvites, guildID)
		}
	}
	s.dmInviteMu.Unlock()
	if changed {
		s.persistDMState()
	}
}

// pushDMInvite fires the standard DM-invite push to a reachable peer; they dial
// back and are added via handleInviteRequest.
// pushGuildInvite offers a server to a peer. Their client shows it as an invite
// they can accept or ignore — we never redeem it for them.
func (s *Service) pushGuildInvite(pid peer.ID, code, guildName string) {
	req, _ := json.Marshal(dmInvite{Code: code, Kind: "guild", Guild: guildName})
	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, 20*time.Second)
		defer cancel()
		_, _ = s.host.RequestDMInvite(ctx, pid, req)
	}()
}

func (s *Service) pushDMInvite(pid peer.ID, code string) {
	req, _ := json.Marshal(dmInvite{Code: code})
	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, 20*time.Second)
		defer cancel()
		_, _ = s.host.RequestDMInvite(ctx, pid, req)
	}()
}

// deliverPendingDMInvites is called when a peer connects: if they're an
// outstanding group-DM invitee we couldn't reach earlier, invite them now.
// Entries for peers who have since joined are pruned.
func (s *Service) deliverPendingDMInvites(p peer.ID) {
	fpr := s.presence(p).Fingerprint
	if fpr == "" {
		return
	}
	s.dmInviteMu.Lock()
	var invite []string // guild IDs to (re)push for this peer
	pruned := false
	for gid, set := range s.pendingDMInvites {
		if !set[fpr] {
			continue
		}
		if s.guildHasMember(gid, fpr) {
			delete(set, fpr) // already in — nothing to do
			pruned = true
		} else {
			invite = append(invite, gid)
		}
		if len(set) == 0 {
			delete(s.pendingDMInvites, gid)
		}
	}
	s.dmInviteMu.Unlock()
	if pruned {
		s.persistDMState()
	}

	for _, gid := range invite {
		code, err := s.InviteCode(gid)
		if err != nil {
			continue
		}
		s.pushDMInvite(p, code)
	}
}

// shortFpr abbreviates a fingerprint for user-facing messages.
func shortFpr(f string) string {
	if len(f) > 9 {
		return f[:9]
	}
	return f
}

// StartDM opens (creating if needed) the direct-message conversation with the
// peer identified by fingerprint. Clicking someone's profile → Message calls
// this. Exactly ONE 1:1 conversation exists per peer: an existing DM is reused
// (and reopened if it was closed), even when the peer hasn't joined it yet.
// When a new conversation is created, the invite is pushed immediately if the
// peer is reachable and kept pending otherwise — it is (re)delivered when they
// next connect, so starting a DM with someone offline just works.
func (s *Service) StartDM(fingerprint string) (domain.Guild, error) {
	if fingerprint == "" || fingerprint == s.id.Fingerprint() {
		g, err := s.NotesDM()
		if err == nil {
			s.unhideDM(g.ID)
		}
		return g, err
	}
	if g := s.findPeerDM(fingerprint); g != nil {
		s.unhideDM(g.ID)
		// The peer isn't in the group (their join never completed, or they left):
		// re-extend the invite rather than leaving a dead conversation.
		if others, ok := s.dmOtherAccounts(g.GroupID); ok && len(others) == 0 {
			s.queueDMInvite(g.ID, fingerprint)
		}
		return *g, nil
	}

	gid, err := s.mls.CreateGroup(s.ctx)
	if err != nil {
		return domain.Guild{}, fmt.Errorf("app: create dm group: %w", err)
	}
	g := domain.NewGuild("Direct message", gid, s.PublicKey())
	g.Kind = "dm"
	g.Channels[0].Name = "dm"
	if err := s.store.SaveGuild(g); err != nil {
		return domain.Guild{}, err
	}
	s.trackGuild(&g)
	s.mu.Lock()
	s.dmPeers[g.ID] = fingerprint
	s.mu.Unlock()
	s.queueDMInvite(g.ID, fingerprint)
	s.emitGuildUpdate()
	return g, nil
}

// queueDMInvite registers fingerprint as a pending invitee of a DM and pushes
// the invite right away when they're reachable. Pending invites persist across
// restarts and are re-pushed on reconnect and on the heal tick until the peer
// joins — this is what makes "my friend never got my DM" impossible short of
// them never coming online.
func (s *Service) queueDMInvite(guildID, fingerprint string) {
	if fingerprint == "" || s.guildHasMember(guildID, fingerprint) {
		return
	}
	s.dmInviteMu.Lock()
	if s.pendingDMInvites[guildID] == nil {
		s.pendingDMInvites[guildID] = map[string]bool{}
	}
	s.pendingDMInvites[guildID][fingerprint] = true
	s.dmInviteMu.Unlock()
	s.persistDMState()
	if pid, ok := s.peerForFingerprint(fingerprint); ok {
		if code, err := s.InviteCode(guildID); err == nil {
			s.pushDMInvite(pid, code)
		}
	}
}

// handleDMInvite is the recipient side: auto-redeem the pushed invite code so
// the DM appears without any manual step.
func (s *Service) handleDMInvite(_ context.Context, from peer.ID, request []byte) ([]byte, error) {
	var req dmInvite
	if err := json.Unmarshal(request, &req); err != nil {
		return []byte{}, nil
	}
	// Resolve the sender NOW — usually immediate (the fingerprint derives from
	// the authenticated PeerID), making the wait below the rare path.
	senderFpr := s.presence(from).Fingerprint
	if req.Kind == "guild" {
		go func() {
			for i := 0; senderFpr == "" && i < 20; i++ {
				time.Sleep(500 * time.Millisecond)
				senderFpr = s.presence(from).Fingerprint
			}
			// Only someone we VERIFIED may even ring our doorbell about a server.
			// A stranger's invite is dropped without a trace — no prompt to
			// dismiss, no spam surface. A BLOCKED account is likewise dropped even
			// if we'd previously verified them.
			if senderFpr == "" || s.IsBlocked(senderFpr) || !s.VerifiedFingerprints()[senderFpr] {
				return
			}
			s.emitGuildInvite(GuildInvite{
				Code:     req.Code,
				Guild:    req.Guild,
				From:     senderFpr,
				FromName: s.ProfileName(senderFpr),
			})
		}()
		return []byte("ok"), nil
	}
	go func() {
		// Resolving a PeerID to an account can lag a fresh connection (device
		// certs still settling). Wait briefly rather than misjudging — and
		// silently dropping — a legitimate first DM. This has to settle BEFORE we
		// redeem anything: who is asking decides whether we redeem at all.
		for i := 0; senderFpr == "" && i < 20; i++ {
			time.Sleep(500 * time.Millisecond)
			senderFpr = s.presence(from).Fingerprint
		}
		// An unresolvable sender is one we could never authorize below; a blocked
		// one is refused outright. Neither gets so much as a tray row.
		if senderFpr == "" || s.IsBlocked(senderFpr) {
			return
		}
		// A stranger's DM waits in the requests tray (see request.go). Redeeming
		// the code is the disclosure, so declining to redeem it — yet — is the
		// entire gate: they learn nothing until the user says yes.
		if !s.knownContact(senderFpr) {
			s.recordMessageRequest(senderFpr, req.Code)
			return
		}
		g, err := s.JoinViaInvite(req.Code)
		if err != nil {
			return
		}
		// Auto-accept guards against a peer pushing an arbitrary invite code to
		// force us into unsolicited membership (which would disclose our profile +
		// mailbox key). We accept three shapes:
		//   - a genuine 2-person DM whose other member is the sender (like getting
		//     a first text from someone — low harm),
		//   - a GROUP DM whose inviter we already trust: someone we've verified, or
		//     someone we already share a 2-person DM with, or
		//   - a SERVER we were added to by someone we have VERIFIED out-of-band.
		//     Verification is the whole point: you compared safety numbers with
		//     this person, so them dropping you into their guild is a favour, not
		//     an attack. A stranger's server invite still lands nowhere.
		// Anything else is undone immediately. (Hard delete: LeaveGuild would
		// merely close a DM, which must not keep unsolicited membership around.)
		// Blocking is re-checked here, not just above: redeeming dials the sender
		// and can take seconds, and a block landing in that window must still win.
		if s.IsBlocked(senderFpr) {
			_ = s.deleteGuildLocal(g.ID)
			return
		}
		legit := s.isLegitDMWith(g.ID, senderFpr)
		if !legit && !s.isTrustedGroupDMInvite(g.ID, senderFpr) &&
			!s.isVerifiedGuildInvite(g.ID, senderFpr) {
			_ = s.deleteGuildLocal(g.ID)
			return
		}
		// Remember who this 1:1 is with, so the conversation stays identifiable
		// even if the sender later leaves the group.
		if legit {
			s.recordDMPeer(g.ID, senderFpr)
		}
		// A reopened conversation (we closed it, they messaged/re-invited us)
		// must surface again.
		s.unhideDM(g.ID)
		s.emitGuildUpdate()
	}()
	return []byte("ok"), nil
}

// isLegitDMWith reports whether guildID is a 2-person DM whose other account is
// senderFpr — i.e. a real direct message opened by the peer that invited us.
// Accounts, not leaves: the sender's linked devices don't disqualify the DM.
func (s *Service) isLegitDMWith(guildID, senderFpr string) bool {
	if senderFpr == "" {
		return false
	}
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var groupID []byte
	if ok {
		groupID = g.GroupID
	}
	isDM := ok && g.Kind == "dm"
	s.mu.RUnlock()
	if !isDM {
		return false
	}
	others, ok := s.dmOtherAccounts(groupID)
	return ok && len(others) == 1 && others[0] == senderFpr
}

// isVerifiedGuildInvite reports whether guildID is a real guild (not a DM) and
// the peer who pushed us into it is one we have VERIFIED. That's the only way an
// invite for a server auto-lands: no verification, no membership.
func (s *Service) isVerifiedGuildInvite(guildID, senderFpr string) bool {
	if senderFpr == "" {
		return false
	}
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	isGuild := ok && g.Kind != "dm"
	s.mu.RUnlock()
	if !isGuild {
		return false
	}
	return s.VerifiedFingerprints()[senderFpr]
}

// isTrustedGroupDMInvite reports whether guildID is a group DM (a kind="dm"
// group with more than two members) that senderFpr is a member of AND whose
// inviter we already have a relationship with — we've verified them, or we
// already share some other group (a guild or an existing DM) with them. That is
// what "approved" means in practice, and it stops a stranger who merely pushed
// an invite code out of nowhere from pulling us into a group.
func (s *Service) isTrustedGroupDMInvite(guildID, senderFpr string) bool {
	if senderFpr == "" {
		return false
	}
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var groupID []byte
	if ok {
		groupID = g.GroupID
	}
	isDM := ok && g.Kind == "dm"
	s.mu.RUnlock()
	if !isDM {
		return false
	}
	others, ok := s.dmOtherAccounts(groupID)
	if !ok || len(others) < 2 {
		return false // not a group DM (accounts, not device leaves)
	}
	senderIsMember := false
	for _, f := range others {
		if f == senderFpr {
			senderIsMember = true
			break
		}
	}
	if !senderIsMember {
		return false
	}
	if verified, err := s.store.VerifiedFingerprints(); err == nil && verified[senderFpr] {
		return true
	}
	// Accept from someone we already share a group with (a guild or an existing
	// DM). This is consistent with 1:1 DMs, which already auto-accept from any
	// reachable peer. (An audit flagged that a shared *large/public* guild widens
	// this to near-strangers; tightening it to verified-or-existing-DM is a
	// trust-model choice that would require mutual verification for the common
	// "add people from our shared server" flow — left as a deliberate decision.)
	return s.sharesOtherGroupWith(senderFpr, guildID)
}

// sharesOtherGroupWith reports whether we already share some group — a guild or
// an existing DM — with fingerprint, excluding exclID (the group currently being
// evaluated, which the sender is a member of too and would match trivially).
func (s *Service) sharesOtherGroupWith(fingerprint, exclID string) bool {
	s.mu.RLock()
	type ref struct {
		id  string
		gid []byte
	}
	var groups []ref
	for id, g := range s.guilds {
		if id != exclID {
			groups = append(groups, ref{id, g.GroupID})
		}
	}
	s.mu.RUnlock()
	for _, r := range groups {
		creds, err := s.mls.Members(s.ctx, r.gid)
		if err != nil {
			continue
		}
		for _, c := range creds {
			if accountFingerprintOf(c) == fingerprint {
				return true
			}
		}
	}
	return false
}

// findDMByMembers returns an existing DM whose set of OTHER members (everyone
// but us) exactly equals want, or nil. Used to avoid creating a duplicate DM
// for a group of people we already have a conversation with.
func (s *Service) findDMByMembers(want []string) *domain.Guild {
	wantSet := make(map[string]bool, len(want))
	for _, f := range want {
		wantSet[f] = true
	}
	self := s.id.Fingerprint()
	s.mu.RLock()
	var candidates []*domain.Guild
	for _, g := range s.guilds {
		if g.Kind == "dm" {
			candidates = append(candidates, g)
		}
	}
	s.mu.RUnlock()
	for _, g := range candidates {
		creds, err := s.mls.Members(s.ctx, g.GroupID)
		if err != nil {
			continue
		}
		others := make(map[string]bool)
		for _, c := range creds {
			if f := accountFingerprintOf(c); f != self {
				others[f] = true
			}
		}
		if len(others) != len(wantSet) {
			continue
		}
		match := true
		for f := range wantSet {
			if !others[f] {
				match = false
				break
			}
		}
		if match {
			gc := *g
			return &gc
		}
	}
	return nil
}

// findPeerDM returns the existing 1:1 DM with the given fingerprint, or nil.
// Strict by design: only a conversation whose set of OTHER accounts is exactly
// {fingerprint} counts — a group DM that merely contains the peer must never
// match (that bug once routed "Message X" into an unrelated group chat). A DM
// the peer hasn't joined yet (or has left) is matched via the recorded
// intended peer, so reopening never mints a duplicate conversation. If legacy
// duplicates exist, an open conversation beats a closed one, then the most
// recently active wins.
func (s *Service) findPeerDM(fingerprint string) *domain.Guild {
	type cand struct {
		g      domain.Guild // copied under mu: writers mutate guilds in place
		hidden bool
		peer   string
	}
	s.mu.RLock()
	var candidates []cand
	for id, g := range s.guilds {
		if g.Kind == "dm" {
			candidates = append(candidates, cand{*g, s.hiddenDMs[id], s.dmPeers[id]})
		}
	}
	s.mu.RUnlock()

	var best *domain.Guild
	bestHidden, bestActivity := true, int64(-1)
	for i := range candidates {
		c := &candidates[i]
		others, ok := s.dmOtherAccounts(c.g.GroupID)
		if !ok {
			continue // membership unresolvable right now; don't guess
		}
		match := false
		switch len(others) {
		case 1:
			match = others[0] == fingerprint
		case 0:
			// Nobody else in the group (peer never joined, or left): identify the
			// conversation by who it was created for. Notes never matches — it has
			// no recorded peer.
			match = c.peer == fingerprint
		}
		if !match {
			continue
		}
		// A DM received (not created) by this device has no recorded peer yet;
		// note it now so the conversation stays identifiable even if the peer
		// later leaves the group.
		if c.peer == "" && len(others) == 1 {
			s.recordDMPeer(c.g.ID, fingerprint)
		}
		act := s.GuildLastActivity(c.g.ID)
		if best == nil || (bestHidden && !c.hidden) || (bestHidden == c.hidden && act > bestActivity) {
			best, bestHidden, bestActivity = &c.g, c.hidden, act
		}
	}
	return best
}

// recordDMPeer persists which peer a 1:1 DM belongs to (idempotent).
func (s *Service) recordDMPeer(guildID, fingerprint string) {
	s.mu.Lock()
	if s.dmPeers[guildID] == fingerprint {
		s.mu.Unlock()
		return
	}
	s.dmPeers[guildID] = fingerprint
	s.mu.Unlock()
	s.persistDMState()
}

// peerForFingerprint resolves a connected peer's libp2p ID from its fingerprint.
func (s *Service) peerForFingerprint(fingerprint string) (peer.ID, bool) {
	for _, p := range s.host.Peers() {
		if s.presence(p).Fingerprint == fingerprint {
			return p, true
		}
	}
	return "", false
}
