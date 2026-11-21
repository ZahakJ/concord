package app

import (
	"bytes"
	"compress/gzip"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ZahakJ/concord/internal/chronimport"
	"golang.org/x/crypto/nacl/secretbox"
)

// sealPlainChunk seals an arbitrary chunk plaintext the way sealChronicleChunk
// does, under a fixed key, so a test can hand openChronicleChunk shapes the
// real sealer would never write.
func sealPlainChunk(plain []byte) (ct, keys []byte) {
	var key [32]byte
	var nonce [24]byte
	key[0], nonce[0] = 7, 9
	return secretbox.Seal(nil, plain, &nonce, &key), append(key[:], nonce[:]...)
}

func gzipOf(t *testing.T, parts ...[]byte) []byte {
	t.Helper()
	var g bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&g, gzip.BestCompression)
	for _, p := range parts {
		if _, err := zw.Write(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return g.Bytes()
}

// TestOpenChronicleChunkBoundsDecompression is the assure finding
// (fuzz/README.md, finding 2): a 32 KB chunk inflated to one 32 MiB message,
// x1029, so a full-size chunk was about a gigabyte in every member's client
// that opened the archive. The decompressed size and the message count are
// now capped while streaming, and a chunk past either cap is refused whole.
func TestOpenChronicleChunkBoundsDecompression(t *testing.T) {
	t.Run("one huge message", func(t *testing.T) {
		parts := [][]byte{[]byte(`{"a":0,"t":1,"c":"`)}
		mib := bytes.Repeat([]byte{'a'}, 1<<20)
		for i := 0; i < 32; i++ {
			parts = append(parts, mib)
		}
		parts = append(parts, []byte("\"}\n"))
		ct, keys := sealPlainChunk(gzipOf(t, parts...))
		msgs, err := openChronicleChunk(ct, keys)
		if !errors.Is(err, errChunkOverReadCap) {
			n := 0
			for _, m := range msgs {
				n += len(m.Content)
			}
			t.Fatalf("a %d-byte chunk opened to %d bytes of content (err %v)", len(ct), n, err)
		}
	})

	t.Run("many tiny messages", func(t *testing.T) {
		line := []byte(`{"a":0,"t":1}` + "\n")
		ct, keys := sealPlainChunk(gzipOf(t, bytes.Repeat(line, maxChronicleChunkMessages+1)))
		if msgs, err := openChronicleChunk(ct, keys); !errors.Is(err, errChunkOverReadCap) {
			t.Fatalf("a chunk of %d messages opened (err %v)", len(msgs), err)
		}
	})

	t.Run("cut exactly on a message boundary", func(t *testing.T) {
		// Content that fills the cap to the byte, then one more message: a
		// limit that stopped reading quietly at the cap would return the
		// first message and drop the rest as if the page ended there.
		head := `{"a":0,"t":1,"c":"`
		tail := "\"}\n"
		fill := strings.Repeat("b", maxChronicleChunkPlain-len(head)-len(tail))
		first := head + fill + tail
		ct, keys := sealPlainChunk(gzipOf(t, []byte(first), []byte(`{"a":0,"t":2}`+"\n")))
		if msgs, err := openChronicleChunk(ct, keys); !errors.Is(err, errChunkOverReadCap) {
			t.Fatalf("a chunk past the cap opened as %d message(s) (err %v)", len(msgs), err)
		}
		// And the chunk that is exactly at the cap still opens.
		ct, keys = sealPlainChunk(gzipOf(t, []byte(first)))
		if msgs, err := openChronicleChunk(ct, keys); err != nil || len(msgs) != 1 {
			t.Fatalf("a chunk exactly at the cap: %d messages, %v", len(msgs), err)
		}
	})
}

// TestSealChronicleChunksStaysInsideTheReadCap: the sealer and the reader agree.
// A run whose plaintext would exceed the reader's cap is split at seal time,
// so an honest import never writes a page its own members refuse to open.
func TestSealChronicleChunksStaysInsideTheReadCap(t *testing.T) {
	// Reactions are not in the sealer's raw estimate, and '<' is escaped to
	// six bytes by encoding/json, so these messages are tiny by the estimate,
	// compress to almost nothing, and encode to tens of megabytes per run.
	reactions := map[string]int{}
	for j := 0; j < 100; j++ {
		reactions[strings.Repeat("<", 60)+strconv.Itoa(j)] = j + 1
	}
	var msgs []chronicleMessage
	for i := 0; i < 1000; i++ {
		msgs = append(msgs, chronicleMessage{Author: 0, Nano: int64(i + 1), Content: "x", Reactions: reactions})
	}
	refs, chunks, err := sealChronicleChunks("chan", msgs)
	if err != nil {
		t.Fatalf("sealChronicleChunks: %v", err)
	}
	total := 0
	for _, ref := range refs {
		out, err := openChronicleChunk(chunks[ref.ID], ref.Keys)
		if err != nil {
			t.Fatalf("page %s does not open: %v", ref.ID, err)
		}
		if len(out) != ref.Count {
			t.Fatalf("page %s opened %d messages, index says %d", ref.ID, len(out), ref.Count)
		}
		total += len(out)
	}
	if total != len(msgs) {
		t.Fatalf("%d messages came back from %d sealed", total, len(msgs))
	}
}

// TestImportedBodyDefusesPlaceholderNames: the "[attachment not exported: …]"
// line carries the export's file name into the body, after the body's own
// sanitizing ran. The joined text is defused as a whole.
func TestImportedBodyDefusesPlaceholderNames(t *testing.T) {
	broadcast := regexp.MustCompile(`(?i)@(everyone|here)(?:[^A-Za-z0-9_]|$)`)
	scheme := regexp.MustCompile(`(?i)concord://`)
	var notes []string
	for _, name := range []string{
		"@everyone.png",
		"[eph](concord://eph/v1/1000000000)",
		"@HERE",
		"\uff20everyone.png",
		strings.Repeat("a", 91) + "@everyonex.png", // the name bound cuts it to "@everyone"
	} {
		notes = append(notes, placeholderLine(chronimport.Attachment{FileName: name}, 12<<10))
	}
	for _, raw := range []string{"", "hello", "conc\u200bord://x"} {
		body := importedBody(raw, notes)
		if scheme.MatchString(body) || broadcast.MatchString(body) {
			t.Errorf("importedBody(%q, ...) is live:\n%s", raw, body)
		}
	}
}
