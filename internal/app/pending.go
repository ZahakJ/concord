package app

import "fmt"

// pending.go — "pending" guild members: people you've added who haven't joined
// yet. Like a DM you opened before the other side replied, they show in the
// roster immediately (greyed, "pending") instead of vanishing until they sync.
// We keep re-pushing the invite whenever they're reachable, and drop them from
// pending the moment they actually join the group.

func (s *Service) loadPendingMembers() {
	m, err := s.store.PendingMembers()
	if err != nil {
		return
	}
	s.mu.Lock()
	for g, fprs := range m {
		if s.pendingMembers[g] == nil {
			s.pendingMembers[g] = map[string]bool{}
		}
		for _, f := range fprs {
			s.pendingMembers[g][f] = true
		}
	}
	s.mu.Unlock()
}

// addPending records a fingerprint as pending for a guild (memory + store).
func (s *Service) addPending(guildID, fpr string) {
	s.mu.Lock()
	if s.pendingMembers[guildID] == nil {
		s.pendingMembers[guildID] = map[string]bool{}
	}
	s.pendingMembers[guildID][fpr] = true
	s.mu.Unlock()
	_ = s.store.AddPendingMember(guildID, fpr)
}

// clearPending drops a fingerprint from a guild's pending set.
func (s *Service) clearPending(guildID, fpr string) {
	s.mu.Lock()
	if set := s.pendingMembers[guildID]; set != nil {
		delete(set, fpr)
		if len(set) == 0 {
			delete(s.pendingMembers, guildID)
		}
	}
	s.mu.Unlock()
	_ = s.store.RemovePendingMember(guildID, fpr)
}

// PendingMembersFor lists fingerprints added to a guild that haven't joined yet.
// Anyone who has since become a real member is cleared here (lazily) so the UI
// never double-lists them.
func (s *Service) PendingMembersFor(guildID string) []string {
	s.mu.RLock()
	set := s.pendingMembers[guildID]
	fprs := make([]string, 0, len(set))
	for f := range set {
		fprs = append(fprs, f)
	}
	s.mu.RUnlock()

	out := make([]string, 0, len(fprs))
	for _, f := range fprs {
		if s.guildHasMember(guildID, f) {
			s.clearPending(guildID, f) // they made it in — no longer pending
			continue
		}
		out = append(out, f)
	}
	return out
}

// CancelPendingMember drops a not-yet-joined member you added — cancels the
// pending invite so they stop showing in the roster. Manage-members gated.
func (s *Service) CancelPendingMember(guildID, fpr string) error {
	if !s.canManageMembers(guildID) {
		return fmt.Errorf("app: you don't have permission to manage members")
	}
	s.clearPending(guildID, fpr)
	s.emitGuildUpdate()
	return nil
}

// reconcilePendingMembers re-pushes the invite to every reachable pending member
// (and clears any who joined). Called on the heal tick, so an add made while the
// other person was offline lands automatically once they reconnect.
func (s *Service) reconcilePendingMembers() {
	s.mu.RLock()
	guildIDs := make([]string, 0, len(s.pendingMembers))
	for g := range s.pendingMembers {
		guildIDs = append(guildIDs, g)
	}
	s.mu.RUnlock()

	for _, guildID := range guildIDs {
		for _, fpr := range s.PendingMembersFor(guildID) {
			pid, ok := s.peerForFingerprint(fpr)
			if !ok {
				continue // still offline — try again next tick
			}
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
	}
}
