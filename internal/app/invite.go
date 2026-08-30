package app

import (
	"encoding/json"

	"github.com/ZahakJ/concord/internal/domain"
)

// invite.go — bookkeeping for a guild or meeting invite that went to a
// verified contact.
//
// The offer is not membership. The 1:1 with them is where the offer (and,
// later, the join) is written down, so walking away from the invite dialog
// does not lose the fact that you asked, or that they said yes.

// inviteNote is the body of a kind="invite" DM message.
type inviteNote struct {
	Op      string `json:"op"`                // offered | joined
	What    string `json:"what"`              // guild | meeting
	Guild   string `json:"guild"`             // display name
	GuildID string `json:"guildId,omitempty"` // so a stale name is still the same room
	Who     string `json:"who,omitempty"`     // invitee's name, on the join line
	Code    string `json:"code,omitempty"`    // invite code, on the offer only
}

func inviteWhat(g domain.Guild) string {
	if g.Kind == "meeting" {
		return "meeting"
	}
	return "guild"
}

func joinSystemLine(g domain.Guild) string {
	switch g.Kind {
	case "meeting":
		return "has joined the meeting"
	case "dm":
		return ""
	default:
		return "has joined the guild"
	}
}

func (s *Service) noteInviteOffered(guildID, fpr, code string) {
	go s.postInviteNote(fpr, guildID, "offered", code)
}

func (s *Service) noteInviteAccepted(guildID, fpr string) {
	key := guildID + "|" + fpr
	s.mu.Lock()
	if s.inviteJoinNoted == nil {
		s.inviteJoinNoted = map[string]bool{}
	}
	if s.inviteJoinNoted[key] {
		s.mu.Unlock()
		return
	}
	s.inviteJoinNoted[key] = true
	s.mu.Unlock()
	go s.postInviteNote(fpr, guildID, "joined", "")
}

func (s *Service) postInviteNote(peerFpr, guildID, op, code string) {
	if peerFpr == "" || guildID == "" {
		return
	}
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	s.mu.RUnlock()
	if !ok || g == nil || g.Kind == "dm" {
		return
	}
	dm, err := s.StartDM(peerFpr)
	if err != nil || len(dm.Channels) == 0 {
		return
	}
	body, err := json.Marshal(inviteNote{
		Op:      op,
		What:    inviteWhat(*g),
		Guild:   g.Name,
		GuildID: g.ID,
		Who:     s.ProfileName(peerFpr),
		Code:    code,
	})
	if err != nil {
		return
	}
	_, _ = s.send(dm.Channels[0].ID, string(body), "invite", "")
}
