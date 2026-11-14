package app

import (
	"context"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// How a profile edit reaches the account's other devices.
//
// Not over gossip: a profile announce from our own account is deliberately
// ignored by our own devices (learnProfile never binds our own fingerprint),
// so the guild topics carry it to members only. The device hello is the lane,
// and it used to be ridden once: an edit re-greeted every connected device a
// single time, and a hello that failed — the stream on a connection one side
// had already dropped, the device not placed yet — was simply lost until the
// next reconnect. A profile too big for the hello went as an 8-byte hint the
// device then fetched, and that fetch was allowed once per connection, so the
// second large edit on the same connection was never fetched at all. And two
// hellos landing together raced inside AdoptLinkedProfile, whose stamp check
// and writes were not one step: an older copy could write its fields (games
// included) after a newer one had passed the check, and leave the device with
// the newer stamp over the older content for good.
//
// So, per connected device of the account, this keeps two numbers: the newest
// edit stamp we know it holds (told), from what we delivered and from what it
// says about itself, and the newest stamp it says it holds that we do not
// (heard). An edit tells every device behind on it, retried; a hint newer than
// ours is fetched, retried, and asked for again by the next reconcile beat if
// it still failed. Both are compared on the own-device beat (keepDevicesClose),
// which sends nothing at all while every device is current.

type ownProfileSync struct {
	mu       sync.Mutex
	told     map[peer.ID]int64 // newest stamp this device is known to hold
	heard    map[peer.ID]int64 // newest stamp it offered us
	sending  map[peer.ID]bool
	fetching map[peer.ID]bool
	pending  map[peer.ID]bool      // a fetch is waiting out the gap
	asked    map[peer.ID]int64     // newest hint already fetched from it
	lastAsk  map[peer.ID]time.Time // when we last fetched from it
}

func (o *ownProfileSync) init() {
	if o.told == nil {
		o.told, o.heard = map[peer.ID]int64{}, map[peer.ID]int64{}
		o.sending, o.fetching, o.pending = map[peer.ID]bool{}, map[peer.ID]bool{}, map[peer.ID]bool{}
		o.asked, o.lastAsk = map[peer.ID]int64{}, map[peer.ID]time.Time{}
	}
}

// forget drops a peer's state when its connection ends; the next connection's
// hello establishes it again.
func (o *ownProfileSync) forget(p peer.ID) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.init()
	delete(o.told, p)
	delete(o.heard, p)
	delete(o.asked, p)
}

// holds records that p has at least stamp at.
func (o *ownProfileSync) holds(p peer.ID, at int64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.init()
	if at > o.told[p] {
		o.told[p] = at
	}
}

// offered records a hint p gave us.
func (o *ownProfileSync) offered(p peer.ID, at int64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.init()
	if at > o.heard[p] {
		o.heard[p] = at
	}
	if at > o.told[p] {
		o.told[p] = at // a device offering at has at
	}
}

// profileTellTries bounds one push: 1 s and 2 s apart. Past that the device
// is behind on the next reconcile beat and is told again then.
const profileTellTries = 3

// tellOwnDevices sends our profile stamp (and the profile, when it fits a
// hello) to every connected device of the account that is not known to have
// it. Cheap when nothing changed: a map lookup per device.
func (s *Service) tellOwnDevices() {
	at := s.profileStamp()
	if at <= 0 || s.host == nil {
		return
	}
	for _, p := range s.host.Peers() {
		if !s.ownDevice(p) || s.revocationFor(p) != nil {
			continue
		}
		s.ownProfile.mu.Lock()
		s.ownProfile.init()
		behind := s.ownProfile.told[p] < at && !s.ownProfile.sending[p]
		if behind {
			s.ownProfile.sending[p] = true
		}
		s.ownProfile.mu.Unlock()
		if behind {
			go s.sayProfile(p)
		}
	}
}

// sayProfile delivers our profile to one device of the account over a hello
// carrying only our credential, the profile when it fits, and the stamp. The
// receiver adopts the inline copy or fetches the record on the stamp.
func (s *Service) sayProfile(p peer.ID) {
	defer func() {
		s.ownProfile.mu.Lock()
		s.ownProfile.sending[p] = false
		s.ownProfile.mu.Unlock()
	}()
	for attempt, wait := 0, time.Second; attempt < profileTellTries; attempt, wait = attempt+1, wait*2 {
		if attempt > 0 {
			select {
			case <-s.ctx.Done():
				return
			case <-time.After(wait):
			}
			if !s.ownDevice(p) {
				return
			}
		}
		at := s.profileStamp()
		req, err := marshalHello(helloFrame{Credential: s.myCredential, Profile: s.profileForDevice(p), ProfileAt: at})
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(s.ctx, helloTimeout)
		resp, err := s.sayProfileHello(ctx, p, req)
		cancel()
		if err == nil {
			s.ownProfile.holds(p, at)
			s.ingestHello(p, resp)
			return
		}
	}
}

// reconcileOwnProfiles is the own-device beat's profile check: tell devices
// behind us, and fetch from devices ahead of us whose earlier fetch failed.
func (s *Service) reconcileOwnProfiles() {
	s.tellOwnDevices()
	mine := s.profileStamp()
	s.ownProfile.mu.Lock()
	s.ownProfile.init()
	var ahead []peer.ID
	var hints []int64
	for p, at := range s.ownProfile.heard {
		if at > mine {
			ahead = append(ahead, p)
			hints = append(hints, at)
		}
	}
	s.ownProfile.mu.Unlock()
	for i, p := range ahead {
		go s.fetchOwnProfile(p, hints[i])
	}
}

// sayProfileHello is SayHello for the profile push; a test may stand in for
// it to lose a delivery at a chosen moment.
func (s *Service) sayProfileHello(ctx context.Context, p peer.ID, req []byte) ([]byte, error) {
	if f := s.testSayProfile; f != nil {
		return f(ctx, p, req)
	}
	return s.host.SayHello(ctx, p, req)
}
