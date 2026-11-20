package net

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	msmux "github.com/multiformats/go-multistream"
)

// Device linking. The joiner (a new device) dials the issuer (an
// already-unlocked device that displayed the QR) and sends one request frame;
// the issuer answers. Like the invite stream, this layer is semantics-free: it
// moves opaque frames; the app layer defines the JSON shape and the crypto.
//
// The connection is Noise-encrypted by libp2p, and the dialler checks the
// responder against the PeerID it dialled: that, not the secret proofs inside
// the frames, is what keeps a man in the middle out. The responder learns the
// dialler's PeerID the same way, and the app requires it to be the device key
// it is asked to certify.

// linkConfirmProtocol is device linking with the confirm step. After the
// request the issuer may send one wait frame, saying it has taken the code for
// this device and is asking its owner, and then the final frame. Each frame on
// this stream starts with a tag byte (linkFrameWait, linkFrameFinal).
const linkConfirmProtocol protocol.ID = "/concord/link/2.0.0"

// linkProtocol is the protocol before the confirm step: one request, one
// response that carried the account seed. An issuer still registers it, but
// only to refuse an older device with a message telling it to update; nothing
// on this side ever dials it to link, so a newer device never falls back to
// the unconfirmed flow.
const linkProtocol protocol.ID = "/concord/link/1.0.0"

const (
	linkFrameWait  byte = 1
	linkFrameFinal byte = 2
)

// linkHold is how long a link stream may stay open after the wait frame,
// while a person reads the issuer's screen. The app's own window for the
// answer is shorter; this only bounds the stream.
const linkHold = 2 * time.Minute

// maxLinkFrame caps a link frame. The response carries the account seed, profile,
// bootstrap config, and guild list — kept well under 1 MiB.
const maxLinkFrame = 1 << 20

// ErrLinkPeerTooOld: the device that showed the code does not speak the
// linking protocol with the confirm step. It runs a Concord from before 0.61,
// which would hand the account over without asking its owner, so this device
// does not link to it at all.
var ErrLinkPeerTooOld = errors.New("net: the other device runs an older Concord, which links without asking you to confirm; update Concord on it, then make a new code")

// tooOld reports whether err is the peer refusing a protocol it does not have.
func tooOld(err error) bool {
	return errors.Is(err, msmux.ErrNotSupported[protocol.ID]{})
}

// LinkResponder handles an inbound request on the pre-0.61 link protocol and
// returns the response bytes. Returning an error aborts the stream.
type LinkResponder func(ctx context.Context, from peer.ID, request []byte) (response []byte, err error)

// LinkConfirmResponder handles an inbound link request. from is the dialler's
// PeerID as Noise authenticated it. wait sends the one wait frame and keeps
// the stream open while the owner decides; the returned bytes are the final
// frame. Returning an error aborts the stream.
type LinkConfirmResponder func(ctx context.Context, from peer.ID, request []byte, wait func([]byte) error) (response []byte, err error)

// HandleLink registers the responder for the pre-0.61 link protocol.
func (n *Host) HandleLink(responder LinkResponder) {
	n.h.SetStreamHandler(linkProtocol, func(s network.Stream) {
		defer s.Close()
		_ = s.SetDeadline(time.Now().Add(serveTimeout))
		req, err := readFrame(s, maxLinkFrame)
		if err != nil {
			return
		}
		resp, err := responder(n.ctx, s.Conn().RemotePeer(), req)
		if err != nil {
			return
		}
		if writeFrame(s, resp, maxLinkFrame) == nil {
			EndAnswer(s)
		}
	})
}

// HandleLinkConfirmed registers the issuer-side handler of the linking
// protocol with the confirm step.
func (n *Host) HandleLinkConfirmed(responder LinkConfirmResponder) {
	n.h.SetStreamHandler(linkConfirmProtocol, func(s network.Stream) {
		defer s.Close()
		_ = s.SetDeadline(time.Now().Add(serveTimeout))
		req, err := readFrame(s, maxLinkFrame)
		if err != nil {
			return
		}
		waited := false
		wait := func(b []byte) error {
			if waited {
				return errors.New("net: a link sends one wait frame")
			}
			waited = true
			_ = s.SetDeadline(time.Now().Add(linkHold))
			return writeFrame(s, append([]byte{linkFrameWait}, b...), maxLinkFrame)
		}
		resp, err := responder(n.ctx, s.Conn().RemotePeer(), req, wait)
		if err != nil {
			return
		}
		if writeFrame(s, append([]byte{linkFrameFinal}, resp...), maxLinkFrame) == nil {
			EndAnswer(s)
		}
	})
}

// RequestLinkConfirmed dials the issuer (from the QR's address info), sends
// the request, and returns the issuer's final answer. onWait, if set, gets the
// body of the issuer's wait frame when it arrives: the issuer has taken the
// code for this device and is asking its owner.
//
// An issuer without the protocol is reported as ErrLinkPeerTooOld at once,
// with no retry and no fallback to the older protocol.
func (n *Host) RequestLinkConfirmed(ctx context.Context, issuer peer.AddrInfo, request []byte, onWait func([]byte)) ([]byte, error) {
	s, err := n.dialLinkStream(ctx, issuer, linkConfirmProtocol)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	// Never shorter than replyFloor: the request uses up the issuer's
	// single-use code, and leaving before the answer wastes it.
	_ = s.SetDeadline(replyDeadline(ctx, serveTimeout, replyFloor))
	if err := sendRequest(s, request, maxLinkFrame); err != nil {
		if tooOld(err) {
			return nil, ErrLinkPeerTooOld
		}
		return nil, fmt.Errorf("net: send link request: %w", err)
	}
	waited := false
	for {
		f, err := readFrame(s, maxLinkFrame)
		if err != nil {
			if tooOld(err) {
				return nil, ErrLinkPeerTooOld
			}
			return nil, err
		}
		if len(f) == 0 {
			return nil, errors.New("net: empty link frame")
		}
		switch {
		case f[0] == linkFrameWait && !waited:
			waited = true
			// The issuer holds the stream for up to linkHold while its owner
			// decides; wait a little longer than that for the answer.
			_ = s.SetDeadline(time.Now().Add(linkHold + 15*time.Second))
			if onWait != nil {
				onWait(f[1:])
			}
		case f[0] == linkFrameFinal:
			return f[1:], nil
		default:
			return nil, fmt.Errorf("net: unexpected link frame %d", f[0])
		}
	}
}

// RequestLink is the joiner side of the pre-0.61 protocol. Concord no longer
// links with it; it remains so tests can play an older device against an
// issuer and check the refusal it gets.
func (n *Host) RequestLink(ctx context.Context, issuer peer.AddrInfo, request []byte) ([]byte, error) {
	s, err := n.dialLinkStream(ctx, issuer, linkProtocol)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	_ = s.SetDeadline(replyDeadline(ctx, serveTimeout, replyFloor))
	if err := sendRequest(s, request, maxLinkFrame); err != nil {
		return nil, fmt.Errorf("net: send link request: %w", err)
	}
	return readFrame(s, maxLinkFrame)
}

// dialLinkStream connects to the issuer and opens the link stream, retrying the
// whole connect+open with capped exponential backoff until ctx is done.
//
// Getting a connection + stream to the issuer is the flaky part: off-LAN it
// rides a relay reservation and a hole-punch that routinely need several seconds
// and a couple of attempts to settle. So the connect-and-open phase RETRIES with
// backoff until ctx expires, reporting the last real error rather than a bare
// "context deadline exceeded". No request frame is sent during that phase, so
// retrying is safe. A peer that answers but lacks the protocol is not retried.
func (n *Host) dialLinkStream(ctx context.Context, issuer peer.AddrInfo, proto protocol.ID) (network.Stream, error) {
	backoff := time.Second
	var lastErr error
	for attempt := 0; ; attempt++ {
		// Each attempt gets a bounded slice of the overall budget, so one wedged
		// dial can't consume the entire deadline before we retry over a fresh path.
		attemptCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		reused := n.h.Network().Connectedness(issuer.ID) == network.Connected
		if !reused {
			lastErr = n.h.Connect(attemptCtx, issuer)
		} else {
			lastErr = nil
		}
		if lastErr == nil {
			s, serr := n.newStream(attemptCtx, issuer.ID, proto)
			cancel()
			if serr == nil {
				return s, nil
			}
			if tooOld(serr) {
				return nil, ErrLinkPeerTooOld
			}
			// A connection libp2p still calls Connected is not necessarily one
			// that works: a relay circuit whose relay is gone, or a socket the
			// other end forgot across a restart, stays "up" here and only fails
			// when you try to use it. Retrying against it is what turns a link
			// attempt into a minute of "context deadline exceeded" — every pass
			// skips the dial, hangs on the same corpse, and never tries the
			// addresses the QR actually carries. So retire it and make the next
			// attempt a real dial.
			if reused {
				_ = n.h.Network().ClosePeer(issuer.ID)
			}
			lastErr = fmt.Errorf("open link stream: %w", serr)
		} else {
			cancel()
			lastErr = fmt.Errorf("connect to linking device: %w", lastErr)
		}
		select {
		case <-ctx.Done():
			// Surface the concrete reachability failure, not the deadline.
			if lastErr != nil {
				return nil, fmt.Errorf("net: couldn't reach the other device (is it still on the linking screen, and are both devices online?): %w", lastErr)
			}
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 8*time.Second {
			backoff *= 2
		}
	}
}
