package app

// The service side of two strictly-local features:
//
//   - the opt-in Ollama assistant (internal/assist): catch-me-up summaries,
//     drafted replies, and search-term expansion — loopback-only, off by
//     default, config persisted in the encrypted-at-rest settings table;
//   - attachment OCR (internal/ocr): text read out of image attachments on
//     this machine so SearchMessages can find messages by what a screenshot
//     says. Runs regardless of the assistant toggle (it is not generative and
//     makes no network calls at all), but only when a local OCR engine is
//     installed.

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/nacl/secretbox"

	"github.com/zahak/concord/internal/assist"
	"github.com/zahak/concord/internal/brain"
	"github.com/zahak/concord/internal/domain"
	"github.com/zahak/concord/internal/ocr"
)

// -- assistant configuration --------------------------------------------------

// AssistConfig returns the stored assistant settings (off by default).
func (s *Service) AssistConfig() assist.Config {
	enabled, _ := s.store.GetSetting(assist.KeyEnabled)
	endpoint, _ := s.store.GetSetting(assist.KeyEndpoint)
	model, _ := s.store.GetSetting(assist.KeyModel)
	brainOn, _ := s.store.GetSetting(assist.KeyBrainEnabled)
	if endpoint == "" {
		endpoint = assist.DefaultEndpoint
	}
	if model == "" {
		model = assist.DefaultModel
	}
	return assist.Config{
		Enabled:  enabled == "1",
		Endpoint: endpoint,
		Model:    model,
		// The brain opt-in is meaningless on its own: the assistant's own
		// consent gate governs first, and this only narrows it further.
		BrainEnabled: enabled == "1" && brainOn == "1",
	}
}

// SetAssistBrain records the user's separate opt-in to the shared brain.
//
// Kept as its own call — not a field on SetAssistConfig — so that turning the
// assistant on can never turn brain routing on as a side effect. Enabling it
// while the assistant itself is off is accepted and stored, but stays inert:
// AssistConfig reports BrainEnabled false until the assistant's own consent
// gate is satisfied too.
func (s *Service) SetAssistBrain(enabled bool) (assist.Config, error) {
	on := "0"
	if enabled {
		on = "1"
	}
	if err := s.store.SetSetting(assist.KeyBrainEnabled, on); err != nil {
		return assist.Config{}, err
	}
	return s.AssistConfig(), nil
}

// SetAssistConfig validates and persists the assistant settings. The endpoint
// is loopback-validated here AND at call time (assist.NewClient).
func (s *Service) SetAssistConfig(enabled bool, endpoint, model string) (assist.Config, error) {
	base, err := assist.ValidateEndpoint(endpoint)
	if err != nil {
		return assist.Config{}, err
	}
	m := strings.TrimSpace(model)
	if m == "" {
		m = assist.DefaultModel
	}
	on := "0"
	if enabled {
		on = "1"
	}
	if err := s.store.SetSetting(assist.KeyEnabled, on); err != nil {
		return assist.Config{}, err
	}
	if err := s.store.SetSetting(assist.KeyEndpoint, base); err != nil {
		return assist.Config{}, err
	}
	if err := s.store.SetSetting(assist.KeyModel, m); err != nil {
		return assist.Config{}, err
	}
	return s.AssistConfig(), nil
}

// AssistStatus is the honest snapshot for the settings UI: the stored config
// plus a live probe of the local model server, plus the OCR engine state.
type AssistStatusView struct {
	assist.Status
	OCR   OCRStatusView   `json:"ocr"`
	Brain BrainStatusView `json:"brain"`
}

// BrainStatusView reports the shared brain's availability for the settings UI.
// Everything false is the normal state on a machine without Aether, and the
// assistant works unchanged there.
type BrainStatusView struct {
	// OptedIn is the user's own toggle (off by default).
	OptedIn bool `json:"optedIn"`
	// Available means Aether answered; Connected means a Claude Code session
	// is actually polling its queue right now.
	Available bool `json:"available"`
	Connected bool `json:"connected"`
	// Pinned means CONCORD_BRAIN pins this machine local-only, overriding the
	// toggle. Surfaced so the UI can explain why the switch looks inert.
	Pinned bool   `json:"pinned"`
	Queued int    `json:"queued"`
	Note   string `json:"note"`
}

// OCRStatusView reports the attachment-OCR pipeline's state.
type OCRStatusView struct {
	Available bool           `json:"available"`
	Engine    string         `json:"engine,omitempty"`
	Counts    map[string]int `json:"counts"`
}

func (s *Service) AssistStatus() AssistStatusView {
	cfg := s.AssistConfig()
	st := assist.Status{Enabled: cfg.Enabled, Endpoint: cfg.Endpoint, Model: cfg.Model}
	if c, err := assist.NewClient(cfg.Endpoint, cfg.Model); err == nil {
		reachable, models := c.Probe(s.ctx)
		st.Reachable = reachable
		st.Models = models
		st.ModelPresent = assist.HasModel(cfg.Model, models)
		if !reachable {
			st.Hint = "No local Ollama server at " + cfg.Endpoint +
				" — install it from ollama.com, start it, then `ollama pull " + cfg.Model + "`."
		} else if !st.ModelPresent {
			st.Hint = "Ollama is running but hasn't pulled “" + cfg.Model +
				"” — run `ollama pull " + cfg.Model + "`."
		}
	} else {
		st.Hint = err.Error()
	}
	st.BrainEnabled = cfg.BrainEnabled
	out := AssistStatusView{Status: st, OCR: OCRStatusView{Counts: map[string]int{}}}
	out.Brain = s.brainStatus(cfg)
	if s.ocrWorker != nil {
		out.OCR.Available = s.ocrWorker.Available()
		out.OCR.Engine = s.ocrWorker.EngineName()
	}
	if counts, err := s.store.AttachmentOCRCounts(); err == nil {
		out.OCR.Counts = counts
	}
	return out
}

func (s *Service) assistClient() (*assist.Client, error) {
	cfg := s.AssistConfig()
	if !cfg.Enabled {
		return nil, assist.ErrDisabled
	}
	return assist.NewClient(cfg.Endpoint, cfg.Model)
}

// -- the shared brain (see internal/brain) -------------------------------------
//
// Two different defaults live in here, and the difference is the whole design:
//
//   - DISCOVERY (brainStatus) is on by default. It moves no user content, so
//     there is nothing for a consent gate to protect.
//   - ROUTING (brainUsable, and everything downstream of it) is off by default
//     and double-opt-in, because every job Concord can build for the brain
//     contains decrypted message text.

// brain returns the brain client, building it lazily. Nil-safe throughout.
func (s *Service) brain() *brain.Client {
	if s.brainClient == nil {
		s.brainClient = brain.New()
	}
	return s.brainClient
}

// brainStatus snapshots the brain for the settings UI.
//
// # Discovery is ON by default; routing is not
//
// This probe is the one brain surface that carries no user content whatsoever.
// It runs `aether brain status` on this machine and reads back three derived
// facts: whether a brain harness exists, whether a session is attached right
// now, and how deep the queue is. A boolean, a boolean and an integer. No
// message text, no channel names, no metadata drawn from the store, and no
// network egress of any kind — Aether is another process on this same box.
//
// Gating that behind a consent toggle bought the user no privacy and cost them
// the ability to find out the feature exists at all, so it now runs by default.
// It is still short-circuited by CONCORD_BRAIN=off, which therefore remains a
// complete kill switch: a pinned machine does not even look.
//
// What is emphatically NOT on by default is routing. Discovering a brain never
// enqueues anything to it. Every path that would put decrypted message text in
// front of Claude is still double-gated — see brainUsable — and finding a brain
// here does not satisfy either gate.
func (s *Service) brainStatus(cfg assist.Config) BrainStatusView {
	out := BrainStatusView{OptedIn: cfg.BrainEnabled, Pinned: brain.Pinned()}
	if out.Pinned {
		out.Note = "Brain routing is pinned off on this machine by CONCORD_BRAIN — " +
			"everything runs on your local model."
		return out
	}
	st := s.brain().Status(s.ctx)
	out.Available, out.Connected, out.Queued, out.Note = st.Available, st.Connected, st.Queued, st.Note
	if !cfg.Enabled && out.Available {
		// Found one, but both consent gates are still shut. Say so plainly
		// rather than letting "available" read as "in use".
		out.Note = "A shared brain was found on this machine. Nothing is being " +
			"sent to it: switch the assistant on, then opt in separately, to use " +
			"it for drafted replies."
	}
	return out
}

// brainUsable reports whether a job may actually be routed to the brain right
// now. Every one of these must hold:
//
//   - the assistant's existing consent gate is satisfied (cfg.Enabled);
//   - the user separately opted this path in (cfg.BrainEnabled);
//   - CONCORD_BRAIN does not pin the machine local;
//   - a Claude Code session is attached right now.
//
// The last one matters twice over. Enqueuing to an unattended queue would
// leave the user staring at a spinner for a job nobody is going to answer, and
// an attached session that has since hit its usage limit shows up here as
// Connected=false — so a brain that has run out of capacity degrades to the
// local model rather than swallowing the job. With no session we go straight to
// the local model instead.
//
// Note what is NOT in this list: whether a brain was DISCOVERED. Discovery is
// on by default (see brainStatus); it is a fact about the machine, not a
// permission, and it can never stand in for either consent gate.
func (s *Service) brainUsable(cfg assist.Config) bool {
	if !cfg.Enabled || !cfg.BrainEnabled || brain.Pinned() {
		return false
	}
	st := s.brain().Status(s.ctx)
	return st.Available && st.Connected && st.Enabled
}

// brainWait is how long a foreground request will wait on an accepted job
// before handing the caller a pending result to poll. Short on purpose:
// nothing in Concord blocks on the brain.
const brainWait = 20 * time.Second

// brainPollInterval is how often an in-flight job is re-checked while a
// foreground request waits on it.
const brainPollInterval = 1500 * time.Millisecond

// brainQueue is the slice of the brain client this seam uses — an interface so
// the routing logic can be tested against a fake without a daemon.
type brainQueue interface {
	Enqueue(ctx context.Context, task string) (brain.Job, bool)
	Fetch(ctx context.Context, jobID string) (brain.Result, bool)
}

// askBrain enqueues one job and waits up to wait for the answer.
//
// Returns ok=false for anything the caller should handle by falling back to
// the local model — refusal, failure, unreachable daemon. A job that is merely
// still queued comes back as a pending Result with ok=true: that is an
// answer-in-progress, not a failure, and the UI polls it.
func askBrain(ctx context.Context, q brainQueue, task string, wait time.Duration) (assist.Result, bool) {
	job, ok := q.Enqueue(ctx, task)
	if !ok {
		return assist.Result{}, false
	}
	deadline := time.Now().Add(wait)
	for {
		res, ok := q.Fetch(ctx, job.ID)
		switch {
		case !ok:
			// The job was accepted but we cannot read it — Aether stopped, or
			// the queue is gone. Handing back a job id the UI can never poll
			// would strand the user; a local draft right now is better.
			return assist.Result{}, false
		case res.Done():
			return assist.BrainResult(res.Text, job.ID), true
		case !res.Pending():
			// failed / unknown state — fall back rather than show an error
			return assist.Result{}, false
		}
		if !time.Now().Before(deadline) {
			return assist.PendingResult(job.ID), true
		}
		select {
		case <-ctx.Done():
			return assist.Result{}, false
		case <-time.After(brainPollInterval):
		}
	}
}

func (s *Service) brainAsk(task string) (assist.Result, bool) {
	return askBrain(s.ctx, s.brain(), task, brainWait)
}

// AssistBrainJob polls a previously-returned pending job. The consent gate is
// re-checked here too: a job id is not a capability, and a user who switched
// the assistant off between enqueue and poll gets nothing back.
func (s *Service) AssistBrainJob(jobID string) (assist.Result, error) {
	cfg := s.AssistConfig()
	if !cfg.Enabled {
		return assist.Result{}, assist.ErrDisabled
	}
	if !cfg.BrainEnabled || brain.Pinned() {
		return assist.Result{}, fmt.Errorf("assist: brain routing is switched off")
	}
	res, ok := s.brain().Fetch(s.ctx, jobID)
	if !ok {
		return assist.Result{}, fmt.Errorf("assist: that brain job can't be read — Aether may have stopped")
	}
	switch {
	case res.Done():
		return assist.BrainResult(res.Text, jobID), nil
	case res.Pending():
		return assist.PendingResult(jobID), nil
	}
	msg := res.Error
	if msg == "" {
		msg = "the brain didn't answer"
	}
	return assist.Result{}, fmt.Errorf("assist: %s — try again and it will use your local model", msg)
}

// -- assistant features -------------------------------------------------------

// AssistCatchUp summarizes the channel's recent history.
//
// Routing: LOCAL, always. Summarizing a transcript is what the aether-brief
// specialist is for, and the brief's rule is not to hand the brain work a
// specialist already does well. Keeping the most-used feature fully on-device
// is also the better privacy trade.
func (s *Service) AssistCatchUp(channelID string) (assist.Result, error) {
	c, err := s.assistClient()
	if err != nil {
		return assist.Result{}, err
	}
	msgs, err := s.Messages(channelID, assist.CatchUpWindow())
	if err != nil {
		return assist.Result{}, err
	}
	return c.CatchUp(s.ctx, msgs)
}

// AssistDraftReply drafts a reply to the channel's recent conversation,
// optionally steered by instruction.
//
// Routing: brain -> local model -> honest failure. This is the one assistant
// job that is genuinely hard — writing in someone else's voice, following a
// steering instruction, and reading a thread's subtext are exactly what a 3B
// model does badly and a frontier model does well.
//
// It is also the one that would put decrypted message text in front of Claude,
// so it is double-gated (assistant on AND brain opted in), never on by
// default, and the result always names the engine that produced it.
func (s *Service) AssistDraftReply(channelID, instruction string) (assist.Result, error) {
	c, err := s.assistClient()
	if err != nil {
		return assist.Result{}, err
	}
	msgs, err := s.Messages(channelID, assist.CatchUpWindow())
	if err != nil {
		return assist.Result{}, err
	}
	self, _ := s.store.GetSetting("display_name")

	if s.brainUsable(s.AssistConfig()) {
		if task, ok := assist.BrainDraftTask(msgs, instruction, self); ok {
			if res, ok := s.brainAsk(task); ok {
				return res, nil
			}
			// Refused, failed, or unreachable — fall through to the local
			// model. The user gets a draft either way, correctly labeled.
		}
	}
	return c.DraftReply(s.ctx, msgs, instruction, self)
}

// AssistSearchView is an assisted search's result: the extra terms the local
// model suggested plus the merged hits (original query first).
type AssistSearchView struct {
	Terms    []string         `json:"terms"`
	Messages []domain.Message `json:"messages"`
	// Engine/Note name what expanded the query. Always local: five synonyms is
	// cheap structured work a small model is good at, and routing every
	// keystroke-driven search to the brain would be both slow and a needless
	// disclosure.
	Engine assist.Engine `json:"engine"`
	Note   string        `json:"note"`
}

// AssistSearch runs the normal local search, asks the local model for related
// terms, and folds in their hits — semantic-lite search over the user's own
// decrypted store, still 100% on-machine.
func (s *Service) AssistSearch(query string) (AssistSearchView, error) {
	c, err := s.assistClient()
	if err != nil {
		return AssistSearchView{}, err
	}
	cfg := s.AssistConfig()
	out := AssistSearchView{Terms: []string{}, Engine: assist.EngineLocal, Note: assist.LocalNote(cfg.Model)}
	seen := map[string]bool{}
	add := func(msgs []domain.Message) {
		for _, m := range msgs {
			if !seen[m.ID] {
				seen[m.ID] = true
				out.Messages = append(out.Messages, m)
			}
		}
	}
	if msgs, err := s.SearchMessages(query, 50); err == nil {
		add(msgs)
	}
	terms, err := c.ExpandQuery(s.ctx, query)
	if err != nil {
		// the plain results still stand — degrade honestly
		return out, nil
	}
	for _, t := range terms {
		if msgs, err := s.SearchMessages(t, 20); err == nil && len(msgs) > 0 {
			out.Terms = append(out.Terms, t)
			add(msgs)
		}
	}
	return out, nil
}

// -- attachment OCR wiring ----------------------------------------------------

const ocrSweepInterval = 30 * time.Minute

// initOCR builds the worker (engine from $CONCORD_OCR_CMD or PATH), starts
// it, and schedules the sweep that picks up blobs stored before the worker
// existed (or received while the engine was missing).
func (s *Service) initOCR() {
	var command []string
	if env := strings.TrimSpace(os.Getenv("CONCORD_OCR_CMD")); env != "" {
		command = strings.Fields(env)
	}
	s.ocrWorker = ocr.NewWorker(s.store, command)
	if !s.ocrWorker.Available() {
		return // no engine on this machine — everything else degrades cleanly
	}
	s.ocrWorker.Start()
	go func() {
		// A first pass shortly after unlock, then periodically.
		timer := time.NewTimer(20 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-timer.C:
				s.sweepOCR()
				timer.Reset(ocrSweepInterval)
			}
		}
	}()
}

// enqueueOCR hands one decrypted attachment image to the worker (no-ops
// without a worker/engine — hot paths never care).
func (s *Service) enqueueOCR(blobID string, plain []byte) {
	if s.ocrWorker != nil && s.ocrWorker.Available() {
		s.ocrWorker.Enqueue(blobID, plain)
	}
}

// sweepOCR walks stored messages for locally-cached attachment blobs with no
// OCR row yet, decrypts each with its token key, and queues it. Bounded per
// pass; runs on its own goroutine.
func (s *Service) sweepOCR() {
	blobIDs, keys, err := s.store.AttachmentsMissingOCR(40)
	if err != nil {
		return
	}
	for i, id := range blobIDs {
		ct, ok, err := s.store.GetAttachment(id)
		if err != nil || !ok {
			continue
		}
		plain, err := openBlob(ct, keys[i])
		if err != nil {
			// wrong/corrupt key: record it so the sweep never retries forever
			_ = s.store.SaveAttachmentOCR(id, "", "error")
			continue
		}
		s.enqueueOCR(id, plain)
	}
}

// openBlob decrypts an attachment ciphertext with a token's key||nonce string
// (the same format attach.go seals with).
func openBlob(ct []byte, keysB64 string) ([]byte, error) {
	kb, err := base64.RawURLEncoding.DecodeString(keysB64)
	if err != nil || len(kb) != attachKeysLen {
		return nil, fmt.Errorf("app: bad attachment key")
	}
	var key [32]byte
	var nonce [24]byte
	copy(key[:], kb[:32])
	copy(nonce[:], kb[32:])
	plain, ok := secretbox.Open(nil, ct, &nonce, &key)
	if !ok {
		return nil, fmt.Errorf("app: attachment decrypt failed")
	}
	return plain, nil
}
