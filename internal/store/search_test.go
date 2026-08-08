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
