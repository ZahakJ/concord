package app

import (
	"bytes"
	"strconv"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/store"
)

// Delivery and read receipts, for DMs and group DMs only.
//
// Not for guild channels, on purpose: per-member receipts in a channel with
// hundreds of members are noise for the sender and tell a large audience when
// you were online and what you looked at. A DM has a handful of people who
// already chose to talk to each other, so it is the one place either is worth
// having.
//
// Two different things travel, with different rules:
//
//   - "delivered" is a fact about transport: this device received the message
//     and applied it. It is always on, and goes to the sender's devices as an
//     MLS application message on the DM's own meta topic, so it is encrypted
//     to the conversation and no rendezvous or relay sees anything new. Only a
//     message this device really stored is acked, never one of our own, and
//     acks coalesce over a short beat so a burst is one publish.
//
//   - "read_receipt" is about the person: they looked at it. It is opt-in and
//     mutual. A device with the setting off neither sends one nor keeps one it
//     receives, and switching it on never reports what was read while it was
//     off (a receipt covers only the span the read cursor moved across after
//     the switch, see queueReadReceipt).
//
// While presence is invisible neither leaves this device: an ack says the
// device is online now, and a read receipt says the person is. Both stay in
// the queue below, under its usual bounds, and go out once presence is
// anything else (releaseHeldReceipts), whether it was changed here or on a
// sibling. The queue is in memory: what is held when the app closes is not
// sent later.
//
// Every inbound receipt is authenticated in receiveReceipt: from a device of an
// account that is a member of THIS DM, naming messages in this DM's channel
// that we sent (the store enforces the last part). Anything else is dropped
// silently. Times are kept to the second.

const (
	readReceiptsKey   = "read_receipts"    // "1" = on; absent = off
	readReceiptsAtKey = "read_receipts_at" // UnixMilli the setting last changed

	ackBeat         = 900 * time.Millisecond
	ackMaxPending   = 500 // ids held per channel before the beat; extras wait for next delivery
	ackMaxPerFrame  = 200
	receiptMaxFrame = 500 // most ids an inbound frame may name
)

// receiptsPref is the setting as it travels between the account's own devices.
type receiptsPref struct {
	On bool  `json:"on"`
	At int64 `json:"at"`
}

// OnReceipts registers a callback fired when receipts for a channel changed
// (channelID "" = the setting changed; refresh everything).
func (s *Service) OnReceipts(fn func(channelID string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onReceipts = append(s.onReceipts, fn)
}

func (s *Service) emitReceipts(channelID string) {
	s.mu.RLock()
	cbs := append([]func(string){}, s.onReceipts...)
	s.mu.RUnlock()
	for _, fn := range cbs {
		fn(channelID)
	}
}

func (s *Service) loadReceiptsPref() {
	v, _ := s.store.GetSetting(readReceiptsKey)
	s.receiptsOn.Store(v == "1")
}

// ReadReceiptsEnabled reports whether read receipts are exchanged.
func (s *Service) ReadReceiptsEnabled() bool { return s.receiptsOn.Load() }

// SetReadReceiptsEnabled flips the setting for the whole account: it is stamped,
// stored, and sent to the account's other devices.
func (s *Service) SetReadReceiptsEnabled(on bool) error {
	return s.applyReceiptsPref(receiptsPref{On: on}, true)
}

// applyReceiptsPref stores p if it is newer than what we hold. local marks a
// change made here, which is then broadcast; a remote one never is.
//
// A local change is stamped after the stamp held (stampAfter), read under the
// same lock. A sibling's clock may run up to readSkew ahead, and a later
// choice made here must still win everywhere.
func (s *Service) applyReceiptsPref(p receiptsPref, local bool) error {
	s.receiptsPrefMu.Lock()
	defer s.receiptsPrefMu.Unlock()
	cur, _ := s.store.GetSetting(readReceiptsAtKey)
	curAt, _ := strconv.ParseInt(cur, 10, 64)
	if local {
		p.At = stampAfter(curAt)
	} else {
		if p.At <= curAt {
			return nil
		}
		if p.At > time.Now().Add(readSkew).UnixMilli() {
			p.At = time.Now().UnixMilli() // a clock in the future must not freeze the setting
		}
	}
	val := "0"
	if p.On {
		val = "1"
	}
	if err := s.store.SetSetting(readReceiptsKey, val); err != nil {
		return err
	}
	if err := s.store.SetSetting(readReceiptsAtKey, strconv.FormatInt(p.At, 10)); err != nil {
		return err
	}
	was := s.receiptsOn.Swap(p.On)
	if !p.On {
		// What others told us while it was on is not kept once it is off.
		_ = s.store.ClearReadReceipts()
		s.dropPendingReadReceipts()
	}
	if was != p.On {
		s.emitReceipts("")
	}
	if local {
		s.broadcastReceiptsPref(p)
	}
	return nil
}

// currentReceiptsPref is the setting with its stamp, for the hello.
func (s *Service) currentReceiptsPref() *receiptsPref {
	at, _ := s.store.GetSetting(readReceiptsAtKey)
	n, _ := strconv.ParseInt(at, 10, 64)
	if n == 0 {
		return nil // never touched: nothing to say, the default is off everywhere
	}
	return &receiptsPref{On: s.receiptsOn.Load(), At: n}
}

func (s *Service) broadcastReceiptsPref(p receiptsPref) {
	notes, ok := s.findNotesDM()
	if !ok {
		return
	}
	s.publishMeta(notes.GroupID, guildMeta{Type: "receipts_pref", Receipts: &p})
}

// receiptGuild returns the DM that owns channelID when it is one receipts
// apply to: a DM or group DM that is not our Notes (which has only our own
// devices in it).
func (s *Service) receiptGuild(channelID string) (domain.Guild, bool) {
	s.mu.RLock()
	gid, ok := s.channelToGuild[channelID]
	gp, ok2 := s.guilds[gid]
	var g domain.Guild
	if ok2 {
		g = *gp
	}
	s.mu.RUnlock()
	if !ok || !ok2 || g.Kind != "dm" || isNotesGuild(g, s.PublicKey()) {
		return domain.Guild{}, false
	}
	return g, true
}

// noteDelivered queues an ack for a message this device has just applied.
func (s *Service) noteDelivered(m domain.Message) {
	if m.Kind != "" || bytes.Equal(m.Sender, s.PublicKey()) {
		return // only ordinary messages, and never our own (any device of ours)
	}
	if s.IsBlocked(accountFingerprintOf(m.Sender)) {
		return // blocking someone includes not telling them anything
	}
	if _, ok := s.receiptGuild(m.ChannelID); !ok {
		return
	}
	s.receiptMu.Lock()
	if s.pendingAcks == nil {
		s.pendingAcks = map[string][]string{}
	}
	if len(s.pendingAcks[m.ChannelID]) < ackMaxPending {
		s.pendingAcks[m.ChannelID] = append(s.pendingAcks[m.ChannelID], m.ID)
	}
	s.armReceiptTimerLocked()
	s.receiptMu.Unlock()
}

// queueReadReceipt queues "read through at" for a channel whose cursor moved
// from since to at (UnixMilli). Only the span the cursor crossed just now is
// reported, so messages read while the setting was off stay unreported.
func (s *Service) queueReadReceipt(channelID string, since, at int64) {
	if !s.receiptsOn.Load() || at <= since {
		return
	}
	if _, ok := s.receiptGuild(channelID); !ok {
		return
	}
	s.receiptMu.Lock()
	if s.pendingReads == nil {
		s.pendingReads = map[string][2]int64{}
	}
	if cur, ok := s.pendingReads[channelID]; ok {
		if cur[0] < since {
			since = cur[0]
		}
		if cur[1] > at {
			at = cur[1]
		}
	}
	s.pendingReads[channelID] = [2]int64{since, at}
	s.armReceiptTimerLocked()
	s.receiptMu.Unlock()
}

func (s *Service) dropPendingReadReceipts() {
	s.receiptMu.Lock()
	s.pendingReads = nil
	s.receiptMu.Unlock()
}

// armReceiptTimerLocked starts the beat unless one is running or the service
// is closing (stopBeats): a timer must not fire into a closed service.
func (s *Service) armReceiptTimerLocked() {
	if s.receiptTimer == nil && !s.beatsStopped.Load() {
		s.receiptTimer = time.AfterFunc(ackBeat, s.beat(s.flushReceipts))
	}
}

// releaseHeldReceipts sends what was held while presence was invisible, once
// it no longer is. Called whenever presence may have changed.
func (s *Service) releaseHeldReceipts() {
	if s.invisible() {
		return
	}
	s.receiptMu.Lock()
	if len(s.pendingAcks) > 0 || len(s.pendingReads) > 0 {
		s.armReceiptTimerLocked()
	}
	s.receiptMu.Unlock()
}

// flushReceipts publishes what has queued, one frame per channel. While
// presence is invisible it publishes nothing and keeps the queue.
func (s *Service) flushReceipts() {
	if s.beatsStopped.Load() || s.ctx.Err() != nil {
		return
	}
	hold := s.invisible()
	s.receiptMu.Lock()
	s.receiptTimer = nil
	if hold {
		s.receiptMu.Unlock()
		return
	}
	acks, reads := s.pendingAcks, s.pendingReads
	s.pendingAcks, s.pendingReads = nil, nil
	s.receiptMu.Unlock()
	on := s.receiptsOn.Load()
	for ch, ids := range acks {
		for len(ids) > 0 {
			n := min(len(ids), ackMaxPerFrame)
			s.publishReceipt(ch, guildMeta{Type: "delivered", ChannelID: ch, MsgIDs: ids[:n]})
			ids = ids[n:]
		}
	}
	if !on {
		return
	}
	for ch, span := range reads {
		s.publishReceipt(ch, guildMeta{Type: "read_receipt", ChannelID: ch, Since: span[0], At: span[1]})
	}
}

func (s *Service) publishReceipt(channelID string, m guildMeta) {
	g, ok := s.receiptGuild(channelID)
	if !ok || s.mls == nil || s.ps == nil {
		return
	}
	// publishMeta sends nothing from a copy that CannotRepairHere.
	s.publishMeta(g.GroupID, m)
}

// receiveReceipt handles an inbound "delivered" or "read_receipt" frame. actor
// is the account MLS authenticated as the sender. Silent on anything off.
func (s *Service) receiveReceipt(guildID, actor string, m guildMeta) {
	if actor == "" || actor == s.id.Fingerprint() || m.ChannelID == "" {
		return
	}
	g, ok := s.receiptGuild(m.ChannelID)
	if !ok || g.ID != guildID || !s.guildHasMember(guildID, actor) {
		return
	}
	now := time.Now()
	var changed []string
	var err error
	switch m.Type {
	case "delivered":
		if len(m.MsgIDs) == 0 || len(m.MsgIDs) > receiptMaxFrame {
			return
		}
		changed, err = s.store.RecordDelivered(s.PublicKey(), m.ChannelID, actor, m.MsgIDs, now.Unix())
	case "read_receipt":
		// Mutual: with ours off we do not keep what others report.
		if !s.receiptsOn.Load() || m.At <= 0 || m.Since < 0 || m.At <= m.Since ||
			m.At > now.Add(readSkew).UnixMilli() {
			return
		}
		// Message times are UnixNano; the cursor is UnixMilli, rounded down.
		since := (m.Since+1)*int64(time.Millisecond) - 1
		through := (m.At+1)*int64(time.Millisecond) - 1
		changed, err = s.store.RecordRead(s.PublicKey(), m.ChannelID, actor, since, through, now.Unix())
	}
	if err == nil && len(changed) > 0 {
		s.emitReceipts(m.ChannelID)
	}
}

// ReceiptView is what the UI draws under one of our messages in a DM.
// Delivered/Read are true when EVERY other member's devices have reported
// (for a one-to-one DM, the other person). The counts are the per-member
// detail, which came free with the aggregate.
type ReceiptView struct {
	Delivered   bool `json:"delivered"`
	Read        bool `json:"read"`
	DeliveredBy int  `json:"deliveredBy"`
	ReadBy      int  `json:"readBy"`
	Of          int  `json:"of"`
}

// Receipts returns the receipts held for our messages in a DM channel, by
// message id. Read is only ever reported while our own setting is on.
func (s *Service) Receipts(channelID string) (map[string]ReceiptView, error) {
	out := map[string]ReceiptView{}
	g, ok := s.receiptGuild(channelID)
	if !ok {
		return out, nil
	}
	me := s.id.Fingerprint()
	others := map[string]bool{}
	for _, f := range s.guildMemberFingerprints(g.ID) {
		if f != me {
			others[f] = true
		}
	}
	if len(others) == 0 {
		return out, nil
	}
	rows, err := s.store.ReceiptsFor(channelID)
	if err != nil {
		return nil, err
	}
	showRead := s.receiptsOn.Load()
	for id, rs := range rows {
		v := ReceiptView{Of: len(others)}
		for _, r := range storeReceiptsOf(rs, others) {
			if r.Delivered > 0 {
				v.DeliveredBy++
			}
			if showRead && r.Read > 0 {
				v.ReadBy++
			}
		}
		v.Delivered = v.DeliveredBy == v.Of
		v.Read = v.ReadBy == v.Of
		out[id] = v
	}
	return out, nil
}

func storeReceiptsOf(rs []store.Receipt, members map[string]bool) []store.Receipt {
	out := rs[:0:0]
	for _, r := range rs {
		if members[r.Account] {
			out = append(out, r)
		}
	}
	return out
}

// receiptsPrefFor offers the setting to a proven device of this account only.
func (s *Service) receiptsPrefFor(p peer.ID) *receiptsPref {
	if !s.ownDevice(p) || s.revocationFor(p) != nil {
		return nil
	}
	return s.currentReceiptsPref()
}

func (s *Service) adoptOfferedReceiptsPref(from peer.ID, p *receiptsPref) {
	if p == nil || p.At <= 0 || !s.ownDevice(from) || s.revocationFor(from) != nil {
		return
	}
	_ = s.applyReceiptsPref(*p, false)
}
