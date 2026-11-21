package app

import (
	"bytes"
	"compress/gzip"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/ZahakJ/concord/internal/identity"
)

// TestImportArchiveBoundsDecompression: ImportArchive gunzipped the opened
// archive with io.ReadAll and no bound, so a sealed file of about a megabyte
// inflated to whatever its author chose before the JSON was even looked at.
// The other decoders (chronicle chunks, link cards, imports) stream through a
// cap; this one now does too, scaled to the size of the file, and refuses an
// archive that reaches it without reading on.
//
// The archive is 1 GiB of JSON whitespace followed by a byte that is not
// JSON, written as 1024 gzip members of 1 MiB each (gzip readers read every
// member in turn), so the import fails either way and never touches the
// Service; what the test checks is why it failed and what that cost.
func TestImportArchiveBoundsDecompression(t *testing.T) {
	member := func(b []byte) []byte {
		var g bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&g, gzip.BestCompression)
		if _, err := zw.Write(b); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		return g.Bytes()
	}
	spaces := member(bytes.Repeat([]byte{' '}, 1<<20))
	var gz bytes.Buffer
	gz.Write(member([]byte("{")))
	for i := 0; i < 1024; i++ {
		gz.Write(spaces)
	}
	gz.Write(member([]byte("x")))
	sealed, err := identity.SealWithPassphrase("bomb", gz.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	s := &Service{}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err = s.ImportArchive(sealed, "bomb")
	runtime.ReadMemStats(&after)

	if !errors.Is(err, errArchiveOverCap) || !strings.Contains(err.Error(), "decompressed") {
		t.Fatalf("a %d-byte archive that inflates to 1 GiB: got %v, want a refusal for its decompressed size", len(sealed), err)
	}
	// The cap here is the 64 MiB floor. Reading up to it in a growing buffer
	// allocates about three and a half times that (222 MiB measured alone,
	// 380 MiB in the full -race package run, where TotalAlloc also counts
	// other tests' services). Reading the whole bomb holds 1 GiB in its last
	// buffer alone.
	if got := after.TotalAlloc - before.TotalAlloc; got > 10*archivePlainFloor {
		t.Fatalf("the refusal allocated %d MiB; the cap should stop the read at the limit", got>>20)
	}
}

// An honest archive still opens, and the cap scales with the file and stays
// inside its floor and ceiling.
func TestDecodeArchiveAcceptsAnHonestFile(t *testing.T) {
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write([]byte(`{"version":1,"guilds":[{"id":"g","name":"n","channels":[{"id":"c","name":"general"}]}],"messages":[]}`)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	af, err := decodeArchive(gz.Bytes())
	if err != nil || len(af.Guilds) != 1 || af.Guilds[0].Channels[0].Name != "general" {
		t.Fatalf("honest archive: %+v, %v", af, err)
	}
	if got := archivePlainCap(10); got != archivePlainFloor {
		t.Errorf("small file cap %d, want the floor", got)
	}
	if got := archivePlainCap(4 << 20); got != 256<<20 {
		t.Errorf("4 MiB file cap %d, want 64 times it", got)
	}
	if got := archivePlainCap(1 << 30); got != archivePlainCeiling {
		t.Errorf("1 GiB file cap %d, want the ceiling", got)
	}
}
