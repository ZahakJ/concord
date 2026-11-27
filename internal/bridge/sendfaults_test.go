package bridge

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/store"
)

// The phone's path to a send is DispatchJSON, the string-in/string-out form
// gomobile binds. A failing disk on it is answered as on the desktop: a send
// refused before it went out reports the disk's error, a retry under the same
// send id is one message, and a reaction retried under its id is one reaction.
func TestThePhonePathSendsOnceThroughAFailingDisk(t *testing.T) {
	b := slotBridge(t)
	call := func(method string, args ...any) (json.RawMessage, string) {
		t.Helper()
		a, _ := json.Marshal(args)
		var out struct {
			Result json.RawMessage `json:"result"`
			Error  string          `json:"error"`
		}
		if err := json.Unmarshal([]byte(b.DispatchJSON(method, string(a))), &out); err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		return out.Result, out.Error
	}
	res, e := call("CreateGuild", "phone")
	if e != "" {
		t.Fatal(e)
	}
	var g struct {
		Channels []struct {
			ID string `json:"id"`
		} `json:"channels"`
	}
	_ = json.Unmarshal(res, &g)
	if len(g.Channels) == 0 {
		t.Fatalf("no channel in %s", res)
	}
	ch := g.Channels[0].ID
	svc, err := b.service()
	if err != nil {
		t.Fatal(err)
	}
	defer svc.InjectStoreFault(nil)
	held := func(body string) (id string, n int) {
		res, e := call("Messages", ch)
		if e != "" {
			t.Fatal(e)
		}
		var ms []struct {
			ID        string              `json:"id"`
			Content   string              `json:"content"`
			Reactions map[string][]string `json:"reactions"`
		}
		_ = json.Unmarshal(res, &ms)
		for _, m := range ms {
			if m.Content == body || strings.HasSuffix(m.Content, "\n"+body) {
				id, n = m.ID, n+1
			}
		}
		return id, n
	}

	for _, tc := range []struct {
		op, err string
	}{
		{store.OpReadMessage, "disk I/O error (10)"},
		{store.OpSaveAttachment, "database or disk is full (13)"},
		{store.OpSaveMessage, "disk I/O error (10)"},
	} {
		body := "from the phone, " + tc.op
		var armed atomic.Bool
		armed.Store(true)
		svc.InjectStoreFault(func(op string) error {
			if armed.Load() && op == tc.op {
				return errors.New(tc.err)
			}
			return nil
		})
		send := func() string {
			if tc.op == store.OpSaveAttachment {
				_, e := call("SendAttachment", ch, tinyPNG(t), 4, 4, "", false, "", "", body, "", "sid-"+tc.op)
				return e
			}
			_, e := call("SendMessage", ch, body, "", "", "sid-"+tc.op)
			return e
		}
		e := send()
		switch tc.op {
		case store.OpSaveMessage:
			// Past the publish: it went out, and is no failure.
			if e != "" {
				t.Fatalf("%s: a message that went out was reported as failed: %s", tc.op, e)
			}
		case store.OpSaveAttachment:
			if !strings.Contains(e, "disk is full") {
				t.Fatalf("%s: the send did not report the disk: %q", tc.op, e)
			}
		case store.OpReadMessage:
			if !strings.Contains(e, "disk I/O error") {
				t.Fatalf("%s: the send did not report the disk: %q", tc.op, e)
			}
		}
		armed.Store(false)
		for i := 0; i < 2; i++ {
			if e := send(); e != "" {
				t.Fatalf("%s: retry %d: %s", tc.op, i, e)
			}
		}
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, n := held(body); n == 1 {
				break
			} else if n > 1 || time.Now().After(deadline) {
				t.Fatalf("%s: the phone holds the message %d times", tc.op, n)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	// A reaction: the core's copy fails after it went out, and the tap is
	// retried twice. It ends on, once.
	target, _ := held("from the phone, " + store.OpSaveMessage)
	var armed atomic.Bool
	armed.Store(true)
	svc.InjectStoreFault(func(op string) error {
		if armed.Load() && op == store.OpReact {
			return errors.New("disk I/O error (10)")
		}
		return nil
	})
	for i := 0; i < 2; i++ {
		if _, e := call("ToggleReaction", ch, target, "👍", "tap-1"); e != "" {
			t.Fatalf("reaction attempt %d: %s", i, e)
		}
	}
	armed.Store(false)
	on := func() int {
		res, _ := call("Messages", ch)
		var ms []struct {
			ID        string              `json:"id"`
			Reactions map[string][]string `json:"reactions"`
		}
		_ = json.Unmarshal(res, &ms)
		for _, m := range ms {
			if m.ID == target {
				return len(m.Reactions["👍"])
			}
		}
		return -1
	}
	deadline := time.Now().Add(10 * time.Second)
	for on() != 1 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if _, e := call("ToggleReaction", ch, target, "👍", "tap-1"); e != "" {
		t.Fatal(e)
	}
	time.Sleep(300 * time.Millisecond)
	if n := on(); n != 1 {
		t.Fatalf("the reaction is on %d times after the retries, want 1", n)
	}
	// A new tap is a new action, and takes it off.
	if _, e := call("ToggleReaction", ch, target, "👍", "tap-2"); e != "" {
		t.Fatal(e)
	}
	if n := on(); n != 0 {
		t.Fatalf("a second tap left the reaction on %d times", n)
	}
}

func tinyPNG(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for i := range 16 {
		img.Set(i%4, i/4, color.RGBA{uint8(i * 15), 80, 160, 255})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}
