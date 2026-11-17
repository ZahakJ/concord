package net

import (
	"io"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
)

// A responder on a one-request, one-response protocol reads the request's one
// frame, writes its answer and closes. On QUIC, Close also stops the reading
// side, which tells the requester to stop sending. A requester that has not
// yet half-closed its own side by then gets an error from CloseWrite, and
// requesters before 0.61 gave up there and dropped the answer already waiting
// in the stream. So after the answer a responder half-closes and waits for the
// requester's end of stream before the deferred Close.
//
// The wait costs a requester nothing: the answer is complete and on its way
// before it starts, and a current requester half-closes right after its
// request, so its end of stream has usually arrived already.

// answerDrain is the longest a responder waits for the requester's end of
// stream after answering. A stream is held at most this much longer than it
// was before the wait existed.
const answerDrain = 5 * time.Second

// answerDrainBytes is how much a responder reads and discards while it waits.
// A requester has nothing more to send; one that keeps sending is not waited
// for.
const answerDrainBytes = 4 << 10

// maxDraining bounds how many streams in this process wait at once. Past it
// a responder closes straight away, as it did before the wait existed, so a
// peer that opens many streams and never half-closes cannot keep more than
// maxDraining of them alive for answerDrain each.
const maxDraining = 256

var draining = make(chan struct{}, maxDraining)

// EndAnswer half-closes s after the responder's answer has been written and
// waits, at most answerDrain, for the requester to half-close too. The caller
// still closes s.
func EndAnswer(s network.Stream) {
	select {
	case draining <- struct{}{}:
	default:
		return
	}
	defer func() { <-draining }()
	if err := s.CloseWrite(); err != nil {
		return
	}
	_ = s.SetReadDeadline(time.Now().Add(answerDrain))
	_, _ = io.CopyN(io.Discard, s, answerDrainBytes)
}
