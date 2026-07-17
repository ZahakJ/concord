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
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/nacl/secretbox"

	"github.com/zahak/concord/internal/assist"
	"github.com/zahak/concord/internal/domain"
	"github.com/zahak/concord/internal/ocr"
)

// -- assistant configuration --------------------------------------------------

// AssistConfig returns the stored assistant settings (off by default).
func (s *Service) AssistConfig() assist.Config {
	enabled, _ := s.store.GetSetting(assist.KeyEnabled)
	endpoint, _ := s.store.GetSetting(assist.KeyEndpoint)
	model, _ := s.store.GetSetting(assist.KeyModel)
	if endpoint == "" {
		endpoint = assist.DefaultEndpoint
	}
	if model == "" {
		model = assist.DefaultModel
	}
	return assist.Config{Enabled: enabled == "1", Endpoint: endpoint, Model: model}
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
	OCR OCRStatusView `json:"ocr"`
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
	out := AssistStatusView{Status: st, OCR: OCRStatusView{Counts: map[string]int{}}}
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

// -- assistant features -------------------------------------------------------

// AssistCatchUp summarizes the channel's recent history with the local model.
func (s *Service) AssistCatchUp(channelID string) (string, error) {
	c, err := s.assistClient()
	if err != nil {
		return "", err
	}
	msgs, err := s.Messages(channelID, assist.CatchUpWindow())
	if err != nil {
		return "", err
	}
	return c.CatchUp(s.ctx, msgs)
}

// AssistDraftReply drafts a reply to the channel's recent conversation,
// optionally steered by instruction.
func (s *Service) AssistDraftReply(channelID, instruction string) (string, error) {
	c, err := s.assistClient()
	if err != nil {
		return "", err
	}
	msgs, err := s.Messages(channelID, assist.CatchUpWindow())
	if err != nil {
		return "", err
	}
	self, _ := s.store.GetSetting("display_name")
	return c.DraftReply(s.ctx, msgs, instruction, self)
}

// AssistSearchView is an assisted search's result: the extra terms the local
// model suggested plus the merged hits (original query first).
type AssistSearchView struct {
	Terms    []string         `json:"terms"`
	Messages []domain.Message `json:"messages"`
}

// AssistSearch runs the normal local search, asks the local model for related
// terms, and folds in their hits — semantic-lite search over the user's own
// decrypted store, still 100% on-machine.
func (s *Service) AssistSearch(query string) (AssistSearchView, error) {
	c, err := s.assistClient()
	if err != nil {
		return AssistSearchView{}, err
	}
	out := AssistSearchView{Terms: []string{}}
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
