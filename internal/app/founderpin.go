package app

import (
	"errors"
	"log"
	"sync/atomic"

	"github.com/ZahakJ/concord/internal/crypto/mls"
	"github.com/ZahakJ/concord/internal/domain"
)

// A guild created by 0.61 or later has an MLS group id derived from its guild
// id and its founder's account key (domain.FounderMatches). A device checks
// the founder against the group it actually holds, never against the word of
// whoever sent the record:
//
//   - a joiner refuses an answer whose owner does not match the group, and
//     joins only the group the answer names (redeemInvite);
//   - a device that holds such a guild with an owner that does not match looks
//     for the founder among the keys it can see, and while it finds none it
//     gives nobody owner powers (auditFounder, ownerUnverified).
//
// Guilds created earlier have 16-byte random group ids that name nobody, and
// take their owner from the record exactly as before.

// errInviteMismatch is the refusal a joiner gets when the answer to its invite
// does not match the guild it names. Nothing is stored.
var errInviteMismatch = errors.New("app: this invite's copy of the guild does not match the guild")

// foundGroup picks a guild id and creates its MLS group under a founder-scheme
// id for this account. Every creator of a guild, DM, Notes or meeting room
// goes through here.
func (s *Service) foundGroup() (string, []byte, error) {
	id := domain.NewID()
	gid, err := domain.NewFounderGroupID(id, s.PublicKey())
	if err != nil {
		return "", nil, err
	}
	out, err := s.mls.CreateGroupWithID(s.ctx, mls.GroupID(gid))
	if err != nil {
		return "", nil, err
	}
	return id, out, nil
}

// joinWant is the MLS group a join for this answer must land in, or an error
// if the answer cannot be joined. held is the device's own record on a probe.
func joinWant(codeGuildID string, answer domain.Guild, held *domain.Guild) ([]byte, error) {
	if held != nil {
		if len(held.GroupID) == 0 {
			return nil, errInviteMismatch
		}
		return held.GroupID, nil
	}
	switch {
	case domain.IsFounderGroupID(answer.GroupID):
		if !domain.FounderMatches(answer.GroupID, codeGuildID, answer.OwnerID) {
			return nil, errInviteMismatch
		}
	case len(answer.GroupID) == domain.LegacyGroupIDLen:
		// A guild from before the founder scheme: the owner is taken from the
		// record, as it always was.
	default:
		return nil, errInviteMismatch
	}
	return answer.GroupID, nil
}

// founderCandidates are the keys that could be a guild's founder as this
// device sees it: the account behind every leaf of its group, every signer in
// its governance log, and any extra keys the caller offers. Only a key the
// group id verifies against is ever adopted, so a candidate from anywhere is
// harmless.
func (s *Service) founderCandidates(guildID string, groupID []byte, extra ...[]byte) [][]byte {
	out := append([][]byte(nil), extra...)
	if founderLocalCandidatesOff.Load() {
		return out
	}
	if creds, err := s.mls.Members(s.ctx, groupID); err == nil {
		for _, c := range creds {
			out = append(out, accountKeyOf(c))
		}
	}
	s.mu.RLock()
	for _, o := range s.govOps[guildID] {
		out = append(out, append([]byte(nil), o.Signer...))
	}
	s.mu.RUnlock()
	return out
}

// founderLocalCandidatesOff makes the audit look only at the keys offered to
// it, not at the tree or the log, so a test can meet a device on which the
// founder's key does not appear. Nothing in the product sets it.
var founderLocalCandidatesOff atomic.Bool

// auditFounder checks a founder-scheme guild's stored owner against its group
// id. A wrong owner is replaced by the candidate the id verifies against; with
// no such candidate the guild is owner-unverified until one turns up. Legacy
// guilds are left alone. It reports whether anything changed.
func (s *Service) auditFounder(guildID string, extra ...[]byte) bool {
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	if !ok || !domain.IsFounderGroupID(g.GroupID) {
		s.mu.RUnlock()
		return false
	}
	gid := append([]byte(nil), g.GroupID...)
	stored := append([]byte(nil), g.OwnerID...)
	was := s.ownerUnverified[guildID]
	s.mu.RUnlock()

	if domain.FounderMatches(gid, guildID, stored) {
		return s.setOwnerUnverified(guildID, false, was)
	}
	var founder []byte
	for _, k := range s.founderCandidates(guildID, gid, extra...) {
		if domain.FounderMatches(gid, guildID, k) {
			founder = k
			break
		}
	}
	if founder == nil {
		if !was {
			log.Printf("concord/app: guild %s: the owner could not be confirmed from the group; nobody holds owner powers here until it is", guildID)
		}
		return s.setOwnerUnverified(guildID, true, was)
	}

	s.mu.Lock()
	cur, ok := s.guilds[guildID]
	if !ok {
		s.mu.Unlock()
		return false
	}
	ng := cur.Clone()
	ng.OwnerID = founder
	s.guilds[guildID] = &ng
	delete(s.ownerUnverified, guildID)
	s.rebuildGovStateLocked(guildID)
	s.mu.Unlock()
	_ = s.store.SaveGuild(ng)
	log.Printf("concord/app: guild %s: owner corrected from the group's founder key", guildID)
	s.emitGuildUpdate()
	return true
}

// setOwnerUnverified records the state and refolds when it changes.
func (s *Service) setOwnerUnverified(guildID string, on, was bool) bool {
	if on == was {
		return false
	}
	s.mu.Lock()
	if on {
		s.ownerUnverified[guildID] = true
	} else {
		delete(s.ownerUnverified, guildID)
	}
	s.rebuildGovStateLocked(guildID)
	s.mu.Unlock()
	s.emitGuildUpdate()
	return true
}

// OwnerUnverified reports whether this device could not confirm who founded
// the guild. While it holds, nobody has owner powers here.
func (s *Service) OwnerUnverified(guildID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ownerUnverified[guildID]
}

// foldAnchorLocked is the key a replay of this guild's log starts from: the
// stored owner, or none while the owner is unverified. Caller holds s.mu.
func (s *Service) foldAnchorLocked(g *domain.Guild) []byte {
	if s.ownerUnverified[g.ID] {
		return nil
	}
	return append([]byte(nil), g.OwnerID...)
}
