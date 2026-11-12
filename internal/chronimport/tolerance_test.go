package chronimport

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// exportOf writes one channel's export with n messages, message 10 replaced by
// odd (a raw JSON object) when it is not "".
func exportOf(t *testing.T, dir, name, chID string, ids []string, odd string) {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, `{"guild":{"name":"G"},"channel":{"id":%q,"name":%q},"messages":[`, chID, name)
	for i, id := range ids {
		if i > 0 {
			b.WriteString(",")
		}
		if i == 10 && odd != "" {
			b.WriteString(odd)
			continue
		}
		fmt.Fprintf(&b, `{"id":%q,"type":"Default","timestamp":"2020-01-01T00:%02d:%02dZ","content":"m %s","author":{"id":"a","name":"ada"}}`,
			id, (i/60)%60, i%60, id)
	}
	b.WriteString("]}")
	if err := os.WriteFile(filepath.Join(dir, name+".json"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func ids(prefix string, from, to int) []string {
	var out []string
	for i := from; i < to; i++ {
		out = append(out, fmt.Sprintf("%s-%d", prefix, i))
	}
	return out
}

// One oddly typed field used to end the walk of its file: every message after
// it vanished, and the scan reported nothing malformed. Now the odd message is
// skipped and counted, and numbers where an exporter wrote an epoch or a
// numeric id are read as such.
func TestOneOddMessageCostsOnlyItself(t *testing.T) {
	for _, tc := range []struct {
		name          string
		odd           string
		messages, bad int64
	}{
		{"numeric stamp", `{"id":"x","type":"Default","timestamp":1577836800,"content":"epoch seconds"}`, 100, 0},
		{"millisecond stamp", `{"id":"x","type":"Default","timestamp":1577836800123,"content":"epoch ms"}`, 100, 0},
		{"numeric id", `{"id":4815162342,"type":"Default","timestamp":"2020-01-01T00:00:00Z","content":"snowflake"}`, 100, 0},
		{"numeric content", `{"id":"x","type":"Default","timestamp":"2020-01-01T00:00:00Z","content":42}`, 99, 1},
		{"object content", `{"id":"x","type":"Default","timestamp":"2020-01-01T00:00:00Z","content":{"text":"hi"}}`, 99, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			exportOf(t, dir, "room", "c1", ids("m", 0, 100), tc.odd)
			st, err := ScanChatExport(dir)
			if err != nil {
				t.Fatalf("ScanChatExport: %v", err)
			}
			if st.Messages != tc.messages || st.Malformed != tc.bad || len(st.FileErrors) != 0 {
				t.Fatalf("scanned %d messages, %d malformed, file errors %+v; want %d, %d, none",
					st.Messages, st.Malformed, st.FileErrors, tc.messages, tc.bad)
			}
		})
	}
}

// A file cut off partway yields what it holds and says it was cut off, rather
// than being listed as a file that could not be opened at all.
func TestATruncatedFileIsPartialNotUnreadable(t *testing.T) {
	dir := t.TempDir()
	exportOf(t, dir, "room", "c1", ids("m", 0, 100), "")
	p := filepath.Join(dir, "room.json")
	raw, _ := os.ReadFile(p)
	if err := os.WriteFile(p, raw[:len(raw)*9/10], 0o644); err != nil {
		t.Fatal(err)
	}
	got := 0
	_, err := Walk(p, nil, func(*Message) error { got++; return nil })
	if !errors.Is(err, ErrTruncated) {
		t.Fatalf("Walk of a cut-off file: %v, want ErrTruncated", err)
	}
	if got < 80 {
		t.Fatalf("read %d messages before the break, want the ~90 that are there", got)
	}
	st, err := ScanChatExport(dir)
	if err != nil {
		t.Fatalf("ScanChatExport: %v", err)
	}
	if len(st.FileErrors) != 1 || !st.FileErrors[0].Partial {
		t.Fatalf("file errors %+v, want one partial", st.FileErrors)
	}
}

// Two files of one channel whose ranges overlap (a re-export of a period
// already exported) used to count, and import, the overlap twice.
func TestOverlappingFilesCountEachMessageOnce(t *testing.T) {
	dir := t.TempDir()
	exportOf(t, dir, "room-a", "c1", ids("d", 0, 200), "")
	exportOf(t, dir, "room-b", "c1", ids("d", 100, 300), "")
	// Another channel reusing the same ids is a different channel, not a copy.
	exportOf(t, dir, "other", "c2", ids("d", 0, 50), "")
	st, err := ScanChatExport(dir)
	if err != nil {
		t.Fatalf("ScanChatExport: %v", err)
	}
	if st.Messages != 350 || st.Duplicates != 100 {
		t.Fatalf("scanned %d messages with %d duplicates, want 350 and 100", st.Messages, st.Duplicates)
	}
}
