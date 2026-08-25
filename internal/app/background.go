package app

import (
	"fmt"
	"log"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/store"
)

// Background mode (phones). Measured on Android with the app backgrounded and
// the screen off, the core kept up the exact same cadence as foregrounded:
// ~4 packets/second combined (DHT discovery every 15s, guild reconcile every
// 20s, mailbox sweep every 60s, own-device dials every 30s, plus gossip
// heartbeats), ~45 KB/min through the radio. The CPU cost is nothing — the
// battery cost is the radio: a packet every few hundred milliseconds means the
// Wi-Fi chip never enters power-save and a cellular modem never leaves the
// high-power RRC state, which reads as "Concord eats the battery" even though
// the app is doing almost no work.
//
// So the shells tell the core when the app leaves the screen, and every
// periodic loop stretches to one slow shared beat. What stays untouched, on
// purpose, is everything that makes a backgrounded phone reachable: the open
// connections (rendezvous, relay reservation, gossip mesh) are kept, gossipsub
// keeps delivering messages instantly, and the foreground-return path (Nudge +
// the loops waking on bgWake) catches up the moment the user comes back.
const backgroundBeat = 3 * time.Minute

// SetBackground switches the periodic loops between their foreground cadence
// and the slow background beat. Idempotent; safe from any goroutine. On the
// background→foreground edge every throttled loop is woken immediately, so
// returning to the app never waits out a beat that was scheduled while asleep.
func (s *Service) SetBackground(bg bool) {
	s.bgMu.Lock()
	changed := s.bg != bg
	s.bg = bg
	var wake chan struct{}
	if changed && !bg && s.bgWake != nil {
		wake = s.bgWake
		s.bgWake = nil
	}
	s.bgMu.Unlock()
	if !changed {
		return
	}
	s.host.SetBackground(bg)
	// Typing topics are meshed only while somebody is looking at a screen; see
	// typing.go. This is the one thing background mode drops outright rather
	// than slowing down, and it is safe to drop precisely because the feature
	// has no meaning in this state.
	s.syncTypingTopics()
	if wake != nil {
		close(wake) // foreground again: run the throttled loops now
	}
}

func (s *Service) backgrounded() bool {
	s.bgMu.Lock()
	defer s.bgMu.Unlock()
	return s.bg
}

// bgPace stretches a foreground interval to the background beat while the app
// is backgrounded. Intervals already slower than the beat are left alone.
func (s *Service) bgPace(fg time.Duration) time.Duration {
	if s.backgrounded() && fg < backgroundBeat {
		return backgroundBeat
	}
	return fg
}

// bgWakeCh returns a channel closed on the next background→foreground switch,
// so paced loops can park on it alongside their timer.
func (s *Service) bgWakeCh() <-chan struct{} {
	s.bgMu.Lock()
	defer s.bgMu.Unlock()
	if s.bgWake == nil {
		s.bgWake = make(chan struct{})
	}
	return s.bgWake
}

// ---- scheduled sends ----
// Send-later used to live in the frontend: localStorage plus a JS ticker, which
// meant closing the window silently killed every queued message. The queue now
// lives in the store and this loop fires it, so a scheduled send only needs
// this device's Concord running — in the tray, backgrounded, window closed —
// when the time comes.

// scheduledSendTick paces the sweep. Coarse on purpose, like the event
// announcer: "send at 9:00" landing at 9:00:29 is invisible, and bgPace
// stretches it to the background beat on a backgrounded phone (a queued
// message then lands within one beat of its time, still better than never).
const scheduledSendTick = 30 * time.Second

// ScheduleSend queues a message to be sent later on this device. fireAt is
// unix seconds; a past time fires on the next sweep. Returns the queue row id.
func (s *Service) ScheduleSend(channelID, content, replyTo string, fireAt int64) (string, error) {
	s.mu.RLock()
	_, ok := s.channelToGuild[channelID]
	s.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("app: unknown channel %s", channelID)
	}
	if content == "" {
		return "", fmt.Errorf("app: empty scheduled message")
	}
	ss := store.ScheduledSend{
		ID:        domain.NewID(),
		ChannelID: channelID,
		Content:   content,
		ReplyTo:   replyTo,
		FireAt:    fireAt,
		Created:   time.Now().Unix(),
	}
	if err := s.store.AddScheduledSend(ss); err != nil {
		return "", err
	}
	return ss.ID, nil
}

// CancelScheduledSend removes a queued send before it fires.
func (s *Service) CancelScheduledSend(id string) error {
	return s.store.DeleteScheduledSend(id)
}

// ScheduledSends returns the queue, soonest first, for the manager UI.
func (s *Service) ScheduledSends() ([]store.ScheduledSend, error) {
	return s.store.ScheduledSends()
}

// runScheduledSendLoop sweeps the send-later queue. Started once at service
// start; lives until shutdown.
func (s *Service) runScheduledSendLoop() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.bgWakeCh():
			// Foregrounded: fire anything that came due during the slow beat.
		case <-time.After(s.bgPace(scheduledSendTick)):
		}
		s.fireDueScheduledSends(time.Now().Unix())
	}
}

// fireDueScheduledSends sends every queued message whose time has come through
// the normal SendMessage path, deleting each row only after its send succeeds —
// a transient failure (guild still healing, no peers yet) keeps the row and
// retries next sweep. A row whose channel no longer exists is dropped: it can
// never send, and retrying it forever would just be a log leak.
// Split from the loop so tests can drive the time boundary directly.
func (s *Service) fireDueScheduledSends(now int64) {
	due, err := s.store.DueScheduledSends(now)
	if err != nil {
		log.Printf("concord/app: scheduled sends: %v", err)
		return
	}
	for _, ss := range due {
		s.mu.RLock()
		_, known := s.channelToGuild[ss.ChannelID]
		s.mu.RUnlock()
		if !known {
			log.Printf("concord/app: dropping scheduled send %s — its channel is gone", ss.ID)
			_ = s.store.DeleteScheduledSend(ss.ID)
			continue
		}
		if _, err := s.SendMessage(ss.ChannelID, ss.Content, ss.ReplyTo, ""); err != nil {
			log.Printf("concord/app: scheduled send %s failed (will retry): %v", ss.ID, err)
			continue
		}
		_ = s.store.DeleteScheduledSend(ss.ID)
	}
}
