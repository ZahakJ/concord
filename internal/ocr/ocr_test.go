package ocr

// The worker's contract: never blocks, caches per blob, honest statuses.
// The engine subprocess is stubbed via the runEngine seam — no real OCR (and
// no real exec) runs in the suite.

import (
	"context"
	"errors"
	"sync"
	"testing"
)

type memSink struct {
	mu    sync.Mutex
	rows  map[string][2]string // blobID -> [text, status]
	saves int
}

func newMemSink() *memSink { return &memSink{rows: map[string][2]string{}} }

func (m *memSink) SaveAttachmentOCR(blobID, text, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[blobID] = [2]string{text, status}
	m.saves++
	return nil
}

func (m *memSink) HasAttachmentOCR(blobID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.rows[blobID]
	return ok, nil
}

var pngHeader = []byte("\x89PNG\r\n\x1a\nrest-of-image")

func testWorker(sink Sink, out string, err error) *Worker {
	w := NewWorker(sink, []string{"/bin/true"}) // command present ⇒ Available
	w.runEngine = func(ctx context.Context, argv []string, stdin []byte) (string, error) {
		return out, err
	}
	return w
}

func TestExtractStatuses(t *testing.T) {
	sink := newMemSink()
	w := testWorker(sink, "Hello From A Screenshot", nil)
	if st, text := w.Extract(pngHeader); st != StatusOK || text != "Hello From A Screenshot" {
		t.Fatalf("ok path: %q %q", st, text)
	}
	if st, _ := w.Extract([]byte("not an image at all")); st != StatusSkipped {
		t.Fatalf("non-image must skip, got %q", st)
	}
	big := append(append([]byte{}, pngHeader...), make([]byte, MaxImageBytes)...)
	if st, _ := w.Extract(big); st != StatusSkipped {
		t.Fatalf("oversized must skip, got %q", st)
	}
	w.runEngine = func(context.Context, []string, []byte) (string, error) {
		return "", errors.New("boom")
	}
	if st, _ := w.Extract(pngHeader); st != StatusError {
		t.Fatalf("engine failure must record error, got %q", st)
	}
	w.runEngine = func(context.Context, []string, []byte) (string, error) { return "  \n ", nil }
	if st, _ := w.Extract(pngHeader); st != StatusEmpty {
		t.Fatalf("blank output must record empty, got %q", st)
	}
}

func TestNoEngineIsUnavailable(t *testing.T) {
	w := NewWorker(newMemSink(), nil)
	w.command = nil // simulate nothing on PATH regardless of the host machine
	if w.Available() {
		t.Fatal("no command ⇒ not available")
	}
	if st, _ := w.Extract(pngHeader); st != StatusUnavailable {
		t.Fatalf("want unavailable, got %q", st)
	}
}

func TestEnqueueCachesPerBlob(t *testing.T) {
	sink := newMemSink()
	w := testWorker(sink, "words in picture", nil)
	blob := "aaaa"
	w.Enqueue(blob, pngHeader)
	w.Enqueue(blob, pngHeader) // duplicate while queued: dropped
	w.Flush()
	if sink.saves != 1 {
		t.Fatalf("one blob must be processed once, got %d saves", sink.saves)
	}
	if row := sink.rows[blob]; row[0] != "words in picture" || row[1] != StatusOK {
		t.Fatalf("bad row: %v", row)
	}
	// a blob with an existing result is never re-run, even after restart
	w2 := testWorker(sink, "different", nil)
	w2.Enqueue(blob, pngHeader)
	w2.Flush()
	if sink.saves != 1 {
		t.Fatalf("cached blob must not re-run, got %d saves", sink.saves)
	}
}

func TestEnqueueNeverBlocksWhenFull(t *testing.T) {
	sink := newMemSink()
	w := testWorker(sink, "x", nil)
	for i := 0; i < 400; i++ { // queue capacity is 256 — the rest must drop
		w.Enqueue(string(rune(i))+"-blob", pngHeader)
	}
	// reaching here without deadlock IS the assertion; drain for cleanliness
	w.Flush()
}
