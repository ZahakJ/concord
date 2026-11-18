package store

import (
	"reflect"
	"testing"
)

// A picture an import pinned is in the scope of the guild it was imported
// into, and of no other. A pin with no guild recorded is in no guild's scope
// until ScopeImportPins records one, which it does only for a pin no archive
// reader made.
func TestAnImportPinRecordsItsGuild(t *testing.T) {
	s := completeStore(t)
	imported, older, readerPin, absent := blobID('a'), blobID('b'), blobID('c'), blobID('d')
	for _, id := range []string{imported, older, readerPin} {
		holdBlob(t, s, id)
	}
	if err := s.PinImportedBlob("g1", imported); err != nil {
		t.Fatal(err)
	}
	if err := s.PinImportedBlob("g1", absent); err != nil {
		t.Fatal(err)
	}
	if err := s.PinAttachment(older, true); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.PinChronicleBlob("g2", "c", readerPin); err != nil || !ok {
		t.Fatalf("PinChronicleBlob: %v %v", ok, err)
	}

	scope := func(id string) []string {
		t.Helper()
		sc, ok := s.BlobServeScope(id)
		if !ok {
			t.Fatalf("blob %s… is not servable", id[:4])
		}
		return sc.Guilds
	}
	if got := scope(imported); !reflect.DeepEqual(got, []string{"g1"}) {
		t.Fatalf("imported picture: guilds %v, want [g1]", got)
	}
	if got := scope(older); len(got) != 0 {
		t.Fatalf("a pin with no guild recorded is in the scope of %v", got)
	}
	if _, ok := s.BlobServeScope(absent); ok || heldBlob(s, absent) {
		t.Fatal("pinning a blob not held made it servable")
	}

	unscoped, err := s.UnscopedPins()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(unscoped, map[string]bool{older: true}) {
		t.Fatalf("UnscopedPins = %v, want only the older pin", unscoped)
	}
	if err := s.ScopeImportPins("g3", []string{older, readerPin}); err != nil {
		t.Fatal(err)
	}
	if got := scope(older); !reflect.DeepEqual(got, []string{"g3"}) {
		t.Fatalf("older pin after ScopeImportPins: guilds %v, want [g3]", got)
	}
	if got := scope(readerPin); !reflect.DeepEqual(got, []string{"g2"}) {
		t.Fatalf("a reader's pin was given an import's guild: %v", got)
	}
}
