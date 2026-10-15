package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

func TestParseBotCommand(t *testing.T) {
	cases := []struct {
		line    string
		cmd     string
		id      string // raw id expected back, even on error
		wantErr string
	}{
		{line: "", cmd: ""},
		{line: "   \r", cmd: ""},
		{line: `{"cmd":"list"}`, cmd: "list"},
		{line: `{"cmd":" LIST ","id":7}`, cmd: "list", id: "7"},
		{line: `{"cmd":"send","channel":"g/#general","text":"hi","replyTo":"m1","id":"a"}`, cmd: "send", id: `"a"`},
		{line: `{"cmd":"send","channel":"g/#general","id":"a"}`, id: `"a"`, wantErr: `missing "text"`},
		{line: `{"cmd":"send","text":"hi"}`, wantErr: `missing "channel"`},
		{line: `{"cmd":"send","channel":"c","text":"   "}`, wantErr: `missing "text"`},
		{line: `{"cmd":"react","channel":"c","message":"m"}`, wantErr: `missing "emoji"`},
		{line: `{"cmd":"react","channel":"c","message":"m","emoji":"👍"}`, cmd: "react"},
		{line: `{"cmd":"join"}`, wantErr: `missing "invite"`},
		{line: `{"cmd":"join","invite":"concord:abc"}`, cmd: "join"},
		{line: `{"cmd":"members","guild":"g"}`, cmd: "members"},
		{line: `{"cmd":"name","text":"dicebot"}`, cmd: "name"},
		{line: `{"cmd":"quit"}`, cmd: "quit"},
		{line: `{"cmd":"explode","id":3}`, id: "3", wantErr: "unknown cmd"},
		{line: `{"id":4}`, id: "4", wantErr: `missing "cmd"`},
		{line: `{"cmd":"send","chanel":"x","text":"t","id":5}`, id: "5", wantErr: "unknown field"},
		{line: `not json`, wantErr: "bad command line"},
		{line: `{"cmd":"list"} {"cmd":"list"}`, wantErr: "one JSON object per line"},
		{line: `["cmd","list"]`, wantErr: "bad command line"},
		{line: `{"cmd":"send","channel":"c","text":"t","id":{"n":1}}`, cmd: "send", id: `{"n":1}`},
	}
	for _, tc := range cases {
		c, err := parseBotCommand([]byte(tc.line))
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("%s: err = %v, want containing %q", tc.line, err, tc.wantErr)
			}
		} else if err != nil {
			t.Errorf("%s: unexpected error %v", tc.line, err)
		} else if c.Cmd != tc.cmd {
			t.Errorf("%s: cmd = %q, want %q", tc.line, c.Cmd, tc.cmd)
		}
		if string(c.ID) != tc.id {
			t.Errorf("%s: id = %s, want %s", tc.line, c.ID, tc.id)
		}
	}
}

func TestEncodeLineOrdersTypeAndID(t *testing.T) {
	got, err := encodeLine(map[string]any{"z": 1, "error": "a<b", "id": json.RawMessage(`"q"`), "type": "error"})
	if err != nil || string(got) != `{"type":"error","id":"q","error":"a<b","z":1}`+"\n" {
		t.Fatalf("got %s, %v", got, err)
	}
	got, _ = encodeLine(botMessageEvent{Type: "message", Content: "x"})
	if !strings.HasPrefix(string(got), `{"type":"message",`) || !strings.HasSuffix(string(got), "}\n") {
		t.Fatalf("struct line = %s", got)
	}
}

func testGuilds() []domain.Guild {
	return []domain.Guild{
		{ID: "g1", Name: "Tavern", Channels: []domain.Channel{{ID: "c1", Name: "general"}, {ID: "c2", Name: "dice"}}},
		{ID: "g2", Name: "Works", Channels: []domain.Channel{{ID: "c3", Name: "general"}, {ID: "c4", Name: "CI"}}},
		{ID: "g3", Name: "a/b", Channels: []domain.Channel{{ID: "c5", Name: "x"}}},
	}
}

func TestResolveChannel(t *testing.T) {
	gs := testGuilds()
	ok := map[string]string{
		"c3":               "c3",
		"Tavern/#general":  "c1",
		"Tavern/general":   "c1",
		"tavern/#GENERAL":  "c1",
		"g2/#general":      "c3",
		"#dice":            "c2",
		"#ci":              "c4",
		"Works/#CI":        "c4",
		" Tavern / #dice ": "c2",
		"a/b/#x":           "c5",
	}
	for ref, want := range ok {
		_, c, err := resolveChannel(gs, ref)
		if err != nil || c.ID != want {
			t.Errorf("resolve %q = %q, %v; want %q", ref, c.ID, err, want)
		}
	}
	for ref, wantErr := range map[string]string{
		"#general":     "ambiguous",
		"general":      "no channel",
		"Nope/#dice":   "no channel",
		"Tavern/#nope": "no channel",
		"":             "no channel",
	} {
		if _, _, err := resolveChannel(gs, ref); err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("resolve %q: err = %v, want %q", ref, err, wantErr)
		}
	}
}

func TestReadLineBoundsAndFinalLine(t *testing.T) {
	long := strings.Repeat("x", 100)
	r := bufio.NewReaderSize(strings.NewReader("a\n"+long+"\nb\nlast"), 16)
	want := []struct {
		s   string
		err error
	}{{"a\n", nil}, {"", errLineTooLong}, {"b\n", nil}, {"last", nil}, {"", io.EOF}}
	for i, w := range want {
		got, err := readLine(r, 50)
		if string(got) != w.s || !errors.Is(err, w.err) {
			t.Fatalf("line %d: %q, %v; want %q, %v", i, got, err, w.s, w.err)
		}
	}
}

// fakeService is a botService with no network: sends are recorded and echoed
// back through the message feed, as the real Service does.
type fakeService struct {
	mu       sync.Mutex
	guilds   []domain.Guild
	members  map[string][]string
	onMsg    []func(domain.Message)
	onUpdate []func()
	sent     []domain.Message
	reacts   []string
	name     string
	joinErr  error
}

func (f *fakeService) Fingerprint() string { return "SELF" }
func (f *fakeService) PeerID() string      { return "12D3KooSelf" }
func (f *fakeService) DisplayName() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.name
}
func (f *fakeService) SetDisplayName(n string) error {
	f.mu.Lock()
	f.name = n
	f.mu.Unlock()
	return nil
}
func (f *fakeService) Guilds() []domain.Guild {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.Guild(nil), f.guilds...)
}
func (f *fakeService) SendMessage(ch, content, replyTo, dir string) (domain.Message, error) {
	m := domain.Message{ID: "own-" + content, ChannelID: ch, Sender: []byte("SELF"), Content: content, ReplyTo: replyTo, Sent: time.Now()}
	f.mu.Lock()
	f.sent = append(f.sent, m)
	f.mu.Unlock()
	f.deliver(m)
	return m, nil
}
func (f *fakeService) ToggleReaction(ch, target, emoji string) error {
	f.mu.Lock()
	f.reacts = append(f.reacts, ch+"/"+target+"/"+emoji)
	f.mu.Unlock()
	return nil
}
func (f *fakeService) JoinViaInvite(code string) (domain.Guild, error) {
	if f.joinErr != nil {
		return domain.Guild{}, f.joinErr
	}
	g := domain.Guild{ID: "g9", Name: "Joined", Channels: []domain.Channel{{ID: "c9", Name: "general"}}}
	f.mu.Lock()
	f.guilds = append(f.guilds, g)
	f.members["g9"] = []string{"SELF", "OWNER"}
	f.mu.Unlock()
	f.fireUpdate()
	return g, nil
}
func (f *fakeService) GuildMembers(gid string) ([][]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out [][]byte
	for _, m := range f.members[gid] {
		out = append(out, []byte(m))
	}
	return out, nil
}
func (f *fakeService) AccountFingerprintOf(cred []byte) string { return string(cred) }
func (f *fakeService) ProfileName(fpr string) string           { return strings.ToLower(fpr) }
func (f *fakeService) NickOf(gid, fpr string) string           { return "" }
func (f *fakeService) SenderBlocked(s []byte) bool             { return string(s) == "BLOCKED" }
func (f *fakeService) OnMessage(fn func(domain.Message)) {
	f.mu.Lock()
	f.onMsg = append(f.onMsg, fn)
	f.mu.Unlock()
}
func (f *fakeService) OnGuildUpdate(fn func()) {
	f.mu.Lock()
	f.onUpdate = append(f.onUpdate, fn)
	f.mu.Unlock()
}
func (f *fakeService) deliver(m domain.Message) {
	f.mu.Lock()
	cbs := append([]func(domain.Message){}, f.onMsg...)
	f.mu.Unlock()
	for _, cb := range cbs {
		cb(m)
	}
}
func (f *fakeService) fireUpdate() {
	f.mu.Lock()
	cbs := append([]func(){}, f.onUpdate...)
	f.mu.Unlock()
	for _, cb := range cbs {
		cb()
	}
}

// botHarness runs a bot over pipes and reads its stdout line by line.
type botHarness struct {
	t     *testing.T
	in    *io.PipeWriter
	lines chan map[string]any
	done  chan error
}

func startBot(t *testing.T, ctx context.Context, svc botService) *botHarness {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	h := &botHarness{t: t, in: inW, lines: make(chan map[string]any, 256), done: make(chan error, 1)}
	go func() {
		err := newBot(svc).run(ctx, inR, outW)
		_ = outW.Close()
		h.done <- err
	}()
	go func() {
		sc := bufio.NewScanner(outR)
		for sc.Scan() {
			var v map[string]any
			if err := json.Unmarshal(sc.Bytes(), &v); err != nil {
				t.Errorf("stdout line is not JSON: %q", sc.Text())
				continue
			}
			h.lines <- v
		}
		close(h.lines)
	}()
	t.Cleanup(func() { _ = inW.Close() })
	return h
}

func (h *botHarness) send(line string) {
	h.t.Helper()
	if _, err := io.WriteString(h.in, line+"\n"); err != nil {
		h.t.Fatalf("write %q: %v", line, err)
	}
}

// next returns the next line, failing the test after a timeout.
func (h *botHarness) next(timeout time.Duration) map[string]any {
	h.t.Helper()
	select {
	case v, ok := <-h.lines:
		if !ok {
			h.t.Fatal("bot stdout closed")
		}
		return v
	case <-time.After(timeout):
		h.t.Fatal("timed out waiting for a bot line")
	}
	return nil
}

// expect returns the next line, failing unless it has the given type.
func (h *botHarness) expect(typ string) map[string]any {
	h.t.Helper()
	v := h.next(5 * time.Second)
	if v["type"] != typ {
		h.t.Fatalf("got %v, want type %q", v, typ)
	}
	return v
}

func TestBotProtocolAgainstFakeService(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := &fakeService{guilds: testGuilds()[:2], members: map[string][]string{"g1": {"SELF", "ALICE"}, "g2": {"SELF"}}, name: "bot"}
	h := startBot(t, ctx, f)

	ready := h.expect("ready")
	if ready["fingerprint"] != "SELF" || ready["peerId"] != "12D3KooSelf" || len(ready["guilds"].([]any)) != 2 {
		t.Fatalf("ready = %v", ready)
	}

	// Malformed and unknown input: an error each, id echoed, still alive.
	h.send(`this is not json`)
	if e := h.expect("error"); e["id"] != nil {
		t.Fatalf("error for an unparseable line carries an id: %v", e)
	}
	h.send(`{"cmd":"frobnicate","id":"q1"}`)
	if e := h.expect("error"); e["id"] != "q1" {
		t.Fatalf("error id = %v, want q1", e["id"])
	}
	h.send(`{"cmd":"send","channel":"#general","text":"hi","id":2}`)
	if e := h.expect("error"); e["id"] != float64(2) || !strings.Contains(e["error"].(string), "ambiguous") {
		t.Fatalf("ambiguous send = %v", e)
	}
	h.send(strings.Repeat("y", maxBotLine+10))
	if e := h.expect("error"); !strings.Contains(e["error"].(string), "too long") {
		t.Fatalf("long line = %v", e)
	}

	// A send by name resolves, replies, and the bot's own message (echoed by
	// the feed) is NOT reported back as incoming.
	h.send(`{"cmd":"send","channel":"Tavern/#dice","text":"rolled 7","replyTo":"m0","id":"s"}`)
	sent := h.expect("sent")
	if sent["id"] != "s" || sent["channelId"] != "c2" || sent["messageId"] != "own-rolled 7" {
		t.Fatalf("sent = %v", sent)
	}
	if f.sent[0].ReplyTo != "m0" {
		t.Fatalf("replyTo not passed through: %+v", f.sent[0])
	}

	// An incoming message is reported once, with its context.
	// A reaction to a message older than the bot (the feed re-emits the
	// stored row) is not news, even though this process never saw it.
	yesterday := time.Now().Add(-24 * time.Hour)
	f.deliver(domain.Message{ID: "m-old", ChannelID: "c1", Sender: []byte("ALICE"), Content: "!ping",
		Sent: yesterday, Reactions: map[string][]string{"👍": {"ALICE"}}})

	at := time.Now().UTC().Truncate(time.Millisecond)
	in := domain.Message{ID: "m1", ChannelID: "c1", Sender: []byte("ALICE"), Name: "Alice", Content: "!roll", ReplyTo: "m0", Sent: at}
	f.deliver(in)
	msg := h.expect("message")
	want := map[string]any{"guildId": "g1", "guild": "Tavern", "channelId": "c1", "channel": "general",
		"messageId": "m1", "sender": "ALICE", "senderName": "Alice", "content": "!roll", "replyTo": "m0",
		"sent": at.Format(time.RFC3339Nano)}
	for k, v := range want {
		if msg[k] != v {
			t.Errorf("message[%s] = %v, want %v", k, msg[k], v)
		}
	}
	// Re-emits: a reaction (same content) is silent, an edit is an edit,
	// a deletion is reported once. Backfill and blocked senders are silent.
	f.deliver(in)
	edited := in
	edited.Content, edited.Edited = "!roll 2d6", true
	f.deliver(edited)
	if e := h.expect("edit"); e["content"] != "!roll 2d6" || e["messageId"] != "m1" {
		t.Fatalf("edit = %v", e)
	}
	gone := edited
	gone.Deleted, gone.Content = true, ""
	f.deliver(gone)
	f.deliver(gone)
	h.expect("delete")
	f.deliver(domain.Message{ID: "old", ChannelID: "c1", Sender: []byte("ALICE"), Content: "old news", Backfill: true, Sent: at})
	f.deliver(domain.Message{ID: "b1", ChannelID: "c1", Sender: []byte("BLOCKED"), Content: "spam", Sent: at})
	f.deliver(domain.Message{ID: "nowhere", ChannelID: "c-unknown", Sender: []byte("ALICE"), Content: "?", Sent: at})

	// react, members, name, list.
	h.send(`{"cmd":"react","channel":"c1","message":"m1","emoji":"🎲","id":"r"}`)
	if r := h.expect("ok"); r["id"] != "r" || f.reacts[0] != "c1/m1/🎲" {
		t.Fatalf("react = %v %v", r, f.reacts)
	}
	h.send(`{"cmd":"members","guild":"Tavern"}`)
	mem := h.expect("members")
	if ms := mem["members"].([]any); len(ms) != 2 || ms[1].(map[string]any)["name"] != "alice" {
		t.Fatalf("members = %v", mem)
	}
	h.send(`{"cmd":"name","text":"dicebot"}`)
	h.expect("ok")
	if f.DisplayName() != "dicebot" {
		t.Fatalf("name not set")
	}

	// Membership changes: someone joins g1, and the bot joins a guild.
	f.mu.Lock()
	f.members["g1"] = []string{"SELF", "ALICE", "BOB"}
	f.mu.Unlock()
	f.fireUpdate()
	if m := h.expect("member"); m["action"] != "join" || m["fingerprint"] != "BOB" || m["name"] != "bob" {
		t.Fatalf("member = %v", m)
	}
	h.send(`{"cmd":"join","invite":"code","id":"j"}`)
	if j := h.expect("joined"); j["id"] != "j" || j["guild"].(map[string]any)["name"] != "Joined" {
		t.Fatalf("joined = %v", j)
	}
	if g := h.expect("guild"); g["action"] != "join" || g["guildId"] != "g9" {
		t.Fatalf("guild = %v", g)
	}
	h.send(`{"cmd":"list","id":"l"}`)
	if l := h.expect("guilds"); len(l["guilds"].([]any)) != 3 {
		t.Fatalf("list = %v", l)
	}

	// quit replies and ends the session cleanly.
	h.send(`{"cmd":"quit"}`)
	h.expect("ok")
	select {
	case err := <-h.done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bot did not stop on quit")
	}
	// Nothing after the quit reply — in particular none of the silent
	// deliveries above leaked out late.
	if v, ok := <-h.lines; ok {
		t.Fatalf("unexpected line after quit: %v", v)
	}
}

func TestBotStopsOnEOFAndCancel(t *testing.T) {
	f := &fakeService{members: map[string][]string{}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := startBot(t, ctx, f)
	h.expect("ready")
	_ = h.in.Close()
	select {
	case err := <-h.done:
		if err != nil {
			t.Fatalf("EOF: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bot did not stop at EOF")
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	h2 := startBot(t, ctx2, &fakeService{members: map[string][]string{}})
	h2.expect("ready")
	cancel2()
	select {
	case err := <-h2.done:
		if err != nil {
			t.Fatalf("cancel: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bot did not stop on cancel")
	}
}
