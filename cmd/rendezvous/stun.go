package main

// This node's own STUN responder.
//
// A call needs each side to learn its external address, or two people behind
// home routers have nothing to offer each other but addresses the other cannot
// reach. Without this responder that question goes to a public STUN server run
// by a third party, which thereby learns the IP of everyone starting a call.
// With it the app asks here and nobody else: the rendezvous already knows the
// caller's address (the app is connected to it), so answering "what is my
// address" tells nobody anything new.
//
// It answers STUN Binding requests over UDP and nothing else: no allocation, no
// relaying, no state per client. Off unless CONCORD_STUN_PORT is set, because it
// is one more open port and the host has to deliver UDP to it. It does not need
// the TURN relay. CONCORD_STUN_LISTEN names the address to bind when "all
// interfaces" is wrong for the host (fly.io delivers UDP only to
// fly-global-services).

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"

	"github.com/pion/stun/v3"
)

// Per-source budget. A Binding answer is a little larger than its request (32
// or 44 bytes for 20), so an attacker forging a victim's address gets a small
// multiple out of this port; the budget caps what one forged address can be
// sent. A real client asks a handful of times per call.
const (
	stunRate  = 2.0
	stunBurst = 20.0
)

// serveSTUN starts the responder on the given UDP port (on addr, or on all
// interfaces when addr is empty) and returns that port, or "" when it is off or
// could not start (then /turn does not list it).
func serveSTUN(ctx context.Context, addr, port string) string {
	if port == "" {
		return ""
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		fmt.Fprintf(os.Stderr, "stun: CONCORD_STUN_PORT=%q is not a port; responder off\n", port)
		return ""
	}
	conn, err := net.ListenPacket("udp", net.JoinHostPort(addr, port))
	if err != nil {
		fmt.Fprintln(os.Stderr, "stun: listen:", err)
		return ""
	}
	fmt.Println("STUN responder listening on " + net.JoinHostPort(addr, port) + " (udp)")
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()
	go answerSTUN(conn)
	return port
}

// answerSTUN replies to Binding requests on conn until it is closed.
func answerSTUN(conn net.PacketConn) {
	var limit ipLimiter
	buf := make([]byte, 1500)
	for {
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			return
		}
		udp, ok := from.(*net.UDPAddr)
		if !ok {
			continue
		}
		reply := stunReply(buf[:n], udp)
		if reply == nil || !limit.allow(udp.IP.String(), stunRate, stunBurst) {
			continue
		}
		_, _ = conn.WriteTo(reply, from)
	}
}

// stunReply builds the answer to one datagram: the sender's own address, for a
// well-formed Binding request, and nil (stay silent) for anything else.
func stunReply(pkt []byte, from *net.UDPAddr) []byte {
	if !stun.IsMessage(pkt) {
		return nil
	}
	req := &stun.Message{Raw: append([]byte(nil), pkt...)}
	if err := req.Decode(); err != nil || req.Type != stun.BindingRequest {
		return nil
	}
	ip := from.IP
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	res, err := stun.Build(
		stun.NewTransactionIDSetter(req.TransactionID),
		stun.BindingSuccess,
		&stun.XORMappedAddress{IP: ip, Port: from.Port},
	)
	if err != nil {
		return nil
	}
	return res.Raw
}
