package bridge

import (
	"encoding/base64"
	"strings"
	"testing"

	appsvc "github.com/ZahakJ/concord/internal/app"
)

// FileGoesAsIs takes at most the first app.FileHeadBytes of a file. A page
// that sends more is refused before anything is decoded, so the call cannot
// be used to push a whole file through the core twice.
func TestFileGoesAsIsTakesOnlyTheHead(t *testing.T) {
	b := &Bridge{}
	limit := base64.StdEncoding.EncodedLen(appsvc.FileHeadBytes)

	pdf := []byte("%PDF-1.4 minutes")
	at := base64.StdEncoding.EncodeToString(append(pdf, make([]byte, appsvc.FileHeadBytes-len(pdf))...))
	if len(at) != limit {
		t.Fatalf("fixture is %d characters, want %d", len(at), limit)
	}
	if got, err := b.FileGoesAsIs(at); err != nil || !got {
		t.Fatalf("a head of exactly the limit: %v, %v; want true, nil", got, err)
	}

	over := at + "AAAA"
	if _, err := b.FileGoesAsIs(over); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("a head past the limit: %v, want the too-long refusal", err)
	}

	if _, err := b.FileGoesAsIs("not base64!"); err == nil {
		t.Fatal("a head that is not base64 was taken")
	}

	mp4 := base64.StdEncoding.EncodeToString([]byte("\x00\x00\x00\x18ftypisom\x00\x00\x00\x00isommp42"))
	if got, err := b.FileGoesAsIs(mp4); err != nil || got {
		t.Fatalf("an MP4's head: %v, %v; want false, nil", got, err)
	}
}
