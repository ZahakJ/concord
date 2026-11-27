package store

import (
	"errors"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// The disk errors SQLite reports, as the owner saw them.
var (
	errIO   = errors.New("disk I/O error (10)")
	errFull = errors.New("database or disk is full (13)")
)

func savedTarget(t *testing.T, s *Store) domain.Message {
	t.Helper()
	m, err := domain.NewMessage("ch-1", []byte("author-key-32-bytes-long-padding"), "a message to act on")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveMessage(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func reactionsOn(t *testing.T, s *Store, id string) []string {
	t.Helper()
	m, ok, err := s.MessageByID(id)
	if err != nil || !ok {
		t.Fatalf("MessageByID: %v %v", ok, err)
	}
	return m.Reactions["👍"]
}

// A reaction is a toggle, so the same action applied twice is the same as
// none: a reaction retried after a failure, or delivered by two paths, used
// to switch itself off again. An action is applied once per id.
func TestAReactionActionIsAppliedOncePerID(t *testing.T) {
	s, _ := openTestStore(t)
	m := savedTarget(t, s)
	for i, want := range []struct {
		action  string
		applied bool
		on      bool
	}{
		{"act-1", true, true},
		{"act-1", false, true}, // the same action again changes nothing
		{"act-2", true, false}, // a new one toggles
		{"act-2", false, false},
	} {
		added, applied, err := s.ToggleReactionOnce(want.action, m.ID, "fpr-a", "👍")
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		if applied != want.applied || (applied && added != want.on) {
			t.Fatalf("step %d (%s): applied=%v added=%v, want applied=%v on=%v", i, want.action, applied, added, want.applied, want.on)
		}
		if on := len(reactionsOn(t, s, m.ID)) == 1; on != want.on {
			t.Fatalf("step %d: reaction on=%v, want %v", i, on, want.on)
		}
	}
}

func TestAPinActionIsAppliedOncePerID(t *testing.T) {
	s, _ := openTestStore(t)
	m := savedTarget(t, s)
	pinned := func() bool {
		got, _, _ := s.MessageByID(m.ID)
		return got.Pinned
	}
	if on, applied, err := s.TogglePinnedOnce("pin-1", m.ID); err != nil || !applied || !on {
		t.Fatalf("first pin: on=%v applied=%v err=%v", on, applied, err)
	}
	if _, applied, err := s.TogglePinnedOnce("pin-1", m.ID); err != nil || applied || !pinned() {
		t.Fatalf("the same pin again: applied=%v pinned=%v err=%v", applied, pinned(), err)
	}
	if on, applied, err := s.TogglePinnedOnce("pin-2", m.ID); err != nil || !applied || on || pinned() {
		t.Fatalf("an unpin: on=%v applied=%v pinned=%v err=%v", on, applied, pinned(), err)
	}
}

// A failing disk applies nothing and records nothing, so the action is
// applied, once, when it is tried again after the disk recovers. The fault
// can strike at the action's record or at the change itself.
func TestAnActionThatHitsAFailingDiskIsAppliedOnceItRecovers(t *testing.T) {
	for _, fault := range []error{errIO, errFull} {
		for _, op := range []string{OpAction, OpReact} {
			s, _ := openTestStore(t)
			m := savedTarget(t, s)
			s.InjectFault(func(at string) error {
				if at == op {
					return fault
				}
				return nil
			})
			if _, _, err := s.ToggleReactionOnce("act-1", m.ID, "fpr-a", "👍"); !errors.Is(err, fault) {
				t.Fatalf("%s at %s: err=%v", fault, op, err)
			}
			if len(reactionsOn(t, s, m.ID)) != 0 {
				t.Fatalf("%s at %s: the reaction was half applied", fault, op)
			}
			if seen, err := s.ActionApplied("act-1"); err != nil || seen {
				t.Fatalf("%s at %s: the failed action is recorded as applied (%v %v)", fault, op, seen, err)
			}
			s.InjectFault(nil)
			if added, applied, err := s.ToggleReactionOnce("act-1", m.ID, "fpr-a", "👍"); err != nil || !applied || !added {
				t.Fatalf("%s at %s, recovered: applied=%v added=%v err=%v", fault, op, applied, added, err)
			}
			if _, applied, _ := s.ToggleReactionOnce("act-1", m.ID, "fpr-a", "👍"); applied {
				t.Fatalf("%s at %s: applied twice after the recovery", fault, op)
			}
		}
	}
}

// The record of applied actions is kept for as long as a duplicate can turn
// up (a mailbox holds a copy for weeks), and no longer.
func TestAppliedActionsArePrunedPastTheirKeep(t *testing.T) {
	s, _ := openTestStore(t)
	m := savedTarget(t, s)
	if _, _, err := s.ToggleReactionOnce("old", m.ID, "fpr-a", "👍"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.NoteActionApplied("new"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE applied_actions SET at = ? WHERE id = 'old'`, time.Now().Add(-appliedActionKeep-time.Hour).UnixNano()); err != nil {
		t.Fatal(err)
	}
	if err := s.PruneAppliedActions(time.Now()); err != nil {
		t.Fatal(err)
	}
	if seen, _ := s.ActionApplied("old"); seen {
		t.Fatal("an action past its keep was not pruned")
	}
	if seen, _ := s.ActionApplied("new"); !seen {
		t.Fatal("a recent action was pruned")
	}
}

// Every store step a send goes through takes the injected fault, so the app's
// tests can fail any one of them.
func TestTheInjectedFaultReachesEverySendStep(t *testing.T) {
	s, _ := openTestStore(t)
	m := savedTarget(t, s)
	steps := map[string]func() error{
		OpSaveMessage: func() error {
			n, _ := domain.NewMessage("ch-1", []byte("k"), "x")
			_, err := s.SaveMessage(n)
			return err
		},
		OpSaveAttachment: func() error { return s.SaveAttachment("ab", []byte("ct")) },
		OpSetSetting:     func() error { return s.SetSetting("k", "v") },
		OpReadMessage:    func() error { _, _, err := s.MessageByID(m.ID); return err },
		OpEdit: func() error {
			_, err := s.UpdateContent(m.ID, m.Sender, "edited", nil, "", false)
			return err
		},
		OpPin:   func() error { _, _, err := s.TogglePinnedOnce("p", m.ID); return err },
		OpReact: func() error { _, _, err := s.ToggleReactionOnce("r", m.ID, "f", "👍"); return err },
	}
	for op, run := range steps {
		s.InjectFault(func(at string) error {
			if at == op {
				return errIO
			}
			return nil
		})
		if err := run(); !errors.Is(err, errIO) {
			t.Errorf("%s: the fault did not reach it (err=%v)", op, err)
		}
	}
	s.InjectFault(nil)
}
