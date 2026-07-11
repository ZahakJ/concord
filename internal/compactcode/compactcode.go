// Package compactcode is the compact binary layout behind Concord's shareable
// codes (guild invites, device-link offers). The original codes were
// base64url(JSON) — field names, string multiaddrs, and base58 peer IDs made
// them comically long for something a human pastes or scans. Here every field
// is length-prefixed binary: peer IDs as their multihash bytes, multiaddrs as
// their binary form, with a one-byte flag so values that don't parse (tests,
// hand-typed addresses) fall back to raw UTF-8 losslessly.
//
// The encoders in internal/link (device-link offers, "CL1…") and internal/app
// (guild invites, "CI1…") build on these helpers; both keep decoding the
// legacy JSON codes, so new clients read old codes (old clients can't read new
// ones — acceptable, codes are ephemeral).
package compactcode

import (
	"encoding/binary"
	"errors"
	"strings"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

// Per-entry flag: packed binary representation vs raw UTF-8 fallback.
const (
	rawEntry    = 0x00
	packedEntry = 0x01
)

// MaxAddrs caps how many addresses a code carries — beyond a handful they're
// redundant and only fatten the QR.
const MaxAddrs = 8

// AppendBytes appends a uvarint length prefix + the bytes.
func AppendBytes(b, p []byte) []byte {
	b = binary.AppendUvarint(b, uint64(len(p)))
	return append(b, p...)
}

// AppendString appends a length-prefixed UTF-8 string.
func AppendString(b []byte, s string) []byte { return AppendBytes(b, []byte(s)) }

// AppendPeerID appends a peer ID as its multihash bytes (≈38 bytes vs a
// 52-char base58 string) when it parses, else as a raw string.
func AppendPeerID(b []byte, id string) []byte {
	if pid, err := peer.Decode(id); err == nil {
		b = append(b, packedEntry)
		return AppendBytes(b, []byte(pid))
	}
	b = append(b, rawEntry)
	return AppendString(b, id)
}

// AppendAddrs appends a count-prefixed list of multiaddrs, each in binary form
// when it parses (an /ip4/…/tcp/… addr is ~10 bytes vs ~25 chars).
func AppendAddrs(b []byte, addrs []string) []byte {
	b = binary.AppendUvarint(b, uint64(len(addrs)))
	for _, a := range addrs {
		if ma, err := multiaddr.NewMultiaddr(a); err == nil {
			b = append(b, packedEntry)
			b = AppendBytes(b, ma.Bytes())
		} else {
			b = append(b, rawEntry)
			b = AppendString(b, a)
		}
	}
	return b
}

// Reader walks an encoded payload with sticky-error semantics: after the first
// malformed field every further read returns a zero value, and Err() reports.
type Reader struct {
	b   []byte
	err error
}

func NewReader(b []byte) *Reader { return &Reader{b: b} }

var errBad = errors.New("compactcode: truncated or malformed")

func (r *Reader) fail() {
	if r.err == nil {
		r.err = errBad
	}
}

// Err reports the first decode error, if any.
func (r *Reader) Err() error { return r.err }

// Uvarint reads one varint.
func (r *Reader) Uvarint() uint64 {
	if r.err != nil {
		return 0
	}
	v, n := binary.Uvarint(r.b)
	if n <= 0 {
		r.fail()
		return 0
	}
	r.b = r.b[n:]
	return v
}

// Byte reads one byte.
func (r *Reader) Byte() byte {
	if r.err != nil || len(r.b) == 0 {
		r.fail()
		return 0
	}
	v := r.b[0]
	r.b = r.b[1:]
	return v
}

// Take reads exactly n raw bytes (no length prefix). The returned slice is
// copied, so it stays valid independent of the underlying buffer.
func (r *Reader) Take(n int) []byte {
	if r.err != nil || n < 0 || len(r.b) < n {
		r.fail()
		return nil
	}
	p := append([]byte(nil), r.b[:n]...)
	r.b = r.b[n:]
	return p
}

// Bytes reads a length-prefixed byte string.
func (r *Reader) Bytes() []byte {
	n := r.Uvarint()
	if r.err != nil || n > uint64(len(r.b)) {
		r.fail()
		return nil
	}
	return r.Take(int(n))
}

// String reads a length-prefixed UTF-8 string.
func (r *Reader) String() string { return string(r.Bytes()) }

// PeerID reads a peer ID written by AppendPeerID, returning its string form.
func (r *Reader) PeerID() string {
	flag := r.Byte()
	raw := r.Bytes()
	if r.err != nil {
		return ""
	}
	if flag == packedEntry {
		pid, err := peer.IDFromBytes(raw)
		if err != nil {
			r.err = err
			return ""
		}
		return pid.String()
	}
	return string(raw)
}

// Addrs reads a list written by AppendAddrs, returning string multiaddrs.
func (r *Reader) Addrs() []string {
	n := r.Uvarint()
	if r.err != nil {
		return nil
	}
	if n > 64 { // sanity: no legitimate code carries this many
		r.fail()
		return nil
	}
	out := make([]string, 0, n)
	for range n {
		flag := r.Byte()
		raw := r.Bytes()
		if r.err != nil {
			return nil
		}
		if flag == packedEntry {
			ma, err := multiaddr.NewMultiaddrBytes(raw)
			if err != nil {
				r.err = err
				return nil
			}
			out = append(out, ma.String())
		} else {
			out = append(out, string(raw))
		}
	}
	return out
}

// ---- address-list slimming ----
// Both code kinds embed the issuer's rendezvous nodes AND relay circuit
// addresses derived from them ("<bootstrap>/p2p-circuit"). The circuits are
// pure derivation — eliding them at encode time and re-deriving at decode
// saves the largest entries in the code without losing a dialable path.

// ElideCircuits drops addresses that are exactly a carried bootstrap entry
// plus the /p2p-circuit suffix (RestoreCircuits reconstructs them).
func ElideCircuits(addrs, bootstrap []string) []string {
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		base, isCircuit := strings.CutSuffix(a, "/p2p-circuit")
		if isCircuit {
			derivable := false
			for _, b := range bootstrap {
				if strings.TrimSpace(b) == base {
					derivable = true
					break
				}
			}
			if derivable {
				continue
			}
		}
		out = append(out, a)
	}
	return out
}

// RestoreCircuits re-derives the circuit addresses ElideCircuits dropped.
func RestoreCircuits(addrs, bootstrap []string) []string {
	out := append([]string(nil), addrs...)
	for _, b := range bootstrap {
		if b = strings.TrimSpace(b); b != "" {
			out = append(out, b+"/p2p-circuit")
		}
	}
	return DedupeCap(out, len(out))
}

// DedupeCap removes duplicates (keeping first-seen order) and caps the list.
func DedupeCap(addrs []string, max int) []string {
	seen := make(map[string]bool, len(addrs))
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if a == "" || seen[a] {
			continue
		}
		seen[a] = true
		out = append(out, a)
		if len(out) == max {
			break
		}
	}
	return out
}
