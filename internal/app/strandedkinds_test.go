package app

import (
	"os"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/identity"
	"github.com/ZahakJ/concord/internal/store"
)

// The first screen tells three kinds of data without a keystore apart: data
// the account's own phrase keeps, a linked device's data, and a never-linked
// install with nothing to check a phrase against. The last is neither
// keepable nor linked, and must not be reported as linked.
func TestTheFirstScreenTellsLinkedDataFromDataItCannotCheck(t *testing.T) {
	keepable := t.TempDir()
	seedDevice(t, keepable, "pw", "Ada", false)

	linked := t.TempDir()
	bea := seedDevice(t, linked, "pw", "Bea", false)
	dev, _ := identity.Generate()
	if err := saveDeviceMarker(linked, bea.IssueDeviceCertFor(dev.DevicePublicKey(), "phone", time.Now().Unix())); err != nil {
		t.Fatal(err)
	}

	unchecked := t.TempDir()
	cy, err := identity.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.CreateKeystore(keystorePathIn(unchecked), "pw", cy); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(dbPathIn(unchecked), deriveStoreKey(cy))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting("display_name", "Cy"); err != nil {
		t.Fatal(err)
	}
	st.Close()

	for _, c := range []struct {
		name               string
		dir                string
		keepable, isLinked bool
	}{
		{"never linked, with a message", keepable, true, false},
		{"linked", linked, false, true},
		{"never linked, no message", unchecked, false, false},
	} {
		if err := os.Remove(keystorePathIn(c.dir)); err != nil {
			t.Fatal(err)
		}
		stranded, keep, isLinked := DataWithoutKeystore(c.dir)
		if !stranded || keep != c.keepable || isLinked != c.isLinked {
			t.Errorf("%s: stranded %v keepable %v linked %v, want true %v %v", c.name, stranded, keep, isLinked, c.keepable, c.isLinked)
		}
	}
}
