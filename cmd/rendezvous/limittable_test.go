package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A table full of recent entries must still admit a new visitor.
func TestFullLimiterTablesAdmitNewVisitors(t *testing.T) {
	var g ipLimiter
	var b bookLimiter
	for i := 0; i < maxBookBuckets; i++ {
		ip := fmt.Sprintf("10.%d.%d.%d", i>>16, (i>>8)&255, i&255)
		g.allow(ip, 1, 5)
		b.allow(ip, "slots")
	}
	if !g.allow("203.0.113.7", 1, 5) || !b.allow("203.0.113.7", "slots") {
		t.Fatal("full table refused a new visitor")
	}
	if len(g.buckets) > maxBookBuckets || len(b.buckets) > maxBookBuckets {
		t.Fatalf("table grew past its cap: %d, %d", len(g.buckets), len(b.buckets))
	}
}

func TestMakeRoomDropsIdleThenOldest(t *testing.T) {
	now := time.Now()
	m := map[string]*ipBucket{}
	for i := 0; i < 100; i++ {
		m[fmt.Sprint(i)] = &ipBucket{last: now.Add(time.Duration(i) * time.Second)}
	}
	makeRoom(m, 100, time.Hour, now.Add(200*time.Second), func(b *ipBucket) time.Time { return b.last })
	if len(m) >= 100 || len(m) < 85 {
		t.Fatalf("left %d entries", len(m))
	}
	if m["0"] != nil || m["99"] == nil {
		t.Fatal("evicted the wrong end")
	}
	m["old"] = &ipBucket{last: now.Add(-2 * time.Hour)}
	for len(m) < 100 {
		m[fmt.Sprint("x", len(m))] = &ipBucket{last: now.Add(time.Hour)}
	}
	makeRoom(m, 100, time.Hour, now.Add(200*time.Second), func(b *ipBucket) time.Time { return b.last })
	if m["old"] != nil || len(m) != 99 {
		t.Fatalf("idle pass: old present=%v, len %d (want only the idle one gone)", m["old"] != nil, len(m))
	}
}

func TestLimiterKeyIsCapped(t *testing.T) {
	var g ipLimiter
	g.allow(strings.Repeat("a", 10000), 1, 5)
	for k := range g.buckets {
		if len(k) > maxLimitKey {
			t.Fatalf("key of %d bytes stored", len(k))
		}
	}
}

func TestBodiesCappedPerRoute(t *testing.T) {
	read := func(path string, n int) error {
		var err error
		h := capBodies(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, err = io.ReadAll(r.Body)
		}))
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", path, strings.NewReader(strings.Repeat("x", n))))
		return err
	}
	if err := read("/book/api", maxBookingBody); err != nil {
		t.Fatalf("booking body at its cap refused: %v", err)
	}
	if read("/book/api", maxBookingBody+1) == nil {
		t.Fatal("booking body over its cap accepted")
	}
	for _, p := range []string{"/guest", "/guest/ws", "/turn", "/healthz", "/book/x", "/nope"} {
		if read(p, 1) == nil {
			t.Fatalf("%s accepted a body", p)
		}
		if err := read(p, 0); err != nil {
			t.Fatalf("%s refused an empty body: %v", p, err)
		}
	}
}

func TestGuestServerTimeouts(t *testing.T) {
	s := guestServer(":0", http.NewServeMux())
	if s.ReadHeaderTimeout != 10*time.Second || s.ReadTimeout != 30*time.Second ||
		s.IdleTimeout != 60*time.Second || s.MaxHeaderBytes != 16<<10 {
		t.Fatalf("server limits: %+v", s)
	}
}
