package net

import (
	"context"
	"fmt"
	"sync"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
)

// PubSub is Concord's group message bus, built on libp2p gossipsub. Each
// Concord channel maps to one gossipsub topic; peers publish MLS ciphertext to
// it and gossipsub fans it out across the mesh. Because payloads are already
// end-to-end encrypted by the MLS layer before they reach here, the pubsub
// network only ever moves ciphertext.
type PubSub struct {
	ps   *pubsub.PubSub
	self peer.ID

	mu     sync.Mutex
	topics map[string]*pubsub.Topic
	subs   map[string]*pubsub.Subscription
}

// TopicHandler receives a decrypted-elsewhere payload published to a topic,
// along with the peer that authored it.
type TopicHandler func(from peer.ID, data []byte)

// gossipsubParams returns the router configuration for this platform, and
// whether it differs from the library's.
//
// Desktop runs stock gossipsub — D=6 mesh per topic, a 1s heartbeat, Dlazy=6
// gossip — and that is a decision, not an omission: the defaults are what the
// protocol was measured against, and a machine on mains power has no reason to
// trade any of it away.
//
// A phone does. Concord meshes a topic per guild control channel, a topic per
// guild's metadata, and TWO per channel (messages and typing), so a member of
// one twenty-channel guild is holding forty-two permanently-meshed topics. Each
// of them wakes on every heartbeat to maintain its mesh and emit gossip, and on
// a phone the cost of that is not CPU, it is the radio: a steady dribble of
// small packets is the one traffic shape that keeps a cellular modem out of its
// low-power state and a Wi-Fi chip out of power-save.
//
// Two changes, both off the message-delivery path:
//
//   - HeartbeatInterval 1s → 2s. The heartbeat drives mesh maintenance, gossip
//     emission and the message cache's shift; it does NOT drive forwarding,
//     which happens the instant a message arrives. So delivery latency for a
//     message riding the mesh is unchanged, and what halves is the idle floor.
//     The cost is that repairing a mesh whose peer just vanished can take up to
//     2s instead of 1s. Longer was considered and declined: mesh repair IS on
//     the reliability path, and 3s+ starts trading real recovery time for a
//     saving that is already mostly banked at 2s.
//
//   - Dlazy 6 → 3. Dlazy is how many peers OUTSIDE the mesh get an IHAVE each
//     heartbeat. An IHAVE carries no message, only an offer to resend one, and
//     every peer in the mesh already received the message itself. Halving it,
//     on top of the doubled heartbeat, cuts idle gossip per topic to a quarter.
//     What it costs is recovery breadth in a partitioned mesh — fewer peers
//     hear that we have something they missed — and in a Concord guild, where
//     the whole membership usually fits inside D, the mesh is complete and
//     gossip is redundant anyway.
//
// GossipFactor is deliberately left at 0.25: it only overrides Dlazy above
// twenty-four non-mesh peers on a single topic, and the mobile connection
// manager caps the whole node at forty-eight connections, so changing it would
// be a number that never applies.
//
// None of this is negotiated. GossipSubParams is node-local — the protocol IDs
// the router speaks come from GossipSubDefaultProtocols and are untouched — so
// a phone and a desktop on these two configurations interoperate exactly as two
// desktops do.
func gossipsubParams(mobile bool) (pubsub.GossipSubParams, bool) {
	p := pubsub.DefaultGossipSubParams()
	if !mobile {
		return p, false
	}
	p.HeartbeatInterval = 2 * time.Second
	p.Dlazy = 3
	return p, true
}

// NewPubSub starts a gossipsub instance on the node's libp2p host.
func (n *Host) NewPubSub(ctx context.Context) (*PubSub, error) {
	var opts []pubsub.Option
	if params, tuned := gossipsubParams(onMobile); tuned {
		opts = append(opts, pubsub.WithGossipSubParams(params))
	}
	ps, err := pubsub.NewGossipSub(ctx, n.h, opts...)
	if err != nil {
		return nil, fmt.Errorf("net: start gossipsub: %w", err)
	}
	return &PubSub{
		ps:     ps,
		self:   n.h.ID(),
		topics: map[string]*pubsub.Topic{},
		subs:   map[string]*pubsub.Subscription{},
	}, nil
}

// Subscribe joins a topic (if not already joined) and delivers every message
// from *other* peers to handler until ctx is cancelled. Messages this node
// published itself are filtered out, since the sender already has them.
func (p *PubSub) Subscribe(ctx context.Context, topic string, handler TopicHandler) error {
	t, err := p.join(topic)
	if err != nil {
		return err
	}

	p.mu.Lock()
	if _, exists := p.subs[topic]; exists {
		p.mu.Unlock()
		return nil // already subscribed
	}
	sub, err := t.Subscribe()
	if err != nil {
		p.mu.Unlock()
		return fmt.Errorf("net: subscribe %s: %w", topic, err)
	}
	p.subs[topic] = sub
	p.mu.Unlock()

	go func() {
		for {
			msg, err := sub.Next(ctx)
			if err != nil {
				return // ctx cancelled or subscription cancelled
			}
			if msg.GetFrom() == p.self {
				continue
			}
			handler(msg.GetFrom(), msg.Data)
		}
	}()
	return nil
}

// Subscribed reports whether this node currently holds a subscription to a
// topic — i.e. whether it is meshed and listening, as opposed to merely having
// a Topic handle cached.
func (p *PubSub) Subscribed(topic string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.subs[topic]
	return ok
}

// Unsubscribe stops delivery for a topic: the subscription is cancelled and the
// reader goroutine exits on its next Next.
//
// Cancelling the last subscription is also the mesh LEAVE. gossipsub announces
// our disinterest to every peer and tears down the mesh for that topic, which
// is what makes this the right lever for a topic we want to stop paying for
// (see the typing topics, dropped while the app is off screen) rather than
// merely stop reading. The Topic handle stays cached on purpose — gossipsub
// refuses a second Join of the same name, so dropping the handle would
// complicate a later re-Subscribe; an idle cached handle is not in any mesh and
// costs nothing. Unsubscribing an unknown topic is a no-op.
func (p *PubSub) Unsubscribe(topic string) {
	p.mu.Lock()
	sub := p.subs[topic]
	delete(p.subs, topic)
	p.mu.Unlock()
	if sub != nil {
		sub.Cancel()
	}
}

// Publish sends data to a topic, joining it if necessary.
func (p *PubSub) Publish(ctx context.Context, topic string, data []byte) error {
	t, err := p.join(topic)
	if err != nil {
		return err
	}
	if err := t.Publish(ctx, data); err != nil {
		return fmt.Errorf("net: publish %s: %w", topic, err)
	}
	return nil
}

func (p *PubSub) join(topic string) (*pubsub.Topic, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if t, ok := p.topics[topic]; ok {
		return t, nil
	}
	t, err := p.ps.Join(topic)
	if err != nil {
		return nil, fmt.Errorf("net: join topic %s: %w", topic, err)
	}
	p.topics[topic] = t
	return t, nil
}
