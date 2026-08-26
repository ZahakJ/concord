package mls

import (
	"context"
	"testing"

	"github.com/ZahakJ/concord/internal/identity"
)

// These benchmarks exist to answer one question that decides a lot of design
// above this package: what does it cost to ASK the engine something about a
// group of a realistic size?
//
//	go test ./internal/crypto/mls/ -run XXX -bench . -benchtime 200x
//
// The answer on a fast desktop, for a fifty-one member group, is that reading
// the epoch number takes 404µs and listing the members 370µs. Both are the same
// cost — the upstream client is configured to reload and unmarshal the whole
// group state on every operation (CacheNone), so the work is the deserialize,
// not the question. Encrypting one message costs 592µs for the same reason.
//
// With the upstream client's CacheAlways strategy those become 81ns, 6.8µs and
// 183µs — a five-thousand-fold difference on the cheapest question. That change
// is NOT made here, and the reason is recorded with the numbers: a partially
// applied commit is currently forgotten because the next call reloads from
// storage, and with the state pinned in memory it would persist instead. The
// upstream API exposes no way to evict a cached group, so there would be no
// recovery from it. The right order is an eviction hook first, when the library
// is next re-vendored, and the strategy flip after.
func benchGroup(b *testing.B, n int) (Engine, GroupID) {
	ctx := context.Background()
	id, _ := identity.Generate()
	eng, err := New([]byte(id.PublicKey()), id.PrivateKey())
	if err != nil {
		b.Fatal(err)
	}
	gid, _ := eng.CreateGroup(ctx)
	kps := [][]byte{}
	for i := 0; i < n; i++ {
		o, _ := identity.Generate()
		oe, _ := New([]byte(o.PublicKey()), o.PrivateKey())
		kp, _ := oe.KeyPackage(ctx)
		kps = append(kps, kp)
		if len(kps) == 16 {
			if _, _, _, err := eng.AddMembers(ctx, gid, kps); err != nil {
				b.Fatal(err)
			}
			kps = kps[:0]
		}
	}
	if len(kps) > 0 {
		if _, _, _, err := eng.AddMembers(ctx, gid, kps); err != nil {
			b.Fatal(err)
		}
	}
	return eng, gid
}

func BenchmarkEpoch50(b *testing.B) {
	ctx := context.Background()
	eng, gid := benchGroup(b, 50)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := eng.Epoch(ctx, gid); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMembers50(b *testing.B) {
	ctx := context.Background()
	eng, gid := benchGroup(b, 50)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := eng.Members(ctx, gid); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncrypt50(b *testing.B) {
	ctx := context.Background()
	eng, gid := benchGroup(b, 50)
	msg := []byte("a message of ordinary length, nothing special about it")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := eng.Encrypt(ctx, gid, msg); err != nil {
			b.Fatal(err)
		}
	}
}
