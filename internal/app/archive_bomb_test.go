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
// archive with io.ReadAll and no bound, so a few hundred kilobytes of sealed
// file inflated to whatever its author chose before the JSON was even looked
// at. The other decoders (chronicle chunks, link cards, imports) stream through
// a cap; this one now does too, scaled to the size of the file, and refuses an
// archive that reaches it without reading on.
//
// The archive is 256 MiB of JSON whitespace (about 250 KB once gzipped)
// followed by a byte that is not JSON, so the import fails either way and
// never touches the Service; what the test checks is why it failed and what
// that cost.
func TestImportArchiveBoundsDecompression(t *testing.T) {
	var gz bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&gz, gzip.BestCompression)
	if _, err := zw.Write([]byte("{")); err != nil {
		t.Fatal(err)
	}
	mib := bytes.Repeat([]byte{' '}, 1<<20)
	for i := 0; i < 256; i++ {
		if _, err := zw.Write(mib); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := zw.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
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
		t.Fatalf("a %d-byte archive that inflates to 256 MiB: got %v, want a refusal for its decompressed size", len(sealed), err)
	}
	// The floor is 64 MiB; reading up to it in a growing buffer allocates
	// about three and a half times that in all (222 MiB measured); reading the
	// whole 256 MiB bomb the same way allocates several times the bound.
	if got := after.TotalAlloc - before.TotalAlloc; got > 4*archivePlainFloor {
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
