package app

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/ZahakJ/concord/internal/identity"
)

// An archive must survive a round trip through the seal, and must restore into
// a device that has the guild but has lost its messages — which is the actual
// disaster it exists for.
func TestArchiveRoundTripRestoresHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := startServiceInDir(t, ctx, t.TempDir())
	g, err := svc.CreateGuild("Night Owls")
	if err != nil {
		t.Fatalf("create guild: %v", err)
	}
	ch := g.Channels[0].ID
	for _, body := range []string{"first", "second", "third"} {
		if _, err := svc.SendMessage(ch, body, ""); err != nil {
			t.Fatalf("send %q: %v", body, err)
		}
	}

	sealed, st, err := svc.ExportArchive("archive-pass", false)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if st.Messages < 3 {
		t.Fatalf("archive carried %d messages, want at least the 3 sent", st.Messages)
	}
	// The bodies must not be legible in the file.
	if strings.Contains(string(sealed), "second") {
		t.Fatal("message text is readable in the sealed archive")
	}

	// Lose the history but keep the guild, then restore.
	if _, err := svc.store.PruneMessagesBefore([]string{ch}, 1<<62); err != nil {
		t.Fatalf("wipe: %v", err)
	}
	if msgs, _ := svc.store.Messages(ch, 0); len(msgs) != 0 {
		t.Fatalf("precondition: %d messages survived the wipe", len(msgs))
	}

	in, err := svc.ImportArchive(sealed, "archive-pass")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if in.Messages < 3 {
		t.Fatalf("restored %d messages, want at least 3", in.Messages)
	}
	back, err := svc.store.Messages(ch, 0)
	if err != nil {
		t.Fatal(err)
	}
	var bodies []string
	for _, m := range back {
		bodies = append(bodies, m.Content)
	}
	joined := strings.Join(bodies, " ")
	for _, want := range []string{"first", "second", "third"} {
		if !strings.Contains(joined, want) {
			t.Errorf("%q did not come back; got %q", want, joined)
		}
	}
}

// Restore is additive: a second pass must change nothing, and must never
// overwrite what is already there. Restoring a stale archive onto a live
// account cannot be allowed to eat anything newer, because nothing else holds a
// copy to recover from.
func TestArchiveImportIsAdditiveAndIdempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := startServiceInDir(t, ctx, t.TempDir())
	g, _ := svc.CreateGuild("Night Owls")
	ch := g.Channels[0].ID
	if _, err := svc.SendMessage(ch, "in the archive", ""); err != nil {
		t.Fatal(err)
	}
	sealed, _, err := svc.ExportArchive("archive-pass", false)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	// Said AFTER the archive was taken — a stale restore must not disturb it.
	if _, err := svc.SendMessage(ch, "said afterwards", ""); err != nil {
		t.Fatal(err)
	}

	before, _ := svc.store.Messages(ch, 0)
	first, err := svc.ImportArchive(sealed, "archive-pass")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if first.Messages != 0 {
		t.Errorf("re-imported %d messages that were already present", first.Messages)
	}
	second, err := svc.ImportArchive(sealed, "archive-pass")
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if second.Messages != 0 {
		t.Errorf("second pass inserted %d messages; import is not idempotent", second.Messages)
	}
	after, _ := svc.store.Messages(ch, 0)
	if len(after) != len(before) {
		t.Fatalf("message count moved from %d to %d across two imports", len(before), len(after))
	}
	var found bool
	for _, m := range after {
		if m.Content == "said afterwards" {
			found = true
		}
	}
	if !found {
		t.Fatal("a message newer than the archive was lost by restoring it")
	}
}

// A wrong passphrase must fail cleanly, not half-restore.
func TestArchiveWrongPassphraseRestoresNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := startServiceInDir(t, ctx, t.TempDir())
	g, _ := svc.CreateGuild("Night Owls")
	if _, err := svc.SendMessage(g.Channels[0].ID, "secret", ""); err != nil {
		t.Fatal(err)
	}
	sealed, _, err := svc.ExportArchive("right", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ImportArchive(sealed, "wrong"); err != identity.ErrWrongPassphrase {
		t.Fatalf("got %v, want ErrWrongPassphrase", err)
	}
}

// The archive must never carry the identity seed or MLS group state. Both would
// be actively harmful in a file people are told to copy about: the seed has its
// own recovery path already, and restoring a leaf private key onto a second
// device puts one member's key on two machines.
func TestArchiveExcludesIdentityAndGroupState(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := startServiceInDir(t, ctx, t.TempDir())
	if _, err := svc.CreateGuild("Night Owls"); err != nil {
		t.Fatal(err)
	}
	sealed, _, err := svc.ExportArchive("archive-pass", false)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := identity.OpenWithPassphrase("archive-pass", sealed)
	if err != nil {
		t.Fatal(err)
	}
	// Decompressed content is what a determined reader would see with the
	// passphrase; assert on that rather than on the ciphertext.
	body := decompressForTest(t, plain)
	for _, forbidden := range []string{"seed", "leaf", "private", "keystore", "epoch_secret"} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Errorf("archive contains %q, which suggests identity or group state leaked in", forbidden)
		}
	}
}

func decompressForTest(t *testing.T, gz []byte) string {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	defer zr.Close()
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(raw)
}
