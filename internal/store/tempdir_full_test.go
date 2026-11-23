package store

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// A full TMPDIR must not stop messages being saved. SQLite puts its scratch
// files (statement journals, sort spills) there unless told otherwise; on a
// desktop /tmp is often a RAM disk, and when it filled, saves failed with
// "database or disk is full" though the data's own disk had room.
//
// A full filesystem needs a mount: the test re-runs itself in a user and mount
// namespace with a 64 KiB tmpfs as TMPDIR, filled to the brim, and skips where
// unprivileged namespaces are not available.
func TestTempDirFullStillSaves(t *testing.T) {
	if os.Getenv("CONCORD_TMPFULL_CHILD") == "" {
		if testing.Short() {
			t.Skip("re-runs itself under unshare")
		}
		unshare, err := exec.LookPath("unshare")
		if err != nil {
			t.Skip("no unshare")
		}
		exe, _ := os.Executable()
		mnt := t.TempDir()
		db := t.TempDir()
		script := `mount -t tmpfs -o size=64k tmpfs "$1" || exit 77
dd if=/dev/zero of="$1/fill" bs=4k 2>/dev/null
TMPDIR="$1" SQLITE_TMPDIR="$1" CONCORD_TMPFULL_CHILD="$2" exec "$3" -test.run '^TestTempDirFullStillSaves$' -test.v`
		out, err := exec.Command(unshare, "-rm", "sh", "-c", script, "sh", mnt, db, exe).CombinedOutput()
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 77 {
			t.Skipf("no tmpfs in a user namespace here: %s", out)
		}
		if err != nil {
			t.Fatalf("child: %v\n%s", err, out)
		}
		return
	}
	s, err := Open(filepath.Join(os.Getenv("CONCORD_TMPFULL_CHILD"), "c.db"), make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	pub := make([]byte, 32)
	body := make([]byte, 3000)
	for i := 0; i < 800; i++ {
		_, _ = rand.Read(body)
		m, _ := domain.NewMessage("ch", pub, hex.EncodeToString(body))
		m.Sent = time.Now()
		if _, err := s.SaveMessage(m); err != nil {
			t.Fatalf("save %d with TMPDIR full: %v", i, err)
		}
	}
}
