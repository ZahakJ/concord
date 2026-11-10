package app

import "github.com/ZahakJ/concord/internal/domain"

// channelname.go settles what a channel is called from the governance log.
//
// The rename itself travels on the guild-meta lane, and that lane is applied
// in arrival order. Two admins renaming the same channel at the same moment
// therefore left each peer with whichever frame reached it last, and nothing
// ever reconciled them: the owner's sidebar said one name and the admin's the
// other, through restarts. Every rename is also a signed channel_rename op in
// the log, and the log has one order on every device, so once it holds a
// rename the newest authorized one is the name, exactly as loggedGuildName
// settles the guild's own.

// loggedChannelNames maps each renamed channel to the name its newest
// authorized channel_rename gives it, in canonical log order.
func (s *Service) loggedChannelNames(guildID string) map[string]string {
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	if !ok {
		s.mu.RUnlock()
		return nil
	}
	ops := append([]govOp(nil), s.govOps[guildID]...)
	owner := append([]byte(nil), g.OwnerID...)
	s.mu.RUnlock()
	renamed := false
	for _, o := range ops {
		if o.Type == "channel_rename" {
			renamed = true
			break
		}
	}
	if !renamed {
		return nil
	}
	_, verdicts := replayGuildOpsVerifying(owner, ops, true, s.govSigs.verify)
	out := map[string]string{}
	for _, o := range canonicalOps(ops) {
		if o.Type == "channel_rename" && o.ChannelID != "" && o.Name != "" && verdicts[o.hash()].Applied {
			out[o.ChannelID] = o.Name
		}
	}
	return out
}

// settleChannelNames puts every renamed channel on the name the log gives it.
func (s *Service) settleChannelNames(guildID string) {
	for chID, name := range s.loggedChannelNames(guildID) {
		if !s.channelInGuild(chID, guildID) {
			continue
		}
		if _, _, changed := s.mutateChannel(guildID, chID, func(c *domain.Channel) bool {
			if c.Name == name {
				return false
			}
			c.Name = name
			return true
		}); changed {
			_ = s.store.UpdateChannelName(chID, name)
		}
	}
}
