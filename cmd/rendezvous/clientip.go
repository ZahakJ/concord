package main

import (
	"net"
	"net/http"
	"os"
)

// behindFly: fly.io sets FLY_APP_NAME in every machine it runs. Only then is
// Fly-Client-IP trustworthy — fly's HTTP edge writes it, replacing any copy the
// caller sent. Anywhere else the header is just something the caller typed.
var behindFly = os.Getenv("FLY_APP_NAME") != ""

// clientIP is the address the per-address limits (booking, guest connects,
// TURN credentials) are keyed on. It must be one the caller cannot choose:
//
//   - behind fly: Fly-Client-IP, set by the edge;
//   - otherwise: the socket peer.
//
// X-Forwarded-For is never read. Its leftmost entry is whatever the caller put
// there, and fly appends to it rather than replacing it, so trusting it let one
// caller mint a fresh limit bucket per request.
//
// An IPv6 address is cut to its /64: one subscriber is routinely handed a whole
// /64, so keying on the full address would hand them 2^64 buckets.
func clientIP(r *http.Request) string {
	if behindFly {
		// Trust boundary: fly's edge overwrites this header. A peer on the same fly
		// org's private network (6PN) that reaches :8080 directly skips the edge and
		// can set it; the edge also arrives over 6PN, so the socket can't tell them apart.
		if ip := net.ParseIP(r.Header.Get("Fly-Client-IP")); ip != nil {
			return limitKey(ip)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil {
		return limitKey(ip)
	}
	return host
}

func limitKey(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}
