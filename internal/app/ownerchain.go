package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/store"
	"github.com/libp2p/go-libp2p/core/peer"
)

// The commit chain served to an owner's device this tree no longer seats.
// docs/dev/owner-rejoin.md is the design; this is the responder's half.
//
// An owner's device that was offline when a member removed it never reads the
// removal: gossip is gone by the time it is back, and every member that
// applied the commit declines its catch-up (handleSyncRequest serves members
// only), so screenCommit never sees the commit and noteTakenOut never latches.
// The device sits at the old epoch for good. The fix is to hand it the commits,
// and nothing else, so it can verify them itself.
//
// What the responder decides is only whether to send public bytes: commits
// travel the control topic in the clear, and the heal door (handleInviteRequest)
// already serves this requester a Welcome, which carries the whole current tree.
// What the requester does with them is decided on its side, by the same checks
// any commit passes: commitAuthorized, then screenCommit's VerifyCommit against
// its own tree at its own epoch. Nothing in the answer is taken on the
// responder's word.

const (
	// ownerChainMaxCommits and ownerChainMaxBytes bound one answer. The sync
	// frame is 1 MiB and commits travel base64 in JSON; the requester asks
	// again in the same catch-up (maxSyncRounds) when the page moved its epoch.
	ownerChainMaxCommits = 64
	ownerChainMaxBytes   = 512 << 10
	// ownerChainWindow is how often one peer may have the same page again. A
	// requester that applied a whole page asks from past it and is served at
	// once; one that stopped inside a page (at its removal, say) is served
	// again only after the window.
	ownerChainWindow = 30 * time.Second
	// ownerChainTrack bounds the throttle's memory, and ownerChainPerGuild
	// one guild's share of it. A full table or guild share loses its oldest
	// entry rather than refusing: refusing let the owner of one guild, with
	// many device keys, lock the owners of other guilds out.
	ownerChainTrack    = 256
	ownerChainPerGuild = 8
)

type ownerChainServe struct {
	guild string
	at    time.Time
	next  uint64 // the requester epoch at which the page served is used up
}

// serveOwnerChain answers a catch-up from a requester this tree does not seat.
// It serves only when every one of these holds, and otherwise declines with the
// empty body every older build sends:
//
//   - the requester asked for it (OwnerChain), so a build that cannot read a
//     chain it is not seated in is never handed one;
//   - the requester's account, resolved from its Noise-authenticated peer key
//     through a device certificate bound to that key (presence), is this
//     guild's effective owner by this device's governance state;
//   - this copy is not one that CannotRepairHere;
//   - the requester stands behind this device's epoch;
//   - the throttle allows it (claimOwnerChain).
//
// The answer carries commits only, never a payload: this device's tree does not
// seat the requester, so nothing encrypted to it could be read, and no history
// goes to a device the group has removed. When the log cannot bridge the
// requester it says EpochGap, as it does for a seated member, which the
// requester treats as a claim.
func (s *Service) serveOwnerChain(ctx context.Context, req syncRequest, guild domain.Guild, from peer.ID, fpr string) ([]byte, error) {
	if !req.OwnerChain || fpr == "" || fpr != s.effectiveOwner(req.GuildID) {
		return []byte{}, nil
	}
	if s.CannotRepairHere(req.GuildID) {
		return []byte{}, nil
	}
	myEpoch, err := s.mls.Epoch(ctx, guild.GroupID)
	if err != nil || req.Epoch >= myEpoch {
		return []byte{}, nil
	}
	// A repeat inside the window is declined before the log is read.
	if s.ownerChainThrottled(req.GuildID, fpr, req.Epoch) {
		return []byte{}, nil
	}
	rows, err := s.store.CommitsAfter(guild.GroupID, req.Epoch)
	resp := syncResponse{Epoch: myEpoch}
	if err != nil || !bridges(rows, req.Epoch, myEpoch) {
		if !s.claimOwnerChain(req.GuildID, fpr, req.Epoch, 0) {
			return []byte{}, nil
		}
		resp.EpochGap = true
		return json.Marshal(resp)
	}
	page := ownerChainPage(rows)
	if !s.claimOwnerChain(req.GuildID, fpr, req.Epoch, len(page)) {
		return []byte{}, nil
	}
	resp.Commits = page
	return json.Marshal(resp)
}

// ownerChainPage is the first commits of a chain within the count and byte
// bounds, and always the first commit, so a chain is never served empty.
func ownerChainPage(rows []store.CommitRow) [][]byte {
	var out [][]byte
	size := 0
	for _, r := range rows {
		if len(out) == ownerChainMaxCommits || (len(out) > 0 && size+len(r.Commit) > ownerChainMaxBytes) {
			break
		}
		out = append(out, r.Commit)
		size += len(r.Commit)
	}
	return out
}

// ownerChainThrottled reports whether every answer to this request would be
// refused by claimOwnerChain: the same page, or a point inside it, again
// inside the window. A gap answer may still be refused by the claim.
func (s *Service) ownerChainThrottled(guildID, fpr string, epoch uint64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	last, known := s.ownerChainServed[guildID+"|"+fpr]
	return known && time.Since(last.at) < ownerChainWindow && epoch < last.next
}

// claimOwnerChain reports whether a page of n commits from epoch may go to this
// peer now, and records it if so. The same page goes again only after
// ownerChainWindow; a request from where the last page ended is the requester
// paging through and is served at once, which bounds the serves in a window by
// the chain's length over the page size. A new key that finds its guild's
// share or the table full takes the place of the oldest entry there.
//
// The throttle is the requesting account's, not its peer id's: every device of
// the owner account shares one, so fresh peer ids (any device can mint them)
// buy no further serves.
func (s *Service) claimOwnerChain(guildID, fpr string, epoch uint64, n int) bool {
	key := guildID + "|" + fpr
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ownerChainServed == nil {
		s.ownerChainServed = map[string]ownerChainServe{}
	}
	last, known := s.ownerChainServed[key]
	if known && now.Sub(last.at) < ownerChainWindow && (n == 0 || epoch < last.next) {
		return false
	}
	if !known {
		inGuild := 0
		for k, v := range s.ownerChainServed {
			if now.Sub(v.at) >= ownerChainWindow {
				delete(s.ownerChainServed, k)
			} else if v.guild == guildID {
				inGuild++
			}
		}
		if inGuild >= ownerChainPerGuild {
			s.evictOldestOwnerChain(guildID)
		}
		if len(s.ownerChainServed) >= ownerChainTrack {
			s.evictOldestOwnerChain("")
		}
	}
	s.ownerChainServed[key] = ownerChainServe{guild: guildID, at: now, next: epoch + uint64(n)}
	return true
}

// evictOldestOwnerChain drops the oldest throttle entry, of guildID when it is
// set and of the whole table otherwise. The caller holds s.mu.
func (s *Service) evictOldestOwnerChain(guildID string) {
	oldest, at := "", time.Time{}
	for k, v := range s.ownerChainServed {
		if guildID != "" && v.guild != guildID {
			continue
		}
		if oldest == "" || v.at.Before(at) {
			oldest, at = k, v.at
		}
	}
	if oldest != "" {
		delete(s.ownerChainServed, oldest)
	}
}
