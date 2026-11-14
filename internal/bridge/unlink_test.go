package bridge

import (
	"encoding/json"
	"errors"
	"testing"

	appsvc "github.com/ZahakJ/concord/internal/app"
)

// A pending or failed removal is part of a successful unlink, in a shape the
// screen can read without parsing an error string. Only a real error is one.
func TestUnlinkResultIsStructured(t *testing.T) {
	r, err := unlinkResult(nil)
	if err != nil {
		t.Fatalf("complete unlink: %v", err)
	}
	if got, _ := json.Marshal(r); string(got) != `{"pending":[],"failed":[]}` {
		t.Fatalf("complete unlink: %s", got)
	}

	r, err = unlinkResult(&appsvc.UnlinkIncompleteError{
		Pending: []appsvc.UnlinkSpot{{Kind: "dm", ID: "g1", Name: "Carol", RemovableBy: "Carol"}},
		Failed:  []appsvc.UnlinkSpot{{Kind: "guild", ID: "g2", Name: "Book club"}},
	})
	if err != nil {
		t.Fatalf("incomplete unlink came back as an error: %v", err)
	}
	const want = `{"pending":[{"kind":"dm","id":"g1","name":"Carol","removableBy":"Carol"}],"failed":[{"kind":"guild","id":"g2","name":"Book club"}]}`
	if got, _ := json.Marshal(r); string(got) != want {
		t.Fatalf("incomplete unlink:\n got %s\nwant %s", got, want)
	}

	if _, err := unlinkResult(errors.New("app: not a device key")); err == nil {
		t.Fatal("a real failure came back as a result")
	}
}
