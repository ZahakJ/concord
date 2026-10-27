package ble

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ZahakJ/concord/internal/transport"
)

// fakeAir is a room full of fake radios. A radio "sees" every other radio's
// advertisement when the test calls announce; Connect makes a GATT link on
// which writes reach the peripheral's Inbound and the peripheral's Notify
// reaches every subscribed central.
type fakeAir struct {
	mu     sync.Mutex
	radios map[string]*fakeRadio
	// apart lists pairs that are out of range of each other.
	apart map[[2]string]bool
}

func newFakeAir() *fakeAir {
	return &fakeAir{radios: map[string]*fakeRadio{}, apart: map[[2]string]bool{}}
}

func (a *fakeAir) radio(id string) *fakeRadio {
	r := &fakeRadio{air: a, id: id, subs: map[string]bool{}}
	a.mu.Lock()
	a.radios[id] = r
	a.mu.Unlock()
	return r
}

func (a *fakeAir) inRange(x, y string) bool {
	return !a.apart[[2]string{x, y}] && !a.apart[[2]string{y, x}]
}

// announce delivers every up radio's current advertisement to every other up
// radio in range, as one scan pass would.
func (a *fakeAir) announce() {
	a.mu.Lock()
	var rs []*fakeRadio
	for _, r := range a.radios {
		rs = append(rs, r)
	}
	a.mu.Unlock()
	for _, obs := range rs {
		obs.mu.Lock()
		ev, scanning := obs.ev, obs.scanning && obs.up
		obs.mu.Unlock()
		if !scanning {
			continue
		}
		for _, adv := range rs {
			if adv == obs || !a.inRange(obs.id, adv.id) {
				continue
			}
			adv.mu.Lock()
			ad, advertising := adv.ad, adv.ad != nil && adv.up
			adv.mu.Unlock()
			if advertising {
				ev.Seen(adv.id, ad, a.linked(obs.id, adv.id))
			}
		}
	}
}

// linked reports whether x and y share a GATT link in either direction.
func (a *fakeAir) linked(x, y string) bool {
	rx, ry := a.get(x), a.get(y)
	if rx == nil || ry == nil {
		return false
	}
	rx.mu.Lock()
	ok := rx.subs[y]
	rx.mu.Unlock()
	ry.mu.Lock()
	ok = ok || ry.subs[x]
	ry.mu.Unlock()
	return ok
}

func (a *fakeAir) get(id string) *fakeRadio {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.radios[id]
}

type fakeRadio struct {
	air *fakeAir
	id  string

	mu       sync.Mutex
	up       bool
	ev       Events
	ad       []byte
	ads      [][]byte // every advertisement ever set
	scanning bool
	subs     map[string]bool // centrals subscribed to us
	downs    int
	failUp   error
	failConn error
	conns    []*fakeConn
	notified [][]byte
}

func (r *fakeRadio) Probe() transport.Status { return transport.Status{State: transport.Available} }

func (r *fakeRadio) Up(_ context.Context, ev Events) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failUp != nil {
		return r.failUp
	}
	r.up, r.ev = true, ev
	return nil
}

func (r *fakeRadio) Advertise(data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.up {
		return errors.New("not up")
	}
	r.ad = append([]byte(nil), data...)
	r.ads = append(r.ads, r.ad)
	return nil
}

func (r *fakeRadio) Scan(on bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.scanning = on
	return nil
}

func (r *fakeRadio) Notify(frame []byte) error {
	r.mu.Lock()
	r.notified = append(r.notified, append([]byte(nil), frame...))
	var to []string
	for c := range r.subs {
		to = append(to, c)
	}
	r.mu.Unlock()
	for _, c := range to {
		if cr := r.air.get(c); cr != nil {
			cr.mu.Lock()
			ev, up := cr.ev, cr.up
			cr.mu.Unlock()
			if up {
				ev.Notified(r.id, append([]byte(nil), frame...))
			}
		}
	}
	return nil
}

func (r *fakeRadio) Connect(ctx context.Context, id string) (PeerConn, error) {
	r.mu.Lock()
	failConn := r.failConn
	r.mu.Unlock()
	if failConn != nil {
		return nil, failConn
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p := r.air.get(id)
	if p == nil || !r.air.inRange(r.id, id) {
		return nil, fmt.Errorf("no device %s", id)
	}
	p.mu.Lock()
	if !p.up {
		p.mu.Unlock()
		return nil, errors.New("peer down")
	}
	first := len(p.subs) == 0
	p.subs[r.id] = true
	ev := p.ev
	p.mu.Unlock()
	if first {
		ev.Subscribed(true)
	}
	c := &fakeConn{from: r, to: p}
	r.mu.Lock()
	r.conns = append(r.conns, c)
	r.mu.Unlock()
	return c, nil
}

func (r *fakeRadio) Down() error {
	r.mu.Lock()
	r.up, r.ad, r.scanning = false, nil, false
	r.downs++
	r.subs = map[string]bool{}
	r.mu.Unlock()
	return nil
}

func (r *fakeRadio) openConns() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, c := range r.conns {
		if !c.isClosed() {
			n++
		}
	}
	return n
}

type fakeConn struct {
	from, to *fakeRadio
	mu       sync.Mutex
	closed   bool
	failNext error
}

func (c *fakeConn) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *fakeConn) Write(_ context.Context, frame []byte) error {
	c.mu.Lock()
	closed, fail := c.closed, c.failNext
	c.mu.Unlock()
	if closed {
		return errors.New("closed")
	}
	if fail != nil {
		return fail
	}
	c.to.mu.Lock()
	ev, up := c.to.ev, c.to.up
	c.to.mu.Unlock()
	if !up {
		return errors.New("peer gone")
	}
	ev.Inbound(append([]byte(nil), frame...))
	return nil
}

func (c *fakeConn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()
	c.to.mu.Lock()
	delete(c.to.subs, c.from.id)
	empty := len(c.to.subs) == 0
	ev, up := c.to.ev, c.to.up
	c.to.mu.Unlock()
	if up && empty {
		ev.Subscribed(false)
	}
	return nil
}
