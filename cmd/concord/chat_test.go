package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/store"
)

type fakeChat struct {
	guilds  []domain.Guild
	msgs    map[string][]domain.Message
	marks   map[string]int64
	unread  map[string]int
	last    map[string]int64
	me      []byte
	sends   []string // "channel|content|replyTo|sendID"
	reacts  []string
	edits   []string
	handler func(domain.Message)
	onWatch chan struct{} // closed once a handler is set, if not nil
}

func (f *fakeChat) Guilds() []domain.Guild { return f.guilds }
func (f *fakeChat) Messages(ch string, limit int) ([]domain.Message, error) {
	return f.msgs[ch], nil
}
func (f *fakeChat) ReadState() (map[string]int64, error) { return f.marks, nil }
func (f *fakeChat) UnreadCounts(map[string]int64) (map[string]int, error) {
	return f.unread, nil
}
func (f *fakeChat) ChannelsLastActivity([]string) map[string]int64 { return f.last }
func (f *fakeChat) SearchMessages(q string, limit int, filter ...store.SearchFilter) ([]domain.Message, error) {
	var out []domain.Message
	for ch, ms := range f.msgs {
		if len(filter) > 0 && filter[0].InChannel != ch {
			continue
		}
		for _, m := range ms {
			if strings.Contains(strings.ToLower(m.Content), strings.ToLower(q)) {
				out = append(out, m)
			}
		}
	}
	return out, nil
}
func (f *fakeChat) SendMessageOnce(ch, content, replyTo, dir, sendID string) (domain.Message, error) {
	f.sends = append(f.sends, strings.Join([]string{ch, content, replyTo, sendID}, "|"))
	return domain.Message{ID: "feedbeef00000000", ChannelID: ch, Content: content, Sender: f.me}, nil
}
func (f *fakeChat) ToggleReaction(ch, target, emoji string, sendID ...string) error {
	f.reacts = append(f.reacts, strings.Join(append([]string{ch, target, emoji}, sendID...), "|"))
	return nil
}
func (f *fakeChat) EditMessage(ch, target, text, dir string, sendID ...string) error {
	f.edits = append(f.edits, strings.Join(append([]string{ch, target, text}, sendID...), "|"))
	return nil
}
func (f *fakeChat) OnMessage(fn func(domain.Message)) {
	f.handler = fn
	if f.onWatch != nil {
		close(f.onWatch)
	}
}
func (f *fakeChat) PublicKey() []byte   { return f.me }
func (f *fakeChat) Fingerprint() string { return "fpr-me" }

var t0 = time.Date(2026, 11, 26, 18, 25, 0, 0, time.Local)

func lantern() *fakeChat {
	me := []byte("alice-key")
	bashir := []byte("bashir-key")
	b64 := base64.RawURLEncoding.EncodeToString
	blob := strings.Repeat("ab", 32)
	return &fakeChat{
		me: me,
		guilds: []domain.Guild{
			{ID: "g1", Name: "Lantern Makers", Channels: []domain.Channel{{ID: "c-gen", Name: "general"}, {ID: "c-ideas", Name: "ideas"}, {ID: "c-v", Name: "Lounge", Type: "voice"}}},
			{ID: "g2", Name: "Bashir", Kind: "dm", Channels: []domain.Channel{{ID: "c-dm", Name: "dm"}}},
		},
		msgs: map[string][]domain.Message{
			"c-gen": {
				{ID: "a1b2c3d4e5", ChannelID: "c-gen", Sender: bashir, Name: "Bashir", Content: "Kiln is warm, first batch at ten.", Sent: t0, Reactions: map[string][]string{"👍": {"fpr-me", "x"}, "🔥": {"y"}}},
				{ID: "b2c3d4e5f6", ChannelID: "c-gen", Sender: bashir, Name: "Bashir", Content: "Bring the frosted glass.", Sent: t0.Add(time.Minute)},
				{ID: "react-act", ChannelID: "c-gen", Sender: bashir, Kind: "reaction", Content: "👍", ReplyTo: "a1b2c3d4e5", Sent: t0.Add(90 * time.Second)},
				{ID: "c3d4e5f6a7", ChannelID: "c-gen", Sender: me, Name: "Alice", Content: "On it\n[Glaze](https://en.wikipedia.org/wiki/Ceramic_glaze) · wikipedia.org", ReplyTo: "a1b2c3d4e5", Sent: t0.Add(2 * time.Minute), Edited: true},
				{ID: "d4e5f6a7b8", ChannelID: "c-gen", Sender: bashir, Name: "Bash\x1b]0;pwned\x07ir", Content: "photos \x1b[2Jhere\n![image](concord://attach/v1/" + blob + "/k/jpeg/4x4)\n[file](concord://file/v1/" + blob + "/k/2048/" + b64([]byte("text/plain")) + "/" + b64([]byte("cut list.txt")) + ")", Sent: t0.Add(3 * time.Minute)},
			},
		},
		marks:  map[string]int64{},
		unread: map[string]int{"c-gen": 3, "c-dm": 0, "c-ideas": 0},
		last:   map[string]int64{"c-gen": t0.UnixMilli(), "c-dm": t0.Add(-time.Hour).UnixMilli(), "c-ideas": t0.Add(-48 * time.Hour).UnixMilli()},
	}
}

func runChat(t *testing.T, f *fakeChat, color bool, cmd string, args ...string) string {
	t.Helper()
	o, err := parseChatArgs(cmd, args, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("%s %v: %v", cmd, args, err)
	}
	var out bytes.Buffer
	c := &chatCLI{svc: f, out: &out, st: style{on: color, truec: true}, o: o, now: func() time.Time { return t0.Add(5 * time.Minute) }}
	if err := c.run(context.Background(), cmd); err != nil {
		t.Fatalf("%s %v: %v", cmd, args, err)
	}
	return out.String()
}

// Piped, chats is one tab-separated line per chat, unread first, with no
// escape codes, and voice channels are not chats.
func TestChatsListsUnreadFirstInPlainLines(t *testing.T) {
	out := runChat(t, lantern(), false, "chats")
	want := "Lantern Makers/#general\t3\t5m\nBashir/#dm\t0\t1h\nLantern Makers/#ideas\t0\t2d\n"
	if out != want {
		t.Fatalf("chats:\n%q\nwant\n%q", out, want)
	}
}

func TestChatsOnATerminalWearTheAppsColours(t *testing.T) {
	out := runChat(t, lantern(), true, "chats")
	if !strings.Contains(out, "\x1b[38;2;20;163;148m") {
		t.Fatalf("no accent colour (#14a394) in:\n%s", out)
	}
	if !strings.Contains(out, " 3 ") || !strings.Contains(out, "@Bashir") {
		t.Fatalf("no unread badge or DM name in:\n%s", out)
	}
}

func TestReadRendersNamesTimesRepliesReactionsAndAttachments(t *testing.T) {
	out := runChat(t, lantern(), true, "read", "#general")
	for _, want := range []string{
		"Bashir", "18:25", "a1b2c3d4", // header: name, time, short id
		"Kiln is warm",
		"👍 2", "🔥 1", // reactions, most first
		"╭", "Bashir: Kiln is warm", // the reply it answers
		"You", "(edited)",
		"🔗", "Glaze — wikipedia.org", // a card
		"🖼", "picture", "📎", "cut list.txt · 2 KB", // a picture and a file
	} {
		if !strings.Contains(out, want) {
			t.Errorf("read lacks %q:\n%s", want, out)
		}
	}
	// The second message from Bashir a minute later is grouped under the first.
	if strings.Contains(out, "b2c3d4e5") {
		t.Errorf("a run of messages was not grouped under one header:\n%s", out)
	}
	if strings.Contains(out, "concord://") || strings.Contains(out, "react-act") {
		t.Errorf("a token or an action leaked into the feed:\n%s", out)
	}
	// What a stranger wrote cannot drive the terminal.
	if strings.Contains(out, "\x1b]0;") || strings.Contains(out, "\x1b[2J") || strings.Contains(out, "\x07") {
		t.Errorf("control sequences from a message reached the terminal: %q", out)
	}
}

func TestReadPipedIsOneLinePerMessage(t *testing.T) {
	out := runChat(t, lantern(), false, "read", "Lantern Makers/#general", "-n", "2")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 || strings.Contains(out, "\x1b") {
		t.Fatalf("read -n 2 piped:\n%q", out)
	}
	if !strings.HasPrefix(lines[0], "c3d4e5f6\t") || !strings.Contains(lines[0], "reply-to:a1b2c3d4") {
		t.Fatalf("first line: %q", lines[0])
	}
}

func TestReadJSONIsOneObjectPerLine(t *testing.T) {
	out := runChat(t, lantern(), true, "read", "--json", "#general")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 4 {
		t.Fatalf("%d lines", len(lines))
	}
	var m chatMessageJSON
	if err := json.Unmarshal([]byte(lines[0]), &m); err != nil || m.ID != "a1b2c3d4e5" || m.Reactions["👍"] != 2 || m.Chat != "Lantern Makers/#general" {
		t.Fatalf("first line %s: %+v %v", lines[0], m, err)
	}
}

// Each send, reaction and edit carries a send id of its own, so the core
// sends it once whatever happens to the answer.
func TestSendReplyReactAndEditCarryASendID(t *testing.T) {
	f := lantern()
	runChat(t, f, false, "send", "#general", "Kiln", "is", "ready")
	runChat(t, f, false, "reply", "#general", "a1b2", "On my way")
	runChat(t, f, false, "react", "#general", "last", "🔥")
	runChat(t, f, false, "edit", "#general", "c3d4e5", "On it, with glaze")
	if len(f.sends) != 2 || !strings.HasPrefix(f.sends[0], "c-gen|Kiln is ready||cli-") || !strings.HasPrefix(f.sends[1], "c-gen|On my way|a1b2c3d4e5|cli-") {
		t.Fatalf("sends: %q", f.sends)
	}
	if len(f.reacts) != 1 || !strings.HasPrefix(f.reacts[0], "c-gen|d4e5f6a7b8|🔥|cli-") {
		t.Fatalf("reacts: %q", f.reacts)
	}
	if len(f.edits) != 1 || !strings.HasPrefix(f.edits[0], "c-gen|c3d4e5f6a7|On it, with glaze|cli-") {
		t.Fatalf("edits: %q", f.edits)
	}
	if strings.Split(f.sends[0], "|")[3] == strings.Split(f.sends[1], "|")[3] {
		t.Fatal("two commands shared a send id")
	}
}

func TestEditRefusesSomeoneElsesMessage(t *testing.T) {
	f := lantern()
	o, _ := parseChatArgs("edit", []string{"#general", "a1b2", "x"}, &bytes.Buffer{})
	c := &chatCLI{svc: f, out: &bytes.Buffer{}, o: o, now: time.Now}
	if err := c.run(context.Background(), "edit"); err == nil || !strings.Contains(err.Error(), "only your own") {
		t.Fatalf("err = %v", err)
	}
}

func TestSearchFindsAndHighlights(t *testing.T) {
	out := runChat(t, lantern(), true, "search", "frosted")
	if !strings.Contains(out, "Lantern Makers #general") || !strings.Contains(out, "\x1b[1;4m") {
		t.Fatalf("search:\n%s", out)
	}
	if out := runChat(t, lantern(), false, "search", "nothing-like-this"); !strings.Contains(out, "No messages match") {
		t.Fatalf("no hits: %q", out)
	}
}

func TestWatchPrintsNewMessagesOnce(t *testing.T) {
	f := lantern()
	f.onWatch = make(chan struct{})
	o, _ := parseChatArgs("watch", []string{"#general"}, &bytes.Buffer{})
	var out bytes.Buffer
	c := &chatCLI{svc: f, out: &out, o: o, now: time.Now}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- c.run(ctx, "watch") }()
	<-f.onWatch
	m := domain.Message{ID: "e5f6a7b8c9", ChannelID: "c-gen", Sender: []byte("b"), Name: "Bashir", Content: "Stands are in the van", Sent: time.Now()}
	f.handler(m)
	f.handler(m) // re-emitted by a reaction
	f.handler(domain.Message{ID: "other", ChannelID: "c-ideas", Content: "elsewhere"})
	f.handler(domain.Message{ID: "old", ChannelID: "c-gen", Content: "synced", Backfill: true})
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done
	if got := out.String(); strings.Count(got, "Stands are in the van") != 1 || strings.Contains(got, "elsewhere") || strings.Contains(got, "synced") {
		t.Fatalf("watch:\n%s", got)
	}
}

func TestChatArgumentsAndHelp(t *testing.T) {
	if _, err := parseChatArgs("read", nil, &bytes.Buffer{}); err == nil {
		t.Fatal("read without a chat")
	}
	if _, err := parseChatArgs("read", []string{"#g", "--color", "rainbow"}, &bytes.Buffer{}); err == nil {
		t.Fatal("a bad --color")
	}
	o, err := parseChatArgs("send", []string{"#g", "--", "-5 degrees", "outside"}, &bytes.Buffer{})
	if err != nil || strings.Join(o.args, "|") != "#g|-5 degrees|outside" {
		t.Fatalf("after --: %v %q", err, o.args)
	}
	var out bytes.Buffer
	if code := runChatCommand("read", []string{"--help"}, nil, &out, &bytes.Buffer{}); code != 0 || out.String() != chatUsage {
		t.Fatalf("--help: %d", code)
	}
	for _, c := range []string{"chats", "read", "send", "reply", "react", "edit", "search", "watch", "--json", "--color", "NO_COLOR"} {
		if !strings.Contains(chatUsage, c) {
			t.Errorf("help lacks %s", c)
		}
	}
	for c := range chatCommands {
		if !strings.Contains(usage, c) {
			t.Errorf("concord --help lacks the %s command", c)
		}
	}
}

func TestFindChatTakesNamesAndDMs(t *testing.T) {
	g := lantern().guilds
	for ref, want := range map[string]string{"#general": "c-gen", "Lantern Makers/#ideas": "c-ideas", "@Bashir": "c-dm", "bashir": "c-dm", "lantern makers": "c-gen", "c-ideas": "c-ideas"} {
		_, ch, err := findChat(g, ref)
		if err != nil || ch.ID != want {
			t.Errorf("%q: %v %s", ref, err, ch.ID)
		}
	}
	if _, _, err := findChat(g, "#nowhere"); err == nil {
		t.Error("an unknown chat")
	}
}

func TestPlainOutputWithoutATerminal(t *testing.T) {
	var b bytes.Buffer
	if newStyle(&b, "auto").on {
		t.Fatal("colour on a buffer")
	}
	t.Setenv("NO_COLOR", "1")
	if newStyle(&b, "auto").on {
		t.Fatal("colour with NO_COLOR")
	}
	if !newStyle(&b, "always").on {
		t.Fatal("--color always")
	}
}
