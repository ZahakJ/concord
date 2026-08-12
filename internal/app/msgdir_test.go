package app

import (
	"context"
	"testing"

	"github.com/ZahakJ/concord/internal/domain"
)

// A message's base direction has to survive being sent, stored and read back,
// because the whole reason it travels at all is that the reader cannot work it
// out: the per-line heuristic is exactly what the author overrode. A direction
// that were dropped anywhere along this path would render the message the way
// the author explicitly said not to, silently and only for other people.
func TestMessageDirectionSurvivesStorage(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := startServiceInDir(t, ctx, t.TempDir())
	g, err := svc.CreateGuild("Bidi")
	if err != nil {
		t.Fatalf("create guild: %v", err)
	}
	ch := g.Channels[0].ID

	// One of each, in one channel: the two overrides and the default. The
	// default is in here on purpose — "" has to stay "" rather than being
	// helpfully resolved to something, or every message ever written before
	// this field existed changes appearance the first time it is re-read.
	want := map[string]string{
		"مرحبا hello":   "rtl",
		"hello مرحبا":   "ltr",
		"no preference": "",
	}
	for body, dir := range want {
		if _, err := svc.SendMessage(ch, body, "", dir); err != nil {
			t.Fatalf("send %q: %v", body, err)
		}
	}

	msgs, err := svc.Messages(ch, 0)
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	seen := 0
	for _, m := range msgs {
		exp, ok := want[m.Content]
		if !ok {
			continue // the channel's own creation notice
		}
		seen++
		if m.Dir != exp {
			t.Errorf("message %q: dir = %q, want %q", m.Content, m.Dir, exp)
		}
	}
	if seen != len(want) {
		t.Fatalf("read back %d of the %d messages sent", seen, len(want))
	}
}

// Dir arrives inside a message from another member and ends up as a `dir`
// attribute in every client's DOM, so it is bounded on the way in and on the
// way out. The fallback has to be "" — the behaviour every message had before
// the field existed — and not an error or a guess: a peer sending nonsense
// should get the per-line heuristic, not a broken channel.
func TestValidDirFailsClosed(t *testing.T) {
	for _, ok := range []string{"rtl", "ltr"} {
		if got := domain.ValidDir(ok); got != ok {
			t.Errorf("ValidDir(%q) = %q, want it kept", ok, got)
		}
	}
	for _, bad := range []string{
		"", "RTL", "auto", "ltr ", "rtl;drop", "inherit",
		`" onload="alert(1)`, "rtlrtl", "0",
	} {
		if got := domain.ValidDir(bad); got != "" {
			t.Errorf("ValidDir(%q) = %q, want \"\"", bad, got)
		}
	}
}

// The direction a sender asks for is bounded by the SENDING node too, not only
// on receipt. A caller inside this process (a script driving the RPC, a future
// bridge method) is not more trusted than the network for a value that becomes
// markup on someone else's screen.
func TestSendBoundsDirectionOnTheWayOut(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := startServiceInDir(t, ctx, t.TempDir())
	g, err := svc.CreateGuild("Bounds")
	if err != nil {
		t.Fatalf("create guild: %v", err)
	}
	ch := g.Channels[0].ID

	m, err := svc.SendMessage(ch, "hello", "", `" onload="alert(1)`)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if m.Dir != "" {
		t.Fatalf("a junk direction reached the message as %q; it must be dropped", m.Dir)
	}
}
