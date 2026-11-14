package main

import (
	"sort"
	"time"
)

// maxLimitKey caps a limiter key. clientIP only produces addresses (at most 45
// bytes for IPv6, plus "/64"), so this never trims a real key; it stops any future
// caller-influenced key from making one table entry arbitrarily large.
const maxLimitKey = 64

func limitKeyOf(k string) string {
	if len(k) > maxLimitKey {
		return k[:maxLimitKey]
	}
	return k
}

// makeRoom keeps a per-address table under max entries without ever refusing a
// new visitor. Once it's full, idle entries go first. If that isn't enough (a
// flood of fresh addresses), the least recently seen tenth goes, so the next
// max/10 arrivals cost nothing and a burst can't turn every insert into a scan.
// Refusing instead would lock out every real visitor for as long as a flood
// keeps the table full.
//
// An evicted address just starts again from a full bucket. That gives it no
// more than a fresh address already gets.
func makeRoom[B any](m map[string]*B, max int, idle time.Duration, now time.Time, last func(*B) time.Time) {
	if len(m) < max {
		return
	}
	for k, b := range m {
		if now.Sub(last(b)) > idle {
			delete(m, k)
		}
	}
	if len(m) < max {
		return
	}
	times := make([]time.Time, 0, len(m))
	for _, b := range m {
		times = append(times, last(b))
	}
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	cut := times[len(times)/10]
	for k, b := range m {
		if !last(b).After(cut) {
			delete(m, k)
		}
	}
}
