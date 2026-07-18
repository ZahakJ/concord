package app

// End-to-end routing through a REAL Service: a real identity, a real encrypted
// store, real messages, and the actual AssistDraftReply/AssistCatchUp entry
// points the UI calls. Ollama is a loopback httptest server; the brain is
// absent, which is the state on any machine without Aether and therefore the
// path that must be exactly right.
//
// What this pins down that the unit tests cannot: that the consent gate is
// really consulted before anything runs, and that with the brain unreachable
// the user still gets a draft, labeled honestly as local.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zahak/concord/internal/assist"
	"github.com/zahak/concord/internal/brain"
)

// fakeOllamaServer answers /api/tags and /api/generate on loopback.
func fakeOllamaServer(t *testing.T, reply string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			// Only a general model — no aether-brief on this box, so the
			// specialist preference must fall through cleanly.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]string{{"name": "llama3.2:3b"}},
			})
		case "/api/generate":
			_ = json.NewEncoder(w).Encode(map[string]any{"response": reply})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// startAssistService brings up a real Service with a channel holding a short
// conversation, and returns it plus that channel's id.
func startAssistService(t *testing.T) (*Service, string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	svc, err := Start(ctx, Config{
		DataDir:     filepath.Join(t.TempDir(), "home"),
		Passphrase:  "test-pass",
		DisableMDNS: true,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })

	g, err := svc.CreateGuild("Test hall")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	ch := g.Channels[0].ID
	for _, line := range []string{
		"are we still shipping on friday?",
		"depends on whether the sync bug is fixed",
	} {
		if _, err := svc.SendMessage(ch, line, ""); err != nil {
			t.Fatalf("SendMessage: %v", err)
		}
	}
	// A machine payload in the same channel: it must never reach the model.
	if _, err := svc.SendAppMessage(ch, "APPBUS:sentinel:1\n{\"cpu\":91}", ""); err != nil {
		t.Fatalf("SendAppMessage: %v", err)
	}
	return svc, ch
}

// The consent gate comes first: with the assistant switched off, nothing runs
// at all — no model call, no brain call, no partial answer.
func TestAssistRequiresConsentGate(t *testing.T) {
	svc, ch := startAssistService(t)

	if _, err := svc.AssistCatchUp(ch); !errors.Is(err, assist.ErrDisabled) {
		t.Fatalf("catch-up while off must be ErrDisabled, got %v", err)
	}
	if _, err := svc.AssistDraftReply(ch, ""); !errors.Is(err, assist.ErrDisabled) {
		t.Fatalf("draft while off must be ErrDisabled, got %v", err)
	}
	if _, err := svc.AssistBrainJob("job-1"); !errors.Is(err, assist.ErrDisabled) {
		t.Fatalf("polling a job while off must be ErrDisabled, got %v", err)
	}

	// Opting into the brain while the assistant is off is stored but inert:
	// the assistant's own gate still governs.
	cfg, err := svc.SetAssistBrain(true)
	if err != nil {
		t.Fatalf("SetAssistBrain: %v", err)
	}
	if cfg.BrainEnabled {
		t.Fatal("brain opt-in must not take effect while the assistant is off")
	}
	if _, err := svc.AssistDraftReply(ch, ""); !errors.Is(err, assist.ErrDisabled) {
		t.Fatalf("brain opt-in must not bypass the assistant gate, got %v", err)
	}
}

// The headline case: brain opted in, but Aether is not installed. The user
// must still get a draft, and it must be labeled local — never brain.
func TestDraftReplyFallsBackToLocalWhenAetherAbsent(t *testing.T) {
	// Point the brain client at a binary that does not exist anywhere.
	t.Setenv(brain.BinEnv, filepath.Join(t.TempDir(), "no-such-aether"))

	ollama := fakeOllamaServer(t, "friday works if the sync bug lands tomorrow")
	svc, ch := startAssistService(t)
	if _, err := svc.SetAssistConfig(true, ollama.URL, "llama3.2"); err != nil {
		t.Fatalf("SetAssistConfig: %v", err)
	}
	if _, err := svc.SetAssistBrain(true); err != nil {
		t.Fatalf("SetAssistBrain: %v", err)
	}
	if cfg := svc.AssistConfig(); !cfg.BrainEnabled {
		t.Fatal("both gates are on, so brain routing should be enabled")
	}

	res, err := svc.AssistDraftReply(ch, "agree, but hedge")
	if err != nil {
		t.Fatalf("a missing Aether must never fail the feature: %v", err)
	}
	if res.Text != "friday works if the sync bug lands tomorrow" {
		t.Fatalf("draft text: %q", res.Text)
	}
	// The whole point: no local answer may claim the brain wrote it.
	if res.Engine != assist.EngineLocal {
		t.Fatalf("engine = %q, want local", res.Engine)
	}
	if res.Pending || res.JobID != "" {
		t.Fatalf("nothing was queued, so nothing may be pending: %+v", res)
	}
	if !strings.Contains(res.Note, "never left this machine") {
		t.Fatalf("local note must claim on-device provenance, got %q", res.Note)
	}

	// The brain's status is reported honestly rather than hidden.
	st := svc.AssistStatus()
	if st.Brain.Available || st.Brain.Connected {
		t.Fatalf("no Aether means no brain: %+v", st.Brain)
	}
	if !st.Brain.OptedIn || st.Brain.Note == "" {
		t.Fatalf("the opt-in and an explanation must still surface: %+v", st.Brain)
	}
	if !st.BrainEnabled {
		t.Fatal("status must mirror the stored opt-in")
	}
}

// Catch-up is deliberately never routed to the brain, and its answer says so.
// It also must not be fed the channel's machine traffic.
func TestCatchUpStaysLocalAndSkipsAppTraffic(t *testing.T) {
	t.Setenv(brain.BinEnv, filepath.Join(t.TempDir(), "no-such-aether"))

	ollama := fakeOllamaServer(t, "• shipping friday is in question")
	svc, ch := startAssistService(t)
	if _, err := svc.SetAssistConfig(true, ollama.URL, "llama3.2"); err != nil {
		t.Fatalf("SetAssistConfig: %v", err)
	}
	if _, err := svc.SetAssistBrain(true); err != nil {
		t.Fatalf("SetAssistBrain: %v", err)
	}

	res, err := svc.AssistCatchUp(ch)
	if err != nil {
		t.Fatalf("AssistCatchUp: %v", err)
	}
	if res.Engine != assist.EngineLocal {
		t.Fatalf("catch-up must always be local, got %q", res.Engine)
	}
	if res.Model == "" || res.Note == "" {
		t.Fatalf("catch-up must name the model that wrote it: %+v", res)
	}
}

// CONCORD_BRAIN=off is the operator kill switch: it overrides the in-app
// opt-in, and the UI is told why the toggle looks inert.
func TestEnvPinOverridesTheOptIn(t *testing.T) {
	t.Setenv(brain.PinEnv, "off")

	ollama := fakeOllamaServer(t, "sure")
	svc, ch := startAssistService(t)
	if _, err := svc.SetAssistConfig(true, ollama.URL, "llama3.2"); err != nil {
		t.Fatalf("SetAssistConfig: %v", err)
	}
	if _, err := svc.SetAssistBrain(true); err != nil {
		t.Fatalf("SetAssistBrain: %v", err)
	}

	res, err := svc.AssistDraftReply(ch, "")
	if err != nil {
		t.Fatalf("AssistDraftReply: %v", err)
	}
	if res.Engine != assist.EngineLocal {
		t.Fatalf("pinned machines must answer locally, got %q", res.Engine)
	}
	st := svc.AssistStatus()
	if !st.Brain.Pinned || !strings.Contains(st.Brain.Note, "CONCORD_BRAIN") {
		t.Fatalf("the pin must be explained to the user: %+v", st.Brain)
	}
	// And a queued job cannot be polled into existence either.
	if _, err := svc.AssistBrainJob("job-1"); err == nil {
		t.Fatal("a pinned machine must refuse to poll brain jobs")
	}
}

// -- discovery on, routing off -------------------------------------------------
//
// Discovery is the one brain surface that is on by default, because it moves no
// user content. These tests pin the two halves of that: that it really does run
// with no consent given, and that it buys the brain nothing — a discovered,
// connected, idle brain still receives absolutely nothing until both gates are
// open.

// fakeAether writes an executable stub of Aether's CLI and points the brain
// client at it. Every invocation is appended to a log file, so a test can
// assert not just what came back but whether Concord shelled out at all — and
// in particular whether it ever tried to enqueue a job.
func fakeAether(t *testing.T, statusJSON, askJSON, showJSON string) (logPath string) {
	t.Helper()
	dir := t.TempDir()
	logPath = filepath.Join(dir, "argv.log")
	bin := filepath.Join(dir, "aether")
	script := "#!/bin/sh\n" +
		"echo \"$@\" >> " + logPath + "\n" +
		"case \"$1 $2\" in\n" +
		"  'brain status') printf '%s' '" + statusJSON + "' ;;\n" +
		"  'ask --brain')  printf '%s' '" + askJSON + "' ;;\n" +
		"  'brain show')   printf '%s' '" + showJSON + "' ;;\n" +
		"esac\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake aether: %v", err)
	}
	t.Setenv(brain.BinEnv, bin)
	return logPath
}

// aetherCalls returns the argv lines the stub recorded.
func aetherCalls(t *testing.T, logPath string) []string {
	t.Helper()
	raw, err := os.ReadFile(logPath)
	if err != nil {
		return nil // never invoked
	}
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// Discovery runs with NO consent given at all: a fresh install, assistant off,
// brain opt-in off. It reports honestly that a brain is here — and it must not
// let "available" be mistaken for "in use".
func TestBrainDiscoveryIsOnByDefault(t *testing.T) {
	logPath := fakeAether(t,
		`{"connected":true,"enabled":true,"counts":{"queued":3}}`, "", "")

	svc, _ := startAssistService(t)
	// Deliberately no SetAssistConfig and no SetAssistBrain: nothing consented.
	if cfg := svc.AssistConfig(); cfg.Enabled || cfg.BrainEnabled {
		t.Fatalf("a fresh install must have both gates shut, got %+v", cfg)
	}

	st := svc.AssistStatus()
	if !st.Brain.Available || !st.Brain.Connected || st.Brain.Queued != 3 {
		t.Fatalf("discovery must report the brain it found: %+v", st.Brain)
	}
	if st.Brain.OptedIn || st.BrainEnabled {
		t.Fatalf("discovering a brain must not opt anyone in: %+v", st.Brain)
	}
	// The note has to say nothing was sent — "available" alone reads as "on".
	if !strings.Contains(st.Brain.Note, "Nothing is being") {
		t.Fatalf("discovery note must say nothing was sent, got %q", st.Brain.Note)
	}

	// Discovery asks about the brain and nothing else. Not one enqueue.
	calls := aetherCalls(t, logPath)
	if len(calls) == 0 {
		t.Fatal("discovery is on by default, so it must actually probe")
	}
	for _, c := range calls {
		if !strings.HasPrefix(c, "brain status") {
			t.Fatalf("discovery must only ask for status, saw %q", c)
		}
	}
}

// The pin outranks the new default: a pinned machine does not even look, so
// CONCORD_BRAIN=off stays a complete kill switch rather than a routing-only one.
func TestPinnedMachineDoesNotEvenDiscover(t *testing.T) {
	logPath := fakeAether(t, `{"connected":true,"enabled":true}`, "", "")
	t.Setenv(brain.PinEnv, "off")

	svc, _ := startAssistService(t)
	st := svc.AssistStatus()
	if st.Brain.Available || st.Brain.Connected {
		t.Fatalf("a pinned machine must report no brain: %+v", st.Brain)
	}
	if !st.Brain.Pinned || !strings.Contains(st.Brain.Note, "CONCORD_BRAIN") {
		t.Fatalf("the pin must be explained: %+v", st.Brain)
	}
	if calls := aetherCalls(t, logPath); len(calls) != 0 {
		t.Fatalf("a pinned machine must not shell out at all, saw %v", calls)
	}
}

// The load-bearing negative: a brain that is present, connected and idle still
// gets nothing while the second opt-in is off. Discovery is not consent.
func TestDiscoveredBrainIsNotConsent(t *testing.T) {
	logPath := fakeAether(t,
		`{"connected":true,"enabled":true,"counts":{"queued":0}}`,
		`{"ok":true,"connected":true,"job":{"id":"j-1"}}`,
		`{"job":{"state":"done","result":"a brain-written draft"}}`)

	ollama := fakeOllamaServer(t, "locally written draft")
	svc, ch := startAssistService(t)
	// Assistant ON — but the brain opt-in is deliberately left alone.
	if _, err := svc.SetAssistConfig(true, ollama.URL, "llama3.2"); err != nil {
		t.Fatalf("SetAssistConfig: %v", err)
	}

	res, err := svc.AssistDraftReply(ch, "")
	if err != nil {
		t.Fatalf("AssistDraftReply: %v", err)
	}
	if res.Engine != assist.EngineLocal || res.Text != "locally written draft" {
		t.Fatalf("one gate open is not two: %+v", res)
	}
	for _, c := range aetherCalls(t, logPath) {
		if strings.HasPrefix(c, "ask --brain") {
			t.Fatal("a message was enqueued without the second opt-in")
		}
	}
}

// -- the brain is there but cannot answer --------------------------------------
//
// The realistic failure in daily use is not "Aether is missing" — it is a
// session that has hit its usage limit. Concord must notice and fall back, not
// hang and not invent.

// A session that dropped (usage limit, closed terminal) leaves Aether up and
// connected=false. Both gates are open, so this is the path where a wrong
// answer would be a real privacy lie.
func TestBrainWithNoSessionDegradesToLocal(t *testing.T) {
	logPath := fakeAether(t,
		`{"connected":false,"enabled":true,"counts":{"queued":0},"note":"no session attached"}`,
		`{"ok":true,"connected":false,"job":{"id":"j-1"}}`,
		`{"job":{"state":"queued"}}`)

	ollama := fakeOllamaServer(t, "local draft, honestly labeled")
	svc, ch := startAssistService(t)
	if _, err := svc.SetAssistConfig(true, ollama.URL, "llama3.2"); err != nil {
		t.Fatalf("SetAssistConfig: %v", err)
	}
	if _, err := svc.SetAssistBrain(true); err != nil {
		t.Fatalf("SetAssistBrain: %v", err)
	}

	res, err := svc.AssistDraftReply(ch, "")
	if err != nil {
		t.Fatalf("an unattended brain must never fail the feature: %v", err)
	}
	if res.Engine != assist.EngineLocal {
		t.Fatalf("engine = %q, want local", res.Engine)
	}
	if res.Text != "local draft, honestly labeled" {
		t.Fatalf("draft text: %q", res.Text)
	}
	if res.Pending || res.JobID != "" {
		t.Fatalf("nothing was queued, so nothing may be pending: %+v", res)
	}
	if !strings.Contains(res.Note, "never left this machine") {
		t.Fatalf("a local answer must claim on-device provenance, got %q", res.Note)
	}
	// With no session we go straight to the local model — the conversation is
	// never handed to a queue nobody is reading.
	for _, c := range aetherCalls(t, logPath) {
		if strings.HasPrefix(c, "ask --brain") {
			t.Fatal("an unattended queue must not receive the conversation")
		}
	}
	// And the UI is told the truth about why.
	st := svc.AssistStatus()
	if !st.Brain.Available || st.Brain.Connected {
		t.Fatalf("status must show available-but-unattended: %+v", st.Brain)
	}
}

// A session that accepts the job and then dies mid-flight — the shape of
// hitting a usage limit at the worst moment. The draft still arrives, from the
// local model, labeled local.
func TestBrainFailingMidJobFallsBackAndNeverFabricates(t *testing.T) {
	fakeAether(t,
		`{"connected":true,"enabled":true,"counts":{"queued":0}}`,
		`{"ok":true,"connected":true,"job":{"id":"j-1"}}`,
		`{"job":{"state":"failed","error":"usage limit reached"}}`)

	ollama := fakeOllamaServer(t, "local draft after the brain gave up")
	svc, ch := startAssistService(t)
	if _, err := svc.SetAssistConfig(true, ollama.URL, "llama3.2"); err != nil {
		t.Fatalf("SetAssistConfig: %v", err)
	}
	if _, err := svc.SetAssistBrain(true); err != nil {
		t.Fatalf("SetAssistBrain: %v", err)
	}

	res, err := svc.AssistDraftReply(ch, "")
	if err != nil {
		t.Fatalf("a failed brain job must never fail the feature: %v", err)
	}
	if res.Engine != assist.EngineLocal || res.Text != "local draft after the brain gave up" {
		t.Fatalf("a failed brain job must yield a local draft, got %+v", res)
	}
	if res.Pending || res.JobID != "" {
		t.Fatalf("a failed job must not be handed back as pending: %+v", res)
	}

	// Polling that job directly is an honest error naming the real reason —
	// not an empty success, and not a made-up answer.
	out, err := svc.AssistBrainJob("j-1")
	if err == nil {
		t.Fatalf("a failed job must not poll into a result, got %+v", out)
	}
	if !strings.Contains(err.Error(), "usage limit reached") {
		t.Fatalf("the error must repeat the brain's own reason, got %v", err)
	}
	if out.Text != "" || out.Engine != assist.EngineNone {
		t.Fatalf("a failed poll must carry no text and no engine: %+v", out)
	}
}

// Nothing in Concord blocks on the brain. A queue that never answers must hand
// back a pending job promptly, well inside any wall-clock patience a user has.
func TestAskBrainNeverHangs(t *testing.T) {
	q := &fakeQueue{accept: true, states: []string{"running"}}
	start := time.Now()
	res, ok := askBrain(context.Background(), q, "task", 2*brainPollInterval)
	elapsed := time.Since(start)
	if !ok || !res.Pending {
		t.Fatalf("an unanswered job is pending, not a failure: %+v ok=%v", res, ok)
	}
	if res.Text != "" {
		t.Fatalf("a pending job must carry no text — that would be fabrication: %q", res.Text)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("askBrain waited %v; it must be bounded by its own deadline", elapsed)
	}
}
