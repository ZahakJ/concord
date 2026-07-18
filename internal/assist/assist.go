// Package assist is Concord's opt-in, strictly-local AI helper.
//
// It talks to an Ollama server on THIS machine (loopback is enforced, not
// merely defaulted) and does three things with the user's own decrypted
// history: summarize a channel ("catch me up"), draft a reply, and expand a
// search query into related terms so local search finds more.
//
// Privacy is the product, so the rules are structural, not policy:
//
//   - OFF by default. Nothing here runs until the user flips the toggle, and
//     the toggle is per-identity, stored in the local encrypted-at-rest DB.
//   - Loopback only. A non-loopback endpoint is rejected at configuration
//     time AND again at call time — a config file edit cannot exfiltrate.
//   - Local data only. The model sees exactly the messages the user's own
//     screen shows (their own store, already decrypted with their own key),
//     over localhost, and the response comes back the same way. No cloud,
//     no third party, no telemetry.
//   - E2EE untouched. This package only ever consumes plaintext the user
//     already holds; it adds no new network surface and never touches the
//     MLS/transport layers.
package assist

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zahak/concord/internal/domain"
)

const (
	// DefaultEndpoint is the standard local Ollama address.
	DefaultEndpoint = "http://127.0.0.1:11434"
	// DefaultModel is a small, widely-pulled local model; anything the user
	// has pulled works and the settings UI lists what the server reports.
	DefaultModel = "llama3.2"

	probeTimeout    = 2 * time.Second
	generateTimeout = 150 * time.Second

	// maxTranscriptChars caps what one call feeds the model — this box may be
	// CPU-only, so prompts stay modest (the most recent stretch wins).
	maxTranscriptChars = 6000
	// catchUpMessages is how much recent history "catch me up" reads.
	catchUpMessages = 80
)

// Settings keys in the store's settings table (encrypted-at-rest DB).
const (
	KeyEnabled  = "assist.enabled"
	KeyEndpoint = "assist.endpoint"
	KeyModel    = "assist.model"
	// KeyBrainEnabled is the SECOND, separate opt-in for routing hard jobs to
	// the shared brain (a local Claude Code session — see internal/brain).
	// Off by default and meaningless unless KeyEnabled is also on: the
	// assistant's existing consent gate still governs every path, and this key
	// only ever narrows what that gate already allows.
	KeyBrainEnabled = "assist.brain.enabled"
)

// Config is the user's assistant configuration.
type Config struct {
	Enabled  bool   `json:"enabled"`
	Endpoint string `json:"endpoint"`
	Model    string `json:"model"`
	// BrainEnabled opts the hard-reasoning path in to the shared brain.
	// Requires Enabled; off by default.
	BrainEnabled bool `json:"brainEnabled"`
}

// Status is the honest snapshot the settings UI shows.
type Status struct {
	Enabled      bool     `json:"enabled"`
	Endpoint     string   `json:"endpoint"`
	Model        string   `json:"model"`
	Reachable    bool     `json:"reachable"`
	ModelPresent bool     `json:"modelPresent"`
	Models       []string `json:"models"`
	Hint         string   `json:"hint,omitempty"`
	// BrainEnabled mirrors the user's separate brain opt-in (off by default).
	BrainEnabled bool `json:"brainEnabled"`
}

// ErrDisabled is returned when a feature is invoked while the toggle is off —
// callers surface it verbatim; nothing runs.
var ErrDisabled = fmt.Errorf("assist: the local assistant is switched off")

// ValidateEndpoint normalizes an endpoint and rejects anything that is not
// plain http to a loopback address. This is the structural privacy guarantee:
// there is no configuration in which assist traffic leaves the machine.
func ValidateEndpoint(endpoint string) (string, error) {
	e := strings.TrimSpace(endpoint)
	if e == "" {
		return DefaultEndpoint, nil
	}
	u, err := url.Parse(e)
	if err != nil || u.Scheme != "http" || u.Host == "" {
		return "", fmt.Errorf("assist: endpoint must be a local http:// URL")
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	loop := host == "localhost" || (ip != nil && ip.IsLoopback())
	if !loop {
		return "", fmt.Errorf("assist: endpoint must be loopback (127.0.0.1) — the assistant never talks to another machine")
	}
	return strings.TrimRight(u.Scheme+"://"+u.Host, "/"), nil
}

// Client calls one local Ollama server.
type Client struct {
	endpoint string // validated loopback base URL
	model    string
	http     *http.Client
}

// NewClient builds a client, re-validating the endpoint (defense in depth —
// even a hand-edited stored value cannot point off-machine).
func NewClient(endpoint, model string) (*Client, error) {
	base, err := ValidateEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	m := strings.TrimSpace(model)
	if m == "" {
		m = DefaultModel
	}
	return &Client{endpoint: base, model: m, http: &http.Client{}}, nil
}

// Models returns the tags the local server has pulled.
func (c *Client) Models(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var body struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(body.Models))
	for _, m := range body.Models {
		if m.Name != "" {
			out = append(out, m.Name)
		}
	}
	return out, nil
}

// resolveModel maps a configured base name ("llama3.2") to an exact pulled
// tag ("llama3.2:3b"), preferring the exact tag, else any tag sharing the
// base. Returns the configured name unchanged when the probe fails.
func (c *Client) resolveModel(ctx context.Context) string {
	names, err := c.Models(ctx)
	if err != nil {
		return c.model
	}
	for _, n := range names {
		if n == c.model {
			return n
		}
	}
	base := strings.SplitN(c.model, ":", 2)[0]
	best := ""
	for _, n := range names {
		if strings.SplitN(n, ":", 2)[0] == base && n > best {
			best = n // tags sort usefully enough (3b > 1b) for a tiebreak
		}
	}
	if best != "" {
		return best
	}
	return c.model
}

// pickModel resolves the model for one call, preferring a fine-tuned local
// specialist when this machine has pulled it.
//
// The specialists (aether-brief and friends) are small models trained for one
// job; where one exists it beats the user's configured general model at that
// job for a fraction of the latency. Where it doesn't, we fall through to the
// configured model and the feature is unchanged. Returns the tag actually
// chosen so the caller can name it in the UI.
func (c *Client) pickModel(ctx context.Context, specialist string) string {
	if specialist == "" {
		return c.resolveModel(ctx)
	}
	names, err := c.Models(ctx)
	if err == nil && HasModel(specialist, names) {
		for _, n := range names {
			if n == specialist || strings.SplitN(n, ":", 2)[0] == specialist {
				return n
			}
		}
	}
	return c.resolveModel(ctx)
}

// generate runs one non-streaming completion against an explicit model tag.
// format, when non-nil, is an Ollama structured-output JSON schema.
func (c *Client) generate(ctx context.Context, model, prompt string, format any, temperature float64) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, generateTimeout)
	defer cancel()
	payload := map[string]any{
		"model":   model,
		"prompt":  prompt,
		"stream":  false,
		"options": map[string]any{"temperature": temperature},
	}
	if format != nil {
		payload["format"] = format
	}
	raw, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/api/generate", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("assist: the local model isn't answering — is Ollama running?")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("assist: the local model returned HTTP %d", resp.StatusCode)
	}
	var body struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	out := strings.TrimSpace(body.Response)
	if out == "" {
		return "", fmt.Errorf("assist: the local model returned nothing")
	}
	return out, nil
}

// Transcript renders messages as the plain text the model sees, newest kept
// under the cap. Attachment/file tokens read as placeholders.
func Transcript(msgs []domain.Message) string {
	lines := make([]string, 0, len(msgs))
	for _, m := range msgs {
		if m.Kind != "" || m.Deleted {
			continue
		}
		who := m.Name
		if who == "" {
			who = "someone"
		}
		lines = append(lines, who+": "+stripTokens(m.Content))
	}
	return capTail(strings.Join(lines, "\n"))
}

// capTail keeps the most recent stretch under the prompt cap, trimming to a
// line boundary so the model never starts mid-sentence.
func capTail(text string) string {
	if len(text) <= maxTranscriptChars {
		return text
	}
	text = text[len(text)-maxTranscriptChars:]
	if i := strings.IndexByte(text, '\n'); i >= 0 && i < 200 {
		text = text[i+1:]
	}
	return text
}

func stripTokens(content string) string {
	out := content
	for {
		i := strings.Index(out, "](concord://")
		if i < 0 {
			return out
		}
		start := strings.LastIndex(out[:i], "[")
		if start > 0 && out[start-1] == '!' {
			start--
		}
		end := strings.IndexByte(out[i:], ')')
		if start < 0 || end < 0 {
			return out
		}
		out = out[:start] + "[shared an image/file]" + out[i+end+1:]
	}
}

// Ledger renders messages as the "HH:MM event" lines the aether-brief
// specialist is trained on (see ~/work/apex_llm/INTEGRATION.md). Same content
// as Transcript, same cap — only the shape differs.
func Ledger(msgs []domain.Message) string {
	lines := make([]string, 0, len(msgs))
	for _, m := range msgs {
		if m.Kind != "" || m.Deleted {
			continue
		}
		who := m.Name
		if who == "" {
			who = "someone"
		}
		lines = append(lines, m.Sent.Local().Format("15:04")+" "+who+": "+stripTokens(m.Content))
	}
	return capTail(strings.Join(lines, "\n"))
}

// CatchUp summarizes recent channel history for someone who just came back.
//
// Classification: cheap/structured. This is exactly what the aether-brief
// specialist was trained for, so it stays local — a summary of a transcript is
// not the kind of cross-context reasoning worth handing to the brain, and
// keeping it local keeps the strongest privacy story on the most-used feature.
func (c *Client) CatchUp(ctx context.Context, msgs []domain.Message) (Result, error) {
	model := c.pickModel(ctx, SpecialistBrief)
	if strings.SplitN(model, ":", 2)[0] == SpecialistBrief {
		l := Ledger(msgs)
		if strings.TrimSpace(l) == "" {
			return Result{}, fmt.Errorf("assist: nothing in this channel to catch up on")
		}
		// The specialist's system prompt is baked into its Modelfile; the user
		// turn is the ledger in the shape it was trained on.
		out, err := c.generate(ctx, model,
			"Today's activity ledger:\n"+l+"\n\nWrite the pulse.", nil, 0.3)
		if err != nil {
			return Result{}, err
		}
		return LocalResult(out, SpecialistBrief), nil
	}
	t := Transcript(msgs)
	if strings.TrimSpace(t) == "" {
		return Result{}, fmt.Errorf("assist: nothing in this channel to catch up on")
	}
	prompt := "You are a private, on-device assistant inside a chat app. " +
		"Summarize the conversation below for someone catching up: 3-6 short " +
		"bullet points covering what was discussed, decided, or asked. Use only " +
		"what the transcript says — invent nothing. Attribute by name where it " +
		"matters. Reply with just the bullets, no preamble.\n\nConversation:\n" + t
	out, err := c.generate(ctx, model, prompt, nil, 0.2)
	if err != nil {
		return Result{}, err
	}
	return LocalResult(out, model), nil
}

// draftPrompt builds the drafting instruction shared by both engines, so the
// brain and the local model are asked for exactly the same thing and the only
// difference in the output is quality.
func draftPrompt(msgs []domain.Message, instruction, selfName string) (string, bool) {
	t := Transcript(msgs)
	if strings.TrimSpace(t) == "" {
		return "", false
	}
	who := strings.TrimSpace(selfName)
	if who == "" {
		who = "the user"
	}
	p := "Draft the reply that " + who + " could send next in the chat " +
		"conversation below. Match the conversation's tone and language. Keep " +
		"it short and natural (1-3 sentences unless the content demands more). " +
		"Reply with ONLY the message text — no quotes, no preamble.\n"
	if s := strings.TrimSpace(instruction); s != "" {
		p += "The user asked for the reply to: " + s + "\n"
	}
	return p + "\nConversation:\n" + t, true
}

// DraftReply proposes a reply to the recent conversation, optionally steered
// by the user's instruction ("politely decline", "ask for the logs"), using
// the on-device model. This is the fallback whenever the brain is off,
// unavailable, or declines the job.
func (c *Client) DraftReply(ctx context.Context, msgs []domain.Message, instruction, selfName string) (Result, error) {
	prompt, ok := draftPrompt(msgs, instruction, selfName)
	if !ok {
		return Result{}, fmt.Errorf("assist: nothing here to reply to yet")
	}
	model := c.pickModel(ctx, "")
	out, err := c.generate(ctx, model,
		"You are a private, on-device assistant inside a chat app. "+prompt, nil, 0.6)
	if err != nil {
		return Result{}, err
	}
	return LocalResult(out, model), nil
}

// BrainDraftTask builds the self-contained task string for the shared brain.
//
// The brain has no access to Concord's store — whatever it needs must be in
// the task text. That is precisely why this path is double-opt-in: the
// transcript below is decrypted message content, and a Claude session reads
// it. The task says so in its first line, so the exposure is legible in
// Aether's own audit ledger too, not just in Concord's UI.
func BrainDraftTask(msgs []domain.Message, instruction, selfName string) (string, bool) {
	prompt, ok := draftPrompt(msgs, instruction, selfName)
	if !ok {
		return "", false
	}
	return "[concord] The user has explicitly opted in to sharing this chat " +
		"excerpt with you in order to get a better reply draft.\n\n" + prompt, true
}

// ExpandQuery turns a search query into up to five related terms (synonyms,
// re-phrasings) for local search to try alongside the original. Structured
// output keeps small models on the rails.
func (c *Client) ExpandQuery(ctx context.Context, query string) ([]string, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, fmt.Errorf("assist: empty query")
	}
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"terms": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
		},
		"required": []string{"terms"},
	}
	prompt := "A user is searching their own chat history for: \"" + q + "\". " +
		"Suggest up to 5 alternative short search terms that could find the " +
		"same thing — synonyms, related words, likely phrasings people use in " +
		"chat. Single words or 2-word phrases only."
	raw, err := c.generate(ctx, c.pickModel(ctx, ""), prompt, schema, 0.3)
	if err != nil {
		return nil, err
	}
	var out struct {
		Terms []string `json:"terms"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("assist: the model's answer wasn't usable")
	}
	seen := map[string]bool{strings.ToLower(q): true}
	terms := make([]string, 0, 5)
	for _, t := range out.Terms {
		t = strings.TrimSpace(t)
		lt := strings.ToLower(t)
		if t == "" || seen[lt] || len(t) > 40 {
			continue
		}
		seen[lt] = true
		terms = append(terms, t)
		if len(terms) == 5 {
			break
		}
	}
	return terms, nil
}

// Probe reports reachability + pulled models for the status snapshot.
func (c *Client) Probe(ctx context.Context) (reachable bool, models []string) {
	names, err := c.Models(ctx)
	if err != nil {
		return false, nil
	}
	return true, names
}

// HasModel reports whether tag (or its base name) is among names.
func HasModel(model string, names []string) bool {
	base := strings.SplitN(model, ":", 2)[0]
	for _, n := range names {
		if n == model || strings.SplitN(n, ":", 2)[0] == base {
			return true
		}
	}
	return false
}

// CatchUpWindow is how much history the service hands CatchUp.
func CatchUpWindow() int { return catchUpMessages }
