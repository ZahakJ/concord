package app

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// A profile read that overlaps an edit must never report the edit's stamp
// over fields from before the edit. Such a copy fits inline in a device hello
// (the old pictures are small), the account's other device adopts it as the
// edit, and the record with the real pictures is then not newer: the device
// keeps the old ones for good. This failed TestRestoredDeviceGetsProfileFromLinkedDevice
// about one run in ten, at "the phone never had the profile to give back".
//
// Every field below names the edit that wrote it, so a read can be checked
// field by field against the stamp it reports. Newer fields under an older
// stamp are allowed: the newer stamp follows them.
func TestSelfProfileNeverPairsANewStampWithOlderFields(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startService(t, ctx)

	const edits = 150
	mark := func(i int) string { return base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("edit-%04d", i))) }
	edit := func(i int) Profile {
		p := sentinelProfile(fmt.Sprintf("edit %d", i))
		pad := ""
		if i%2 == 1 { // every other edit carries pictures the size real ones are
			pad = strings.Repeat("QUJD", 4<<10)
		}
		p.Avatar = "data:image/png;base64," + mark(i) + pad
		p.Banner = "data:image/jpeg;base64," + mark(i) + pad
		return p
	}
	editOf := func(field, prefix string) int {
		raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(field, prefix)[:12])
		if err != nil {
			return -1
		}
		var n int
		if _, err := fmt.Sscanf(string(raw), "edit-%04d", &n); err != nil {
			return -1
		}
		return n
	}

	if err := s.SetProfile(edit(0)); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}
	stampOf := map[int64]int{s.profileStamp(): 0}
	type read struct {
		at                     int64
		status, avatar, banner int
	}
	var reads []read
	var done atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for !done.Load() {
			p := s.selfStoredProfile()
			var st int
			if _, err := fmt.Sscanf(p.Status, "edit %d", &st); err != nil {
				st = -1
			}
			reads = append(reads, read{p.UpdatedAt, st,
				editOf(p.Avatar, "data:image/png;base64,"), editOf(p.Banner, "data:image/jpeg;base64,")})
		}
	}()
	for i := 1; i <= edits; i++ {
		if err := s.SetProfile(edit(i)); err != nil {
			t.Fatalf("SetProfile %d: %v", i, err)
		}
		stampOf[s.profileStamp()] = i
	}
	done.Store(true)
	wg.Wait()

	torn := 0
	for _, r := range reads {
		n, ok := stampOf[r.at]
		if !ok {
			t.Fatalf("a read reported stamp %d, which no edit wrote", r.at)
		}
		if r.status < n || r.avatar < n || r.banner < n {
			if torn == 0 {
				t.Errorf("a read reported edit %d's stamp over older fields: status from edit %d, avatar from %d, banner from %d",
					n, r.status, r.avatar, r.banner)
			}
			torn++
		}
	}
	if torn > 0 {
		t.Errorf("%d of %d reads paired a stamp with fields older than it", torn, len(reads))
	}
	t.Logf("%d reads across %d edits", len(reads), edits)
}
