package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// signedRoleOps mints k role_upsert ops signed by the service's own account,
// the guild's owner — a governance log as a sync payload would carry it, none
// of it ingested yet.
func signedRoleOps(t testing.TB, s *Service, k int) []json.RawMessage {
	t.Helper()
	raws := make([]json.RawMessage, 0, k)
	base := time.Now().UnixNano()
	for i := 0; i < k; i++ {
		o := govOp{
			Seq: uint64(i + 1), Signer: s.PublicKey(), Type: "role_upsert",
			RoleID: fmt.Sprintf("r%04d", i), Name: fmt.Sprintf("role %d", i),
			Position: i % 50, Time: base + int64(i),
		}
		o.Sig = s.id.Sign(o.signingBytes())
		raw, err := json.Marshal(o)
		if err != nil {
			t.Fatalf("marshal op: %v", err)
		}
		raws = append(raws, raw)
	}
	return raws
}

// TestGovSyncIngestFoldsOnce: a governance log arriving in a sync payload used
// to be ingested one op at a time, each one re-folding the WHOLE log — every
// signature verified again — under the service's write lock, and announcing a
// guild update to the UI per op. A member catching up on K ops paid K folds of
// up to K ops and K redraws. The batch is now recorded whole, folded once and
// announced once, and the state it folds to is the same.
func TestGovSyncIngestFoldsOnce(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := Start(ctx, Config{DataDir: t.TempDir(), Passphrase: "test-pass", DisableMDNS: true})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()
	g, err := s.CreateGuild("governed")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	const k = 500
	raws := signedRoleOps(t, s, k)

	var updates atomic.Int32
	s.OnGuildUpdate(func() { updates.Add(1) })
	start := time.Now()
	s.ingestGovOpsRaw(g.ID, raws)
	took := time.Since(start)
	t.Logf("ingesting %d governance ops from one payload took %v", k, took)

	if n := len(s.Roles(g.ID)); n != k {
		t.Fatalf("folded state holds %d roles, want %d", n, k)
	}
	if n := updates.Load(); n != 1 {
		t.Fatalf("one sync payload announced %d guild updates, want 1", n)
	}
	// The same payload again is all duplicates: nothing to fold, nothing to say.
	s.ingestGovOpsRaw(g.ID, raws)
	if n := updates.Load(); n != 1 {
		t.Fatalf("a payload of ops already held announced an update (%d total)", n)
	}
}

// BenchmarkGovSyncIngest500 measures the payload-sized case directly.
func BenchmarkGovSyncIngest500(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		s, err := Start(ctx, Config{DataDir: b.TempDir(), Passphrase: "test-pass", DisableMDNS: true})
		if err != nil {
			b.Fatalf("Start: %v", err)
		}
		g, err := s.CreateGuild("governed")
		if err != nil {
			b.Fatalf("CreateGuild: %v", err)
		}
		raws := signedRoleOps(b, s, 500)
		b.StartTimer()
		s.ingestGovOpsRaw(g.ID, raws)
		b.StopTimer()
		_ = s.Close()
	}
}
