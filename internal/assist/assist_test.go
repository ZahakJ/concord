package assist

// The assistant's structural guarantees: loopback-only endpoints (config AND
// call time), off-by-default behavior enforced by the app layer, transcripts
// capped and stripped of attachment tokens, and prompts carrying only the
// user's own messages. Ollama is a local httptest server here — the suite
// makes no real model calls.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zahak/concord/internal/domain"
)

func TestValidateEndpointLoopbackOnly(t *testing.T) {
	for _, bad := range []string{
		"http://192.168.1.9:11434", "http://ollama.example.com",
		"https://127.0.0.1:11434", // https to loopback is pointless; keep http-only
		"http://0.0.0.0:11434", "ftp://127.0.0.1", "http://",
	} {
		if _, err := ValidateEndpoint(bad); err == nil {
			t.Fatalf("endpoint %q must be rejected", bad)
		}
	}
	for _, good := range []string{
		"", "http://127.0.0.1:11434", "http://localhost:11434",
		"http://[::1]:11434", "http://127.0.0.1:11434/",
	} {
		if _, err := ValidateEndpoint(good); err != nil {
			t.Fatalf("endpoint %q must be accepted: %v", good, err)
		}
	}
	if got, _ := ValidateEndpoint(""); got != DefaultEndpoint {
		t.Fatalf("empty endpoint must default, got %q", got)
	}
	if _, err := NewClient("http://10.0.0.5:11434", "m"); err == nil {
		t.Fatal("NewClient must re-validate (defense in depth)")
	}
}

func msg(name, content string) domain.Message {
	return domain.Message{Name: name, Content: content, Sent: time.Now()}
}

func TestTranscriptStripsTokensAndCaps(t *testing.T) {
	msgs := []domain.Message{
		msg("Brahma", "look ![image](concord://attach/v1/aaaa/bbbb/png/1x1) neat"),
		msg("Euclid", "nice"),
		{Name: "sys", Kind: "system", Content: "joined"},
		{Name: "Euclid", Content: "gone", Deleted: true},
	}
	tr := Transcript(msgs)
	if strings.Contains(tr, "concord://") {
		t.Fatal("attachment tokens must never reach the model")
	}
	if !strings.Contains(tr, "[shared an image/file]") || !strings.Contains(tr, "Euclid: nice") {
		t.Fatalf("transcript malformed: %q", tr)
	}
	if strings.Contains(tr, "joined") || strings.Contains(tr, "gone") {
		t.Fatal("system/deleted rows must be excluded")
	}
	long := make([]domain.Message, 200)
	for i := range long {
		long[i] = msg("A", strings.Repeat("x", 100)+" tail"+string(rune('a'+i%26)))
	}
	if got := Transcript(long); len(got) > maxTranscriptChars {
		t.Fatalf("transcript must be capped, got %d chars", len(got))
	}
}

// fakeOllama serves /api/tags and /api/generate, capturing prompts.
func fakeOllama(t *testing.T, response string) (*httptest.Server, *[]string) {
	t.Helper()
	var prompts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]any{{"name": "llama3.2:1b"}, {"name": "llama3.2:3b"}}})
		case "/api/generate":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			prompts = append(prompts, body["prompt"].(string))
			if m, _ := body["model"].(string); m != "llama3.2:3b" {
				t.Errorf("base model name must resolve to the largest pulled tag, got %q", m)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"response": response})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &prompts
}

func TestCatchUpAndDraftReply(t *testing.T) {
	srv, prompts := fakeOllama(t, "• you two tested the app\n• the sync worked")
	c, err := NewClient(srv.URL, "llama3.2")
	if err != nil {
		t.Fatalf("NewClient against httptest loopback: %v", err)
	}
	msgs := []domain.Message{msg("Euclid", "hi, testing concord"), msg("Brahma", "it works!")}
	out, err := c.CatchUp(context.Background(), msgs)
	if err != nil || !strings.Contains(out.Text, "sync worked") {
		t.Fatalf("CatchUp: %+v %v", out, err)
	}
	// The engine label must be present and honest: this came from Ollama.
	if out.Engine != EngineLocal || out.Note == "" {
		t.Fatalf("a local answer must be labeled local, got %+v", out)
	}
	if !strings.Contains((*prompts)[0], "Euclid: hi, testing concord") {
		t.Fatal("the prompt must carry the transcript")
	}
	if _, err := c.CatchUp(context.Background(), nil); err == nil {
		t.Fatal("empty channel must be an honest error, not a model call")
	}
	if _, err := c.DraftReply(context.Background(), msgs, "politely agree", "Brahma"); err != nil {
		t.Fatalf("DraftReply: %v", err)
	}
	last := (*prompts)[len(*prompts)-1]
	if !strings.Contains(last, "politely agree") || !strings.Contains(last, "Brahma") {
		t.Fatalf("instruction/self name missing from prompt: %q", last)
	}
}

func TestExpandQueryStructured(t *testing.T) {
	srv, _ := fakeOllama(t, `{"terms": ["gpu speed", "benchmark", "Latency", "gpu speed", "", "benchmarks", "fps", "extra-sixth"]}`)
	c, _ := NewClient(srv.URL, "llama3.2")
	terms, err := c.ExpandQuery(context.Background(), "latency")
	if err != nil {
		t.Fatalf("ExpandQuery: %v", err)
	}
	// deduped (case-insensitive), the original query excluded, capped at 5
	want := []string{"gpu speed", "benchmark", "benchmarks", "fps", "extra-sixth"}
	if len(terms) != len(want) {
		t.Fatalf("terms = %v", terms)
	}
	for i := range want {
		if terms[i] != want[i] {
			t.Fatalf("terms = %v, want %v", terms, want)
		}
	}
}

func TestGenerateErrorsAreHonest(t *testing.T) {
	c, _ := NewClient("http://127.0.0.1:1", "m") // nothing listens on port 1
	if _, err := c.CatchUp(context.Background(), []domain.Message{msg("A", "hello")}); err == nil ||
		!strings.Contains(err.Error(), "Ollama") {
		t.Fatalf("unreachable server must produce an actionable error, got %v", err)
	}
}
