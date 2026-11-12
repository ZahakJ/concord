package store

import (
	"bytes"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// The rows here stand for one message as two lanes carry it. The store does not
// check signatures (internal/app does, and passes its verdict in Unverified),
// so a placeholder signature is enough to tell "the copy carried one" from
// "it did not".

func authoredRow(sender []byte, body string) domain.Message {
	return domain.Message{
		ID:        domain.NewID(),
		ChannelID: "ch1",
		Sender:    sender,
		Name:      "hafez",
		Content:   body,
		Sent:      time.Now().UTC().Truncate(time.Microsecond),
	}
}

func heldRow(t *testing.T, s *Store, id string) domain.Message {
	t.Helper()
	m, ok, err := s.MessageByID(id)
	if err != nil || !ok {
		t.Fatalf("MessageByID(%s): ok=%v err=%v", id, ok, err)
	}
	return m
}

// TestSyncedCopyCannotDowngradeAProvenRow: we hold a message proven — it came
// live, MLS authenticated its sender, and it carried a signature — and a trusted
// member's copy arrives newer (somebody reacted or pinned on their side) without
// a signature, because their build predates them. The newer state is news; the
// missing signature is not, and must not erase ours.
func TestSyncedCopyCannotDowngradeAProvenRow(t *testing.T) {
	s, _ := openTestStore(t)
	sig := bytes.Repeat([]byte{9}, 64)
	live := authoredRow(bytes.Repeat([]byte{1}, 32), "a proven message")
	live.Sig = sig
	if _, err := s.SaveMessage(live); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}

	served := live
	served.Sig = nil
	served.Unverified = true // what messageAttestation makes of an unsigned relayed row
	served.Pinned = true
	served.Updated = time.Now().Add(time.Hour)
	if _, err := s.UpsertSyncedMessage(served, "self", true); err != nil {
		t.Fatalf("UpsertSyncedMessage: %v", err)
	}
	got := heldRow(t, s, live.ID)
	if got.Unverified {
		t.Fatal("a trusted member's unsigned copy downgraded a row we hold proven")
	}
	if !bytes.Equal(got.Sig, sig) {
		t.Fatal("the row lost the signature it arrived with")
	}
	if !got.Pinned {
		t.Fatal("the newer state (the pin) must still be adopted")
	}

	// Different words are a different claim: an unsigned copy that changes the
	// body is not proven by what we held, and is still marked.
	rewritten := served
	rewritten.Content = "words nobody here saw the author write"
	rewritten.Edited = true
	rewritten.Updated = time.Now().Add(2 * time.Hour)
	if _, err := s.UpsertSyncedMessage(rewritten, "self", true); err != nil {
		t.Fatalf("UpsertSyncedMessage: %v", err)
	}
	if got := heldRow(t, s, live.ID); !got.Unverified || got.Content != rewritten.Content {
		t.Fatalf("an unsigned rewrite from a trusted source must be adopted marked, got unverified=%v %q", got.Unverified, got.Content)
	}
}

// TestProvenCopyClearsAMarkedRow: we hold a row marked, and a copy of the same
// words arrives that the app layer proved (a signature, or served by the
// author's own account). Proof is arithmetic on bytes, so it counts from any
// member, trusted or not.
func TestProvenCopyClearsAMarkedRow(t *testing.T) {
	for _, trusted := range []bool{false, true} {
		s, _ := openTestStore(t)
		relayed := authoredRow(bytes.Repeat([]byte{1}, 32), "first came the relay")
		relayed.Unverified = true
		if _, err := s.UpsertSyncedMessage(relayed, "self", false); err != nil {
			t.Fatalf("UpsertSyncedMessage: %v", err)
		}
		if !heldRow(t, s, relayed.ID).Unverified {
			t.Fatal("setup: the relayed row must start marked")
		}
		proven := relayed
		proven.Unverified = false
		proven.Sig = bytes.Repeat([]byte{9}, 64)
		if _, err := s.UpsertSyncedMessage(proven, "self", trusted); err != nil {
			t.Fatalf("UpsertSyncedMessage: %v", err)
		}
		got := heldRow(t, s, relayed.ID)
		if got.Unverified || !bytes.Equal(got.Sig, proven.Sig) {
			t.Fatalf("trusted=%v: a proven copy of the same words must clear the mark and bring its signature", trusted)
		}

		// A "proof" of DIFFERENT words from an untrusted member proves nothing
		// about the row we hold and changes nothing.
		other := authoredRow(bytes.Repeat([]byte{1}, 32), "kept marked")
		other.Unverified = true
		if _, err := s.UpsertSyncedMessage(other, "self", false); err != nil {
			t.Fatalf("UpsertSyncedMessage: %v", err)
		}
		claim := other
		claim.Content = "something else entirely"
		claim.Unverified = false
		if _, err := s.UpsertSyncedMessage(claim, "self", false); err != nil {
			t.Fatalf("UpsertSyncedMessage: %v", err)
		}
		if got := heldRow(t, s, other.ID); !got.Unverified || got.Content != "kept marked" {
			t.Fatal("a proven copy of other words must not touch the held row")
		}
	}
}

// TestProveRelayedMessage covers the live lane's half: a catch-up stored the
// row first, marked, and then the same message arrived in an MLS-authenticated
// frame.
func TestProveRelayedMessage(t *testing.T) {
	s, _ := openTestStore(t)
	author := bytes.Repeat([]byte{1}, 32)
	mark := func(m domain.Message) domain.Message {
		m.Unverified = true
		if _, err := s.UpsertSyncedMessage(m, "self", false); err != nil {
			t.Fatalf("UpsertSyncedMessage: %v", err)
		}
		return m
	}

	// Same words: the mark goes, the frame's signature (if any) is adopted.
	same := mark(authoredRow(author, "an old build's message"))
	frame := same
	frame.Unverified = false
	frame.Sig = bytes.Repeat([]byte{9}, 64)
	if ok, err := s.ProveRelayedMessage(frame); err != nil || !ok {
		t.Fatalf("ProveRelayedMessage: ok=%v err=%v", ok, err)
	}
	if got := heldRow(t, s, same.ID); got.Unverified || !bytes.Equal(got.Sig, frame.Sig) {
		t.Fatal("a live, authenticated copy of the same words must clear the mark")
	}

	// The relay's copy said something else under the author's id: the author's
	// authenticated words replace it.
	bent := mark(authoredRow(author, "what the relay said they wrote"))
	truth := bent
	truth.Content = "what they actually wrote"
	truth.Unverified = false
	if ok, err := s.ProveRelayedMessage(truth); err != nil || !ok {
		t.Fatalf("ProveRelayedMessage: ok=%v err=%v", ok, err)
	}
	if got := heldRow(t, s, bent.ID); got.Unverified || got.Content != truth.Content {
		t.Fatalf("the author's live words must replace a relayed version, got %q unverified=%v", got.Content, got.Unverified)
	}

	// Another member reusing the id is not a correction of somebody else's
	// words: it must neither clear nor replace them.
	theirs := mark(authoredRow(author, "the author's relayed words"))
	hijack := theirs
	hijack.Sender = bytes.Repeat([]byte{2}, 32)
	hijack.Content = "replaced by someone else"
	if ok, _ := s.ProveRelayedMessage(hijack); ok {
		t.Fatal("a frame from a different sender must not settle the row")
	}
	if got := heldRow(t, s, theirs.ID); !got.Unverified || got.Content != "the author's relayed words" {
		t.Fatal("a different sender's frame changed the held row")
	}

	// A proven row is never touched: this only ever adds proof.
	proven := authoredRow(author, "already proven")
	if _, err := s.SaveMessage(proven); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}
	other := proven
	other.Content = "a replay with other words"
	if ok, _ := s.ProveRelayedMessage(other); ok {
		t.Fatal("a row that is not marked must not be rewritten")
	}
}
