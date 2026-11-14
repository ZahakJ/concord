package main

import (
	"fmt"
	"net/http/httptest"
	"testing"
)

func TestClientIPIgnoresForwardedFor(t *testing.T) {
	for _, fly := range []bool{false, true} {
		old := behindFly
		behindFly = fly
		r := httptest.NewRequest("POST", "/book", nil)
		r.RemoteAddr = "203.0.113.7:5555"
		if fly {
			r.Header.Set("Fly-Client-IP", "198.51.100.9")
		}
		want := clientIP(r)
		seen := map[string]bool{}
		for _, spoof := range []string{"1.2.3.4", "5.6.7.8, 9.9.9.9", "10.0.0.1", "garbage", ""} {
			r.Header.Set("X-Forwarded-For", spoof)
			got := clientIP(r)
			seen[got] = true
			if got != want {
				t.Errorf("fly=%v XFF %q moved the key: %q, want %q", fly, spoof, got, want)
			}
		}
		if len(seen) != 1 {
			t.Errorf("fly=%v: spoofed XFF produced %d buckets", fly, len(seen))
		}
		behindFly = old
	}
}

func TestClientIPSources(t *testing.T) {
	old := behindFly
	defer func() { behindFly = old }()
	cases := []struct {
		fly         bool
		remote, hdr string
		want        string
	}{
		{false, "203.0.113.7:5555", "198.51.100.9", "203.0.113.7"}, // not on fly: header is caller-typed
		{true, "172.16.0.2:5555", "198.51.100.9", "198.51.100.9"},  // on fly: the edge's header
		{true, "172.16.0.2:5555", "not-an-ip", "172.16.0.2"},       // malformed: socket peer
		{true, "172.16.0.2:5555", "", "172.16.0.2"},                // missing: socket peer
		{false, "[2001:db8:1:2:aaaa::1]:5555", "", "2001:db8:1:2::/64"},
		{true, "172.16.0.2:5555", "2001:db8:1:2:ffff::9", "2001:db8:1:2::/64"},
		{false, "[::ffff:203.0.113.7]:5555", "", "203.0.113.7"},
	}
	for _, c := range cases {
		behindFly = c.fly
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remote
		if c.hdr != "" {
			r.Header.Set("Fly-Client-IP", c.hdr)
		}
		if got := clientIP(r); got != c.want {
			t.Errorf("fly=%v remote=%s hdr=%q: got %q, want %q", c.fly, c.remote, c.hdr, got, c.want)
		}
	}
}

// 500 bookings, each with a different invented X-Forwarded-For, from one real address: only the burst gets through.
func TestBookingLimitNotLiftedBySpoofedXFF(t *testing.T) {
	old := behindFly
	behindFly = true
	defer func() { behindFly = old }()
	l := &bookLimiter{}
	allowed := 0
	for i := 0; i < 500; i++ {
		r := httptest.NewRequest("POST", "/", nil)
		r.RemoteAddr = "172.16.0.2:1"
		r.Header.Set("Fly-Client-IP", "198.51.100.9")
		r.Header.Set("X-Forwarded-For", fmt.Sprintf("10.9.%d.%d", i/250, i%250))
		if l.allow(clientIP(r), "book") {
			allowed++
		}
	}
	if allowed > int(bookBookBurst) {
		t.Fatalf("spoofed XFF let %d bookings through", allowed)
	}
}
