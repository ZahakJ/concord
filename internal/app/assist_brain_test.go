package app

// The assistant's routing seam. What matters here is not that the brain works
// — that is Aether's problem — but that Concord behaves correctly when it
// doesn't: every failure falls back to the local model, and no answer is ever
// mislabeled. A local draft that claims to have come from the brain would be a
// privacy lie, so the engine label is asserted on every path.

import (
	"context"
	"testing"
	"time"

	"github.com/zahak/concord/internal/assist"
	"github.com/zahak/concord/internal/brain"
	"github.com/zahak/concord/internal/domain"
)

// fakeQueue is a brain that behaves however a test needs it to.
type fakeQueue struct {
	accept    bool
	states    []string // one Fetch result per call, last repeats
	text      string
	fetchFail bool
	enqueued  []string
	fetches   int
}

func (f *fakeQueue) Enqueue(_ context.Context, task string) (brain.Job, bool) {
	f.enqueued = append(f.enqueued, task)
	if !f.accept {
		return brain.Job{}, false
	}
	return brain.Job{ID: "job-1", Connected: true}, true
}

func (f *fakeQueue) Fetch(context.Context, string) (brain.Result, bool) {
	if f.fetchFail {
		return brain.Result{}, false
	}
	i := f.fetches
	f.fetches++
	if i >= len(f.states) {
		i = len(f.states) - 1
	}
	state := f.states[i]
	res := brain.Result{State: state}
	if state == "done" {
		res.Text = f.text
	}
	return res, true
}

func TestAskBrainAnswered(t *testing.T) {
	q := &fakeQueue{accept: true, states: []string{"done"}, text: "sure, sounds good"}
	res, ok := askBrain(context.Background(), q, "draft a reply", time.Second)
	if !ok {
		t.Fatal("an answered job must succeed")
	}
	if res.Text != "sure, sounds good" || res.Engine != assist.EngineBrain {
		t.Fatalf("got %+v", res)
	}
	if res.Pending || res.JobID != "job-1" {
		t.Fatalf("got %+v", res)
	}
	// The note must own up to what the brain actually is.
	if res.Note == "" {
		t.Fatal("a brain answer must carry its provenance note")
	}
	if len(q.enqueued) != 1 {
		t.Fatalf("exactly one job should be queued, got %v", q.enqueued)
	}
}

func TestAskBrainStillQueuedIsPendingNotFailure(t *testing.T) {
	q := &fakeQueue{accept: true, states: []string{"queued"}}
	// wait=0 so the deadline has already passed on the first check.
	res, ok := askBrain(context.Background(), q, "task", 0)
	if !ok {
		t.Fatal("a queued job is an answer in progress, not a failure")
	}
	if !res.Pending || res.JobID != "job-1" || res.Text != "" {
		t.Fatalf("got %+v", res)
	}
	if res.Engine != assist.EngineBrain {
		t.Fatalf("a pending job is still the brain's, got %+v", res)
	}
}

// Every way the brain can let us down must produce ok=false, which the caller
// turns into a local draft. None of these are user-visible errors.
func TestAskBrainFallsBackOnEveryFailure(t *testing.T) {
	cases := map[string]*fakeQueue{
		"refused at enqueue": {accept: false},
		"job failed":         {accept: true, states: []string{"failed"}},
		"unknown state":      {accept: true, states: []string{"weird"}},
		"unreadable job":     {accept: true, fetchFail: true},
		"done but empty":     {accept: true, states: []string{"done"}, text: ""},
	}
	for name, q := range cases {
		t.Run(name, func(t *testing.T) {
			if res, ok := askBrain(context.Background(), q, "task", 0); ok {
				t.Fatalf("must fall back, got %+v", res)
			}
		})
	}
}

func TestAskBrainRespectsCancellation(t *testing.T) {
	// A job that never finishes must not hold a request open past its context.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	q := &fakeQueue{accept: true, states: []string{"running"}}
	if _, ok := askBrain(ctx, q, "task", time.Hour); ok {
		t.Fatal("a cancelled context must abandon the job, not block")
	}
}

// The env pin is an operator kill switch: with it set, nothing is enqueued no
// matter what the user has toggled in the UI.
func TestBrainPinnedNeverEnqueues(t *testing.T) {
	t.Setenv(brain.PinEnv, "off")
	c := brain.New()
	if _, ok := c.Enqueue(context.Background(), "task"); ok {
		t.Fatal("CONCORD_BRAIN=off must block enqueue outright")
	}
	if st := c.Status(context.Background()); st.Available || st.Connected {
		t.Fatalf("pinned status must report no brain, got %+v", st)
	}
}

// -- the app plane -------------------------------------------------------------
//
// Machine payloads must be recognizable as such whether they carry the modern
// kind field or only the legacy content prefix.

func TestMessageIsApp(t *testing.T) {
	cases := []struct {
		name string
		msg  domain.Message
		app  bool
	}{
		{"plain chat", domain.Message{Content: "hey, did it sync?"}, false},
		{"explicit kind", domain.Message{Kind: domain.KindApp, Content: "{}"}, true},
		{"legacy prefix, no kind", domain.Message{Content: "APPBUS:sentinel:1\n{\"cpu\":9}"}, true},
		{"system notice", domain.Message{Kind: "system", Content: "joined"}, false},
		{"merely mentions appbus", domain.Message{Content: "the APPBUS: thing is noisy"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.msg.IsApp(); got != tc.app {
				t.Fatalf("IsApp = %v, want %v", got, tc.app)
			}
			if tc.app && tc.msg.EffectiveKind() != domain.KindApp {
				t.Fatalf("EffectiveKind = %q, want app", tc.msg.EffectiveKind())
			}
		})
	}
	// The producing app is parseable for the integrations view.
	if got := (domain.Message{Content: "APPBUS:sentinel:1\n{}"}).AppBusApp(); got != "sentinel" {
		t.Fatalf("AppBusApp = %q, want sentinel", got)
	}
	if got := (domain.Message{Content: "hello"}).AppBusApp(); got != "" {
		t.Fatalf("AppBusApp on a chat message = %q, want empty", got)
	}
}

// The assistant must never be fed machine payloads: they are noise in a
// summary and they are not what the user's screen shows.
func TestTranscriptExcludesAppTraffic(t *testing.T) {
	msgs := []domain.Message{
		{Name: "Brahma", Content: "did the deploy land?"},
		{Name: "bridge", Kind: domain.KindApp, Content: "APPBUS:sentinel:1\n{\"cpu\":91}"},
		{Name: "Euclid", Content: "yep, green"},
	}
	got := assist.Transcript(msgs)
	if want := "Brahma: did the deploy land?\nEuclid: yep, green"; got != want {
		t.Fatalf("Transcript = %q, want %q", got, want)
	}
}
