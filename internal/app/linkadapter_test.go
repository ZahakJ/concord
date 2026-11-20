package app

import (
	"context"
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
	return s.handleLinkRequest(ctx, from, req)
}

// manualLinkConfirm makes the issuer wait for a person instead of the tests'
// automatic yes. There is no confirm step to wait for yet.
func manualLinkConfirm(s *Service) {}

// declineLinkPrompt waits up to d for the issuer to ask its owner about a
// device, answers no, and returns what the prompt showed. There is no prompt.
func declineLinkPrompt(s *Service, d time.Duration) (name, sas string, asked bool) {
	return "", "", false
}

// ageLinkOffer makes the issuer's current offer d older on the issuer's own
// clock. The issuer keeps no clock for offers.
func ageLinkOffer(s *Service, d time.Duration) {}
