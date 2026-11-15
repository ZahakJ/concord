package app

import (
	"context"
	"strings"
	"time"
)

// Adopting the guilds a link hands over.
//
// The handover used to be fire and forget: each code was redeemed once, any
// error was dropped, and the link reported plain success. A guild whose join
// was refused simply never appeared, and nothing said so or tried again.
// AdoptLinkedGuilds reports every guild it could not join, and keeps trying
// those for a while on its own.

// LinkJoinFailure is one guild a newly linked device could not join.
type LinkJoinFailure struct {
	GuildID string
	Name    string
	// Refused is set when the admitting device answered that it could not
	// add this device ("invite failed"), which is what an admitting device
	// still holding an earlier copy of this device answers. Other refusals
	// (catching up, not authorized, banned) leave it unset.
	Refused bool
	// Retrying is set when the join is tried again in the background.
	Retrying bool
	Err      string
}

// linkRetryBackoff is the wait before each retry of a handed-over guild the
// first attempt could not join. A wait in which the admitting device cannot
// be reached is not counted against these; linkRetryWindow bounds the whole.
var linkRetryBackoff = []time.Duration{
	15 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute,
}

// linkRetryWindow is how long after a link the retries go on at most.
var linkRetryWindow = time.Hour

// AdoptLinkedGuilds joins each guild in a link handover (Notes first), and
// returns the ones it could not join. Those are retried in the background
// with backoff while this device runs and the device that can admit it is
// reachable, unless the refusal was final (banned, removed).
func (s *Service) AdoptLinkedGuilds(codes []string) []LinkJoinFailure {
	var failed []LinkJoinFailure
	var retry []string
	for _, code := range NotesFirst(codes) {
		err := s.joinOfferedInviteErr(code)
		if err == nil {
			continue
		}
		ic, _ := decodeInviteCode(strings.TrimSpace(code))
		f := LinkJoinFailure{GuildID: ic.GuildID, Name: ic.GuildName, Err: err.Error()}
		f.Refused = strings.Contains(f.Err, "invite failed")
		f.Retrying = linkJoinWorthRetrying(err)
		failed = append(failed, f)
		if f.Retrying {
			retry = append(retry, code)
		}
	}
	if len(retry) > 0 {
		go s.retryLinkedJoins(retry)
	}
	return failed
}

// linkJoinWorthRetrying: everything but a verdict about this account. A
// committer that turned the join down for any other reason ("invite failed",
// "catching up", not authorized) may answer differently later, or another
// committer may be the one asked.
func linkJoinWorthRetrying(err error) bool {
	msg := err.Error()
	return !strings.Contains(msg, bannedRefusal) && !strings.Contains(msg, removedRefusal)
}

// retryLinkedJoins retries the handed-over codes that failed, each on its
// own backoff, until each joins, gets a final refusal, or runs out of tries.
func (s *Service) retryLinkedJoins(codes []string) {
	start := time.Now()
	deadline := start.Add(linkRetryWindow)
	type pending struct {
		code  string
		tries int
		due   time.Time
	}
	left := make([]*pending, 0, len(codes))
	for _, c := range codes {
		left = append(left, &pending{code: c, due: start.Add(linkRetryBackoff[0])})
	}
	for len(left) > 0 {
		due := left[0].due
		for _, p := range left {
			if p.due.Before(due) {
				due = p.due
			}
		}
		if due.After(deadline) {
			return
		}
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(time.Until(due)):
		}
		next := left[:0]
		for _, p := range left {
			if time.Now().Before(p.due) {
				next = append(next, p)
				continue
			}
			ic, err := decodeInviteCode(strings.TrimSpace(p.code))
			if err != nil || s.holdsGuild(ic.GuildID) {
				continue // joined meanwhile, by a hello offer or the heal tick
			}
			if !s.canReachInviter(ic) {
				// Not a try: the device that can admit us is away.
				p.due = time.Now().Add(linkRetryBackoff[min(p.tries, len(linkRetryBackoff)-1)])
				next = append(next, p)
				continue
			}
			p.tries++
			err = s.joinOfferedInviteErr(p.code)
			if err == nil || !linkJoinWorthRetrying(err) || p.tries >= len(linkRetryBackoff) {
				continue
			}
			p.due = time.Now().Add(linkRetryBackoff[p.tries])
			next = append(next, p)
		}
		left = next
	}
}

// holdsGuild reports whether this device holds the guild and its account is
// seated in it.
func (s *Service) holdsGuild(guildID string) bool {
	s.mu.RLock()
	_, ok := s.guilds[guildID]
	s.mu.RUnlock()
	return ok && s.guildHasMember(guildID, s.id.Fingerprint())
}

// canReachInviter connects to the peer an invite code names, briefly.
func (s *Service) canReachInviter(ic inviteCode) bool {
	ai, err := ownerAddrInfo(ic)
	if err != nil {
		return false
	}
	if s.host.HasConnection(ai.ID) {
		return true
	}
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()
	return s.host.Connect(ctx, ai) == nil
}
