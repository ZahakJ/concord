package appbus

// The app-bus contract: bearer auth (constant-time, no empty-token bypass),
// channel resolution (id / bare name / guild-qualified, ambiguity is an
// error), cursor reads that never miss or re-read, and the per-channel send
// rate limit. The Concord node is a stub — no network, no crypto here.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zahak/concord/internal/domain"
)

type stubNode struct {
	guilds []domain.Guild
	msgs   map[string][]domain.Message // channelID -> chronological
	sent   []string
}

func (n *stubNode) Fingerprint() string { return "AAAA BBBB" }
func (n *stubNode) DisplayName() string { return "test bridge" }
func (n *stubNode) Guilds() []domain.Guild {
	return n.guilds
}
func (n *stubNode) SendMessage(chID, content, replyTo string) (domain.Message, error) {
	n.sent = append(n.sent, chID+"|"+content)
	return domain.Message{ID: fmt.Sprintf("m%d", len(n.sent)), ChannelID: chID,
		Content: content, Sent: time.Now()}, nil
}
func (n *stubNode) MessagesSince(chID string, sinceNano int64, limit int) ([]domain.Message, error) {
	var out []domain.Message
	for _, m := range n.msgs[chID] {
		if m.Sent.UnixNano() > sinceNano && len(out) < limit {
			out = append(out, m)
		}
	}
	return out, nil
}
func (n *stubNode) ProfileName(fpr string) string {
	if fpr == "FPR1" {
		return "Brahma"
	}
	return ""
}
func (n *stubNode) AccountFingerprintOf(cred []byte) string { return string(cred) }

func newStub() *stubNode {
	base := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	return &stubNode{
		guilds: []domain.Guild{
			{ID: "g1", Name: "Grey mane", Channels: []domain.Channel{
				{ID: "c1", Name: "general", Type: "text"},
				{ID: "cv", Name: "hall", Type: "voice"}, // filtered out
			}},
			{ID: "g2", Name: "Other", Channels: []domain.Channel{
				{ID: "c2", Name: "general", Type: ""},
				{ID: "c3", Name: "scores", Type: "text"},
			}},
		},
		msgs: map[string][]domain.Message{
			"c3": {
				{ID: "a", Sender: []byte("FPR1"), Name: "old-name",
					Content: "APPBUS:sentinel:1\n{\"score\":1}", Sent: base},
				{ID: "b", Sender: []byte("FPR2"), Name: "Zahak",
					Content: "nice score", Sent: base.Add(time.Second)},
				{ID: "sys", Kind: "system", Content: "joined",
					Sent: base.Add(2 * time.Second)},
			},
		},
	}
}

func doReq(t *testing.T, h http.Handler, method, path, token, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w, out
}

func TestAuthRequired(t *testing.T) {
	h := New(newStub(), "sekrit").Handler()
	if w, _ := doReq(t, h, "GET", "/api/health", "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("no token must be 401, got %d", w.Code)
	}
	if w, _ := doReq(t, h, "GET", "/api/health", "wrong", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token must be 401, got %d", w.Code)
	}
	// an empty configured token must NOT open the door
	h2 := New(newStub(), "").Handler()
	if w, _ := doReq(t, h2, "GET", "/api/health", "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("empty configured token must fail closed, got %d", w.Code)
	}
	w, out := doReq(t, h, "GET", "/api/health", "sekrit", "")
	if w.Code != 200 || out["ok"] != true {
		t.Fatalf("health: %d %v", w.Code, out)
	}
	ident := out["identity"].(map[string]any)
	if ident["fingerprint"] != "AAAA BBBB" || ident["name"] != "test bridge" {
		t.Fatalf("identity: %v", ident)
	}
	// voice channels never appear on the bus
	if strings.Contains(w.Body.String(), "hall") {
		t.Fatal("voice channels must be filtered from the directory")
	}
}

func TestChannelResolution(t *testing.T) {
	s := New(newStub(), "t")
	if id, err := s.resolveChannel("c1"); err != nil || id != "c1" {
		t.Fatalf("id: %q %v", id, err)
	}
	if id, err := s.resolveChannel("scores"); err != nil || id != "c3" {
		t.Fatalf("unique name: %q %v", id, err)
	}
	if id, err := s.resolveChannel("Grey mane/general"); err != nil || id != "c1" {
		t.Fatalf("qualified: %q %v", id, err)
	}
	if id, err := s.resolveChannel("#scores"); err != nil || id != "c3" {
		t.Fatalf("hash prefix: %q %v", id, err)
	}
	if _, err := s.resolveChannel("general"); err == nil ||
		!strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("duplicate bare name must be ambiguous, got %v", err)
	}
	if _, err := s.resolveChannel("nope"); err == nil {
		t.Fatal("unknown channel must error")
	}
}

func TestSendAndRateLimit(t *testing.T) {
	stub := newStub()
	s := New(stub, "t")
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	h := s.Handler()

	w, out := doReq(t, h, "POST", "/api/send", "t",
		`{"channel":"scores","text":"APPBUS:sentinel:1\n{}"}`)
	if w.Code != 200 || out["ok"] != true || out["message_id"] != "m1" {
		t.Fatalf("send: %d %v", w.Code, out)
	}
	if !strings.HasPrefix(stub.sent[0], "c3|APPBUS:sentinel:1") {
		t.Fatalf("sent: %v", stub.sent)
	}
	if w, _ := doReq(t, h, "POST", "/api/send", "t", `{"channel":"scores","text":""}`); w.Code != 400 {
		t.Fatalf("empty text must 400, got %d", w.Code)
	}
	// burst of 5 allowed, the 6th (in the same instant) is limited…
	for i := 0; i < 4; i++ {
		if w, _ := doReq(t, h, "POST", "/api/send", "t", `{"channel":"scores","text":"x"}`); w.Code != 200 {
			t.Fatalf("burst send %d: %d", i, w.Code)
		}
	}
	if w, _ := doReq(t, h, "POST", "/api/send", "t", `{"channel":"scores","text":"x"}`); w.Code != http.StatusTooManyRequests {
		t.Fatalf("6th instant send must 429, got %d", w.Code)
	}
	// …another channel has its own bucket…
	if w, _ := doReq(t, h, "POST", "/api/send", "t", `{"channel":"c1","text":"x"}`); w.Code != 200 {
		t.Fatalf("other channel must not share the bucket, got %d", w.Code)
	}
	// …and a second later a token has refilled.
	now = now.Add(time.Second)
	if w, _ := doReq(t, h, "POST", "/api/send", "t", `{"channel":"scores","text":"x"}`); w.Code != 200 {
		t.Fatalf("refill after 1s must allow a send, got %d", w.Code)
	}
}

func TestMessagesCursorNeverMissesOrRereads(t *testing.T) {
	h := New(newStub(), "t").Handler()
	w, out := doReq(t, h, "GET", "/api/messages?channel=scores", "t", "")
	if w.Code != 200 {
		t.Fatalf("messages: %d", w.Code)
	}
	msgs := out["messages"].([]any)
	if len(msgs) != 2 { // the system row is excluded
		t.Fatalf("want 2 messages, got %v", msgs)
	}
	first := msgs[0].(map[string]any)
	if first["sender"] != "FPR1" || first["sender_display"] != "Brahma" {
		t.Fatalf("sender mapping: %v", first)
	}
	if !strings.HasPrefix(first["text"].(string), "APPBUS:sentinel:1\n") {
		t.Fatalf("payload text: %v", first["text"])
	}
	if _, err := time.Parse(time.RFC3339Nano, first["ts"].(string)); err != nil {
		t.Fatalf("ts must be RFC3339: %v", err)
	}
	cursor := out["next_cursor"].(string)
	// same cursor again: nothing new, cursor stable
	w2, out2 := doReq(t, h, "GET", "/api/messages?channel=scores&since="+cursor, "t", "")
	if w2.Code != 200 || len(out2["messages"].([]any)) != 0 {
		t.Fatalf("cursor re-read must be empty: %v", out2)
	}
	if out2["next_cursor"] != cursor {
		t.Fatalf("empty read must keep the cursor: %v vs %v", out2["next_cursor"], cursor)
	}
	if w3, _ := doReq(t, h, "GET", "/api/messages?channel=scores&since=banana", "t", ""); w3.Code != 400 {
		t.Fatalf("garbage cursor must 400, got %d", w3.Code)
	}
}
