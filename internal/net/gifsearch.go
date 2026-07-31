package net

import (
	"context"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// GIF search, transport half.
//
// A member's client never talks to Tenor. It asks its own rendezvous node over
// this stream, and the node makes the outbound HTTPS call. That is the whole
// privacy argument, so the protocol carries the MEDIA BYTES too, not just the
// result metadata: if a search reply contained tenor.com URLs and the client
// put them in an <img src>, every member's browser would connect to Google
// directly and the proxy would buy nothing at all. Op "media" is what keeps
// that from happening — thumbnails and full GIFs both come back through here.
//
// A libp2p stream rather than plain HTTP to the node, because it is what the
// client already has: the connection to the rendezvous exists, it is Noise-
// encrypted and mutually authenticated by PeerID, and it needs no TLS
// certificate, no extra port and no new listener on the node. A plain HTTP
// endpoint would also hand the node (and anything on the path to it) a fresh
// TCP connection per search, with the search terms in a URL that proxies and
// server logs are built to record.
const gifSearchProtocol protocol.ID = "/concord/gifsearch/1.0.0"

// maxGifSearchRequest bounds the request frame — a tiny JSON op plus, at worst,
// a signed media handle.
const maxGifSearchRequest = 8 << 10 // 8 KiB

// MaxGifMediaBytes is the largest single image the proxy will fetch and relay.
// It is deliberately the same ceiling the app layer puts on an inline image
// attachment (maxAttachmentPlain): a GIF that came back bigger than that could
// not be posted anyway, so fetching it would only waste the node's bandwidth.
const MaxGifMediaBytes = 5 << 20 // 5 MiB

// MaxGifSearchResponse bounds a served frame: one image at MaxGifMediaBytes,
// base64'd into JSON (4/3), plus headroom for a page of result metadata.
const MaxGifSearchResponse = 8 << 20 // 8 MiB

// gifSearchStreamTimeout is the ceiling on one round trip. A libp2p stream has
// no deadline unless one is set and readFrame is a bare io.ReadFull, so without
// this a node that accepts the stream and then says nothing parks the caller
// forever — the "spinner that spins forever" this feature is not allowed to
// have. It is generous because the node has to make its own HTTPS call inside
// this budget.
const gifSearchStreamTimeout = 30 * time.Second

// GifSearchResponder answers one inbound GIF-search request. Only the
// rendezvous node registers one; clients are pure callers.
type GifSearchResponder func(ctx context.Context, from peer.ID, request []byte) (response []byte, err error)

// ServeGifSearch registers the GIF-search handler on a libp2p host. It takes a
// bare host.Host rather than hanging off *Host because the only node that
// serves this is cmd/rendezvous, which builds its libp2p host directly.
func ServeGifSearch(ctx context.Context, h host.Host, responder GifSearchResponder) {
	h.SetStreamHandler(gifSearchProtocol, func(s network.Stream) {
		defer s.Close()
		// A stream is a goroutine plus buffers; a peer that opens one and never
		// asks anything must not hold them indefinitely.
		_ = s.SetDeadline(time.Now().Add(gifSearchStreamTimeout))
		req, err := readFrame(s, maxGifSearchRequest)
		if err != nil {
			return
		}
		resp, err := responder(ctx, s.Conn().RemotePeer(), req)
		if err != nil {
			return
		}
		_ = writeFrame(s, resp, MaxGifSearchResponse)
	})
}

// RequestGifSearch asks an already-connected node to run a GIF search, or to
// fetch one image it previously offered.
func (n *Host) RequestGifSearch(ctx context.Context, to peer.ID, request []byte) ([]byte, error) {
	s, err := n.h.NewStream(relayCtx(ctx, "gif-search"), to, gifSearchProtocol)
	if err != nil {
		return nil, fmt.Errorf("net: open gif search stream: %w", err)
	}
	defer s.Close()
	// The caller's context stops applying the moment the stream is open, so its
	// patience has to be transferred onto the stream itself. Not streamDeadline:
	// that falls back to the release protocol's much longer budget.
	deadline := time.Now().Add(gifSearchStreamTimeout)
	if d, ok := ctx.Deadline(); ok {
		deadline = d
	}
	_ = s.SetDeadline(deadline)
	if err := writeFrame(s, request, maxGifSearchRequest); err != nil {
		return nil, err
	}
	if err := s.CloseWrite(); err != nil {
		return nil, err
	}
	return readFrame(s, MaxGifSearchResponse)
}
