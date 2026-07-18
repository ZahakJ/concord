package mailbox

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// Protocol is the mailbox stream protocol ID.
const Protocol protocol.ID = "/concord/mailbox/1.0.0"

const maxFrame = 1 << 20 // 1 MiB request/response frame

// mailboxSalt namespaces the mailbox-tag hash.
var mailboxSalt = []byte("concord-mbx-v1")

// MailboxID is the opaque 16-byte (hex) tag a recipient's account pubkey maps
// to. Senders compute it from the recipient's account pubkey (which group
// members already hold as the MLS credential); the node cannot reverse it.
func MailboxID(accountPub []byte) string {
	h := sha256.New()
	h.Write(accountPub)
	h.Write(mailboxSalt)
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// Request is the client→node message.
type Request struct {
	Op         string   `json:"op"`               // register | deposit | drain | ack | register-push | unregister-push
	Target     string   `json:"target,omitempty"` // deposit: recipient mailbox ID
	Envelope   []byte   `json:"envelope,omitempty"`
	TTLSeconds int64    `json:"ttl,omitempty"`
	AckIDs     []string `json:"ackIds,omitempty"`
	// Push registration: bind a device push token to the caller's own mailbox so
	// the node can send a contentless wake when a deposit lands while they're
	// offline. Platform is "apns" or "fcm". Tokens are keyed only by the opaque
	// mailbox tag — never by identity — so this adds no linkage the node didn't
	// already have (it maps tag↔peer at drain time regardless).
	Platform string `json:"platform,omitempty"`
	Token    string `json:"token,omitempty"`
}

// Response is the node→client reply.
type Response struct {
	OK        bool          `json:"ok"`
	Envelopes []EnvelopeMsg `json:"envelopes,omitempty"` // drain result
	Error     string        `json:"error,omitempty"`
}

// EnvelopeMsg is a drained envelope on the wire.
type EnvelopeMsg struct {
	ID   string `json:"id"`
	Data []byte `json:"data"`
}

// Deposit rate limiting. A deposit targets an arbitrary (registered) mailbox, so
// unlike register/drain/ack it isn't self-scoped — a single peer could otherwise
// flood the store and evict everyone's genuine offline messages (an availability
// attack bounded only by the byte cap). A per-peer token bucket caps the rate:
// bursts up to depositBurst (enough to fan one message out to many offline
// members at once), then depositRate/sec sustained.
const (
	depositRate  = 2.0 // sustained deposits per second per peer
	depositBurst = 120.0
)

type bucket struct {
	tokens float64
	last   time.Time
}

// Service handles inbound mailbox requests on a libp2p host (the rendezvous
// node). Register/drain/ack derive the caller's mailbox from the Noise-
// authenticated peer identity, so no separate signature is needed — the
// libp2p PeerID *is* the account key.
type Service struct {
	store *Store

	// notifier, when set, is asked to send a contentless push wake to a
	// mailbox's registered devices after a deposit lands. nil disables push
	// (the mailbox still works over live sockets + drain-on-reconnect).
	notifier Notifier
	// pushes maps a mailbox tag to its registered device tokens. Persisted by
	// the notifier's own store; here it's the in-memory lookup for deposits.
	pushes *PushStore

	mu      sync.Mutex
	buckets map[peer.ID]*bucket
}

// NewService wraps a store as a stream handler.
func NewService(store *Store) *Service {
	return &Service{store: store, buckets: map[peer.ID]*bucket{}}
}

// WithPush enables push notifications: register-push tokens go to ps, and a
// deposit to a mailbox with registered tokens triggers n.Notify. Both may be
// nil independently (a store with no notifier just remembers tokens).
func (svc *Service) WithPush(ps *PushStore, n Notifier) *Service {
	svc.pushes = ps
	svc.notifier = n
	return svc
}

// allowDeposit reports whether peer p may deposit right now, consuming a token.
func (svc *Service) allowDeposit(p peer.ID) bool {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	now := time.Now()
	b := svc.buckets[p]
	if b == nil {
		b = &bucket{tokens: depositBurst, last: now}
		svc.buckets[p] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * depositRate
	if b.tokens > depositBurst {
		b.tokens = depositBurst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// pruneBuckets drops fully-refilled, idle buckets so the map can't grow without
// bound as distinct peers come and go.
func (svc *Service) pruneBuckets() {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	now := time.Now()
	for p, b := range svc.buckets {
		if b.tokens >= depositBurst && now.Sub(b.last) > time.Hour {
			delete(svc.buckets, p)
		}
	}
}

// Attach registers the mailbox handler on a host and starts periodic GC.
func (svc *Service) Attach(ctx context.Context, h host.Host) {
	h.SetStreamHandler(Protocol, svc.handle)
	go func() {
		t := time.NewTicker(10 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				svc.store.GC()
				svc.pruneBuckets()
			}
		}
	}()
}

func (svc *Service) handle(s network.Stream) {
	defer s.Close()
	reqBytes, err := readFrame(s)
	if err != nil {
		return
	}
	var req Request
	if json.Unmarshal(reqBytes, &req) != nil {
		return
	}

	// The caller's account pubkey (== their PeerID key).
	callerMailbox := ""
	if pk, err := s.Conn().RemotePeer().ExtractPublicKey(); err == nil {
		if raw, err := pk.Raw(); err == nil {
			callerMailbox = MailboxID(raw)
		}
	}

	var resp Response
	switch req.Op {
	case "register":
		if callerMailbox != "" {
			svc.store.Register(callerMailbox)
			resp.OK = true
		}
	case "deposit":
		if req.Target != "" {
			if !svc.allowDeposit(s.Conn().RemotePeer()) {
				resp.Error = "rate limited"
			} else if _, ok := svc.store.Deposit(req.Target, s.Conn().RemotePeer().String(), req.Envelope, time.Duration(req.TTLSeconds)*time.Second); ok {
				resp.OK = true
				// A deposit means the recipient is (probably) offline — wake their
				// registered devices so they foreground and drain. Contentless and
				// rate-limited inside the notifier; runs in the background so the
				// depositor isn't blocked on a push round-trip.
				if svc.notifier != nil && svc.pushes != nil {
					if toks := svc.pushes.Tokens(req.Target); len(toks) > 0 {
						go svc.notifier.Notify(req.Target, toks)
					}
				}
			} else {
				resp.Error = "rejected"
			}
		}
	case "register-push":
		if callerMailbox != "" && req.Platform != "" && req.Token != "" && svc.pushes != nil {
			svc.pushes.Register(callerMailbox, DeviceToken{Platform: req.Platform, Token: req.Token})
			resp.OK = true
		}
	case "unregister-push":
		if callerMailbox != "" && req.Token != "" && svc.pushes != nil {
			svc.pushes.Unregister(callerMailbox, req.Token)
			resp.OK = true
		}
	case "drain":
		if callerMailbox != "" {
			for _, e := range svc.store.Drain(callerMailbox) {
				resp.Envelopes = append(resp.Envelopes, EnvelopeMsg{ID: e.ID, Data: e.Data})
			}
			resp.OK = true
		}
	case "ack":
		if callerMailbox != "" {
			svc.store.Ack(callerMailbox, req.AckIDs)
			resp.OK = true
		}
	}

	out, _ := json.Marshal(resp)
	_ = writeFrame(s, out)
}

// ---- framing (4-byte big-endian length prefix) ----

func writeFrame(w io.Writer, data []byte) error {
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(data)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}

func readFrame(r io.Reader) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > maxFrame {
		return nil, io.ErrShortBuffer
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// RequestOn opens a mailbox stream to a node and returns its response. Shared
// by the client-side Host methods.
func RequestOn(ctx context.Context, h host.Host, node peer.ID, req Request) (Response, error) {
	s, err := h.NewStream(ctx, node, Protocol)
	if err != nil {
		return Response{}, err
	}
	defer s.Close()
	body, _ := json.Marshal(req)
	if err := writeFrame(s, body); err != nil {
		return Response{}, err
	}
	if err := s.CloseWrite(); err != nil {
		return Response{}, err
	}
	respBytes, err := readFrame(s)
	if err != nil {
		return Response{}, err
	}
	var resp Response
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return Response{}, err
	}
	return resp, nil
}
