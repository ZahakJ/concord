package app

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"testing"
	"time"
)

// A picture an import pinned before the guild was recorded is given the
// guild whose archive names it, once; a pin no archive names is given none.
func TestAnOlderImportPinIsGivenItsGuild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := startService(t, ctx)
	waitUntil(t, 20*time.Second, func() bool {
		v, _ := a.store.GetSetting(importPinsScopedKey)
		return v != ""
	}, "the launch never ran the scope pass")
	g, err := a.CreateGuild("g")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	if _, err := a.CreateGuild("another"); err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}

	named, _, err := a.sealBlob([]byte("archived picture"))
	if err != nil {
		t.Fatal(err)
	}
	stray, _, err := a.sealBlob([]byte("a picture no archive names"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{named, stray} {
		if err := a.store.PinAttachment(id, true); err != nil {
			t.Fatal(err)
		}
	}
	_, keys, _ := a.sealBlob([]byte("unused"))
	msgs := []chronicleMessage{{
		ID: "p0", Nano: 1_300_000_000_000_000_000, Content: "photo",
		Attach: []string{fmt.Sprintf("concord://attach/v1/%s/%s/png/0x0", named, keys)},
	}}
	raw, chunks, err := a.buildChronicle(g.ID, "the old forum", "", []chronicleChannel{{ID: "src-pics", Name: "pics", Type: "text"}},
		[]chronicleAuthor{{Name: "ada"}}, map[string][]chronicleMessage{"src-pics": msgs}, 1, 0)
	if err != nil {
		t.Fatalf("buildChronicle: %v", err)
	}
	if err := a.AttachChronicle(g.ID, raw, chunks); err != nil {
		t.Fatalf("AttachChronicle: %v", err)
	}

	guildsOf := func(id string) []string {
		t.Helper()
		sc, ok := a.store.BlobServeScope(id)
		if !ok {
			t.Fatalf("blob %s… is not servable", id[:6])
		}
		return sc.Guilds
	}
	if got := guildsOf(named); len(got) != 0 {
		t.Fatalf("before the pass the older pin has guilds %v", got)
	}
	if err := a.store.SetSetting(importPinsScopedKey, ""); err != nil {
		t.Fatal(err)
	}
	a.scopeImportPinsOnce()
	if got := guildsOf(named); !reflect.DeepEqual(got, []string{g.ID}) {
		t.Fatalf("the archive's picture has guilds %v, want [%s]", got, g.ID)
	}
	if got := guildsOf(stray); len(got) != 0 {
		t.Fatalf("a pin no archive names was given guilds %v", got)
	}
	if v, _ := a.store.GetSetting(importPinsScopedKey); v == "" {
		t.Fatal("the pass was not recorded")
	}
}

// A pass that cannot read the archive from the store is not recorded, and the
// next pass gives the pin its guild. The read errors come from renaming a
// table through a second connection to the same file: the store has no hook
// for a failing read, and this leaves its code as it runs.
func TestTheScopePassRunsAgainAfterAReadError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	for _, table := range []string{"chronicle_manifests", "chronicle_chunks"} {
		t.Run(table, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			a := startService(t, ctx)
			waitUntil(t, 20*time.Second, func() bool {
				v, _ := a.store.GetSetting(importPinsScopedKey)
				return v != ""
			}, "the launch never ran the scope pass")
			g, err := a.CreateGuild("g")
			if err != nil {
				t.Fatalf("CreateGuild: %v", err)
			}
			named, keys, err := a.sealBlob([]byte("archived picture"))
			if err != nil {
				t.Fatal(err)
			}
			if err := a.store.PinAttachment(named, true); err != nil {
				t.Fatal(err)
			}
			msgs := []chronicleMessage{{
				ID: "p0", Nano: 1_300_000_000_000_000_000, Content: "photo",
				Attach: []string{fmt.Sprintf("concord://attach/v1/%s/%s/png/0x0", named, keys)},
			}}
			raw, chunks, err := a.buildChronicle(g.ID, "the old forum", "", []chronicleChannel{{ID: "src-pics", Name: "pics", Type: "text"}},
				[]chronicleAuthor{{Name: "ada"}}, map[string][]chronicleMessage{"src-pics": msgs}, 1, 0)
			if err != nil {
				t.Fatalf("buildChronicle: %v", err)
			}
			if err := a.AttachChronicle(g.ID, raw, chunks); err != nil {
				t.Fatalf("AttachChronicle: %v", err)
			}
			if err := a.store.SetSetting(importPinsScopedKey, ""); err != nil {
				t.Fatal(err)
			}

			db, err := sql.Open("sqlite", dbPathIn(a.dataDir))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			rename := func(from, to string) {
				t.Helper()
				if _, err := db.Exec(`PRAGMA busy_timeout=5000; ALTER TABLE ` + from + ` RENAME TO ` + to); err != nil {
					t.Fatalf("rename %s: %v", from, err)
				}
			}
			rename(table, table+"_away")
			a.scopeImportPinsOnce()
			rename(table+"_away", table)
			if v, _ := a.store.GetSetting(importPinsScopedKey); v != "" {
				t.Fatal("a pass that could not read the archive was recorded as done")
			}

			a.scopeImportPinsOnce()
			sc, ok := a.store.BlobServeScope(named)
			if !ok || !reflect.DeepEqual(sc.Guilds, []string{g.ID}) {
				t.Fatalf("after the clean pass the pin has guilds %v (servable %v), want [%s]", sc.Guilds, ok, g.ID)
			}
			if v, _ := a.store.GetSetting(importPinsScopedKey); v == "" {
				t.Fatal("the clean pass was not recorded")
			}
		})
	}
}
