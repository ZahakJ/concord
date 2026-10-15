package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ZahakJ/concord/internal/app"
	"github.com/ZahakJ/concord/internal/domain"
)

// Bot mode: a peer driven by a script over newline-delimited JSON.
//
// A bot is an ordinary Concord identity. It is invited into a guild the way a
// person is, holds its own keys, and reads exactly what that membership lets it
// read — nothing about end-to-end encryption changes to accommodate it. What
// this file adds is only a front end: events go to stdout as one JSON object
// per line, commands arrive on stdin the same way, and logs go to stderr so a
// script never has to tell the two apart. docs/BOTS.md is the reference.

// botService is the slice of app.Service the bot uses. It exists so the line
// protocol can be tested against a fake without starting a network.
type botService interface {
	Fingerprint() string
	PeerID() string
	DisplayName() string
	SetDisplayName(name string) error
	Guilds() []domain.Guild
	SendMessage(channelID, content, replyTo, dir string) (domain.Message, error)
	ToggleReaction(channelID, targetID, emoji string) error
	JoinViaInvite(code string) (domain.Guild, error)
	GuildMembers(guildID string) ([][]byte, error)
	AccountFingerprintOf(cred []byte) string
	ProfileName(fingerprint string) string
	NickOf(guildID, fingerprint string) string
	SenderBlocked(sender []byte) bool
	OnMessage(fn func(domain.Message))
	OnGuildUpdate(fn func())
}

// maxBotLine bounds one command line. A longer line is discarded with an
// error rather than buffered without limit or allowed to end the session.
const maxBotLine = 1 << 20

// ---- Commands (stdin) ----

// botCommand is one stdin line. ID is any JSON value the script chose; it is
// echoed verbatim on the reply so a script can match answers to questions.
type botCommand struct {
	ID      json.RawMessage `json:"id,omitempty"`
	Cmd     string          `json:"cmd"`
	Channel string          `json:"channel,omitempty"`
	Text    string          `json:"text,omitempty"`
	ReplyTo string          `json:"replyTo,omitempty"`
	Message string          `json:"message,omitempty"`
	Emoji   string          `json:"emoji,omitempty"`
	Invite  string          `json:"invite,omitempty"`
	Guild   string          `json:"guild,omitempty"`
}

// parseBotCommand decodes and validates one line. The returned command carries
// its ID even when the line is otherwise rejected, so the error can be
// correlated. A blank line yields an empty Cmd and no error: it is skipped.
func parseBotCommand(line []byte) (botCommand, error) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return botCommand{}, nil
	}
	// Recover the id first, loosely, so a typo elsewhere in the line still
	// gets an error the script can attribute.
	var idOnly struct {
		ID json.RawMessage `json:"id"`
	}
	_ = json.Unmarshal(line, &idOnly)

	var c botCommand
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return botCommand{ID: idOnly.ID}, fmt.Errorf("bad command line: %v", err)
	}
	if dec.More() {
		return botCommand{ID: c.ID}, errors.New("bad command line: one JSON object per line")
	}
	c.Cmd = strings.ToLower(strings.TrimSpace(c.Cmd))
	need := func(field, val string) error {
		if strings.TrimSpace(val) == "" {
			return fmt.Errorf("%s: missing %q", c.Cmd, field)
		}
		return nil
	}
	var err error
	switch c.Cmd {
	case "":
		err = errors.New(`missing "cmd"`)
	case "send":
		if err = need("channel", c.Channel); err == nil {
			err = need("text", c.Text)
		}
	case "react":
		if err = need("channel", c.Channel); err == nil {
			if err = need("message", c.Message); err == nil {
				err = need("emoji", c.Emoji)
			}
		}
	case "join":
		err = need("invite", c.Invite)
	case "members":
		err = need("guild", c.Guild)
	case "name":
		err = need("text", c.Text)
	case "list", "quit":
	default:
		err = fmt.Errorf("unknown cmd %q", c.Cmd)
	}
	return c, err
}

// resolveChannel finds a channel by id, by "guild/#channel" (guild by name or
// id, the '#' optional), or by a bare "#channel" when exactly one guild has a
// channel of that name. Names match exactly first, then case-insensitively; an
// ambiguous name is an error rather than a guess, because a guess posts into
// the wrong room.
func resolveChannel(guilds []domain.Guild, ref string) (domain.Guild, domain.Channel, error) {
	ref = strings.TrimSpace(ref)
	for _, g := range guilds {
		for _, c := range g.Channels {
			if c.ID == ref {
				return g, c, nil
			}
		}
	}
	guildRef, chanRef := "", ref
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		guildRef, chanRef = strings.TrimSpace(ref[:i]), ref[i+1:]
	} else if !strings.HasPrefix(ref, "#") {
		return domain.Guild{}, domain.Channel{}, fmt.Errorf("no channel %q (use an id, \"guild/#channel\" or \"#channel\")", ref)
	}
	chanRef = strings.TrimPrefix(strings.TrimSpace(chanRef), "#")

	type hit struct {
		g domain.Guild
		c domain.Channel
	}
	match := func(fold bool) []hit {
		eq := func(a, b string) bool {
			if fold {
				return strings.EqualFold(a, b)
			}
			return a == b
		}
		var hits []hit
		for _, g := range guilds {
			if guildRef != "" && g.ID != guildRef && !eq(g.Name, guildRef) {
				continue
			}
			for _, c := range g.Channels {
				if eq(c.Name, chanRef) {
					hits = append(hits, hit{g, c})
				}
			}
		}
		return hits
	}
	hits := match(false)
	if len(hits) == 0 {
		hits = match(true)
	}
	switch len(hits) {
	case 0:
		return domain.Guild{}, domain.Channel{}, fmt.Errorf("no channel %q", ref)
	case 1:
		return hits[0].g, hits[0].c, nil
	}
	return domain.Guild{}, domain.Channel{}, fmt.Errorf("channel %q is ambiguous (%d matches); use its id", ref, len(hits))
}

// ---- Events (stdout) ----

type botChannelView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
	Ref  string `json:"ref"` // "guild/#channel", accepted wherever a channel is
}

type botGuildView struct {
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	DM       bool             `json:"dm,omitempty"`
	Channels []botChannelView `json:"channels"`
}

type botMessageEvent struct {
	Type       string `json:"type"` // "message"
	GuildID    string `json:"guildId"`
	Guild      string `json:"guild"`
	ChannelID  string `json:"channelId"`
	Channel    string `json:"channel"`
	MessageID  string `json:"messageId"`
	Sender     string `json:"sender"`
	SenderName string `json:"senderName"`
	Content    string `json:"content"`
	ReplyTo    string `json:"replyTo,omitempty"`
	Sent       string `json:"sent"`
	Kind       string `json:"kind,omitempty"`
	DM         bool   `json:"dm,omitempty"`
}

type botMember struct {
	Fingerprint string `json:"fingerprint"`
	Name        string `json:"name"`
}

func guildViews(guilds []domain.Guild) []botGuildView {
	sort.Slice(guilds, func(i, j int) bool {
		if guilds[i].Name != guilds[j].Name {
			return guilds[i].Name < guilds[j].Name
		}
		return guilds[i].ID < guilds[j].ID
	})
	out := make([]botGuildView, 0, len(guilds))
	for _, g := range guilds {
		v := botGuildView{ID: g.ID, Name: g.Name, DM: g.Kind == "dm", Channels: []botChannelView{}}
		for _, c := range g.Channels {
			v.Channels = append(v.Channels, botChannelView{ID: c.ID, Name: c.Name, Type: c.Type, Ref: g.Name + "/#" + c.Name})
		}
		out = append(out, v)
	}
	return out
}

// ---- The bot ----

type bot struct {
	svc   botService
	self  string
	since time.Time // messages sent before this are history, never news

	out  chan any // every stdout line goes through here, in order
	done chan struct{}

	mu   sync.Mutex
	seen map[string]string // message id -> content last reported
	ring []string          // insertion order of seen, for eviction

	// Membership as last reported, guild id -> member fingerprints.
	members map[string]map[string]bool
	names   map[string]string // guild id -> name, for guild leave events
	update  chan struct{}
}

// seenCap bounds the message-id memory. It has to outlive the re-emits a
// message gets while it is fresh (reactions, an edit). Past it, the start-time
// rule below still keeps anything older than this process from being news.
const seenCap = 8192

// newsSlack is how far before the bot started a message may have been sent
// and still be reported. The feed re-emits a stored message whenever somebody
// reacts to it, and a process that has just started has seen none of them, so
// without a cut-off a reaction to yesterday's "!ping" would be yesterday's
// "!ping" all over again. The slack absorbs sender clock skew and the
// mailbox deposits made in the moments before a restart.
const newsSlack = 5 * time.Minute

// deletedMark is what the id memory holds for a deletion already reported.
const deletedMark = "\x00deleted"

func newBot(svc botService) *bot {
	return &bot{
		svc:     svc,
		self:    svc.Fingerprint(),
		since:   time.Now().Add(-newsSlack),
		out:     make(chan any, 256),
		done:    make(chan struct{}),
		seen:    map[string]string{},
		members: map[string]map[string]bool{},
		names:   map[string]string{},
		update:  make(chan struct{}, 1),
	}
}

func (b *bot) emit(v any) {
	select {
	case b.out <- v:
	case <-b.done:
	}
}

func (b *bot) reply(id json.RawMessage, fields map[string]any) {
	if len(id) > 0 {
		fields["id"] = id
	}
	b.emit(fields)
}

func (b *bot) fail(id json.RawMessage, err error) {
	b.reply(id, map[string]any{"type": "error", "error": err.Error()})
}

// remember records a message id and reports whether it was new, plus the
// content previously reported for it.
func (b *bot) remember(id, content string) (fresh bool, prev string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	prev, known := b.seen[id]
	if prev != deletedMark { // a deletion is final; later re-emits do not undo it
		b.seen[id] = content
	}
	if !known {
		b.ring = append(b.ring, id)
		if len(b.ring) > seenCap {
			delete(b.seen, b.ring[0])
			b.ring = b.ring[1:]
		}
	}
	return !known, prev
}

// onMessage turns the Service's message feed into events. That feed fires for
// every persisted message, sent or received, and again whenever a stored
// message changes (a reaction, an edit, a deletion) — so the bot keeps a
// memory of ids and reports a message once, then edits and deletions as such.
func (b *bot) onMessage(m domain.Message) {
	fresh, prev := b.remember(m.ID, m.Content)
	sender := b.svc.AccountFingerprintOf(m.Sender)
	// Its own words never come back as input: a bot that answers itself
	// loops. Its other devices count as itself too.
	if sender == b.self || b.svc.SenderBlocked(m.Sender) {
		return
	}
	g, c, err := resolveChannel(b.svc.Guilds(), m.ChannelID)
	if err != nil {
		return // a channel we cannot place (e.g. a closed DM): nothing to address a reply to
	}
	switch {
	case fresh && !m.Deleted && !m.Backfill && !m.Sent.Before(b.since):
		// History is not news, whether it arrives by sync or as a re-emit of
		// an old row: replaying it would have a bot answer week-old commands
		// on every restart.
		name := b.svc.NickOf(g.ID, sender)
		if name == "" {
			name = m.Name
		}
		if name == "" {
			name = b.svc.ProfileName(sender)
		}
		b.emit(botMessageEvent{
			Type: "message", GuildID: g.ID, Guild: g.Name, ChannelID: c.ID, Channel: c.Name,
			MessageID: m.ID, Sender: sender, SenderName: name, Content: m.Content,
			ReplyTo: m.ReplyTo, Sent: m.Sent.UTC().Format(time.RFC3339Nano), Kind: m.Kind,
			DM: g.Kind == "dm",
		})
	case fresh:
	case m.Deleted:
		if prev != deletedMark {
			b.remember(m.ID, deletedMark)
			b.emit(map[string]any{"type": "delete", "guildId": g.ID, "channelId": c.ID, "channel": c.Name, "messageId": m.ID})
		}
	case m.Edited && m.Content != prev:
		b.emit(map[string]any{"type": "edit", "guildId": g.ID, "channelId": c.ID, "channel": c.Name, "messageId": m.ID, "sender": sender, "content": m.Content})
	}
}

// snapshotMembers reads current membership as account fingerprints.
func (b *bot) snapshotMembers() (map[string]map[string]bool, map[string]string) {
	members := map[string]map[string]bool{}
	names := map[string]string{}
	for _, g := range b.svc.Guilds() {
		creds, err := b.svc.GuildMembers(g.ID)
		if err != nil {
			continue
		}
		set := map[string]bool{}
		for _, cred := range creds {
			set[b.svc.AccountFingerprintOf(cred)] = true
		}
		members[g.ID] = set
		names[g.ID] = g.Name
	}
	return members, names
}

func (b *bot) memberName(guildID, fpr string) string {
	if n := b.svc.NickOf(guildID, fpr); n != "" {
		return n
	}
	return b.svc.ProfileName(fpr)
}

// diffMembers reports guilds joined or left and members arriving or leaving
// since the last snapshot. Members of a guild the bot has just joined are not
// each reported as joining — they were there first.
func (b *bot) diffMembers() {
	now, names := b.snapshotMembers()
	ids := func(m map[string]bool) []string {
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	gids := make([]string, 0, len(now))
	for id := range now {
		gids = append(gids, id)
	}
	sort.Strings(gids)
	for _, gid := range gids {
		before, had := b.members[gid]
		if !had {
			b.emit(map[string]any{"type": "guild", "action": "join", "guildId": gid, "guild": names[gid]})
			continue
		}
		for _, f := range ids(now[gid]) {
			if !before[f] && f != b.self {
				b.emit(map[string]any{"type": "member", "action": "join", "guildId": gid, "guild": names[gid], "fingerprint": f, "name": b.memberName(gid, f)})
			}
		}
		for _, f := range ids(before) {
			if !now[gid][f] && f != b.self {
				b.emit(map[string]any{"type": "member", "action": "leave", "guildId": gid, "guild": names[gid], "fingerprint": f, "name": b.memberName(gid, f)})
			}
		}
	}
	for gid := range b.members {
		if _, still := now[gid]; !still {
			b.emit(map[string]any{"type": "guild", "action": "leave", "guildId": gid, "guild": b.names[gid]})
		}
	}
	b.members, b.names = now, names
}

// watchMembers coalesces guild updates — the Service fires one for nearly
// every state change, often in bursts — into one membership diff.
func (b *bot) watchMembers(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-b.update:
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(150 * time.Millisecond):
		}
		b.diffMembers()
	}
}

// handle runs one parsed command and writes its reply.
func (b *bot) handle(c botCommand) (quit bool) {
	switch c.Cmd {
	case "list":
		b.reply(c.ID, map[string]any{"type": "guilds", "guilds": guildViews(b.svc.Guilds())})
	case "send":
		_, ch, err := resolveChannel(b.svc.Guilds(), c.Channel)
		if err != nil {
			b.fail(c.ID, err)
			return
		}
		m, err := b.svc.SendMessage(ch.ID, c.Text, strings.TrimSpace(c.ReplyTo), "")
		if err != nil {
			b.fail(c.ID, err)
			return
		}
		b.reply(c.ID, map[string]any{"type": "sent", "channelId": ch.ID, "messageId": m.ID})
	case "react":
		_, ch, err := resolveChannel(b.svc.Guilds(), c.Channel)
		if err != nil {
			b.fail(c.ID, err)
			return
		}
		if err := b.svc.ToggleReaction(ch.ID, strings.TrimSpace(c.Message), strings.TrimSpace(c.Emoji)); err != nil {
			b.fail(c.ID, err)
			return
		}
		b.reply(c.ID, map[string]any{"type": "ok", "cmd": "react"})
	case "join":
		g, err := b.svc.JoinViaInvite(strings.TrimSpace(c.Invite))
		if err != nil {
			b.fail(c.ID, err)
			return
		}
		b.reply(c.ID, map[string]any{"type": "joined", "guild": guildViews([]domain.Guild{g})[0]})
	case "members":
		var gid string
		for _, g := range b.svc.Guilds() {
			if g.ID == c.Guild || g.Name == c.Guild {
				if gid != "" {
					b.fail(c.ID, fmt.Errorf("guild %q is ambiguous; use its id", c.Guild))
					return
				}
				gid = g.ID
			}
		}
		if gid == "" {
			b.fail(c.ID, fmt.Errorf("no guild %q", c.Guild))
			return
		}
		creds, err := b.svc.GuildMembers(gid)
		if err != nil {
			b.fail(c.ID, err)
			return
		}
		seen := map[string]bool{}
		list := []botMember{}
		for _, cred := range creds {
			f := b.svc.AccountFingerprintOf(cred)
			if seen[f] {
				continue // one account, several devices
			}
			seen[f] = true
			name := b.memberName(gid, f)
			if f == b.self {
				name = b.svc.DisplayName()
			}
			list = append(list, botMember{Fingerprint: f, Name: name})
		}
		b.reply(c.ID, map[string]any{"type": "members", "guildId": gid, "members": list})
	case "name":
		if err := b.svc.SetDisplayName(c.Text); err != nil {
			b.fail(c.ID, err)
			return
		}
		b.reply(c.ID, map[string]any{"type": "ok", "cmd": "name"})
	case "quit":
		b.reply(c.ID, map[string]any{"type": "ok", "cmd": "quit"})
		return true
	}
	return false
}

// errLineTooLong marks a command line over maxBotLine; it was discarded.
var errLineTooLong = errors.New("command line too long")

// readLine returns the next line without its newline, discarding (and
// reporting) one longer than max instead of growing without bound.
func readLine(r *bufio.Reader, max int) ([]byte, error) {
	var buf []byte
	over := false
	for {
		chunk, err := r.ReadSlice('\n')
		if !over {
			if len(buf)+len(chunk) > max+1 {
				over, buf = true, nil
			} else {
				buf = append(buf, chunk...)
			}
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if over {
			if err != nil {
				return nil, err
			}
			return nil, errLineTooLong
		}
		if err == io.EOF && len(buf) > 0 {
			return buf, nil // a last line with no newline still counts
		}
		return buf, err
	}
}

// run speaks the protocol until ctx ends, stdin closes, or a quit command.
// Every queued line is written before it returns.
func (b *bot) run(ctx context.Context, in io.Reader, out io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Writer: the only goroutine that touches stdout, so lines never
	// interleave and arrive in the order they were produced.
	written := make(chan error, 1)
	go func() {
		var werr error
		write := func(v any) {
			if werr == nil {
				var line []byte
				if line, werr = encodeLine(v); werr == nil {
					_, werr = out.Write(line)
				}
			}
		}
		for {
			select {
			case v := <-b.out:
				write(v)
			case <-b.done:
				for {
					select {
					case v := <-b.out:
						write(v)
					default:
						written <- werr
						return
					}
				}
			}
		}
	}()

	// Membership baseline before anything can change it, then the feeds.
	b.members, b.names = b.snapshotMembers()
	b.svc.OnGuildUpdate(func() {
		select {
		case b.update <- struct{}{}:
		default:
		}
	})
	b.svc.OnMessage(b.onMessage)
	go b.watchMembers(ctx)

	b.emit(map[string]any{
		"type":        "ready",
		"fingerprint": b.self,
		"peerId":      b.svc.PeerID(),
		"name":        b.svc.DisplayName(),
		"guilds":      guildViews(b.svc.Guilds()),
	})

	lines := make(chan []byte)
	readErr := make(chan error, 1)
	go func() {
		r := bufio.NewReaderSize(in, 64*1024)
		for {
			line, err := readLine(r, maxBotLine)
			if err == errLineTooLong {
				b.fail(nil, fmt.Errorf("%w (limit %d bytes); discarded", errLineTooLong, maxBotLine))
				continue
			}
			if len(line) > 0 {
				select {
				case lines <- line:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				readErr <- err
				return
			}
		}
	}()

	var result error
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case err := <-readErr:
			if err != io.EOF {
				result = err
			}
			break loop
		case line := <-lines:
			c, err := parseBotCommand(line)
			if err != nil {
				b.fail(c.ID, err)
				continue
			}
			if c.Cmd == "" {
				continue
			}
			if b.handle(c) {
				break loop
			}
		}
	}

	// Stop producing and let the writer drain what is queued. Service
	// callbacks may still fire until the caller closes the Service; once done
	// is closed, emit never blocks on them.
	cancel()
	close(b.done)
	if werr := <-written; werr != nil && result == nil {
		result = werr
	}
	return result
}

// encodeLine renders one stdout line. Maps put "type" and then "id" first —
// the two keys a reader scans for — with the rest in sorted order.
func encodeLine(v any) ([]byte, error) {
	enc := func(x any) ([]byte, error) {
		var buf bytes.Buffer
		e := json.NewEncoder(&buf)
		e.SetEscapeHTML(false)
		if err := e.Encode(x); err != nil {
			return nil, err
		}
		return bytes.TrimRight(buf.Bytes(), "\n"), nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		line, err := enc(v)
		return append(line, '\n'), err
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		if k != "type" && k != "id" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range []string{"id", "type"} {
		if _, ok := m[k]; ok {
			keys = append([]string{k}, keys...)
		}
	}
	out := []byte{'{'}
	for i, k := range keys {
		if i > 0 {
			out = append(out, ',')
		}
		kb, _ := enc(k)
		vb, err := enc(m[k])
		if err != nil {
			return nil, err
		}
		out = append(append(append(out, kb...), ':'), vb...)
	}
	return append(out, '}', '\n'), nil
}

// runBot boots the identity and networking and hands stdin/stdout to the line
// protocol. stdout is reserved for protocol lines: anything else in the
// process that prints is pointed at stderr before the Service starts.
func runBot() error {
	proto := os.Stdout
	os.Stdout = os.Stderr
	log.SetOutput(os.Stderr)

	dataDir, err := app.DataDir()
	if err != nil {
		return err
	}
	pass, err := readPassphrase()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	svc, err := app.Start(ctx, serviceConfig(dataDir, pass))
	if err != nil {
		return err
	}
	defer svc.Close()
	fmt.Fprintf(os.Stderr, "concord bot %s running in %s\n", svc.Fingerprint(), dataDir)
	return newBot(svc).run(ctx, os.Stdin, proto)
}
