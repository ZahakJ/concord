package net

import (
	"context"
	"fmt"
	"sync"

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

// NewPubSub starts a gossipsub instance on the node's libp2p host.
func (n *Host) NewPubSub(ctx context.Context) (*PubSub, error) {
	ps, err := pubsub.NewGossipSub(ctx, n.h)
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
