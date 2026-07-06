package net

import (
	"fmt"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
)

// mdnsNotifee receives peers found on the LAN and asks the host to dial them.
// Auto-dialing on discovery is what makes two Concord instances on the same
// network find and connect to each other with zero configuration.
type mdnsNotifee struct {
	host *Host
}

// HandlePeerFound implements mdns.Notifee.
func (m *mdnsNotifee) HandlePeerFound(pi peer.AddrInfo) {
	if pi.ID == m.host.h.ID() {
		return // never dial ourselves
	}
	// Best-effort dial; discovery re-announces periodically so a transient
	// failure here is retried on the next broadcast.
	go func() {
		_ = m.host.h.Connect(m.host.ctx, pi)
	}()
}

// startMDNS launches libp2p's mDNS service under Concord's service tag.
func (n *Host) startMDNS() error {
	svc := mdns.NewMdnsService(n.h, n.serviceTag, &mdnsNotifee{host: n})
	if err := svc.Start(); err != nil {
		return fmt.Errorf("net: start mDNS: %w", err)
	}
	n.mdns = svc
	return nil
}
