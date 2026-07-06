package mailbox

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
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
	Op         string   `json:"op"`               // register | deposit | drain | ack
	Target     string   `json:"target,omitempty"` // deposit: recipient mailbox ID
	Envelope   []byte   `json:"envelope,omitempty"`
	TTLSeconds int64    `json:"ttl,omitempty"`
	AckIDs     []string `json:"ackIds,omitempty"`
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

// Service handles inbound mailbox requests on a libp2p host (the rendezvous
// node). Register/drain/ack derive the caller's mailbox from the Noise-
// authenticated peer identity, so no separate signature is needed — the
// libp2p PeerID *is* the account key.
type Service struct {
	store *Store
}

// NewService wraps a store as a stream handler.
func NewService(store *Store) *Service { return &Service{store: store} }

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
			if _, ok := svc.store.Deposit(req.Target, req.Envelope, time.Duration(req.TTLSeconds)*time.Second); ok {
				resp.OK = true
			} else {
				resp.Error = "rejected"
			}
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
