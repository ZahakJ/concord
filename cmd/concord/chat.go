package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ZahakJ/concord/internal/app"
	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/store"
)

// The chat commands: Concord from a terminal, one command at a time.
//
//	concord chats | read | send | reply | react | edit | search | watch
//
// Each unlocks the profile, does its one thing and exits (watch runs until
// interrupted). On a terminal they print in the app's colours; piped, they
// print plain lines, and --json prints one JSON object per line.

const chatUsage = `Usage: concord <command> [options] [arguments]

Concord from a terminal. Each command unlocks your profile, does one thing and
exits; watch keeps following until you press Ctrl-C. The passphrase is asked
for, or read from CONCORD_PASSPHRASE. Close the app first: one profile is open
in one place at a time.

Commands:
  chats                         your conversations, unread first, with counts
  read <chat> [-n 30]           the latest messages in a chat
  send <chat> <text…>           send a message ("-" reads the text from stdin)
  reply <chat> <msg> <text…>    reply to a message
  react <chat> <msg> <emoji>    add your reaction, or take it off again
  edit <chat> <msg> <text…>     change one of your own messages
  search <words…> [--in <chat>] [-n 20]
                                search every chat on this device
  watch [<chat>]                follow new messages as they arrive

A <chat> is "guild/#channel", "#channel" when only one guild has it, a guild
or direct message by name (its first text channel), or a channel id.
A <msg> is the short id read and search print (the first 8 characters, or
more), or "last" for the newest message in the chat.

Options (anywhere among the arguments):
  -n <count>        how many messages read or search shows
  --in <chat>       search only this chat
  --json            one JSON object per line, for scripts
  --color <when>    auto (default), always or never; NO_COLOR is honoured

Examples:
  concord chats
  concord read "Lantern Makers/#general" -n 50
  concord send "#general" "Kiln is warm, first batch at ten"
  concord react "#general" last 👍
  concord watch
`

var chatCommands = map[string]bool{
	"chats": true, "read": true, "send": true, "reply": true,
	"react": true, "edit": true, "search": true, "watch": true,
}

// chatService is what the chat commands use of the Service, so they can be
// tested without a network.
type chatService interface {
	Guilds() []domain.Guild
	Messages(channelID string, limit int) ([]domain.Message, error)
	ReadState() (map[string]int64, error)
	UnreadCounts(sinceNano map[string]int64) (map[string]int, error)
	ChannelsLastActivity(channelIDs []string) map[string]int64
	SearchMessages(query string, limit int, filter ...store.SearchFilter) ([]domain.Message, error)
	SendMessageOnce(channelID, content, replyTo, dir, sendID string) (domain.Message, error)
	ToggleReaction(channelID, targetID, emoji string, sendID ...string) error
	EditMessage(channelID, targetID, newContent, dir string, sendID ...string) error
	OnMessage(fn func(domain.Message))
	PublicKey() []byte
	Fingerprint() string
}

// cleanService hands the commands every name a peer chose as one clean line
// (guild, channel, display name, reaction), so no output path can print a
// raw one. Bodies stay as written: renderBody cleans what it prints, and
// --json escapes (jsonSafe).
type cleanService struct{ chatService }

func (c cleanService) Guilds() []domain.Guild {
	gs := c.chatService.Guilds()
	out := make([]domain.Guild, len(gs))
	for i, g := range gs {
		g.Name = cleanLine(g.Name)
		chs := make([]domain.Channel, len(g.Channels))
		for j, ch := range g.Channels {
			ch.Name = cleanLine(ch.Name)
			chs[j] = ch
		}
		g.Channels = chs
		out[i] = g
	}
	return out
}

func cleanMessage(m domain.Message) domain.Message {
	m.Name = cleanLine(m.Name)
	if len(m.Reactions) > 0 {
		r := make(map[string][]string, len(m.Reactions))
		for e, f := range m.Reactions {
			k := cleanLine(e)
			r[k] = append(r[k], f...)
		}
		m.Reactions = r
	}
	return m
}

func cleanMessages(ms []domain.Message, err error) ([]domain.Message, error) {
	for i := range ms {
		ms[i] = cleanMessage(ms[i])
	}
	return ms, err
}

func (c cleanService) Messages(channelID string, limit int) ([]domain.Message, error) {
	return cleanMessages(c.chatService.Messages(channelID, limit))
}

func (c cleanService) SearchMessages(q string, limit int, f ...store.SearchFilter) ([]domain.Message, error) {
	return cleanMessages(c.chatService.SearchMessages(q, limit, f...))
}

func (c cleanService) OnMessage(fn func(domain.Message)) {
	c.chatService.OnMessage(func(m domain.Message) { fn(cleanMessage(m)) })
}

type chatOpts struct {
	n     int
	in    string
	json  bool
	color string
	args  []string
}

// parseChatArgs reads a command's options, which may come before or among
// its arguments ("read #general -n 50").
func parseChatArgs(cmd string, args []string, stderr io.Writer) (chatOpts, error) {
	o := chatOpts{n: 0, color: "auto"}
	fs := flag.NewFlagSet("concord "+cmd, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.IntVar(&o.n, "n", 0, "")
	fs.StringVar(&o.in, "in", "", "")
	fs.BoolVar(&o.json, "json", false, "")
	fs.StringVar(&o.color, "color", "auto", "")
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return o, flag.ErrHelp
			}
			return o, fmt.Errorf("concord %s: %v", cmd, err)
		}
		if fs.NArg() == 0 {
			break
		}
		// After "--" everything is an argument, even "-5 degrees".
		if at := len(rest) - fs.NArg() - 1; at >= 0 && rest[at] == "--" {
			o.args = append(o.args, fs.Args()...)
			break
		}
		o.args = append(o.args, fs.Arg(0))
		rest = fs.Args()[1:]
	}
	switch o.color {
	case "auto", "always", "never":
	default:
		return o, fmt.Errorf("concord %s: --color is auto, always or never, not %q", cmd, o.color)
	}
	if o.n < 0 || o.n > 1000 {
		return o, fmt.Errorf("concord %s: -n is between 1 and 1000", cmd)
	}
	want := map[string][2]int{ // fewest, most arguments (-1: any)
		"chats": {0, 0}, "read": {1, 1}, "send": {2, -1}, "reply": {3, -1},
		"react": {3, 3}, "edit": {3, -1}, "search": {1, -1}, "watch": {0, 1},
	}[cmd]
	if len(o.args) < want[0] || (want[1] >= 0 && len(o.args) > want[1]) {
		return o, fmt.Errorf("concord %s: wrong number of arguments", cmd)
	}
	return o, nil
}

// runChatCommand is main's entry for a chat command; it returns the exit code.
func runChatCommand(cmd string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	o, err := parseChatArgs(cmd, args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(stdout, chatUsage)
		return 0
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		fmt.Fprint(stderr, chatUsage)
		return 2
	}
	// The text of a send read from stdin, before the passphrase can take it.
	if (cmd == "send" || cmd == "reply" || cmd == "edit") && o.args[len(o.args)-1] == "-" && len(o.args) == map[string]int{"send": 2, "reply": 3, "edit": 3}[cmd] {
		b, err := io.ReadAll(io.LimitReader(stdin, 64<<10))
		if err != nil {
			fmt.Fprintln(stderr, "concord:", err)
			return 1
		}
		o.args[len(o.args)-1] = strings.TrimRight(string(b), "\n")
	}
	dataDir, err := app.DataDir()
	if err != nil {
		fmt.Fprintln(stderr, "concord:", err)
		return 1
	}
	// A chat command opens a profile; it never makes one. A mistyped
	// CONCORD_HOME would otherwise be a new, empty account.
	if ks, err := app.KeystorePath(); err != nil {
		fmt.Fprintln(stderr, "concord:", err)
		return 1
	} else if _, err := os.Stat(ks); err != nil {
		fmt.Fprintf(stderr, "concord: no Concord profile in %s; set one up in the app, or with concord --status\n", dataDir)
		return 1
	}
	// The core's own log is for debugging (CONCORD_LOG=1); a terminal reader
	// sees the command's answer and its errors.
	if os.Getenv("CONCORD_LOG") != "1" {
		log.SetOutput(io.Discard)
	}
	pass, err := readPassphrase()
	if err != nil {
		fmt.Fprintln(stderr, "concord:", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	svc, err := app.Start(ctx, serviceConfig(dataDir, pass))
	if err != nil {
		fmt.Fprintln(stderr, "concord:", err)
		return 1
	}
	defer svc.Close()
	c := &chatCLI{svc: cleanService{svc}, out: stdout, st: newStyle(stdout, o.color), o: o, now: time.Now}
	if err := c.run(ctx, cmd); err != nil {
		fmt.Fprintln(stderr, "concord:", err)
		return 1
	}
	// A send lingers a moment, so the message has gone to whoever is
	// connected before the process ends. It is saved either way: the next
	// time this profile is online it reaches everyone by history sync.
	if cmd == "send" || cmd == "reply" || cmd == "react" || cmd == "edit" {
		select {
		case <-ctx.Done():
		case <-time.After(sendLinger):
		}
	}
	return 0
}

var sendLinger = 2 * time.Second

type chatCLI struct {
	svc chatService
	out io.Writer
	st  style
	o   chatOpts
	now func() time.Time
}

func (c *chatCLI) run(ctx context.Context, cmd string) error {
	switch cmd {
	case "chats":
		return c.chats()
	case "read":
		return c.read(c.o.args[0])
	case "send":
		return c.send(c.o.args[0], "", strings.Join(c.o.args[1:], " "))
	case "reply":
		return c.send(c.o.args[0], c.o.args[1], strings.Join(c.o.args[2:], " "))
	case "react":
		return c.react(c.o.args[0], c.o.args[1], c.o.args[2])
	case "edit":
		return c.edit(c.o.args[0], c.o.args[1], strings.Join(c.o.args[2:], " "))
	case "search":
		return c.search(strings.Join(c.o.args, " "))
	case "watch":
		ref := ""
		if len(c.o.args) == 1 {
			ref = c.o.args[0]
		}
		return c.watch(ctx, ref)
	}
	return fmt.Errorf("unknown command %q", cmd)
}

// ---- finding things ----

// findChat resolves a <chat>: everything resolveChannel takes, plus a guild
// or direct message by name ("@" optional), which means its first text
// channel.
func findChat(guilds []domain.Guild, ref string) (domain.Guild, domain.Channel, error) {
	g, ch, err := resolveChannel(guilds, ref)
	if err == nil {
		return g, ch, nil
	}
	name := strings.TrimPrefix(strings.TrimSpace(ref), "@")
	var hits []domain.Guild
	for _, fold := range []bool{false, true} {
		for _, g := range guilds {
			if g.Name == name || (fold && strings.EqualFold(g.Name, name)) {
				hits = append(hits, g)
			}
		}
		if len(hits) > 0 {
			break
		}
	}
	if len(hits) > 1 {
		return domain.Guild{}, domain.Channel{}, fmt.Errorf("%q names %d chats; use \"guild/#channel\" or an id", ref, len(hits))
	}
	if len(hits) == 1 {
		for _, ch := range hits[0].Channels {
			if ch.Type == "" || ch.Type == "text" || ch.Type == "announcement" {
				return hits[0], ch, nil
			}
		}
		return domain.Guild{}, domain.Channel{}, fmt.Errorf("%q has no text channel", ref)
	}
	return domain.Guild{}, domain.Channel{}, err
}

// findMessage resolves a <msg> in a channel: "last", or an id prefix of at
// least four characters that names exactly one message there.
func (c *chatCLI) findMessage(channelID, ref string) (domain.Message, error) {
	msgs, err := c.svc.Messages(channelID, 500)
	if err != nil {
		return domain.Message{}, err
	}
	msgs = visible(msgs)
	if ref == "last" {
		if len(msgs) == 0 {
			return domain.Message{}, errors.New("this chat has no messages")
		}
		return msgs[len(msgs)-1], nil
	}
	ref = strings.ToLower(strings.TrimPrefix(ref, "#"))
	if len(ref) < 4 {
		return domain.Message{}, fmt.Errorf("message id %q is too short (at least 4 characters)", ref)
	}
	var hit []domain.Message
	for _, m := range msgs {
		if strings.HasPrefix(strings.ToLower(m.ID), ref) {
			hit = append(hit, m)
		}
	}
	switch len(hit) {
	case 0:
		return domain.Message{}, fmt.Errorf("no message %q in the latest 500 here", ref)
	case 1:
		return hit[0], nil
	}
	return domain.Message{}, fmt.Errorf("message id %q matches %d messages; give more of it", ref, len(hit))
}

// visible drops what the feed does not show as a row: actions and the like.
func visible(msgs []domain.Message) []domain.Message {
	out := msgs[:0:0]
	for _, m := range msgs {
		if m.Kind == "" || m.Kind == "system" {
			out = append(out, m)
		}
	}
	return out
}

// ---- chats ----

type chatRow struct {
	Guild     string `json:"guild"`
	GuildID   string `json:"guildId"`
	Channel   string `json:"channel"`
	ChannelID string `json:"channelId"`
	Ref       string `json:"ref"`
	DM        bool   `json:"dm,omitempty"`
	Unread    int    `json:"unread"`
	Last      int64  `json:"last,omitempty"` // Unix ms of the latest activity
}

func (c *chatCLI) chats() error {
	guilds := c.svc.Guilds()
	marks, err := c.svc.ReadState()
	if err != nil {
		return err
	}
	since := map[string]int64{}
	var ids []string
	for _, g := range guilds {
		for _, ch := range g.Channels {
			if ch.Type == "voice" {
				continue
			}
			ids = append(ids, ch.ID)
			since[ch.ID] = marks[ch.ID] * int64(time.Millisecond)
		}
	}
	counts, err := c.svc.UnreadCounts(since)
	if err != nil {
		return err
	}
	last := c.svc.ChannelsLastActivity(ids)
	var rows []chatRow
	for _, g := range guilds {
		for _, ch := range g.Channels {
			if ch.Type == "voice" {
				continue
			}
			rows = append(rows, chatRow{
				Guild: g.Name, GuildID: g.ID, Channel: ch.Name, ChannelID: ch.ID,
				Ref: g.Name + "/#" + ch.Name, DM: g.Kind == "dm",
				Unread: counts[ch.ID], Last: last[ch.ID],
			})
		}
	}
	// Unread first, then the most recently active, then by name.
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if (a.Unread > 0) != (b.Unread > 0) {
			return a.Unread > 0
		}
		if a.Last != b.Last {
			return a.Last > b.Last
		}
		return a.Ref < b.Ref
	})
	if c.o.json {
		for _, r := range rows {
			c.jsonLine(r)
		}
		return nil
	}
	if len(rows) == 0 {
		fmt.Fprintln(c.out, c.st.muted("No chats yet. Join a guild with an invite in the app, or with concord --serve."))
		return nil
	}
	if !c.st.on {
		for _, r := range rows {
			fmt.Fprintf(c.out, "%s\t%d\t%s\n", r.Ref, r.Unread, ago(c.now(), r.Last))
		}
		return nil
	}
	width := 0
	for _, r := range rows {
		width = max(width, len([]rune(displayRef(r))))
	}
	total := 0
	for _, r := range rows {
		total += r.Unread
	}
	head := "Chats"
	if total > 0 {
		head += "  " + c.st.badge(total)
	}
	fmt.Fprintln(c.out, c.st.bold(head))
	for _, r := range rows {
		ref := displayRef(r)
		pad := strings.Repeat(" ", width-len([]rune(ref)))
		var label string
		if r.DM {
			label = c.st.name(r.GuildID, ref)
		} else {
			label = c.st.muted(r.Guild+" ") + c.st.accent("#") + r.Channel
			if r.Unread > 0 {
				label = r.Guild + " " + c.st.accent("#") + c.st.bold(r.Channel)
			}
		}
		count := ""
		if r.Unread > 0 {
			count = " " + c.st.badge(r.Unread)
		}
		fmt.Fprintf(c.out, "  %s%s  %s%s\n", label, pad, c.st.muted(ago(c.now(), r.Last)), count)
	}
	return nil
}

func displayRef(r chatRow) string {
	if r.DM {
		return "@" + r.Guild
	}
	return r.Guild + " #" + r.Channel
}

// ago is a short relative time: "now", "5m", "3h", "2d", or a date.
func ago(now time.Time, ms int64) string {
	if ms <= 0 {
		return "—"
	}
	d := now.Sub(time.UnixMilli(ms))
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	return time.UnixMilli(ms).Format("2 Jan 2006")
}

// ---- read ----

func (c *chatCLI) read(ref string) error {
	g, ch, err := findChat(c.svc.Guilds(), ref)
	if err != nil {
		return err
	}
	n := c.o.n
	if n == 0 {
		n = 30
	}
	all, err := c.svc.Messages(ch.ID, 500)
	if err != nil {
		return err
	}
	all = visible(all)
	shown := all
	if len(shown) > n {
		shown = shown[len(shown)-n:]
	}
	if c.o.json {
		for _, m := range shown {
			c.jsonLine(c.messageJSON(g, ch, m))
		}
		return nil
	}
	if c.st.on {
		title := c.st.accent("#") + c.st.bold(ch.Name)
		if g.Kind == "dm" {
			title = c.st.bold("@" + g.Name)
		} else {
			title = c.st.muted(g.Name+" ") + title
		}
		fmt.Fprintln(c.out, title)
	}
	byID := map[string]domain.Message{}
	for _, m := range all {
		byID[m.ID] = m
	}
	var prev domain.Message
	for i, m := range shown {
		c.printMessage(m, byID, i > 0 && sameRun(prev, m))
		prev = m
	}
	if len(shown) == 0 {
		fmt.Fprintln(c.out, c.st.muted("Nothing here yet."))
	}
	return nil
}

// sameRun: m follows prev from the same person within five minutes on the
// same day, so it is printed under prev's header, as the feed groups them.
func sameRun(prev, m domain.Message) bool {
	return prev.ID != "" && string(prev.Sender) == string(m.Sender) && m.Kind == "" && prev.Kind == "" &&
		m.ReplyTo == "" && m.Sent.Sub(prev.Sent) < 5*time.Minute && sameDay(prev.Sent, m.Sent)
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Local().Date()
	by, bm, bd := b.Local().Date()
	return ay == by && am == bm && ad == bd
}

func (c *chatCLI) senderName(m domain.Message) string {
	if string(m.Sender) == string(c.svc.PublicKey()) {
		return "You"
	}
	if n := cleanLine(m.Name); n != "" {
		return n
	}
	return "Someone"
}

func shortID(id string) string {
	id = safeID(id)
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// printMessage prints one message: a header with the name, time and short id
// (unless it continues a run), the reply it answers, its words, what it
// carries, and its reactions.
func (c *chatCLI) printMessage(m domain.Message, byID map[string]domain.Message, cont bool) {
	st := c.st
	when := m.Sent.Local()
	if !st.on {
		// One line per message, tab-separated, for grep and cut.
		// A newline in the words would start a record a script reads as real:
		// it is written as a visible mark (renderBody has made tabs spaces).
		body := strings.ReplaceAll(renderBody(m), "\n", " ⏎ ")
		fmt.Fprintf(c.out, "%s\t%s\t%s\t%s", shortID(m.ID), when.Format(time.RFC3339), c.senderName(m), body)
		if m.ReplyTo != "" {
			fmt.Fprintf(c.out, "\treply-to:%s", shortID(m.ReplyTo))
		}
		if r := reactionLine(m, "", style{}); r != "" {
			fmt.Fprintf(c.out, "\t%s", r)
		}
		fmt.Fprintln(c.out)
		return
	}
	indent := "   "
	if !cont {
		stamp := when.Format("15:04")
		if !sameDay(when, c.now()) {
			stamp = when.Format("Mon 2 Jan 15:04")
		}
		fp := hex.EncodeToString(m.Sender)
		name := st.name(fp, c.senderName(m))
		if m.Kind == "system" {
			// A notice is sent by a member like any message: it is theirs, not
			// Concord's, and says so.
			name += st.muted(" · notice")
		}
		fmt.Fprintf(c.out, "\n %s  %s  %s\n", name, st.muted(stamp), st.dim(shortID(m.ID)))
	}
	if m.ReplyTo != "" {
		q := "a message that isn't here"
		if r, ok := byID[m.ReplyTo]; ok {
			q = c.senderName(r) + ": " + firstLine(renderBody(r), 60)
		}
		fmt.Fprintf(c.out, "%s%s %s\n", indent, st.accent("╭"), st.muted(q))
	}
	body := renderBody(m)
	if m.Deleted {
		body = st.muted("This message was deleted.")
	}
	lines := strings.Split(body, "\n")
	// "(edited)" ends the words, as in the app, not what the message carries.
	at := -1
	if m.Edited && !m.Deleted {
		at = len(lines)
		for i, line := range lines {
			if !isAttachLine(line) && line != "" {
				at = i
			}
		}
	}
	for i, line := range lines {
		tail := ""
		if i == at {
			tail = " " + st.muted("(edited)")
		}
		fmt.Fprintf(c.out, "%s%s%s\n", indent, styleLine(st, line), tail)
	}
	if at == len(lines) {
		fmt.Fprintf(c.out, "%s%s\n", indent, st.muted("(edited)"))
	}
	if r := reactionLine(m, c.svc.Fingerprint(), st); r != "" {
		fmt.Fprintf(c.out, "%s%s\n", indent, r)
	}
}

func isAttachLine(line string) bool {
	return strings.HasPrefix(line, "📎 ") || strings.HasPrefix(line, "🖼 ") || strings.HasPrefix(line, "🔗 ") || strings.HasPrefix(line, "▶ ")
}

// styleLine colours what renderBody marked: attachments and links.
func styleLine(st style, line string) string {
	if isAttachLine(line) {
		return st.accent(line[:strings.Index(line, " ")]) + st.muted(line[strings.Index(line, " "):])
	}
	return line
}

func firstLine(s string, max int) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	r := []rune(s)
	if len(r) > max {
		return string(r[:max-1]) + "…"
	}
	return s
}

func reactionLine(m domain.Message, me string, st style) string {
	if len(m.Reactions) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m.Reactions))
	for e := range m.Reactions {
		keys = append(keys, e)
	}
	// Most reacted first, then a stable order.
	sort.Slice(keys, func(i, j int) bool {
		a, b := len(m.Reactions[keys[i]]), len(m.Reactions[keys[j]])
		if a != b {
			return a > b
		}
		return keys[i] < keys[j]
	})
	var parts []string
	for _, e := range keys {
		n := len(m.Reactions[e])
		if n == 0 {
			continue
		}
		p := clean(e) + " " + strconv.Itoa(n)
		mine := false
		for _, f := range m.Reactions[e] {
			mine = mine || f == me
		}
		if mine {
			p = st.accent(p)
		} else {
			p = st.muted(p)
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, "  ")
}

var (
	reImage = regexp.MustCompile(`!\[image\]\(concord://attach/v[12]/[0-9a-f]{64}/[^)\s]*\)`)
	reFile  = regexp.MustCompile(`\[file\]\(concord://file/v1/[0-9a-f]{64}/[A-Za-z0-9_-]+/(\d+)/([A-Za-z0-9_-]*)/([A-Za-z0-9_-]*)\)`)
	reCard  = regexp.MustCompile(`(?m)^\[((?:\\.|[^\]\\\n])+)\]\((https?://[^\s)]+)\) · (\S+)$`)
	reToken = regexp.MustCompile(`\[[a-z]*\]\(concord://([a-z]+)/v\d+/[^)\s]*\)`)
	reEsc   = regexp.MustCompile(`\\(.)`)
)

// renderBody is a message's text for a terminal: Concord's tokens become a
// short line saying what they are (a picture, a file, a link's card, a poll),
// the stamps a composer adds go, and control characters a stranger could
// have hidden in it are taken out.
func renderBody(m domain.Message) string {
	t := m.Content
	var extra []string
	t = reCard.ReplaceAllStringFunc(t, func(s string) string {
		sm := reCard.FindStringSubmatch(s)
		extra = append(extra, "🔗 "+reEsc.ReplaceAllString(sm[1], "$1")+" — "+sm[3])
		return ""
	})
	pics := 0
	t = reImage.ReplaceAllStringFunc(t, func(s string) string {
		if strings.Contains(s, "/2/") && len(extra) > 0 && strings.HasPrefix(extra[len(extra)-1], "🔗 ") {
			return "" // a card's own picture
		}
		pics++
		return ""
	})
	if pics == 1 {
		extra = append([]string{"🖼 picture"}, extra...)
	} else if pics > 1 {
		extra = append([]string{fmt.Sprintf("🖼 %d pictures", pics)}, extra...)
	}
	t = reFile.ReplaceAllStringFunc(t, func(s string) string {
		sm := reFile.FindStringSubmatch(s)
		name, _ := base64.RawURLEncoding.DecodeString(sm[3])
		size, _ := strconv.ParseInt(sm[1], 10, 64)
		n := clean(string(name))
		if n == "" {
			n = "file"
		}
		extra = append(extra, "📎 "+n+" · "+humanSize(size))
		return ""
	})
	t = reToken.ReplaceAllStringFunc(t, func(s string) string {
		kind := reToken.FindStringSubmatch(s)[1]
		switch kind {
		case "eph", "fx", "ts", "lp":
			return ""
		case "poll", "event", "announce", "game", "doodle", "sfx", "embed":
			extra = append(extra, "▶ "+map[string]string{
				"poll": "a poll", "event": "an event", "announce": "an announcement", "game": "a game move",
				"doodle": "a doodle", "sfx": "a sound", "embed": "an embed",
			}[kind]+" (open it in the app)")
			return ""
		}
		return ""
	})
	t = strings.TrimSpace(clean(t))
	lines := []string{}
	if t != "" {
		lines = append(lines, t)
	}
	lines = append(lines, extra...)
	// Last, over the whole: a card's title and site come from the sender too.
	// A tab is spaces: on a terminal it is only a gap, and piped it would be a
	// field separator a sender could forge records with.
	return strings.ReplaceAll(clean(strings.Join(lines, "\n")), "\t", "    ")
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d B", n)
}

// ---- send, reply, react, edit ----

func newSendID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "cli-" + hex.EncodeToString(b)
}

func (c *chatCLI) send(ref, replyRef, text string) error {
	g, ch, err := findChat(c.svc.Guilds(), ref)
	if err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("nothing to send")
	}
	replyTo := ""
	if replyRef != "" {
		r, err := c.findMessage(ch.ID, replyRef)
		if err != nil {
			return err
		}
		replyTo = r.ID
	}
	// One send id for this command: if the core answers late or not at all,
	// running it again is a new command and a new message, but nothing in
	// this one sends twice.
	m, err := c.svc.SendMessageOnce(ch.ID, text, replyTo, "", newSendID())
	if err != nil {
		return err
	}
	if c.o.json {
		c.jsonLine(c.messageJSON(g, ch, m))
		return nil
	}
	if c.st.on {
		fmt.Fprintf(c.out, "%s Sent to %s  %s\n", c.st.accent("✓"), chatTitle(g, ch), c.st.dim(shortID(m.ID)))
	} else {
		fmt.Fprintln(c.out, shortID(m.ID))
	}
	return nil
}

func chatTitle(g domain.Guild, ch domain.Channel) string {
	if g.Kind == "dm" {
		return "@" + g.Name
	}
	return g.Name + " #" + ch.Name
}

func (c *chatCLI) react(ref, msgRef, emoji string) error {
	_, ch, err := findChat(c.svc.Guilds(), ref)
	if err != nil {
		return err
	}
	m, err := c.findMessage(ch.ID, msgRef)
	if err != nil {
		return err
	}
	emoji = strings.TrimSpace(clean(emoji))
	if emoji == "" || len(emoji) > 64 {
		return errors.New("give one emoji")
	}
	had := false
	for _, f := range m.Reactions[emoji] {
		had = had || f == c.svc.Fingerprint()
	}
	if err := c.svc.ToggleReaction(ch.ID, m.ID, emoji, newSendID()); err != nil {
		return err
	}
	verb := "Reacted"
	if had {
		verb = "Took back"
	}
	if c.o.json {
		c.jsonLine(map[string]any{"messageId": m.ID, "emoji": emoji, "on": !had})
	} else if c.st.on {
		fmt.Fprintf(c.out, "%s %s %s on %s\n", c.st.accent("✓"), verb, emoji, c.st.dim(shortID(m.ID)))
	} else {
		fmt.Fprintf(c.out, "%s\t%s\t%v\n", shortID(m.ID), emoji, !had)
	}
	return nil
}

func (c *chatCLI) edit(ref, msgRef, text string) error {
	_, ch, err := findChat(c.svc.Guilds(), ref)
	if err != nil {
		return err
	}
	m, err := c.findMessage(ch.ID, msgRef)
	if err != nil {
		return err
	}
	if string(m.Sender) != string(c.svc.PublicKey()) {
		return errors.New("you can edit only your own messages")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("an edit needs words; delete the message in the app instead")
	}
	if err := c.svc.EditMessage(ch.ID, m.ID, text, "", newSendID()); err != nil {
		return err
	}
	if c.o.json {
		c.jsonLine(map[string]any{"messageId": m.ID, "edited": true})
	} else if c.st.on {
		fmt.Fprintf(c.out, "%s Edited %s\n", c.st.accent("✓"), c.st.dim(shortID(m.ID)))
	} else {
		fmt.Fprintln(c.out, shortID(m.ID))
	}
	return nil
}

// ---- search ----

func (c *chatCLI) search(q string) error {
	n := c.o.n
	if n == 0 {
		n = 20
	}
	guilds := c.svc.Guilds()
	var f []store.SearchFilter
	if c.o.in != "" {
		_, ch, err := findChat(guilds, c.o.in)
		if err != nil {
			return err
		}
		f = append(f, store.SearchFilter{InChannel: ch.ID})
	}
	hits, err := c.svc.SearchMessages(q, n, f...)
	if err != nil {
		return err
	}
	where := map[string][2]any{}
	for _, g := range guilds {
		for _, ch := range g.Channels {
			where[ch.ID] = [2]any{g, ch}
		}
	}
	if len(hits) == 0 && !c.o.json {
		fmt.Fprintln(c.out, c.st.muted("No messages match "+strconv.Quote(q)+"."))
		return nil
	}
	for _, m := range hits {
		w, ok := where[m.ChannelID]
		if !ok {
			continue
		}
		g, ch := w[0].(domain.Guild), w[1].(domain.Channel)
		if c.o.json {
			c.jsonLine(c.messageJSON(g, ch, m))
			continue
		}
		if !c.st.on {
			fmt.Fprintf(c.out, "%s\t", g.Name+"/#"+ch.Name)
			c.printMessage(m, nil, false)
			continue
		}
		fmt.Fprintf(c.out, "\n %s  %s  %s  %s\n", c.st.muted(chatTitle(g, ch)), c.st.name(hex.EncodeToString(m.Sender), c.senderName(m)),
			c.st.muted(m.Sent.Local().Format("2 Jan 15:04")), c.st.dim(shortID(m.ID)))
		for _, line := range strings.Split(highlight(c.st, renderBody(m), q), "\n") {
			fmt.Fprintf(c.out, "   %s\n", line)
		}
	}
	return nil
}

// highlight marks each word of the query in the text, case-insensitively.
func highlight(st style, text, q string) string {
	if !st.on {
		return text
	}
	for _, w := range strings.Fields(q) {
		re, err := regexp.Compile(`(?i)` + regexp.QuoteMeta(w))
		if err != nil {
			continue
		}
		text = re.ReplaceAllStringFunc(text, func(s string) string { return st.wrap("\x1b[1;4m"+st.fg(cAccent), s) })
	}
	return text
}

// ---- watch ----

func (c *chatCLI) watch(ctx context.Context, ref string) error {
	only := ""
	var title string
	if ref != "" {
		g, ch, err := findChat(c.svc.Guilds(), ref)
		if err != nil {
			return err
		}
		only, title = ch.ID, chatTitle(g, ch)
	}
	events := make(chan domain.Message, 256)
	c.svc.OnMessage(func(m domain.Message) {
		if m.Backfill || (only != "" && m.ChannelID != only) || (m.Kind != "" && m.Kind != "system") {
			return
		}
		select {
		case events <- m:
		default: // a burst a terminal cannot keep up with drops, never blocks the core
		}
	})
	if c.st.on && !c.o.json {
		if title == "" {
			title = "every chat"
		}
		fmt.Fprintf(c.out, "%s Following %s. Ctrl-C to stop.\n", c.st.accent("●"), title)
	}
	seen := map[string]bool{}
	for {
		select {
		case <-ctx.Done():
			return nil
		case m := <-events:
			// An edit or a reaction re-emits the message: printed once, as new.
			if seen[m.ID] {
				continue
			}
			seen[m.ID] = true
			g, ch := c.where(m.ChannelID)
			if c.o.json {
				c.jsonLine(c.messageJSON(g, ch, m))
				continue
			}
			if !c.st.on {
				fmt.Fprintf(c.out, "%s\t", g.Name+"/#"+ch.Name)
				c.printMessage(m, nil, false)
				continue
			}
			if only == "" {
				fmt.Fprintf(c.out, "\n %s", c.st.muted(chatTitle(g, ch)))
			}
			c.printMessage(m, nil, false)
		}
	}
}

func (c *chatCLI) where(channelID string) (domain.Guild, domain.Channel) {
	for _, g := range c.svc.Guilds() {
		for _, ch := range g.Channels {
			if ch.ID == channelID {
				return g, ch
			}
		}
	}
	return domain.Guild{Name: "?"}, domain.Channel{ID: channelID, Name: "?"}
}

// ---- JSON ----

type chatMessageJSON struct {
	ID        string         `json:"id"`
	Chat      string         `json:"chat"`
	ChannelID string         `json:"channelId"`
	Sender    string         `json:"sender"`
	Name      string         `json:"name"`
	Mine      bool           `json:"mine,omitempty"`
	Sent      string         `json:"sent"`
	Text      string         `json:"text"`
	Content   string         `json:"content"`
	ReplyTo   string         `json:"replyTo,omitempty"`
	Edited    bool           `json:"edited,omitempty"`
	Deleted   bool           `json:"deleted,omitempty"`
	Reactions map[string]int `json:"reactions,omitempty"`
}

func (c *chatCLI) messageJSON(g domain.Guild, ch domain.Channel, m domain.Message) chatMessageJSON {
	out := chatMessageJSON{
		ID: safeID(m.ID), Chat: g.Name + "/#" + ch.Name, ChannelID: ch.ID,
		Sender: hex.EncodeToString(m.Sender), Name: c.senderName(m),
		Mine: string(m.Sender) == string(c.svc.PublicKey()),
		Sent: m.Sent.UTC().Format(time.RFC3339Nano), Text: renderBody(m), Content: m.Content,
		ReplyTo: safeID(m.ReplyTo), Edited: m.Edited, Deleted: m.Deleted,
	}
	if len(m.Reactions) > 0 {
		out.Reactions = map[string]int{}
		for e, f := range m.Reactions {
			out.Reactions[e] = len(f)
		}
	}
	return out
}

func (c *chatCLI) jsonLine(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	c.out.Write(append(jsonSafe(b), '\n')) //nolint:errcheck // a closed stdout ends the run anyway
}
