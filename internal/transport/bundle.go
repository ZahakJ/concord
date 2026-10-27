package transport

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sync"
	"time"
)

// A Bundle is one ciphertext on its way somewhere, plus the least header that
// lets a stranger's phone carry it and the right recipient recognise it.
//
// Wire form, 12 bytes of header:
//
//	[0]    0xC1           bundle, format 1
//	[1]    hops           how many more times it may be relayed
//	[2:4]  day            days since 1970-01-01, big-endian
//	[4:12] tag            HMAC-SHA256(topic, "concord/tag/v1"‖day)[:8]
//	[12:]  payload        the MLS ciphertext, untouched
//
// What it deliberately does not carry: the sender, the recipient, the topic,
// the guild. A relay learns that some ciphertext exists, roughly when it was
// made, and that it is one of several from the same channel today — the tag
// is stable for one day so a member can look it up, and rotates at midnight
// UTC so a channel cannot be followed across days by anyone outside it. The
// topic it is keyed with is itself a hash of the MLS group ID, which only
// members hold.
//
// Integrity is not this header's job. The payload is an MLS message, which
// the recipient authenticates before it believes a byte of it; a header
// someone tampered with can only misroute a ciphertext into a group that will
// reject it, or spend a hop.
type Bundle struct {
	Hops    uint8
	Day     uint16
	Tag     [8]byte
	Payload []byte
}

const (
	kindBundle   = 0xC1
	kindFragment = 0xC2

	bundleHeader   = 12
	fragmentHeader = 7 // kind, 4-byte set id, index, count
	maxFragments   = 255

	// MaxAgeDays is how long a bundle stays deliverable. A week covers a QR
	// sheet carried across a border or a phone that spent days out of range;
	// past that, the MLS epoch has very likely moved on anyway.
	MaxAgeDays = 7
	// DefaultHops is how far a bundle may travel through other people's
	// phones. Three is a building or a campsite, not a city.
	DefaultHops = 3
)

var (
	ErrTooLarge  = errors.New("transport: bundle too large for this link")
	ErrMalformed = errors.New("transport: malformed frame")
)

// Today is the bundle day number for t.
func Today(t time.Time) uint16 { return uint16(t.UTC().Unix() / 86400) }

// TopicTag is the tag a bundle for topic carries on day.
func TopicTag(topic string, day uint16) [8]byte {
	m := hmac.New(sha256.New, []byte(topic))
	m.Write([]byte("concord/tag/v1"))
	var d [2]byte
	binary.BigEndian.PutUint16(d[:], day)
	m.Write(d[:])
	var tag [8]byte
	copy(tag[:], m.Sum(nil))
	return tag
}

// ID is the deduplication key for a payload: the same ciphertext arriving by
// gossipsub, Bluetooth and a QR code is one message. Hops and day are left out
// on purpose — a relay decrementing hops must not make a copy look new.
func ID(payload []byte) [16]byte {
	sum := sha256.Sum256(payload)
	var id [16]byte
	copy(id[:], sum[:])
	return id
}

// Marshal is the bundle's unfragmented wire form.
func (b Bundle) Marshal() []byte {
	out := make([]byte, bundleHeader+len(b.Payload))
	out[0] = kindBundle
	out[1] = b.Hops
	binary.BigEndian.PutUint16(out[2:4], b.Day)
	copy(out[4:12], b.Tag[:])
	copy(out[12:], b.Payload)
	return out
}

// Unmarshal parses an unfragmented bundle.
func Unmarshal(frame []byte) (Bundle, error) {
	if len(frame) <= bundleHeader || frame[0] != kindBundle {
		return Bundle{}, ErrMalformed
	}
	b := Bundle{Hops: frame[1], Day: binary.BigEndian.Uint16(frame[2:4])}
	copy(b.Tag[:], frame[4:12])
	b.Payload = append([]byte(nil), frame[12:]...)
	return b, nil
}

// Fresh reports whether a bundle is still inside its delivery window on day
// today. A day of clock skew forward is tolerated; a phone with a wrong clock
// should not silently drop everything.
func (b Bundle) Fresh(today uint16) bool {
	return int(b.Day) <= int(today)+1 && int(today)-int(b.Day) <= MaxAgeDays
}

// Frames splits a bundle into frames of at most mtu bytes. A bundle that fits
// goes as one frame with no fragment header at all — the common case on BLE
// and a LAN costs nothing.
func Frames(b Bundle, mtu int) ([][]byte, error) {
	whole := b.Marshal()
	if len(whole) <= mtu {
		return [][]byte{whole}, nil
	}
	chunk := mtu - fragmentHeader
	if chunk <= 0 {
		return nil, ErrTooLarge
	}
	count := (len(whole) + chunk - 1) / chunk
	if count > maxFragments {
		return nil, ErrTooLarge
	}
	var set [4]byte
	_, _ = rand.Read(set[:])
	frames := make([][]byte, 0, count)
	for i := 0; i < count; i++ {
		lo, hi := i*chunk, min((i+1)*chunk, len(whole))
		f := make([]byte, fragmentHeader+hi-lo)
		f[0] = kindFragment
		copy(f[1:5], set[:])
		f[5] = byte(i)
		f[6] = byte(count)
		copy(f[7:], whole[lo:hi])
		frames = append(frames, f)
	}
	return frames, nil
}

// FrameCount is how many frames a payload of n bytes needs at mtu, or 0 if it
// cannot be sent there at all.
func FrameCount(n, mtu int) int {
	whole := bundleHeader + n
	if whole <= mtu {
		return 1
	}
	chunk := mtu - fragmentHeader
	if chunk <= 0 {
		return 0
	}
	c := (whole + chunk - 1) / chunk
	if c > maxFragments {
		return 0
	}
	return c
}

// Reassembler puts fragments back together. It is bounded in both directions
// a hostile radio neighbour could push it: the number of half-finished sets,
// and how long one may sit waiting for a fragment that never comes.
type Reassembler struct {
	mu   sync.Mutex
	sets map[[4]byte]*partial
	ttl  time.Duration
	max  int
	now  func() time.Time
}

type partial struct {
	parts [][]byte
	have  int
	first time.Time
}

// NewReassembler holds at most max unfinished sets, each for at most ttl.
func NewReassembler(max int, ttl time.Duration) *Reassembler {
	return &Reassembler{sets: map[[4]byte]*partial{}, ttl: ttl, max: max, now: time.Now}
}

// Add takes one frame and returns a complete bundle frame when it has one: the
// frame itself if it was never fragmented, the joined bytes when the last
// fragment of a set arrives, or nil while a set is still incomplete.
func (r *Reassembler) Add(frame []byte) ([]byte, error) {
	if len(frame) == 0 {
		return nil, ErrMalformed
	}
	switch frame[0] {
	case kindBundle:
		return frame, nil
	case kindFragment:
	default:
		return nil, ErrMalformed
	}
	if len(frame) <= fragmentHeader {
		return nil, ErrMalformed
	}
	var set [4]byte
	copy(set[:], frame[1:5])
	idx, count := int(frame[5]), int(frame[6])
	if count < 2 || idx >= count {
		return nil, ErrMalformed
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	for k, p := range r.sets {
		if now.Sub(p.first) > r.ttl {
			delete(r.sets, k)
		}
	}
	p := r.sets[set]
	if p == nil {
		if len(r.sets) >= r.max {
			r.evictOldest()
		}
		p = &partial{parts: make([][]byte, count), first: now}
		r.sets[set] = p
	}
	if len(p.parts) != count {
		return nil, ErrMalformed // same set id, different shape: not ours to trust
	}
	if p.parts[idx] == nil {
		p.parts[idx] = append([]byte(nil), frame[fragmentHeader:]...)
		p.have++
	}
	if p.have < count {
		return nil, nil
	}
	delete(r.sets, set)
	var whole []byte
	for _, part := range p.parts {
		whole = append(whole, part...)
	}
	return whole, nil
}

func (r *Reassembler) evictOldest() {
	var oldest [4]byte
	var at time.Time
	for k, p := range r.sets {
		if at.IsZero() || p.first.Before(at) {
			oldest, at = k, p.first
		}
	}
	delete(r.sets, oldest)
}
