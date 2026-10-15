package store

import (
	"bytes"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

func sidebarStore(t testing.TB) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "c.db"), bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// The batched form has to agree with the single-channel one channel for
// channel, including the channel whose newest event is an edit rather than a
// send, and the one with no messages at all.
func TestLatestTimestampsAgreesWithLatestTimestamp(t *testing.T) {
	st := sidebarStore(t)
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 90; i++ {
		if _, err := st.SaveMessage(domain.Message{ID: fmt.Sprintf("m%d", i), ChannelID: fmt.Sprintf("c%d", i%4),
			Sender: []byte("x"), Content: "hi", Sent: base.Add(time.Duration(i) * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	// c1's oldest message is edited now, which is later than anything sent.
	if _, err := st.UpdateContent("m1", []byte("x"), "edited", nil, "", false); err != nil {
		t.Fatal(err)
	}
	ids := []string{"c0", "c1", "c2", "c3", "empty"}
	got, err := st.LatestTimestamps(ids)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		want, err := st.LatestTimestamp(id)
		if err != nil {
			t.Fatal(err)
		}
		if got[id] != want {
			t.Errorf("%s: batched %d, single %d", id, got[id], want)
		}
	}
	if got["c1"] <= got["c0"] {
		t.Errorf("the edit did not count as activity: c1 %d, c0 %d", got["c1"], got["c0"])
	}
}

// NewestMessage stands in for Messages(ch, 1) in the DM preview, so it must
// pick the same row and say the same things about it — including that a
// deleted message is deleted and has no body.
func TestNewestMessageMatchesMessagesOne(t *testing.T) {
	st := sidebarStore(t)
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		if _, err := st.SaveMessage(domain.Message{ID: fmt.Sprintf("m%d", i), ChannelID: "dm",
			Sender: []byte{byte(i)}, Content: fmt.Sprintf("line %d", i), Sent: base.Add(time.Duration(i) * time.Second)}); err != nil {
			t.Fatal(err)
		}
	}
	check := func(label string) {
		t.Helper()
		msgs, err := st.Messages("dm", 1)
		if err != nil {
			t.Fatal(err)
		}
		m, ok, err := st.NewestMessage("dm")
		if err != nil || !ok {
			t.Fatalf("%s: NewestMessage ok=%v err=%v", label, ok, err)
		}
		w := msgs[0]
		if m.ID != w.ID || m.Content != w.Content || m.Deleted != w.Deleted || !bytes.Equal(m.Sender, w.Sender) || !m.Sent.Equal(w.Sent) {
			t.Fatalf("%s: NewestMessage %+v, Messages(1) %+v", label, m, w)
		}
	}
	check("plain")
	if _, _, err := st.MarkDeleted("m4", nil, true); err != nil {
		t.Fatal(err)
	}
	check("deleted")
	if _, ok, err := st.NewestMessage("nothing"); ok || err != nil {
		t.Fatalf("empty channel: ok=%v err=%v", ok, err)
	}
}

// The emoji cache must never serve a set that a write has since changed.
func TestCustomEmojiCacheFollowsWrites(t *testing.T) {
	st := sidebarStore(t)
	names := func(g string) string {
		t.Helper()
		rows, err := st.CustomEmoji(g)
		if err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		for _, r := range rows {
			fmt.Fprintf(&b, "%s=%s;", r.Name, r.Image)
		}
		return b.String()
	}
	if got := names("g"); got != "" {
		t.Fatalf("empty guild: %q", got)
	}
	_ = st.SaveCustomEmoji(CustomEmojiRow{GuildID: "g", Name: "a", Image: "1"})
	if got := names("g"); got != "a=1;" {
		t.Fatalf("after add: %q", got)
	}
	_ = st.SaveCustomEmoji(CustomEmojiRow{GuildID: "g", Name: "a", Image: "2"})
	_ = st.SaveCustomEmoji(CustomEmojiRow{GuildID: "g", Name: "b", Image: "3"})
	if got := names("g"); got != "a=2;b=3;" {
		t.Fatalf("after replace and add: %q", got)
	}
	_ = st.DeleteCustomEmoji("g", "a")
	if got := names("g"); got != "b=3;" {
		t.Fatalf("after delete: %q", got)
	}
	// A caller appending to what it was handed must not grow the cache.
	rows, _ := st.CustomEmoji("g")
	_ = append(rows, CustomEmojiRow{Name: "intruder"})
	if got := names("g"); got != "b=3;" {
		t.Fatalf("caller's slice leaked into the cache: %q", got)
	}
}
