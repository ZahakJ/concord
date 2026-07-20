package store

// Attachment OCR at rest + search integration. Load-bearing assertions:
// extracted text gets the same at-rest sealing as message bodies (never
// plaintext in the DB file); search finds a message whose image text matches
// and flags it OCRMatch; only status='ok' rows join the index; the sweep
// worklist only offers locally-cached, not-yet-processed blobs.

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zahak/concord/internal/domain"
)

const testBlobID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func msgWithToken(id, channel, content string) domain.Message {
	return domain.Message{
		ID: id, ChannelID: channel, Sender: []byte("sender-key"),
		Name: "Brahma", Content: content, Sent: time.Now().UTC(),
	}
}

func TestAttachmentOCRSealedAtRest(t *testing.T) {
	s, path := openTestStore(t)
	secret := "latency dropped to 3ms on the M4 (screenshot)"
	if err := s.SaveAttachmentOCR(testBlobID, secret, "ok"); err != nil {
		t.Fatalf("SaveAttachmentOCR: %v", err)
	}
	text, status, err := s.AttachmentOCR(testBlobID)
	if err != nil || text != secret || status != "ok" {
		t.Fatalf("AttachmentOCR round trip: %q %q %v", text, status, err)
	}
	// the DB file must not contain the plaintext — OCR text IS message content
	_ = s.Close()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read db: %v", err)
	}
	if bytes.Contains(raw, []byte("latency dropped")) {
		t.Fatal("extracted OCR text stored in plaintext — must be sealed like message bodies")
	}
}

func TestSearchMatchesOCRTextAndFlagsIt(t *testing.T) {
	s, _ := openTestStore(t)
	token := "look at this ![image](concord://attach/v1/" + testBlobID + "/somekeys/png/10x10)"
	if _, err := s.SaveMessage(msgWithToken("m1", "chan1", token)); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}
	if _, err := s.SaveMessage(msgWithToken("m2", "chan1", "plain text about benchmarks")); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}
	if err := s.SaveAttachmentOCR(testBlobID, "Quarterly Benchmarks 12345", "ok"); err != nil {
		t.Fatalf("SaveAttachmentOCR: %v", err)
	}

	got, err := s.SearchMessages("12345", 10)
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if len(got) != 1 || got[0].ID != "m1" {
		t.Fatalf("want the token message via OCR text, got %+v", got)
	}
	if !got[0].OCRMatch {
		t.Fatal("OCR-only hit must be flagged OCRMatch (matched text in image)")
	}

	// both match "benchmarks": the content hit is NOT flagged, the OCR hit is
	got, err = s.SearchMessages("benchmarks", 10)
	if err != nil || len(got) != 2 {
		t.Fatalf("want both messages, got %+v (%v)", got, err)
	}
	byID := map[string]domain.Message{got[0].ID: got[0], got[1].ID: got[1]}
	if byID["m2"].OCRMatch {
		t.Fatal("a plain content hit must not be flagged OCRMatch")
	}
	if !byID["m1"].OCRMatch {
		t.Fatal("the image-text hit must be flagged OCRMatch")
	}
}

func TestSearchIgnoresNonOKOCRRows(t *testing.T) {
	s, _ := openTestStore(t)
	token := "shot ![image](concord://attach/v1/" + testBlobID + "/keys/png/0x0)"
	if _, err := s.SaveMessage(msgWithToken("m1", "chan1", token)); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveAttachmentOCR(testBlobID, "should not index", "error"); err != nil {
		t.Fatal(err)
	}
	got, err := s.SearchMessages("index", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("error-status OCR text must not be searchable, got %+v", got)
	}
}

func TestAttachmentsMissingOCRWorklist(t *testing.T) {
	s, _ := openTestStore(t)
	cached := testBlobID
	uncached := strings.Repeat("b", 64)
	done := strings.Repeat("c", 64)
	content := "a ![image](concord://attach/v1/" + cached + "/KEYS1/png/0x0)" +
		" b ![image](concord://attach/v1/" + uncached + "/KEYS2/png/0x0)" +
		" c ![image](concord://attach/v1/" + done + "/KEYS3/png/0x0)"
	if _, err := s.SaveMessage(msgWithToken("m1", "chan1", content)); err != nil {
		t.Fatal(err)
	}
	// cached + done have local blobs; done already has a result row
	if err := s.SaveAttachment(cached, []byte("ct-1")); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveAttachment(done, []byte("ct-3")); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveAttachmentOCR(done, "already read", "ok"); err != nil {
		t.Fatal(err)
	}

	blobs, keys, err := s.AttachmentsMissingOCR(10)
	if err != nil {
		t.Fatalf("AttachmentsMissingOCR: %v", err)
	}
	if len(blobs) != 1 || blobs[0] != cached || keys[0] != "KEYS1" {
		t.Fatalf("worklist must be exactly the cached, unprocessed blob: %v %v", blobs, keys)
	}

	if has, _ := s.HasAttachmentOCR(done); !has {
		t.Fatal("HasAttachmentOCR must see the existing row")
	}
	if has, _ := s.HasAttachmentOCR(uncached); has {
		t.Fatal("HasAttachmentOCR must be false for an unseen blob")
	}
}
