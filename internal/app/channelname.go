package app

import "github.com/ZahakJ/concord/internal/domain"

// channelname.go settles what a channel is called, its topic and its category
// from the governance log.
//
// All three travel on the guild-meta lane, and that lane is applied in arrival
// order and only by whoever is online. Two admins renaming the same channel at
// the same moment therefore left each peer with whichever frame reached it
// last, and a member who was offline for a rename, a topic or a move never
// heard of it at all: catch-up serves the channel list, but only channels the
// member did not have yet were adopted from it. Every one of those changes is
// also a signed op in the log (channel_rename, channel_topic, channel_move),
// the log reaches everyone through catch-up, and it has one order on every
// device, so once it holds such an op the newest authorized one decides,
// exactly as loggedGuildName settles the guild's own name.

// loggedChannel is what the log says about one channel. Each part counts only
// when its has-flag is set: a channel the log never touched in that respect
// keeps whatever it has.
type loggedChannel struct {
	name, topic, category          string
	hasName, hasTopic, hasCategory bool
}

// isChannelLayoutOp reports an op type settleChannelLayout reads.
func isChannelLayoutOp(t string) bool {
	return t == "channel_rename" || t == "channel_topic" || t == "channel_move" || t == "category_delete"
}

// loggedChannels maps each channel the log has renamed, given a topic or moved
// to what its newest authorized ops say, in canonical log order.
func (s *Service) loggedChannels(guildID string) map[string]loggedChannel {
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	if !ok {
		s.mu.RUnlock()
		return nil
	}
	ops := append([]govOp(nil), s.govOps[guildID]...)
	owner := append([]byte(nil), g.OwnerID...)
	s.mu.RUnlock()
	relevant := false
	for _, o := range ops {
		if o.Type != "category_delete" && isChannelLayoutOp(o.Type) {
			relevant = true
			break
		}
	}
	if !relevant {
		return nil
	}
	_, verdicts := replayGuildOpsVerifying(owner, ops, true, s.govSigs.verify)
	out := map[string]loggedChannel{}
	for _, o := range canonicalOps(ops) {
		if !verdicts[o.hash()].Applied || o.ChannelID == "" {
			continue
		}
		c := out[o.ChannelID]
		switch o.Type {
		case "channel_rename":
			if o.Name == "" {
				continue
			}
			c.name, c.hasName = o.Name, true
		case "channel_topic":
			c.topic, c.hasTopic = o.Name, true
		case "channel_move":
			// Only a move that says where it went: Add marks one whose Target
			// names the category ("" for none). Older builds logged only the
			// fact of a move, and those settle nothing.
			if !o.Add {
				continue
			}
			c.category, c.hasCategory = o.Target, true
		case "category_delete":
			// Deleting a category un-categorizes its channels on every peer
			// (applyCategoryRemoved); a move logged before the delete must not
			// put them back into it.
			for id, ch := range out {
				if ch.hasCategory && ch.category == o.ChannelID {
					ch.category = ""
					out[id] = ch
				}
			}
			continue
		default:
			continue
		}
		out[o.ChannelID] = c
	}
	return out
}

// settleChannelLayout puts every channel the log speaks for on the name, topic
// and category the log gives it.
func (s *Service) settleChannelLayout(guildID string) {
	logged := s.loggedChannels(guildID)
	if len(logged) == 0 {
		return
	}
	known := map[string]bool{"": true}
	if cats, err := s.store.Categories(guildID); err == nil {
		for _, c := range cats {
			known[c.ID] = true
		}
	}
	for chID, want := range logged {
		if !s.channelInGuild(chID, guildID) {
			continue
		}
		renamed, rest := false, false
		updated, _, changed := s.mutateChannel(guildID, chID, func(c *domain.Channel) bool {
			if want.hasName && c.Name != want.name {
				c.Name, renamed = want.name, true
			}
			if want.hasTopic && c.Topic != want.topic {
				c.Topic, rest = want.topic, true
			}
			// A category this device has not heard of yet would put the channel
			// in a section that is not on screen; the settle that runs when the
			// category arrives puts it there.
			if want.hasCategory && c.Category != want.category && known[want.category] {
				c.Category, rest = want.category, true
			}
			return renamed || rest
		})
		if !changed {
			continue
		}
		if renamed {
			_ = s.store.UpdateChannelName(chID, updated.Name)
		}
		if rest {
			_ = s.store.UpdateChannelMeta(chID, updated.Type, updated.Category, updated.Position, updated.Topic)
		}
	}
}
