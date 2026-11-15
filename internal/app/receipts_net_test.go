package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// dmChannelFor starts a DM from a to b and returns a's and b's channel ids,
// once both sides hold the 2-member conversation.
func dmChannelFor(t *testing.T, a, b *Service) (guildID, aCh, bCh string) {
	t.Helper()
	dm, err := a.StartDM(b.Fingerprint())
	if err != nil {
		t.Fatalf("StartDM: %v", err)
	}
	waitUntil(t, 30*time.Second, func() bool {
		for _, gg := range b.Guilds() {
			if gg.ID == dm.ID && len(gg.Channels) > 0 {
				bCh = gg.Channels[0].ID
			}
		}
		na, _ := a.MemberCount(dm.ID)
		nb, _ := b.MemberCount(dm.ID)
		return bCh != "" && na == 2 && nb == 2
	}, "DM did not converge")
	return dm.ID, dm.Channels[0].ID, bCh
}

func sendOnce(t *testing.T, s *Service, ch string, r *recorder, body string) domain.Message {
	t.Helper()
	m, err := s.SendMessage(ch, body, "", "")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if r != nil {
		waitUntil(t, 20*time.Second, func() bool { return r.has(body) }, "message never arrived: "+body)
	}
	return m
}

func TestDeliveredThenReadTicksAndForgeriesAreIgnored(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, b, c := startService(t, ctx), startService(t, ctx), startService(t, ctx)

	g, err := a.CreateGuild("hub")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := a.InviteCode(g.ID)
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatal(err)
	}
	if _, err := c.JoinViaInvite(code); err != nil {
		t.Fatal(err)
	}
	waitMembers(t, 30*time.Second, 3, a, b, c)

	abGuild, abA, abB := dmChannelFor(t, a, b)
	acGuild, _, _ := dmChannelFor(t, a, c)
	_ = abGuild
	rb := &recorder{}
	b.OnMessage(rb.add)

	tick := func(s *Service, ch, id string) ReceiptView {
		v, err := s.Receipts(ch)
		if err != nil {
			t.Fatal(err)
		}
		return v[id]
	}

	// Delivered is always on: neither side has touched the setting.
	m1 := sendOnce(t, a, abA, rb, "tick one")
	waitUntil(t, 20*time.Second, func() bool { return tick(a, abA, m1.ID).Delivered }, "no delivered tick")
	if tick(a, abA, m1.ID).Read {
		t.Fatal("read tick with receipts off on both sides")
	}

	// B reads with receipts off on both sides: still nothing.
	if _, err := b.MarkRead(abB, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2500 * time.Millisecond)
	if tick(a, abA, m1.ID).Read {
		t.Fatal("read tick while both have receipts off")
	}

	// Only B turns them on: A still keeps nothing (mutual), and the read of
	// m1, which happened while B's setting was off, is never reported.
	if err := b.SetReadReceiptsEnabled(true); err != nil {
		t.Fatal(err)
	}
	m2 := sendOnce(t, a, abA, rb, "tick two")
	time.Sleep(10 * time.Millisecond)
	if _, err := b.MarkRead(abB, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2500 * time.Millisecond)
	if tick(a, abA, m2.ID).Read {
		t.Fatal("A showed a read receipt with its own setting off")
	}
	if rs, _ := a.store.ReceiptsFor(abA); func() bool {
		for _, rr := range rs {
			for _, r := range rr {
				if r.Read != 0 {
					return true
				}
			}
		}
		return false
	}() {
		t.Fatal("A stored a read receipt with its own setting off")
	}

	// Both on: the read tick arrives, for m2 and not for m1 (read while B was off).
	if err := a.SetReadReceiptsEnabled(true); err != nil {
		t.Fatal(err)
	}
	m3 := sendOnce(t, a, abA, rb, "tick three")
	time.Sleep(10 * time.Millisecond)
	if _, err := b.MarkRead(abB, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 20*time.Second, func() bool { return tick(a, abA, m3.ID).Read }, "no read tick with both on")
	if tick(a, abA, m1.ID).Read {
		t.Fatal("read tick backfilled for a message read while the setting was off")
	}

	// Forgeries. C (a member of the A-C DM only) frames a delivered ack and a
	// read receipt for A's A-B messages inside its own A-C group, and B acks a
	// message that B itself wrote. A must keep none of it.
	bOwn := sendOnce(t, b, abB, nil, "from b")
	waitUntil(t, 20*time.Second, func() bool { return tick(b, abB, bOwn.ID).Delivered }, "A never acked B's message")
	forge := func(s *Service, guildID string, m guildMeta) {
		s.mu.RLock()
		gp := s.guilds[guildID]
		var groupID []byte
		if gp != nil {
			groupID = gp.GroupID
		}
		s.mu.RUnlock()
		if groupID == nil {
			t.Fatalf("forger holds no group %s", guildID)
		}
		payload, _ := json.Marshal(m)
		ct, err := s.mls.Encrypt(s.ctx, groupID, payload)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ps.Publish(s.ctx, domain.GuildMetaTopicID(groupID), ct); err != nil {
			t.Fatal(err)
		}
	}
	forge(c, acGuild, guildMeta{Type: "delivered", ChannelID: abA, MsgIDs: []string{m1.ID, m2.ID, m3.ID}})
	forge(c, acGuild, guildMeta{Type: "read_receipt", ChannelID: abA, Since: 0, At: time.Now().UnixMilli()})
	forge(b, abGuild, guildMeta{Type: "delivered", ChannelID: abA, MsgIDs: []string{bOwn.ID}})
	time.Sleep(3 * time.Second)
	rs, _ := a.store.ReceiptsFor(abA)
	if _, ok := rs[bOwn.ID]; ok {
		t.Fatal("A recorded a receipt for a message A did not send")
	}
	for id, rr := range rs {
		for _, r := range rr {
			if r.Account != b.Fingerprint() {
				t.Fatalf("receipt for %s from %s, who is not in that DM", id, r.Account)
			}
		}
	}
	if tick(a, abA, m1.ID).Read {
		t.Fatal("forged read receipt was accepted")
	}
}
