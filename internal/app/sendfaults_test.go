package app

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/store"
)

// The disk errors SQLite reports: the owner's "disk I/O error", and a full disk.
var (
	faultIO   = errors.New("disk I/O error (10)")
	faultFull = errors.New("database or disk is full (13)")
)

// failing fails one store step with err while armed.
type failing struct {
	op    string
	err   error
	armed atomic.Bool
}

func (f *failing) hook(op string) error {
	if f.armed.Load() && op == f.op {
		return f.err
	}
	return nil
}

func byID(t *testing.T, s *Service, ch, id string) (domain.Message, bool) {
	t.Helper()
	msgs, err := s.Messages(ch, 500)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	for _, m := range msgs {
		if m.ID == id {
			return m, true
		}
	}
	return domain.Message{}, false
}

func reactors(t *testing.T, s *Service, ch, id, emoji string) int {
	m, _ := byID(t, s, ch, id)
	return len(m.Reactions[emoji])
}

// Every step of a send can hit a failing or full disk, and the answer is the
// same at each: what went out is one message (or one action) on every device,
// what did not go out reports the failure and goes out once when it is tried
// again, and no retry of the same send id is a second message or a second
// toggle.
//
// Before the publish: the send-once secret's first write, the lookup of the
// send id, the attachment's blob. After it: the message's row, and this
// device's copy of a reaction, an edit or a pin.
func TestEveryStepOfASendSurvivesAFailingDisk(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	alice := startServiceOn(t, ctx, t.TempDir(), boot)
	bob := startServiceOn(t, ctx, t.TempDir(), boot)
	g, err := alice.CreateGuild("faults")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := alice.InviteCode(g.ID)
	if _, err := bob.JoinViaInvite(code); err != nil {
		t.Fatal(err)
	}
	waitMembers(t, 30*time.Second, 2, alice, bob)
	ch := g.Channels[0].ID
	defer alice.store.InjectFault(nil)
	old := applyOwnFirstWait
	applyOwnFirstWait = 20 * time.Millisecond
	defer func() { applyOwnFirstWait = old }()

	// Bob's count of what arrives, by content, so a duplicate is seen.
	var arrived sync.Map // message id -> *atomic.Int32
	shown := func(id string) int32 {
		n, _ := arrived.LoadOrStore(id, &atomic.Int32{})
		return n.(*atomic.Int32).Load()
	}
	bob.OnMessage(func(m domain.Message) {
		if m.Kind == "" {
			n, _ := arrived.LoadOrStore(m.ID, &atomic.Int32{})
			n.(*atomic.Int32).Add(1)
		}
	})

	for _, fault := range []error{faultIO, faultFull} {
		tag := map[error]string{faultIO: "io", faultFull: "full"}[fault]

		// Before the publish, a failed step is a failed send: nothing went out.
		for _, op := range []string{store.OpSetSetting, store.OpReadMessage, store.OpSaveAttachment} {
			t.Run(fmt.Sprintf("%s/before-publish/%s", tag, op), func(t *testing.T) {
				alice.sendOnceMu.Lock()
				alice.sendOnceKey = nil // the secret's first write is a step too
				alice.sendOnceMu.Unlock()
				_ = alice.store.DeleteSetting(sendOnceKeySetting)
				body := fmt.Sprintf("before %s %s", tag, op)
				f := &failing{op: op, err: fault}
				f.armed.Store(true)
				alice.store.InjectFault(f.hook)
				send := func() (domain.Message, error) {
					if op == store.OpSaveAttachment {
						pic := "data:image/png;base64," + base64.StdEncoding.EncodeToString(picOf(t, 1024))
						return alice.SendAttachment(ch, pic, 23, 23, "", false, "", "", body, "", "sid-"+body)
					}
					return alice.SendMessageOnce(ch, body, "", "", "sid-"+body)
				}
				if _, err := send(); !errors.Is(err, fault) {
					t.Fatalf("the send did not report the failure: %v", err)
				}
				f.armed.Store(false)
				m, err := send()
				if err != nil {
					t.Fatalf("the retry after the disk recovered: %v", err)
				}
				if again, err := send(); err != nil || again.ID != m.ID {
					t.Fatalf("a second retry: %v, id %s want %s", err, again.ID, m.ID)
				}
				waitUntil(t, 30*time.Second, func() bool { _, ok := byID(t, bob, ch, m.ID); return ok }, "Bob to get it")
				time.Sleep(500 * time.Millisecond)
				if c := shown(m.ID); c != 1 {
					t.Fatalf("Bob was shown it %d times", c)
				}
				if c := countByID(t, alice, ch, m.ID); c != 1 {
					t.Fatalf("Alice holds it %d times", c)
				}
			})
		}

		// After the publish: the message's own row.
		t.Run(tag+"/after-publish/row", func(t *testing.T) {
			body := "after the publish " + tag
			f := &failing{op: store.OpSaveMessage, err: fault}
			f.armed.Store(true)
			alice.store.InjectFault(f.hook)
			m, err := alice.SendMessageOnce(ch, body, "", "", "sid-"+body)
			if err != nil {
				t.Fatalf("a message that went out was reported as failed: %v", err)
			}
			if again, err := alice.SendMessageOnce(ch, body, "", "", "sid-"+body); err != nil || again.ID != m.ID {
				t.Fatalf("a retry while the row is unsaved: %v %s", err, again.ID)
			}
			f.armed.Store(false)
			waitUntil(t, 10*time.Second, func() bool { return countByID(t, alice, ch, m.ID) == 1 }, "the row once the disk recovered")
			waitUntil(t, 30*time.Second, func() bool { _, ok := byID(t, bob, ch, m.ID); return ok }, "Bob to get it")
			time.Sleep(500 * time.Millisecond)
			if c := shown(m.ID); c != 1 {
				t.Fatalf("Bob was shown it %d times", c)
			}
		})

		// After the publish: this device's copy of an action. The action is
		// sent, applied here once the disk recovers, and a retry is no second
		// toggle anywhere.
		target, err := alice.SendMessage(ch, "act on me "+tag, "", "")
		if err != nil {
			t.Fatal(err)
		}
		waitUntil(t, 30*time.Second, func() bool { _, ok := byID(t, bob, ch, target.ID); return ok }, "Bob to get the target")

		for _, op := range []string{store.OpAction, store.OpReact} {
			t.Run(fmt.Sprintf("%s/after-publish/reaction-%s", tag, op), func(t *testing.T) {
				emoji := map[string]string{store.OpAction: "🔥", store.OpReact: "👍"}[op]
				f := &failing{op: op, err: fault}
				f.armed.Store(true)
				alice.store.InjectFault(f.hook)
				sid := "react-" + tag + op
				if err := alice.ToggleReaction(ch, target.ID, emoji, sid); err != nil {
					t.Fatalf("a reaction that went out was reported as failed: %v", err)
				}
				if err := alice.ToggleReaction(ch, target.ID, emoji, sid); err != nil {
					t.Fatalf("the retry: %v", err)
				}
				f.armed.Store(false)
				waitUntil(t, 10*time.Second, func() bool { return reactors(t, alice, ch, target.ID, emoji) == 1 }, "the reaction here once the disk recovered")
				if err := alice.ToggleReaction(ch, target.ID, emoji, sid); err != nil {
					t.Fatalf("a retry after it landed: %v", err)
				}
				waitUntil(t, 30*time.Second, func() bool { return reactors(t, bob, ch, target.ID, emoji) == 1 }, "Bob to see the reaction")
				time.Sleep(time.Second)
				for name, s := range map[string]*Service{"Alice": alice, "Bob": bob} {
					if c := reactors(t, s, ch, target.ID, emoji); c != 1 {
						t.Fatalf("%s: the reaction is on %d times after the retries (a second toggle took it off?)", name, c)
					}
				}
			})
		}

		t.Run(tag+"/after-publish/edit", func(t *testing.T) {
			f := &failing{op: store.OpEdit, err: fault}
			f.armed.Store(true)
			alice.store.InjectFault(f.hook)
			text := "edited while failing " + tag
			sid := "edit-" + tag
			if err := alice.EditMessage(ch, target.ID, text, "", sid); err != nil {
				t.Fatalf("an edit that went out was reported as failed: %v", err)
			}
			_ = alice.EditMessage(ch, target.ID, text, "", sid)
			f.armed.Store(false)
			for name, s := range map[string]*Service{"Alice": alice, "Bob": bob} {
				waitUntil(t, 30*time.Second, func() bool { m, _ := byID(t, s, ch, target.ID); return m.Content == text }, name+" to show the edit")
			}
			_ = alice.EditMessage(ch, target.ID, text, "", sid)
			time.Sleep(time.Second)
			for name, s := range map[string]*Service{"Alice": alice, "Bob": bob} {
				vs, err := s.MessageVersions(ch, target.ID)
				if err != nil {
					t.Fatal(err)
				}
				n := 0
				for _, v := range vs {
					if v.Content == text {
						n++
					}
				}
				if n != 1 {
					t.Fatalf("%s's history holds the edit %d times", name, n)
				}
			}
		})

		t.Run(tag+"/after-publish/pin", func(t *testing.T) {
			m0, _ := byID(t, alice, ch, target.ID)
			want := !m0.Pinned
			f := &failing{op: store.OpPin, err: fault}
			f.armed.Store(true)
			alice.store.InjectFault(f.hook)
			sid := "pin-" + tag
			if err := alice.PinMessage(ch, target.ID, sid); err != nil {
				t.Fatalf("a pin that went out was reported as failed: %v", err)
			}
			_ = alice.PinMessage(ch, target.ID, sid)
			f.armed.Store(false)
			_ = alice.PinMessage(ch, target.ID, sid)
			for name, s := range map[string]*Service{"Alice": alice, "Bob": bob} {
				waitUntil(t, 30*time.Second, func() bool { m, _ := byID(t, s, ch, target.ID); return m.Pinned == want }, name+" to show the pin")
			}
			time.Sleep(time.Second)
			for name, s := range map[string]*Service{"Alice": alice, "Bob": bob} {
				if m, _ := byID(t, s, ch, target.ID); m.Pinned != want {
					t.Fatalf("%s: pinned=%v after the retries, want %v", name, m.Pinned, want)
				}
			}
		})
	}
}

// A peer applies an action once per id, however many times it arrives: the
// same reaction delivered twice (a publish whose answer was lost, a mailbox
// copy after the live one) used to switch itself off on the receiver.
func TestAPeerAppliesARepeatedActionOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	alice := startServiceOn(t, ctx, t.TempDir(), boot)
	bob := startServiceOn(t, ctx, t.TempDir(), boot)
	g, err := alice.CreateGuild("dedupe")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := alice.InviteCode(g.ID)
	if _, err := bob.JoinViaInvite(code); err != nil {
		t.Fatal(err)
	}
	waitMembers(t, 30*time.Second, 2, alice, bob)
	ch := g.Channels[0].ID
	target, err := alice.SendMessage(ch, "react to me", "", "")
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 30*time.Second, func() bool { _, ok := byID(t, bob, ch, target.ID); return ok }, "Bob to get the target")

	r, err := alice.sendCore(ch, "👍", "reaction", target.ID, "", "", nil, false, "")
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 30*time.Second, func() bool { return reactors(t, bob, ch, target.ID, "👍") == 1 }, "Bob to see the reaction")
	// The same action again, under its own id: a re-delivery.
	if _, err := alice.sendCore(ch, "👍", "reaction", target.ID, "", "", nil, false, r.ID); err != nil {
		t.Fatal(err)
	}
	p, err := alice.sendCore(ch, "pin", "pin", target.ID, "", "", nil, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := alice.sendCore(ch, "pin", "pin", target.ID, "", "", nil, false, p.ID); err != nil {
		t.Fatal(err)
	}
	marker, err := alice.SendMessage(ch, "after", "", "")
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 30*time.Second, func() bool { _, ok := byID(t, bob, ch, marker.ID); return ok }, "the marker")
	time.Sleep(time.Second)
	for name, s := range map[string]*Service{"Alice": alice, "Bob": bob} {
		if c := reactors(t, s, ch, target.ID, "👍"); c != 1 {
			t.Fatalf("%s: a re-delivered reaction left it on %d times, want 1", name, c)
		}
		if m, _ := byID(t, s, ch, target.ID); !m.Pinned {
			t.Fatalf("%s: a re-delivered pin unpinned the message", name)
		}
	}
}
