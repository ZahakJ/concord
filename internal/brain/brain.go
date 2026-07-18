// Package brain is Concord's client for the shared "brain" — the external
// work queue Aether already runs on this machine.
//
// Some assistant work is genuinely too hard for a 3B model on a CPU box.
// Drafting a reply that matches a thread's tone, follows the user's steering
// instruction, and reads the room is that kind of work: a small local model
// produces something grammatical and tone-deaf.
//
// Rather than reach for a paid API key — which would break Concord's
// structural "nothing leaves this machine" guarantee — or reimplement a second
// work queue, Concord hands that work to the one already running here. Aether
// ships an external brain: a queue that a Claude Code session, signed in on the
// owner's own subscription, long-polls and answers (see ~/work/AETHER/docs/brain.md).
// Aether owns that queue; Concord is just another producer.
//
// # What this means for privacy — read this before enabling it
//
// The brain is a LOCAL process on the user's own machine, on the user's own
// subscription, with no API key and no metered spend. It is still a Claude
// session, which means the content of a job — including decrypted message text
// — is seen by Claude. That is a real and different exposure from the Ollama
// path, where the bytes never leave the box.
//
// So brain routing in Concord is:
//
//   - OFF by default, always. A fresh install never enqueues anything.
//   - Gated by the assistant's EXISTING consent toggle (assist.enabled) AND a
//     second, separate opt-in (assist.brain.enabled). Both must be on.
//   - Pinnable off machine-wide with CONCORD_BRAIN=off.
//   - Labeled. Every answer says which engine produced it; a local answer
//     never claims to have come from the brain.
//
// # The contract
//
// Deliberately thin and entirely one-directional:
//
//   - Status  — is a brain connected right now?
//   - Enqueue — send one hard-reasoning job, get a job id back.
//   - Fetch   — has that job been answered yet?
//
// Everything degrades. If Aether isn't installed, isn't running, or has no
// session attached, every function here returns an honest "no" and the caller
// falls back to the local model. Nothing in Concord ever blocks on the brain.
//
// We talk to Aether through its own CLI, which already holds this machine's
// device token — Concord never handles Aether's credentials and never touches
// Aether's data directory.
package brain

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	// BinEnv overrides the Aether CLI Concord shells out to (tests, odd installs).
	BinEnv = "CONCORD_AETHER_BIN"
	// DefaultBin is the Aether CLI as installed on PATH.
	DefaultBin = "aether"

	// PinEnv forces every assistant job to stay local when set to off/0/no,
	// regardless of what the user has toggled in the UI. An operator kill
	// switch that no in-app setting can override.
	PinEnv = "CONCORD_BRAIN"

	// Origin is how Concord labels itself in Aether's queue and audit ledger.
	Origin = "concord"

	// Short, hard timeouts. A slow or wedged daemon must never stall a Concord
	// request — the local model is always standing by.
	StatusTimeout  = 5 * time.Second
	EnqueueTimeout = 15 * time.Second
	FetchTimeout   = 10 * time.Second
)

// Status is the brain's availability, in Concord's words.
//
// Available means Aether answered at all; Connected means a Claude Code
// session is actually polling its queue. Both false is fine and common — it is
// the normal state on a machine without Aether, and the assistant works
// unchanged.
type Status struct {
	Available bool   `json:"available"`
	Connected bool   `json:"connected"`
	Enabled   bool   `json:"enabled"`
	Queued    int    `json:"queued"`
	Note      string `json:"note"`
}

// Job is an accepted enqueue: the id to poll plus how Aether routed it.
type Job struct {
	ID        string `json:"id"`
	Route     string `json:"route"`
	Reason    string `json:"reason"`
	Connected bool   `json:"connected"`
}

// Result is one poll of a job. State is Aether's own job state
// (queued/running/done/failed). A job still queued is not a failure — the
// brain answers when a session picks it up, and Concord shows it as pending
// until then.
type Result struct {
	State    string `json:"state"`
	Text     string `json:"result"`
	Error    string `json:"error"`
	Attempts int    `json:"attempts"`
}

// Done reports whether the job finished successfully with usable text.
func (r Result) Done() bool { return r.State == "done" && r.Text != "" }

// Pending reports whether the job is still waiting on a brain session.
func (r Result) Pending() bool { return r.State == "queued" || r.State == "running" }

// runner executes one Aether subcommand. Swapped out in tests.
type runner func(ctx context.Context, bin string, args []string) ([]byte, error)

// Client talks to Aether's CLI. The zero value is not usable; use New.
type Client struct {
	bin string
	run runner
}

// New builds a client against the Aether CLI named by $CONCORD_AETHER_BIN
// (default "aether").
func New() *Client {
	bin := strings.TrimSpace(os.Getenv(BinEnv))
	if bin == "" {
		bin = DefaultBin
	}
	return &Client{bin: bin, run: execRunner}
}

// newTestClient builds a client with an injected runner. Test seam only.
func newTestClient(run runner) *Client { return &Client{bin: "aether-test", run: run} }

// execRunner shells out for real, with the caller's context providing the
// timeout. A missing binary is an ordinary error, not a panic.
func execRunner(ctx context.Context, bin string, args []string) ([]byte, error) {
	path, err := exec.LookPath(bin)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, args...)
	// Aether's CLI reads its own config; we deliberately pass no secrets.
	return cmd.Output()
}

// Pinned reports whether CONCORD_BRAIN pins this machine to local-only.
func Pinned() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(PinEnv))) {
	case "0", "off", "no", "false":
		return true
	}
	return false
}

// call runs an `aether ... --json` subcommand and decodes the object. Returns
// nil for ANY failure — pinned off, missing binary, daemon down, timeout,
// non-zero exit, empty output, garbage JSON. A brain that cannot be reached is
// a normal condition, not an error, and no failure here ever escapes to the
// caller as a Go error.
func (c *Client) call(ctx context.Context, timeout time.Duration, args ...string) map[string]any {
	if c == nil || c.run == nil || Pinned() {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := c.run(ctx, c.bin, append(args, "--json"))
	if err != nil {
		return nil
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil
	}
	return data
}

// Status asks whether a brain session is attached right now.
func (c *Client) Status(ctx context.Context) Status {
	data := c.call(ctx, StatusTimeout, "brain", "status")
	if data == nil {
		return Status{Note: "No shared brain on this machine — the assistant " +
			"uses your local model only."}
	}
	counts, _ := data["counts"].(map[string]any)
	if counts == nil {
		counts = data // some builds put queue depths at the top level
	}
	st := Status{
		Available: true,
		Connected: boolOf(data["connected"]),
		Enabled:   boolOfDefault(data["enabled"], true),
		Queued:    intOf(counts["queued"]),
		Note:      strOf(data["note"]),
	}
	if st.Note == "" {
		if st.Connected {
			st.Note = "A Claude Code session on this machine is answering brain jobs."
		} else {
			st.Note = "Aether is here but no brain session is attached; jobs would wait in the queue."
		}
	}
	return st
}

// Enqueue sends one hard-reasoning job. Returns ok=false if the job could not
// be queued at all.
//
// Aether may refuse a job — most notably its PII check, which pins anything
// that looks personal to the local machine. A refusal is ok=false here and a
// local fallback in the caller, never a retry loop.
func (c *Client) Enqueue(ctx context.Context, task string) (Job, bool) {
	task = strings.TrimSpace(task)
	if task == "" {
		return Job{}, false
	}
	data := c.call(ctx, EnqueueTimeout, "ask", "--brain", task)
	if data == nil || !boolOf(data["ok"]) {
		return Job{}, false
	}
	job, _ := data["job"].(map[string]any)
	id := ""
	if job != nil {
		id = strOf(job["id"])
	}
	if id == "" {
		return Job{}, false
	}
	return Job{
		ID:        id,
		Route:     strOf(data["route"]),
		Reason:    strOf(data["reason"]),
		Connected: boolOf(data["connected"]),
	}, true
}

// Fetch polls one job. ok=false means the job could not be read at all.
func (c *Client) Fetch(ctx context.Context, jobID string) (Result, bool) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return Result{}, false
	}
	data := c.call(ctx, FetchTimeout, "brain", "show", jobID)
	if data == nil {
		return Result{}, false
	}
	job, _ := data["job"].(map[string]any)
	if job == nil {
		job = data
	}
	state := strOf(job["state"])
	if state == "" {
		state = strOf(job["status"])
	}
	return Result{
		State:    strings.ToLower(state),
		Text:     strings.TrimSpace(strOf(job["result"])),
		Error:    strings.TrimSpace(strOf(job["error"])),
		Attempts: intOf(job["attempts"]),
	}, true
}

// -- tolerant JSON field readers ---------------------------------------------
//
// Aether's CLI is a separate program on its own release cadence. We read its
// output defensively: a field that changed shape yields a zero value, never a
// panic and never a hard error.

func strOf(v any) string {
	s, _ := v.(string)
	return s
}

func boolOf(v any) bool {
	b, _ := v.(bool)
	return b
}

func boolOfDefault(v any, def bool) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}

func intOf(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}
