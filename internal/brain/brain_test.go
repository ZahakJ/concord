package brain

// The brain client's whole job is to fail quietly. These tests pin that: every
// broken shape Aether's CLI could hand back must become an honest "no", never
// an error escaping to the assistant and never a fabricated answer.

import (
	"context"
	"errors"
	"testing"
)

// fake returns a runner that replies with out/err and records the args it saw.
func fake(out string, err error, seen *[][]string) runner {
	return func(_ context.Context, _ string, args []string) ([]byte, error) {
		if seen != nil {
			*seen = append(*seen, args)
		}
		return []byte(out), err
	}
}

func TestStatusConnected(t *testing.T) {
	var seen [][]string
	c := newTestClient(fake(`{"connected":true,"enabled":true,"counts":{"queued":2}}`, nil, &seen))
	st := c.Status(context.Background())
	if !st.Available || !st.Connected || st.Queued != 2 {
		t.Fatalf("got %+v", st)
	}
	if st.Note == "" {
		t.Fatal("a connected brain still needs a human-readable note")
	}
	// Always --json, always the brain subcommand — never a bare `aether`.
	if len(seen) != 1 || seen[0][0] != "brain" || seen[0][len(seen[0])-1] != "--json" {
		t.Fatalf("bad argv: %v", seen)
	}
}

func TestStatusQueuedAtTopLevel(t *testing.T) {
	// Older/newer CLI shape: depths at the top level rather than under counts.
	c := newTestClient(fake(`{"connected":false,"queued":7}`, nil, nil))
	if st := c.Status(context.Background()); st.Queued != 7 || st.Connected {
		t.Fatalf("got %+v", st)
	}
}

// Every failure mode collapses to "no brain", with a note that says so.
func TestStatusDegradesOnEveryFailure(t *testing.T) {
	cases := map[string]struct {
		out string
		err error
	}{
		"missing binary": {"", errors.New("exec: \"aether\": not found")},
		"non-zero exit":  {"", errors.New("exit status 1")},
		"empty output":   {"", nil},
		"garbage":        {"not json at all", nil},
		"json array":     {`["queued"]`, nil},
		"json null":      {`null`, nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := newTestClient(fake(tc.out, tc.err, nil))
			st := c.Status(context.Background())
			if st.Available || st.Connected {
				t.Fatalf("a broken CLI must read as no-brain, got %+v", st)
			}
			if st.Note == "" {
				t.Fatal("degraded status must still explain itself")
			}
		})
	}
}

func TestEnqueueAndFetch(t *testing.T) {
	var seen [][]string
	c := newTestClient(fake(`{"ok":true,"route":"brain","connected":true,"job":{"id":"j-42"}}`, nil, &seen))
	job, ok := c.Enqueue(context.Background(), "draft a reply")
	if !ok || job.ID != "j-42" || !job.Connected {
		t.Fatalf("got %+v ok=%v", job, ok)
	}
	if seen[0][0] != "ask" || seen[0][1] != "--brain" {
		t.Fatalf("must go through `aether ask --brain`, got %v", seen[0])
	}

	c = newTestClient(fake(`{"job":{"state":"done","result":"  hi there  ","attempts":1}}`, nil, nil))
	res, ok := c.Fetch(context.Background(), "j-42")
	if !ok || !res.Done() || res.Text != "hi there" {
		t.Fatalf("got %+v ok=%v", res, ok)
	}
	if res.Pending() {
		t.Fatal("a done job is not pending")
	}
}

func TestEnqueueRefusalIsNotAnError(t *testing.T) {
	// Aether's PII check pinning a job local looks like ok:false. That must be
	// a quiet local fallback, not a retry and not an error.
	for _, out := range []string{
		`{"ok":false,"reason":"looks personal — pinned local"}`,
		`{"ok":true,"job":{}}`, // accepted but no id: unusable
		`{"ok":true}`,          // no job at all
	} {
		if job, ok := newTestClient(fake(out, nil, nil)).Enqueue(context.Background(), "task"); ok {
			t.Fatalf("%s should refuse, got %+v", out, job)
		}
	}
	// An empty task never reaches the CLI at all.
	called := false
	c := newTestClient(func(context.Context, string, []string) ([]byte, error) {
		called = true
		return nil, nil
	})
	if _, ok := c.Enqueue(context.Background(), "   "); ok || called {
		t.Fatal("an empty task must not be enqueued")
	}
}

func TestFetchPendingIsNotFailure(t *testing.T) {
	for _, state := range []string{"queued", "running"} {
		c := newTestClient(fake(`{"job":{"state":"`+state+`"}}`, nil, nil))
		res, ok := c.Fetch(context.Background(), "j-1")
		if !ok || !res.Pending() || res.Done() {
			t.Fatalf("%s: got %+v ok=%v", state, res, ok)
		}
	}
	// A failed job reads as neither done nor pending — the caller falls back.
	c := newTestClient(fake(`{"job":{"state":"failed","error":"session dropped"}}`, nil, nil))
	res, _ := c.Fetch(context.Background(), "j-1")
	if res.Done() || res.Pending() || res.Error == "" {
		t.Fatalf("got %+v", res)
	}
}

func TestFetchMalformedReply(t *testing.T) {
	// Garbage, and a well-formed envelope whose fields are the wrong types.
	if _, ok := newTestClient(fake("<html>500</html>", nil, nil)).Fetch(context.Background(), "j"); ok {
		t.Fatal("garbage must not read as a job")
	}
	c := newTestClient(fake(`{"job":{"state":123,"result":{"a":1},"attempts":"many"}}`, nil, nil))
	res, ok := c.Fetch(context.Background(), "j")
	if !ok {
		t.Fatal("a decodable envelope should still be read")
	}
	if res.Done() || res.Text != "" || res.Attempts != 0 {
		t.Fatalf("wrong-typed fields must zero out, got %+v", res)
	}
	if _, ok := c.Fetch(context.Background(), ""); ok {
		t.Fatal("an empty job id must not be fetched")
	}
}

func TestPinnedForcesLocal(t *testing.T) {
	for _, v := range []string{"off", "0", "no", "OFF", "False"} {
		t.Setenv(PinEnv, v)
		if !Pinned() {
			t.Fatalf("%q should pin local", v)
		}
		called := false
		c := newTestClient(func(context.Context, string, []string) ([]byte, error) {
			called = true
			return []byte(`{"connected":true}`), nil
		})
		if st := c.Status(context.Background()); st.Available || st.Connected {
			t.Fatalf("%q: pinned must report no brain, got %+v", v, st)
		}
		if _, ok := c.Enqueue(context.Background(), "task"); ok {
			t.Fatalf("%q: pinned must not enqueue", v)
		}
		if called {
			t.Fatalf("%q: pinned must not shell out at all", v)
		}
	}
	for _, v := range []string{"", "on", "1", "yes"} {
		t.Setenv(PinEnv, v)
		if Pinned() {
			t.Fatalf("%q should not pin local", v)
		}
	}
}

func TestNilClientIsSafe(t *testing.T) {
	// A Service built without a brain client must not panic on the seam.
	var c *Client
	if st := c.Status(context.Background()); st.Available {
		t.Fatal("nil client must read as no brain")
	}
	if _, ok := c.Enqueue(context.Background(), "task"); ok {
		t.Fatal("nil client must not enqueue")
	}
	if _, ok := c.Fetch(context.Background(), "j"); ok {
		t.Fatal("nil client must not fetch")
	}
}
