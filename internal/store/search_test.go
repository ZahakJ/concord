package store

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// Search had no test of its own: the only ones that exercised it lived in
// ocr_test.go and went out with that feature, and the very next edit to this
// function silently turned it into "return the newest N messages" — every
// search listing every conversation the user has. So the plain behaviour gets
// a test that owes nothing to any feature.
func TestSearchMessagesOnlyReturnsMatches(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "c.db"), bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	for i, body := range []string{"apples for the pie", "bananas", "secret plans"} {
		m := domain.Message{
			ID: string(rune('a' + i)), ChannelID: "ch", Sender: []byte("me"),
			Content: body, Sent: time.Now().Add(time.Duration(i) * time.Second),
		}
		if _, err := st.SaveMessage(m); err != nil {
			t.Fatal(err)
		}
	}

	hits, err := st.SearchMessages("apples", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Content != "apples for the pie" {
		var got []string
		for _, h := range hits {
			got = append(got, h.Content)
		}
		t.Fatalf("search for %q returned %d rows %q; a non-matching row means every\nsearch is dumping unrelated conversations at the user", "apples", len(hits), got)
	}
	// Case-insensitive, and a query matching nothing must return nothing rather
	// than falling back to "here's everything".
	if hits, _ := st.SearchMessages("APPLES", 50); len(hits) != 1 {
		t.Fatalf("case-insensitive search returned %d rows, want 1", len(hits))
	}
	if hits, _ := st.SearchMessages("zeppelin", 50); len(hits) != 0 {
		t.Fatalf("search with no matches returned %d rows, want 0", len(hits))
	}
}

// A bare operator is a query. The search box offers `from:` as a chip and types
// the prefix for you, and following that to "everything from Bilal" used to
// return nil — an empty needle short-circuited before the WHERE clause that
// answers it ever ran. The filter is what makes the emptiness meaningful, so
// this pins BOTH halves: with a filter it is a real question, and without one
// it is the newest page, which is why the search box refuses to ask it.
func TestSearchMessagesBareFilter(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "c.db"), bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	rows := []struct{ id, name, body string }{
		{"a", "Bilal Rahman", "apples for the pie"},
		{"b", "Amina Sadiq", "bananas"},
		{"c", "Bilal Rahman", "no shared word with the other one"},
	}
	for i, r := range rows {
		m := domain.Message{
			ID: r.id, ChannelID: "ch", Sender: []byte("s"), Name: r.name,
			Content: r.body, Sent: time.Now().Add(time.Duration(i) * time.Second),
		}
		if _, err := st.SaveMessage(m); err != nil {
			t.Fatal(err)
		}
	}

	hits, err := st.SearchMessages("", 50, SearchFilter{FromSender: "bilal"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("from:bilal with no needle: want both of Bilal's, got %d", len(hits))
	}
	for _, h := range hits {
		if h.Name != "Bilal Rahman" {
			t.Fatalf("from:bilal returned %q", h.Name)
		}
	}

	// Still a substring search when there IS a needle, filter or not.
	hits, _ = st.SearchMessages("apples", 50, SearchFilter{FromSender: "bilal"})
	if len(hits) != 1 {
		t.Fatalf("from:bilal apples: want 1, got %d", len(hits))
	}
	hits, _ = st.SearchMessages("apples", 50, SearchFilter{FromSender: "amina"})
	if len(hits) != 0 {
		t.Fatalf("from:amina apples: want 0, got %d", len(hits))
	}

	// Nothing typed and nothing filtered is the newest page, deliberately: the
	// caller decides whether that is worth asking, and the search box does not.
	hits, _ = st.SearchMessages("", 50)
	if len(hits) != 3 {
		t.Fatalf("empty query with no filter: want the newest page (3), got %d", len(hits))
	}
}
