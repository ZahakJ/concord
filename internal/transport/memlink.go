package transport

import (
	"context"
	"errors"
	"sync"
)

// Air is a simulated shared medium: every frame one MemLink sends reaches
// every other MemLink on the same Air, as on a radio channel. Tests use it,
// and the hardware simulators (Meshtastic, Iridium) build on it so the stack
// above them runs the same code against simulated air as against real.
type Air struct {
	mu    sync.Mutex
	links map[*MemLink]bool
	// Drop, when set, decides per frame whether the air eats it.
	Drop func(frame []byte) bool
}

// NewAir returns an empty medium.
func NewAir() *Air { return &Air{links: map[*MemLink]bool{}} }

// MemLink is a Transport on an Air.
type MemLink struct {
	name, label string
	caps        Caps
	air         *Air

	mu      sync.Mutex
	deliver Deliver
}

// NewMemLink returns a link on air with the given identity and capabilities.
func NewMemLink(air *Air, name, label string, caps Caps) *MemLink {
	return &MemLink{name: name, label: label, caps: caps, air: air}
}

func (l *MemLink) Name() string  { return l.name }
func (l *MemLink) Label() string { return l.label }
func (l *MemLink) Caps() Caps    { return l.caps }
func (l *MemLink) Probe() Status { return Status{State: Available, Detail: "simulated"} }

func (l *MemLink) Start(_ context.Context, d Deliver) error {
	l.mu.Lock()
	l.deliver = d
	l.mu.Unlock()
	l.air.mu.Lock()
	l.air.links[l] = true
	l.air.mu.Unlock()
	return nil
}

func (l *MemLink) Stop() error {
	l.air.mu.Lock()
	delete(l.air.links, l)
	l.air.mu.Unlock()
	return nil
}

var errFrameTooBig = errors.New("transport: frame exceeds MTU")

func (l *MemLink) Send(_ context.Context, frame []byte) error {
	if len(frame) > l.caps.MTU {
		return errFrameTooBig
	}
	l.air.mu.Lock()
	if l.air.Drop != nil && l.air.Drop(frame) {
		l.air.mu.Unlock()
		return nil
	}
	var to []*MemLink
	for o := range l.air.links {
		if o != l {
			to = append(to, o)
		}
	}
	l.air.mu.Unlock()
	cp := append([]byte(nil), frame...)
	for _, o := range to {
		o.mu.Lock()
		d := o.deliver
		o.mu.Unlock()
		if d != nil {
			d(cp)
		}
	}
	return nil
}
