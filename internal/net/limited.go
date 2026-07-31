package net

import (
	"context"

	"github.com/libp2p/go-libp2p/core/network"
)

// relayCtx permits a stream to be opened over a LIMITED connection.
//
// A circuit-relay connection is "limited" in libp2p, and host.NewStream REFUSES
// to open a stream on one unless the context says otherwise — it returns
// network.ErrLimitedConn. The reasoning upstream is that a relay is a scarce,
// someone-else's-bandwidth resource that should carry a hole-punch and then get
// out of the way, not become a general transport.
//
// For Concord that default is wrong, and silently so. Two people behind two home
// routers — or a phone on mobile data and a desktop at home — frequently have NO
// direct path at all; the relay is the only way they can talk. Without this the
// connection comes up, both sides show ONLINE (presence is connection-level and
// needs no stream), and then every single protocol fails: history sync, the
// device hello, DM invites, attachments, voice signalling. The app looks
// connected and does nothing, which is exactly the bug this was found chasing.
//
// The relay's own resource limits still apply — the rendezvous caps reservations,
// circuits, duration and bytes (see cmd/rendezvous), so allowing this cannot turn
// a relay into unmetered transit. Hole punching still runs and still upgrades the
// connection to a direct one when it can; this only stops us refusing to talk in
// the meantime.
func relayCtx(ctx context.Context, reason string) context.Context {
	return network.WithAllowLimitedConn(ctx, reason)
}

// A NOTE ON GOSSIPSUB, because this is where the obvious next fix leads and it is
// a dead end: pubsub opens its own streams with a bare context, so the instinct is
// to hand it a host that adds the option above. That does not work, and shipping it
// would be a comment claiming something untrue.
//
// go-libp2p-pubsub gates mesh membership on Connectedness == network.Connected
// (gossipsub.go:735, 745, 1292), and a relayed peer reports network.Limited, which
// is a DIFFERENT state. Gossipsub therefore never grafts a relay-only peer, whatever
// its streams are permitted to do, and there is no option to change that.
//
// So message delivery between two peers with no direct path depends on the relayed
// connection being UPGRADED to a direct one by hole punching (libp2p.EnableHolePunching,
// see New) — the relay is meant to carry the handshake and then get out of the way —
// or, failing that, on the store-and-forward mailbox. Verified by test: streams work
// over a relay once permitted; gossipsub does not deliver over one at all.
