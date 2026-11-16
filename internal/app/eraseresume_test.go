package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/identity"
)

var errStopHere = errors.New("test: the process stops here")

// filesIn lists dataDir's entries (recursively), relative.
func filesIn(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	_ = filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err != nil || p == dir {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		out = append(out, rel)
		return nil
	})
	sort.Strings(out)
	return out
}

// onlyLockLeft fails unless nothing but the keystore lock file is left.
func onlyLockLeft(t *testing.T, dir, when string) {
	t.Helper()
	for _, f := range filesIn(t, dir) {
		if f != "keystore.json.lock" {
			t.Fatalf("%s: %s is still here (all: %v)", when, f, filesIn(t, dir))
		}
	}
}

// stopEraseAt makes the n-th erase step (1-based, counted from now) stop
// the erase, as if the process died there, and records each step's name.
func stopEraseAt(t *testing.T, n int) *[]string {
	t.Helper()
	var steps []string
	orig := eraseStep
	eraseStep = func(step string) error {
		steps = append(steps, step)
		if len(steps) == n {
			return errStopHere
		}
		return nil
	}
	t.Cleanup(func() { eraseStep = orig })
	return &steps
}

// revokedLinkedDevice starts an install linked as a device, holding a guild
// with one message, with a stored revocation of its own device key.
func unlinkedDeviceWithData(t *testing.T, ctx context.Context, dir string) (*Service, string, *identity.DeviceRevocation) {
	t.Helper()
	s, err := Start(ctx, Config{DataDir: dir, Passphrase: "test-pass", DisableMDNS: true})
	if err != nil {
		t.Fatal(err)
	}
	dev := s.id.DevicePublicKey()
	if err := saveDeviceMarker(dir, s.id.IssueDeviceCertFor(dev, "phone", time.Now().Add(-time.Hour).Unix())); err != nil {
		t.Fatal(err)
	}
	g, err := s.CreateGuild("g")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SendMessage(g.Channels[0].ID, "the old account's message", "", ""); err != nil {
		t.Fatal(err)
	}
	rev := s.id.RevokeDevice(dev, time.Now().Unix())
	s.saveRevocations([]*identity.DeviceRevocation{rev})
	if ok, _ := s.selfRevoked(); !ok {
		t.Fatal("setup: the stored revocation does not apply")
	}
	return s, g.Channels[0].ID, rev
}

// nothingShownOrCreated checks a device whose erase has not finished: no
// account shown, and a start opens nothing and creates no keystore.
func nothingShownOrCreated(t *testing.T, ctx context.Context, dir, when string) {
	t.Helper()
	if HasIdentity(dir) {
		t.Fatalf("%s: the device shows an account", when)
	}
	if a := LockedAccount(dir); a != (DeviceAccount{}) {
		t.Fatalf("%s: the lock screen shows %+v", when, a)
	}
}

// The erase steps of a whole unlink erase, in order.
func unlinkEraseSteps(t *testing.T) []string {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	s, _, _ := unlinkedDeviceWithData(t, ctx, dir)
	steps := stopEraseAt(t, -1)
	if err := s.wipeSelf(); err != nil {
		t.Fatal(err)
	}
	onlyLockLeft(t, dir, "after a whole erase")
	return append([]string(nil), (*steps)...)
}

// An unlink erase stopped after any one of its steps is finished by the next
// start before anything is opened: nothing of the old account is left, no
// account is created meanwhile, the start says the device was unlinked, and
// the record is gone at the end.
func TestAnUnlinkEraseStoppedAfterAnyStepIsFinishedAtTheNextStart(t *testing.T) {
	steps := unlinkEraseSteps(t)
	if len(steps) < 8 || steps[0] != "record" || steps[1] != "keystore" {
		t.Fatalf("erase steps: %v (want the record first, then the keystore)", steps)
	}
	for i, step := range steps {
		t.Run(step, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			dir := t.TempDir()
			s, _, _ := unlinkedDeviceWithData(t, ctx, dir)
			stopEraseAt(t, i+1)
			err := s.wipeSelf()
			if !errors.Is(err, errStopHere) {
				t.Fatalf("the erase did not stop at %s: %v", step, err)
			}
			eraseStep = func(string) error { return nil }
			if !erasePendingIn(dir) {
				t.Fatalf("stopped after %s with no erase record", step)
			}
			if keystoreFileHere(dir) && i > 0 {
				t.Fatalf("stopped after %s with the keystore still here", step)
			}
			nothingShownOrCreated(t, ctx, dir, "stopped after "+step)
			_, err = Start(ctx, Config{DataDir: dir, Passphrase: "test-pass", DisableMDNS: true})
			if !errors.Is(err, ErrDeviceUnlinked) {
				t.Fatalf("the next start: %v, want ErrDeviceUnlinked", err)
			}
			onlyLockLeft(t, dir, "after the next start")
			// Then the device is an empty install.
			s2, err := Start(ctx, Config{DataDir: dir, Passphrase: "new-pass", DisableMDNS: true})
			if err != nil {
				t.Fatal(err)
			}
			defer s2.Close()
			for _, g := range s2.Guilds() {
				if g.Name == "g" {
					t.Fatal("the old account's guild came back")
				}
			}
		})
	}
}

// Stopped again and again, each start a step further, the erase still ends
// with nothing left; no start in between opens or creates anything.
func TestAnEraseStoppedAtEveryStartFinishesInTheEnd(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	s, _, _ := unlinkedDeviceWithData(t, ctx, dir)
	stopEraseAt(t, 1)
	if err := s.wipeSelf(); !errors.Is(err, errStopHere) {
		t.Fatal(err)
	}
	for n := 1; ; n++ {
		if n > 40 {
			t.Fatal("the erase never finished")
		}
		stopEraseAt(t, n)
		_, err := Start(ctx, Config{DataDir: dir, Passphrase: "test-pass", DisableMDNS: true})
		if errors.Is(err, ErrDeviceUnlinked) {
			break
		}
		if !errors.Is(err, errStopHere) {
			t.Fatalf("start %d: %v", n, err)
		}
		if _, serr := os.Lstat(keystorePathIn(dir)); serr == nil && n > 1 {
			t.Fatalf("start %d created or kept a keystore", n)
		}
		nothingShownOrCreated(t, ctx, dir, "between starts")
	}
	onlyLockLeft(t, dir, "at the end")
}

// A file that cannot be removed: the erase says so and names it, the record
// stays, nothing is opened or created, and the next start tries again.
func TestAFileThatCannotBeRemovedIsReportedAndRetriedAtTheNextStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	s, _, _ := unlinkedDeviceWithData(t, ctx, dir)
	held := dbPathIn(dir) + "-wal"
	_ = os.Remove(held)
	if err := os.MkdirAll(filepath.Join(held, "held"), 0o700); err != nil {
		t.Fatal(err)
	}
	s.closeSession() // the -wal is a directory now; nothing writes it
	err := s.wipeSelf()
	if !errors.Is(err, ErrDeviceUnlinkedNotErased) || !errors.Is(err, ErrDataNotErased) || !strings.Contains(err.Error(), "concord.db-wal") {
		t.Fatalf("the erase said: %v", err)
	}
	if !erasePendingIn(dir) {
		t.Fatal("no erase record after an erase that did not finish")
	}
	nothingShownOrCreated(t, ctx, dir, "with the file held")
	_, err = Start(ctx, Config{DataDir: dir, Passphrase: "test-pass", DisableMDNS: true})
	if !errors.Is(err, ErrEraseUnfinished) || !strings.Contains(err.Error(), "concord.db-wal") {
		t.Fatalf("a start with the file held: %v", err)
	}
	if _, serr := os.Lstat(keystorePathIn(dir)); serr == nil {
		t.Fatal("a start created a keystore beside the unerased data")
	}
	_ = os.RemoveAll(held)
	if _, err := Start(ctx, Config{DataDir: dir, Passphrase: "test-pass", DisableMDNS: true}); !errors.Is(err, ErrDeviceUnlinked) {
		t.Fatalf("the start after the file was let go: %v", err)
	}
	onlyLockLeft(t, dir, "at the end")
}

// Close after the session's erase writes nothing into the erased directory.
func TestCloseAfterTheEraseWritesNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	s, _, _ := unlinkedDeviceWithData(t, ctx, dir)
	if err := s.wipeSelf(); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	s.peers.Remember("12D3KooWQYhTNQdmr3ArTeUHRYzFg94BKyTkoWBDWez9kSCVe2Xo", []string{"/ip4/192.0.2.1/tcp/1"})
	_ = s.peers.Save(s.dataDir)
	onlyLockLeft(t, dir, "after Close")
}

// Close returns only once an erase in flight has finished.
func TestCloseWaitsForAnEraseInFlight(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	s, _, rev := unlinkedDeviceWithData(t, ctx, dir)
	entered, release := make(chan struct{}), make(chan struct{})
	orig := eraseStep
	eraseStep = func(step string) error {
		if step == "keystore" {
			close(entered)
			<-release
		}
		return nil
	}
	defer func() { eraseStep = orig }()
	s.applyRevocation(rev)
	<-entered
	closed := make(chan struct{})
	go func() { _ = s.Close(); close(closed) }()
	select {
	case <-closed:
		t.Fatal("Close returned with the erase in flight")
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	<-closed
	onlyLockLeft(t, dir, "when Close returned")
}

// After Close, no erase of the session starts, and its retry loop ends
// without another attempt; the next start erases the device.
func TestNoEraseOfAClosedSessionRunsAfterClose(t *testing.T) {
	fastSelfEraseRetry(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	s, _, _ := unlinkedDeviceWithData(t, ctx, dir)
	orig := removeKeystore
	defer func() { removeKeystore = orig }()
	attempts := 0
	removeKeystore = func(p string, still func() bool, then func()) error {
		attempts++
		return identity.ErrKeystoreBusy
	}
	if err := s.wipeSelf(); err == nil {
		t.Fatal("a busy keystore was erased")
	}
	_ = s.Close()
	before := attempts
	selfErasers.Wait()
	if attempts != before {
		t.Fatalf("%d erase attempts after Close", attempts-before)
	}
	removeKeystore = orig
	if err := s.wipeSelf(); !errors.Is(err, errClosedBeforeErase) {
		t.Fatalf("an erase after Close: %v", err)
	}
	if !keystoreFileHere(dir) {
		t.Fatal("an erase ran after Close")
	}
	if _, err := Start(ctx, Config{DataDir: dir, Passphrase: "test-pass", DisableMDNS: true}); !errors.Is(err, ErrDeviceUnlinked) {
		t.Fatalf("the next start: %v", err)
	}
	onlyLockLeft(t, dir, "after the next start")
}

// The Settings wipe and starting over go through the same recorded erase: one
// stopped part-way is finished at the next start before an account is made.
func TestAWipeStoppedPartWayIsFinishedBeforeANewAccount(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	s, ch, _ := unlinkedDeviceWithData(t, ctx, dir)
	_ = s.Close()
	stopEraseAt(t, 3) // record, keystore, then the database
	if err := ResetIdentityIf(dir, func() bool { return true }, nil); !errors.Is(err, errStopHere) {
		t.Fatal(err)
	}
	eraseStep = func(string) error { return nil }
	nothingShownOrCreated(t, ctx, dir, "stopped")
	s2, err := Start(ctx, Config{DataDir: dir, Passphrase: "new-pass", DisableMDNS: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if msgs, _ := s2.Messages(ch, 10); len(msgs) != 0 {
		t.Fatal("the old account's messages are readable")
	}
	if _, err := os.Lstat(deviceMarkerPath(dir)); err == nil {
		t.Fatal("the old device marker is still here")
	}
	if erasePendingIn(dir) {
		t.Fatal("the record is still here")
	}
}

// A record naming a keystore that is not the one here removes nothing but
// itself.
func TestAStaleEraseRecordKeepsTheKeystoreHere(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	s, err := Start(ctx, Config{DataDir: dir, Passphrase: "test-pass", DisableMDNS: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if err := os.WriteFile(eraseRecordPath(dir), []byte(`{"keystore":"00"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s2, err := Start(ctx, Config{DataDir: dir, Passphrase: "test-pass", DisableMDNS: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = s2.Close()
	if erasePendingIn(dir) || !HasIdentity(dir) {
		t.Fatal("the stale record was acted on, or left")
	}
}

func keystoreFileHere(dir string) bool {
	_, err := os.Lstat(keystorePathIn(dir))
	return err == nil
}
