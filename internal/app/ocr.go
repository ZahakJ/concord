package app

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/nacl/secretbox"

	"github.com/zahak/concord/internal/ocr"
)

// OCR: text read out of image attachments by a LOCAL engine subprocess, so
// search can find a message by what a screenshot says. This is a strictly-local
// search enhancement — no cloud, no account, no other product. The engine
// (RapidOCR via scripts/concord-ocr, or $CONCORD_OCR_CMD) is the one non-Go
// piece; when it isn't installed the whole feature degrades cleanly to off and
// the one-binary default is unaffected. Extracted text IS message content, so
// it is sealed at rest exactly like a message body (see store.SaveAttachmentOCR).

const ocrSweepInterval = 30 * time.Minute

// OcrStatusView is the settings readout: whether an engine is installed, its
// name, and how many attachments have been indexed / failed.
type OcrStatusView struct {
	Available bool           `json:"available"`
	Engine    string         `json:"engine"`
	Counts    map[string]int `json:"counts"` // status ("ok"/"error"/…) -> count
}

// OcrStatus reports the local OCR pipeline state for the settings panel.
func (s *Service) OcrStatus() OcrStatusView {
	v := OcrStatusView{Counts: map[string]int{}}
	if s.ocrWorker != nil {
		v.Available = s.ocrWorker.Available()
		v.Engine = s.ocrWorker.EngineName()
	}
	if c, err := s.store.AttachmentOCRCounts(); err == nil {
		v.Counts = c
	}
	return v
}

// initOCR builds the worker (engine from $CONCORD_OCR_CMD or PATH), starts it,
// and schedules the sweep that picks up blobs stored before the worker existed
// (or received while the engine was missing).
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

// enqueueOCR hands one decrypted attachment image to the worker (no-ops without
// a worker/engine — hot paths never care).
func (s *Service) enqueueOCR(blobID string, plain []byte) {
	if s.ocrWorker != nil && s.ocrWorker.Available() {
		s.ocrWorker.Enqueue(blobID, plain)
	}
}

// sweepOCR walks stored messages for locally-cached attachment blobs with no OCR
// row yet, decrypts each with its token key, and queues it. Bounded per pass;
// runs on its own goroutine.
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
