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
	"path/filepath"
	"strings"
	"testing"

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
