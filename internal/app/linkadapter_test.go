package app

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// The issuer-side surface the device-link hardening tests drive. The tests
// themselves describe what must hold; this file is the only part that knows
// how the issuer is reached, so the same test bodies run before and after the
// fix.

// issuerAnswer hands one link request to the issuer as if it arrived from the
// peer `from`, and returns the response bytes, or an error for a refusal.
func issuerAnswer(ctx context.Context, s *Service, from peer.ID, req []byte) ([]byte, error) {
	out, err := s.handleLinkRequest(ctx, from, req, func([]byte) error { return nil })
	if err != nil {
		return nil, err
	}
	var resp linkResponse
	if json.Unmarshal(out, &resp) == nil && resp.Error != "" {
		return nil, errors.New(resp.Error)
	}
	return out, nil
}

// manualLinkConfirm makes the issuer wait for a person instead of the tests'
// automatic yes.
func manualLinkConfirm(s *Service) {
	s.linkMu.Lock()
	s.linkManual = true
	s.linkMu.Unlock()
}

// waitLinkPrompt waits up to d for the issuer to ask its owner about a device.
func waitLinkPrompt(s *Service, d time.Duration) *LinkPrompt {
	for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if p := s.LinkStatus().Prompt; p != nil {
			return p
		}
	}
	return nil
}

// declineLinkPrompt waits up to d for the issuer to ask its owner about a
// device, answers no, and returns what the prompt showed.
func declineLinkPrompt(s *Service, d time.Duration) (name, sas string, asked bool) {
	p := waitLinkPrompt(s, d)
	if p == nil {
		return "", "", false
	}
	_ = s.ConfirmLink(p.ID, false)
	return p.DeviceName, p.SAS, true
}

// ageLinkOffer makes the issuer's current offer d older on the issuer's own
// clock.
func ageLinkOffer(s *Service, d time.Duration) {
	s.linkMu.Lock()
	if s.linkOffer != nil {
		s.linkOffer.made = s.linkOffer.made.Add(-d)
	}
	s.linkMu.Unlock()
}
