// Package ocr reads text out of image attachments so local search can find
// messages by what a screenshot says — entirely on this machine.
//
// Concord itself stays pure Go, so the actual character recognition is done
// by an external local OCR command (an "engine"): a program that receives the
// image bytes on stdin and prints the extracted plain text on stdout. The
// reference engine is scripts/concord-ocr (RapidOCR, a pip-installable ONNX
// model that runs offline); any command with the same contract works, e.g.
// `tesseract stdin stdout`.
//
// Rules, in privacy order:
//
//   - Local only. The engine is a subprocess on this machine; input is an
//     attachment the user's own client already decrypted (their own store,
//     their own keys). Nothing is uploaded anywhere, ever.
//   - Never blocks. Work happens on one background goroutine fed by a
//     bounded queue; enqueueing from the message/attachment paths is O(1)
//     and drops (to be swept up later) rather than waits.
//   - Cached per attachment. One result row per blob ID; a processed blob
//     is never re-run. Results are stored via a Sink — Concord's store keeps
//     them sealed at rest exactly like message text, because extracted text
//     IS message content.
//   - Capped + honest. Oversized or non-image inputs record "skipped";
//     a missing engine records "unavailable"; failures record "error".
package ocr

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Result statuses.
const (
	StatusOK          = "ok"
	StatusEmpty       = "empty"
	StatusSkipped     = "skipped"
	StatusError       = "error"
	StatusUnavailable = "unavailable"
)

// MaxImageBytes caps what one OCR run will read (matches the attachment cap).
const MaxImageBytes = 5 << 20

// runTimeout bounds one engine invocation; CPU-only OCR of a large
// screenshot stays well inside this.
const runTimeout = 120 * time.Second

// EngineNames are the commands probed on PATH, in order, when no explicit
// command is configured.
var EngineNames = []string{"concord-ocr"}

// Sink persists results — implemented by the store (sealed at rest).
type Sink interface {
	SaveAttachmentOCR(blobID, text, status string) error
	// HasAttachmentOCR reports whether a result row already exists.
	HasAttachmentOCR(blobID string) (bool, error)
}

type job struct {
	blobID string
	plain  []byte
}

// Worker runs OCR jobs on one background goroutine.
type Worker struct {
	sink    Sink
	command []string // argv; empty => engine unavailable

	mu      sync.Mutex
	pending map[string]bool // blobIDs queued or running (dedup)
	queue   chan job
	started bool
	ctx     context.Context
	cancel  context.CancelFunc

	// runEngine is the exec seam, swapped by tests.
	runEngine func(ctx context.Context, argv []string, stdin []byte) (string, error)
}

// NewWorker builds a worker. command may be empty, in which case the engine
// is looked up on PATH (EngineNames); if none is found the worker still runs
// and records "unavailable" so a later install can be swept up by clearing
// those rows.
func NewWorker(sink Sink, command []string) *Worker {
	if len(command) == 0 {
		for _, name := range EngineNames {
			if p, err := exec.LookPath(name); err == nil {
				command = []string{p}
				break
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Worker{
		sink:      sink,
		command:   command,
		pending:   make(map[string]bool),
		queue:     make(chan job, 256),
		ctx:       ctx,
		cancel:    cancel,
		runEngine: runCommand,
	}
}

// Available reports whether an OCR engine command is configured/found.
func (w *Worker) Available() bool { return len(w.command) > 0 }

// EngineName is the engine command (for the honest status line), or "".
func (w *Worker) EngineName() string {
	if len(w.command) == 0 {
		return ""
	}
	return w.command[0]
}

// Start launches the background goroutine (idempotent).
func (w *Worker) Start() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.started {
		return
	}
	w.started = true
	go w.loop()
}

// Close stops the worker.
func (w *Worker) Close() { w.cancel() }

// Enqueue queues one decrypted attachment for OCR. Never blocks: when the
// queue is full the job is dropped (a later sweep re-offers it). Already-
// processed and already-queued blobs are skipped.
func (w *Worker) Enqueue(blobID string, plain []byte) {
	if blobID == "" || len(plain) == 0 {
		return
	}
	w.mu.Lock()
	if w.pending[blobID] {
		w.mu.Unlock()
		return
	}
	w.pending[blobID] = true
	w.mu.Unlock()
	if done, err := w.sink.HasAttachmentOCR(blobID); err == nil && done {
		return // cached — keep it marked pending so repeats stay cheap
	}
	// copy: callers may reuse the buffer
	cp := make([]byte, len(plain))
	copy(cp, plain)
	select {
	case w.queue <- job{blobID: blobID, plain: cp}:
	default:
		w.mu.Lock()
		delete(w.pending, blobID) // full — let a sweep re-offer it
		w.mu.Unlock()
	}
}

// Flush processes everything currently queued, synchronously — tests only.
func (w *Worker) Flush() {
	for {
		select {
		case j := <-w.queue:
			w.process(j)
		default:
			return
		}
	}
}

func (w *Worker) loop() {
	for {
		select {
		case <-w.ctx.Done():
			return
		case j := <-w.queue:
			w.process(j)
		}
	}
}

func (w *Worker) process(j job) {
	status, text := w.Extract(j.plain)
	_ = w.sink.SaveAttachmentOCR(j.blobID, text, status)
}

// Extract runs the engine on one image, synchronously. Exposed for tests and
// for the sweep path.
func (w *Worker) Extract(plain []byte) (status, text string) {
	if len(plain) == 0 || len(plain) > MaxImageBytes || !isImage(plain) {
		return StatusSkipped, ""
	}
	if !w.Available() {
		return StatusUnavailable, ""
	}
	ctx, cancel := context.WithTimeout(w.ctx, runTimeout)
	defer cancel()
	out, err := w.runEngine(ctx, w.command, plain)
	if err != nil {
		return StatusError, ""
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return StatusEmpty, ""
	}
	return StatusOK, out
}

func runCommand(ctx context.Context, argv []string, stdin []byte) (string, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin = bytes.NewReader(stdin)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return out.String(), nil
}

// isImage sniffs the magic bytes of the formats attachments allow. GIFs are
// accepted (the engine reads the first frame or fails honestly).
func isImage(data []byte) bool {
	switch {
	case len(data) > 8 && bytes.Equal(data[:8], []byte("\x89PNG\r\n\x1a\n")):
		return true
	case len(data) > 3 && bytes.Equal(data[:3], []byte{0xff, 0xd8, 0xff}):
		return true
	case len(data) > 6 && (bytes.Equal(data[:6], []byte("GIF87a")) || bytes.Equal(data[:6], []byte("GIF89a"))):
		return true
	case len(data) > 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return true
	}
	return false
}
