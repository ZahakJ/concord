package app

import (
	"log"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
	"github.com/libp2p/go-libp2p/core/peer"
)

// admission.go turns a wave of joiners into a small number of epochs.
//
// One join used to be one MLS commit: propose the add, commit it, publish the
// commit, hand back the welcome. That is the right shape for the case it was
// written for — somebody pastes an invite code — and the wrong shape for the
// case a guild grows in. A commit is not free for the person who mints it; it
// is expensive for everybody ELSE, because commits must be applied gaplessly
// and in order. Fifty people following an imported community is fifty commits
// every existing member has to receive, authorize and apply, each one rekeying
// the group and invalidating any message that was already in flight. The
// members who drop one of those fifty frames are the ones who then need a heal,
// which costs two more commits. The cost of a join wave is superlinear in the
// worst case and nobody enjoys any of it.
//
// MLS itself has no such rule: a commit may carry any number of Add proposals
// and produces ONE welcome addressed to all of them. So the admissions are
// collected instead of served one at a time — the same trick a database plays
// when it batches transactions into one fsync — and a wave of N joiners costs
// one epoch per WAVE rather than one per joiner.
//
// The queue is not a delay. A joiner arriving at an idle guild is committed
// immediately, exactly as before. The batching comes from the window that
// already exists: while one commit is in flight, everybody else who dials in is
// waiting anyway, and they wait in a queue that then commits together. The
// short linger below only ever runs when a wave is already visible.

const (
	// admissionWindow is how long a batch that has ALREADY seen more than one
	// request lingers for stragglers before committing. It is bounded by what a
	// joiner will sit through without the join feeling broken, not by what
	// would batch best; a wave arriving over a WAN spreads out by more than
	// this, and the answer to that is the queue behind the in-flight commit,
	// not a longer wait.
	admissionWindow = 300 * time.Millisecond
	// admissionPoll is how often the linger re-reads the batch size. Coarse on
	// purpose: this is a throughput heuristic, and a wakeup that misses one
	// arrival by 10ms costs that arrival the next epoch, nothing more.
	admissionPoll = 10 * time.Millisecond
)

// maxAdmissionBatch closes a lingering batch early. The engine caps a commit's
// adds too (mls.maxBatchAdds); this is the app-side twin so the surplus queues
// for the next commit instead of being dropped on the floor.
//
// A variable rather than a constant only so the load test can pin it to 1 and
// measure what a join wave cost before any of this existed — one joiner per
// commit is exactly the old behaviour. Nothing in the product writes it.
var maxAdmissionBatch = 32

// admissionTicket is one joiner's place in a batch. The requesting goroutine
// fills in req/from and then waits: on done, for the batch it rides to be
// served, or on lead, to be told it is now the one who has to run a batch.
type admissionTicket struct {
	req  inviteRequest
	from peer.ID
	done chan struct{}
	lead chan struct{}

	welcome []byte
	err     string // non-empty: this joiner alone was refused
}

// admissionBatch is the queue of joiners waiting on one guild. Tickets are
// appended under admitMu; a leader takes a commit's worth off the front and
// hands leadership of whatever is left to the next in line, so nobody is ever
// left queued with nobody to serve them.
type admissionBatch struct {
	tickets []*admissionTicket
}

// admit queues a validated join request, waits for its batch to commit, and
// returns that joiner's welcome. The caller has already decided this request is
// one we are willing to serve (authorization, ban list, credential binding); by
// this point the only remaining question is MLS's.
func (s *Service) admit(g *domain.Guild, req inviteRequest, from peer.ID) (welcome []byte, refusal string) {
	t := &admissionTicket{
		req: req, from: from,
		done: make(chan struct{}),
		lead: make(chan struct{}),
	}

	s.admitMu.Lock()
	if s.admitting == nil {
		s.admitting = map[string]*admissionBatch{}
	}
	b, queued := s.admitting[g.ID]
	if !queued {
		b = &admissionBatch{}
		s.admitting[g.ID] = b
	}
	b.tickets = append(b.tickets, t)
	s.admitMu.Unlock()

	for {
		if !queued {
			s.leadAdmission(g, b)
			return t.welcome, t.err
		}
		select {
		case <-t.done:
			return t.welcome, t.err
		case <-t.lead:
			// The batch ahead of us filled up. We inherit the queue.
			queued = false
		}
	}
}

// leadAdmission is the batch leader's job: linger if a wave is visible, take
// the commit lock, take a commit's worth off the queue, and commit it as one
// epoch. If the queue is longer than one commit may carry, the next ticket in
// line is promoted to lead the remainder.
func (s *Service) leadAdmission(g *domain.Guild, b *admissionBatch) {
	s.admitMu.Lock()
	wave := len(b.tickets) > 1 || s.admitCommitting[g.ID] > 0
	s.admitMu.Unlock()
	if wave {
		s.lingerAdmission(b)
	}
	// inviteMu is what keeps epochs sequential; a leader blocked here is the
	// good case, because its queue keeps growing while it waits.
	s.inviteMu.Lock()
	s.admitMu.Lock()
	if s.admitCommitting == nil {
		s.admitCommitting = map[string]int{}
	}
	s.admitCommitting[g.ID]++
	n := len(b.tickets)
	if n > maxAdmissionBatch {
		n = maxAdmissionBatch
	}
	tickets, rest := b.tickets[:n], b.tickets[n:]
	var successor *admissionTicket
	if len(rest) == 0 {
		delete(s.admitting, g.ID)
	} else {
		b.tickets = rest
		successor = rest[0]
	}
	s.admitMu.Unlock()

	s.commitAdmissions(g, tickets)

	s.admitMu.Lock()
	if s.admitCommitting[g.ID]--; s.admitCommitting[g.ID] <= 0 {
		delete(s.admitCommitting, g.ID)
	}
	s.admitMu.Unlock()
	s.inviteMu.Unlock()

	for _, t := range tickets {
		close(t.done)
	}
	if successor != nil {
		close(successor.lead)
	}
}

// lingerAdmission gives a batch that has already seen a second request a short
// window to collect the rest of the wave.
func (s *Service) lingerAdmission(b *admissionBatch) {
	deadline := time.Now().Add(admissionWindow)
	for {
		s.admitMu.Lock()
		n := len(b.tickets)
		s.admitMu.Unlock()
		if n >= maxAdmissionBatch || !time.Now().Before(deadline) {
			return
		}
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(admissionPoll):
		}
	}
}

// commitAdmissions admits a whole batch. Caller holds inviteMu.
//
// The batch may fail for one joiner without failing for the others: a key
// package the group cannot accept is dropped, and the usual reason for that —
// a leaf from an earlier attempt still sitting in the tree, so this joiner is
// retrying — is repaired the way the single-join path always repaired it, by
// evicting the stale leaf and asking again. That second ask is itself batched,
// so a wave in which several joiners are retrying still costs two epochs and
// not two per joiner.
func (s *Service) commitAdmissions(g *domain.Guild, tickets []*admissionTicket) {
	served := s.admitRound(g, tickets)

	var retry []*admissionTicket
	for _, t := range tickets {
		if served[t] || len(t.req.Credential) == 0 {
			continue
		}
		// The stale-leaf eviction. It is its own commit — a Remove cannot be
		// folded into a batch that also re-adds the same signature key, since
		// the tree would hold it twice mid-commit — but it is rare, and the
		// re-add it enables rides the batch below.
		rmCommit, err := s.mls.Remove(s.ctx, g.GroupID, t.req.Credential)
		if err != nil {
			continue
		}
		s.logCommit(g.GroupID, rmCommit)
		_ = s.ps.Publish(s.ctx, domain.ControlTopicID(g.GroupID), rmCommit)
		retry = append(retry, t)
	}
	if len(retry) > 0 {
		for t := range s.admitRound(g, retry) {
			served[t] = true
		}
	}

	for _, t := range tickets {
		if !served[t] {
			t.err = "invite failed"
		}
	}
	if len(served) == 0 {
		return
	}
	// Per-batch bookkeeping. rememberMembers walks the roster and flushes the
	// peer cache, and emitGuildUpdate re-renders every open client: both are
	// per-EPOCH facts, so doing them once is not an optimization, it is the
	// correct count.
	s.rememberMembers()
	s.emitGuildUpdate()
}

// admitRound runs one batch commit and fills in the welcome for every ticket
// that made it in. The returned set is who was served.
func (s *Service) admitRound(g *domain.Guild, tickets []*admissionTicket) map[*admissionTicket]bool {
	served := map[*admissionTicket]bool{}
	kps := make([][]byte, 0, len(tickets))
	for _, t := range tickets {
		kps = append(kps, t.req.KeyPackage)
	}
	commit, welcome, accepted, err := s.mls.AddMembers(s.ctx, g.GroupID, kps)
	if err != nil {
		return served
	}
	s.logCommit(g.GroupID, commit)
	// One publish for the whole batch: the saving that makes a join wave cheap
	// for the members who are merely watching it happen.
	if err := s.ps.Publish(s.ctx, domain.ControlTopicID(g.GroupID), commit); err != nil {
		// The commit is already merged into our own state and logged, so the
		// group has moved whether or not the gossip went out. Members that
		// missed it bridge the gap from the commit log on their next sync.
		log.Printf("concord/app: guild %s: publishing a batch admission commit failed: %v", g.ID, err)
	}
	for _, i := range accepted {
		t := tickets[i]
		t.welcome = welcome
		served[t] = true
		if len(t.req.Credential) > 0 {
			// Learn the joiner's display name over this reliable stream (their
			// gossip announce may be lost while their mesh warms up).
			s.learnProfile(accountFingerprintOf(t.req.Credential), t.req.Profile)
			s.clearPendingDMInvite(g.ID, accountFingerprintOf(t.req.Credential))
		}
		// Keep this member reachable, especially over a relay.
		s.host.Protect(t.from)
	}
	return served
}
