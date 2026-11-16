package app

// deletedBodiesClearedKey records that clearDeletedBodiesOnce has run to the
// end on this device.
const deletedBodiesClearedKey = "app.deleted_bodies_cleared"

// clearDeletedBodiesOnce erases, one time per device, the body that deleted
// rows already on disk still hold wherever this device cannot reveal it.
//
// A deleted message keeps its body for one reader only: a moderator, through
// RevealDeleted. Everywhere else a body at rest under a tombstone serves
// nobody. The store does not record how a deleted row came to be deleted, by
// a delete applied here or by a tombstone from history sync, so the rule is
// the reveal's own: a
// deleted body is kept in a channel of a guild where this device holds
// ManageMessages, outside one-to-one DMs, and cleared everywhere else.
//
// The run is recorded only when it finishes. One stopped by an error or by
// shutdown starts again at the next launch, and what it already cleared is
// not matched again.
func (s *Service) clearDeletedBodiesOnce() {
	if v, err := s.store.GetSetting(deletedBodiesClearedKey); err != nil || v != "" {
		return
	}
	if _, err := s.store.ClearDeletedBodies(s.revealableChannels()); err != nil {
		return
	}
	_ = s.store.SetSetting(deletedBodiesClearedKey, "1")
}

// revealableChannels lists the channels where RevealDeleted would answer for
// this device: those of every guild that is not a one-to-one DM and in which
// it holds ManageMessages.
func (s *Service) revealableChannels() []string {
	type guildChannels struct {
		id       string
		channels []string
	}
	var candidates []guildChannels
	s.mu.RLock()
	for id, g := range s.guilds {
		if g.Kind == "dm" {
			continue
		}
		gc := guildChannels{id: id}
		for _, c := range g.Channels {
			gc.channels = append(gc.channels, c.ID)
		}
		candidates = append(candidates, gc)
	}
	s.mu.RUnlock()
	var keep []string
	for _, gc := range candidates {
		if s.hasPerm(gc.id, PermManageMessages) {
			keep = append(keep, gc.channels...)
		}
	}
	return keep
}
