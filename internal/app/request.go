package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// Message requests: a DM from someone you have no relationship with waits in a
// tray instead of landing in your conversation list.
//
// This is close to free here for a reason peculiar to a serverless design.
// Everywhere else the message has already reached the operator, and "request"
// is a rendering decision made after the fact. In Concord the invite code IS
// the disclosure: redeeming it is what puts us in the sender's MLS group, and
// that is what hands them our profile, our presence, our read/typing signals
// and our mailbox key — a durable handle for depositing mail at us. So the gate
// isn't a filter over things we already accepted. It is simply not redeeming
// yet, and a stranger who is never accepted learns nothing, not even that we
// are online.
//
// The cost of waiting is smaller than it looks: accepting redeems the code
// late, and the ordinary history sync then backfills what they said while they
// waited (a peer re-encrypts served history at the CURRENT epoch, so joining
// late is not the same as losing the conversation).

// messageRequestsKey is the settings-table key holding the pending tray.
const messageRequestsKey = "dm.requests"

// messageRequestTTL drops requests nobody ever answered. Without it the tray is
// the same never-pruned pile the contacts table already is, and a stale code
// points at a group whose invite the sender has long since forgotten.
const messageRequestTTL = 30 * 24 * time.Hour

// maxMessageRequests bounds the tray so a peer that can reach us can't grow it
// without limit. Oldest are dropped first: the newest request is the one the
// user is most likely about to answer.
const maxMessageRequests = 200

// MessageRequest is a stranger's un-redeemed offer to open a DM. It is NOT
// membership and NOT a conversation: we hold their invite code and nothing
// else until the user says yes.
type MessageRequest struct {
	From     string `json:"from"`               // sender's account fingerprint
	FromName string `json:"fromName,omitempty"` // their display name, if we know one
	Code     string `json:"code"`               // the invite we deliberately have NOT redeemed
	At       int64  `json:"at"`                 // UnixMilli, when it first arrived
}

// loadMessageRequests restores the tray at startup, dropping anything past its
// TTL so an old pile doesn't come back with the app.
func (s *Service) loadMessageRequests() {
	raw, err := s.store.GetSetting(messageRequestsKey)
	if err != nil || raw == "" {
		return
	}
	var list []MessageRequest
	if json.Unmarshal([]byte(raw), &list) != nil {
		return
	}
	cutoff := time.Now().Add(-messageRequestTTL).UnixMilli()
	s.reqMu.Lock()
	for _, r := range list {
		if r.From == "" || r.Code == "" || r.At < cutoff {
			continue
		}
		s.requests[r.From] = r
	}
	s.reqMu.Unlock()
}

// persistMessageRequests writes the tray back to the settings table. reqMu must
// NOT be held (it is taken here).
func (s *Service) persistMessageRequests() {
	list := s.MessageRequests()
	raw, err := json.Marshal(list)
	if err != nil {
		return
	}
	_ = s.store.SetSetting(messageRequestsKey, string(raw))
}

// MessageRequests returns the pending tray, newest first.
func (s *Service) MessageRequests() []MessageRequest {
	s.reqMu.Lock()
	out := make([]MessageRequest, 0, len(s.requests))
	for _, r := range s.requests {
		out = append(out, r)
	}
	s.reqMu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].At != out[j].At {
			return out[i].At > out[j].At
		}
		return out[i].From < out[j].From
	})
	return out
}

// recordMessageRequest files a stranger's DM invite in the tray. One row per
// sender: a peer that re-pushes (they do, on every reconnect — see
// deliverPendingDMInvites) refreshes the code in place rather than stacking up
// rows, and keeps its original arrival time so the tray stays in the order
// people actually knocked.
func (s *Service) recordMessageRequest(fingerprint, code string) {
	if fingerprint == "" || code == "" {
		return
	}
	name := s.ProfileName(fingerprint)
	now := time.Now().UnixMilli()

	s.reqMu.Lock()
	prev, existed := s.requests[fingerprint]
	at := now
	if existed {
		at = prev.At
	}
	// The tray only needs redrawing when the row is new or its label changed —
	// a re-push carries a freshly minted code (it embeds current addresses), and
	// that alone must not churn the UI.
	redraw := !existed || prev.FromName != name
	unchanged := existed && prev.Code == code && prev.FromName == name
	s.requests[fingerprint] = MessageRequest{From: fingerprint, FromName: name, Code: code, At: at}
	// Trim oldest-first once over the cap, so a flood can't crowd out the tray.
	if len(s.requests) > maxMessageRequests {
		oldest, oldestAt := "", int64(0)
		for f, r := range s.requests {
			if oldestAt == 0 || r.At < oldestAt {
				oldest, oldestAt = f, r.At
			}
		}
		delete(s.requests, oldest)
	}
	s.reqMu.Unlock()

	// A sender that can't reach us re-pushes on every reconnect and on the heal
	// tick; an identical row must cost nothing at all.
	if unchanged {
		return
	}
	// The refreshed code is kept on disk even when the row looks the same: it is
	// what Accept dials with, and a restart that fell back to a stale one would
	// fail exactly when the user finally said yes.
	s.persistMessageRequests()
	if redraw {
		s.emitGuildUpdate() // the tray badge rides the guild-list refresh
	}
}

// dropMessageRequest removes a sender's request. Reports whether one was there.
func (s *Service) dropMessageRequest(fingerprint string) bool {
	s.reqMu.Lock()
	_, ok := s.requests[fingerprint]
	delete(s.requests, fingerprint)
	s.reqMu.Unlock()
	if ok {
		s.persistMessageRequests()
	}
	return ok
}

// AcceptMessageRequest redeems the held invite — the moment we join their group
// and they learn anything about us. The same shape check the auto-accept path
// applies still runs: saying yes to a person means a conversation with that
// person, not membership in whatever they happened to encode in the code.
func (s *Service) AcceptMessageRequest(fingerprint string) (domain.Guild, error) {
	s.reqMu.Lock()
	req, ok := s.requests[fingerprint]
	s.reqMu.Unlock()
	if !ok {
		return domain.Guild{}, fmt.Errorf("app: no message request from %s", shortFpr(fingerprint))
	}
	if s.IsBlocked(fingerprint) {
		return domain.Guild{}, fmt.Errorf("app: %s is blocked — unblock them first", shortFpr(fingerprint))
	}
	g, err := s.JoinViaInvite(req.Code)
	if err != nil {
		// Keep the request. The usual cause is that the sender is offline right
		// now (redeeming dials them), and discarding it would throw away the only
		// copy of the code — the conversation could never be opened again.
		return domain.Guild{}, fmt.Errorf("app: could not open that conversation — are they online? (%w)", err)
	}
	oneToOne, isDM := s.dmSenderRole(g.ID, fingerprint)
	if !isDM {
		// The code was not a DM with them at all. Undo hard: LeaveGuild merely
		// closes a DM, which must not leave unsolicited membership behind.
		_ = s.deleteGuildLocal(g.ID)
		s.dropMessageRequest(fingerprint)
		s.emitGuildUpdate()
		return domain.Guild{}, fmt.Errorf("app: that invite wasn't a direct message — request discarded")
	}
	if oneToOne {
		s.recordDMPeer(g.ID, fingerprint)
	}
	s.unhideDM(g.ID)
	s.dropMessageRequest(fingerprint)
	s.emitGuildUpdate()
	return g, nil
}

// DeclineMessageRequest drops a request, optionally blocking the sender. The
// sender is never told either way — a decline that reports back is a delivery
// receipt for harassment.
func (s *Service) DeclineMessageRequest(fingerprint string, block bool) error {
	if block {
		if err := s.BlockUser(fingerprint); err != nil {
			return err
		}
	}
	if s.dropMessageRequest(fingerprint) {
		s.emitGuildUpdate()
	}
	return nil
}

// dmSenderRole classifies a group we've just joined from senderFpr's invite:
// isDM is true only for a kind="dm" group the sender is actually in, and
// oneToOne distinguishes a 1:1 from a group DM. Unlike isTrustedGroupDMInvite
// this asks nothing about prior trust — the user supplying the yes IS the trust
// decision — but it still refuses to call a server, or a group the sender isn't
// even in, a direct message.
func (s *Service) dmSenderRole(guildID, senderFpr string) (oneToOne, isDM bool) {
	if senderFpr == "" {
		return false, false
	}
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var groupID []byte
	if ok {
		groupID = g.GroupID
	}
	kindDM := ok && g.Kind == "dm"
	s.mu.RUnlock()
	if !kindDM {
		return false, false
	}
	others, resolved := s.dmOtherAccounts(groupID)
	if !resolved {
		return false, false // membership unreadable right now; don't guess
	}
	if len(others) == 1 {
		return true, others[0] == senderFpr
	}
	for _, f := range others {
		if f == senderFpr {
			return false, true
		}
	}
	return false, false
}

// knownContact reports whether we already have a relationship with an account —
// the test that decides whether their DM lands or waits. Any of: it's us (our
// own linked device), we verified them, we share a group with them already (a
// server or a DM), or we are the ones who reached out to them. Everyone else is
// a stranger.
//
// Sharing a server counts deliberately: "someone from our guild messaged me" is
// the overwhelmingly common first DM in a friend group, and gating it would
// train people to accept everything, which is how a request tray becomes
// decorative.
func (s *Service) knownContact(fingerprint string) bool {
	if fingerprint == "" {
		return false
	}
	if fingerprint == s.id.Fingerprint() {
		return true
	}
	if s.VerifiedFingerprints()[fingerprint] {
		return true
	}
	if s.weReachedOutTo(fingerprint) {
		return true
	}
	return s.sharesGuild(fingerprint)
}

// weReachedOutTo reports whether this device already opened a conversation
// aimed at fingerprint — a 1:1 we created for them, or an outstanding invite we
// queued. Their invite crossing ours in flight must not land in the tray: we
// already made the decision the tray exists to ask about.
func (s *Service) weReachedOutTo(fingerprint string) bool {
	s.mu.RLock()
	for _, f := range s.dmPeers {
		if f == fingerprint {
			s.mu.RUnlock()
			return true
		}
	}
	s.mu.RUnlock()

	s.dmInviteMu.Lock()
	defer s.dmInviteMu.Unlock()
	for _, set := range s.pendingDMInvites {
		if set[fingerprint] {
			return true
		}
	}
	return false
}
