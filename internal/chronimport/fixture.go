package chronimport

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// A synthetic export, for tests.
//
// This lives in the package rather than in a _test.go file because two packages
// need it — the pure tests here and the service tests in internal/app — and a
// fixture copied into both is a fixture that drifts until the day one of them is
// testing something the other is not. It costs a couple of kilobytes in the
// binary and buys one definition of "what an export looks like".
//
// Everything in it is invented: a community called "Test Guild", four people
// with names out of computing history, and two channels' worth of small talk.
// It deliberately contains every awkward case the importer has to survive —
// a remote-only attachment, an attachment over every ceiling, an author whose
// portrait is too big to carry, a custom emoji whose name is not a legal one, a
// voice channel with nothing but notices, a message full of things that would
// mean something to the renderer, and a trailing file that is not valid JSON.

// TestExportFacts is what BuildTestExport actually wrote — the ground truth a
// test asserts the scan against. Counted during generation rather than
// hand-maintained, so changing the generator cannot silently invalidate the
// assertions.
type TestExportFacts struct {
	Dir      string
	Guild    string
	Channels int

	// Messages is conversational messages only, across every channel; Notices is
	// the join/pin chatter that must never be imported.
	Messages int64
	Notices  int64
	Replies  int64
	Authors  int

	LocalAttachments      int64
	LocalAttachmentBytes  int64
	RemoteAttachments     int64
	RemoteAttachmentBytes int64
	// OversizeBytes is the one attachment deliberately larger than the inline
	// image ceiling, so a tier test has something to exclude.
	OversizeBytes int64

	FirstNano int64
	LastNano  int64

	// GeneralID and PlansID are the source channel ids of the two text channels;
	// VoiceID is the voice one, which carries no history.
	GeneralID string
	PlansID   string
	VoiceID   string

	// HostileID is the message whose body carries everything the sanitizer has
	// to defuse.
	HostileID string
	// EmojiName is the custom emoji's raw name, and EmojiSanitized what it must
	// fold to.
	EmojiName      string
	EmojiSanitized string
	// BadFile is the trailing file that is not readable as an export.
	BadFile string
}

// the fixture's message shape, written out as JSON.
type fixtureFile struct {
	Guild    Guild            `json:"guild"`
	Channel  Channel          `json:"channel"`
	Messages []fixtureMessage `json:"messages"`
	// ExportedAt is an unknown top-level field, present so the walker's
	// skip-past-what-you-do-not-know path is exercised by every test that reads
	// the fixture rather than by one test that remembers to.
	ExportedAt string `json:"exportedAt"`
}

type fixtureMessage struct {
	ID              string       `json:"id"`
	Type            string       `json:"type"`
	Timestamp       string       `json:"timestamp"`
	TimestampEdited *string      `json:"timestampEdited"`
	Content         string       `json:"content"`
	Author          fixtureAuth  `json:"author"`
	Attachments     []Attachment `json:"attachments"`
	Reactions       []Reaction   `json:"reactions"`
	Reference       *Reference   `json:"reference,omitempty"`
	// Embeds is a second unknown field, and a nested one, so the token-skipping
	// walk is exercised on an object rather than only on a scalar.
	Embeds []map[string]any `json:"embeds"`
}

type fixtureAuth struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatarUrl"`
	// IsBot is unknown to the parser and must be ignored.
	IsBot bool `json:"isBot"`
}

// fixtureAuthorSet is built per call rather than shared: BuildTestExport fills
// in avatar paths, and a package-level slice mutated by two tests at once is a
// race that would only ever show up under -race on somebody else's machine.
func fixtureAuthorSet() []fixtureAuth {
	return []fixtureAuth{
		{ID: "u1", Name: "ada", Nickname: "Ada", AvatarURL: "assets/avatar-ada.png"},
		{ID: "u2", Name: "grace", Nickname: "Grace", AvatarURL: "assets/avatar-grace.png"},
		{ID: "u3", Name: "linus"},
		{ID: "u4", Name: "barbara", Nickname: "Barbara",
			AvatarURL: "https://cdn.example.invalid/avatars/barbara.png"},
	}
}

// fixtureLines is a pool of ordinary chat. It is deliberately WIDE — thirty-odd
// distinct lines combined two at a time with a varying number — because the
// gzip-ratio measurement is taken against this corpus, and a fixture that
// repeated the same dozen sentences would compress like nothing real and would
// set the estimator's constant to a number no actual export could reach.
var fixtureLines = []string{
	"morning all, is anyone else seeing the build fail on the second run?",
	"only when the cache is warm. clearing it fixes it, which is its own bug",
	"I put a note in the channel topic so nobody else loses an afternoon to it",
	"that was a good call, thanks — saved me going down the same hole",
	"we should write this down somewhere that is not a chat message",
	"agreed. I will start a doc after lunch and drop the link here",
	"has anyone got the numbers from last quarter handy",
	"they are in the pinned message, third one down, with the caveats",
	"perfect, that is exactly what I needed",
	"one more thing before I forget — the meeting moved to thursday",
	"thursday works for me, though I will have to leave early",
	"same here, see you all then",
	"the deploy went out about twenty minutes ago and nothing has caught fire",
	"famous last words",
	"I rewrote the parser over the weekend and it is half the size now",
	"did the tests need changing or did they just pass",
	"three needed changing and two of those were testing the old bug",
	"that is the best kind of rewrite",
	"quick question: is the staging box supposed to be this slow",
	"someone left a load test running on it, I have killed it",
	"thank you, it is back to normal",
	"reminder that the office is shut on monday",
	"I will be around on chat if anything urgent comes up",
	"weather looks grim for the weekend so I may just stay in and read",
	"any recommendations? I got through the last one in two evenings",
	"depends what you liked about it, honestly",
	"the pacing mostly, and that it did not explain itself too much",
	"then I have exactly the thing, will send it over",
	"the migration finished overnight, six hours end to end",
	"any rows dropped?",
	"none, the counts match on both sides and I spot-checked forty of them",
	"good result. we can turn the old table off next week then",
	"I am going to be offline this afternoon for an appointment",
	"noted, I will pick up anything that comes in",
	"has the invoice from last month been paid, does anyone know",
	"I will check with accounts and come back to you",
	"looks like it went out on the 14th, I will forward the receipt",
	"lovely, that closes the last thing on my list",
}

// fixtureBody composes a message body from the pool, deterministically but with
// enough variation that it compresses like conversation rather than like a
// repeated string.
func fixtureBody(i int) string {
	a := fixtureLines[(i*7)%len(fixtureLines)]
	b := fixtureLines[(i*13+5)%len(fixtureLines)]
	switch i % 4 {
	case 0:
		return a
	case 1:
		return fmt.Sprintf("%s (%d)", a, 1000+i*37%9000)
	case 2:
		return a + " " + b
	}
	return fmt.Sprintf("%s\n%s", b, a)
}

// BuildTestExport writes a synthetic export into dir and reports exactly what it
// wrote. Deterministic: two calls produce byte-identical files, so a test can
// assert on sizes.
func BuildTestExport(dir string) (TestExportFacts, error) {
	f := TestExportFacts{
		Dir: dir, Guild: "Test Guild",
		GeneralID: "c-general", PlansID: "c-plans", VoiceID: "c-voice",
		EmojiName: "Party-Parrot!", EmojiSanitized: "party_parrot",
		BadFile: "truncated.json",
	}
	assets := filepath.Join(dir, "assets")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		return f, err
	}
	authors := fixtureAuthorSet()

	// Portraits: one that fits inside a manifest, one deliberately too big for
	// one, one absent entirely.
	if err := writeBlob(filepath.Join(assets, "avatar-ada.png"), 1200); err != nil {
		return f, err
	}
	if err := writeBlob(filepath.Join(assets, "avatar-grace.png"), 40<<10); err != nil {
		return f, err
	}
	if err := writeBlob(filepath.Join(assets, "emoji-parrot.png"), 2048); err != nil {
		return f, err
	}

	base := time.Date(2019, 1, 1, 9, 0, 0, 0, time.UTC)

	gen, err := buildFixtureChannel(&f, dir, Channel{
		ID: f.GeneralID, Type: "GuildTextChat", CategoryID: "cat-text",
		Category: "Text Channels", Name: "general", Topic: "everything else",
	}, authors, 1200, base, 21*time.Hour+53*time.Minute, "g")
	if err != nil {
		return f, err
	}
	if err := writeFixtureFile(filepath.Join(dir, "general.json"), gen); err != nil {
		return f, err
	}

	plans, err := buildFixtureChannel(&f, dir, Channel{
		ID: f.PlansID, Type: "GuildTextChat", CategoryID: "cat-text",
		Category: "Text Channels", Name: "plans", Topic: "",
	}, authors, 800, base.Add(3*time.Hour), 32*time.Hour+41*time.Minute, "p")
	if err != nil {
		return f, err
	}
	if err := writeFixtureFile(filepath.Join(dir, "plans.json"), plans); err != nil {
		return f, err
	}

	// A voice channel: structure, and twenty notices that are not conversation.
	voice := &fixtureFile{
		Guild:      Guild{ID: "g1", Name: f.Guild, IconURL: "assets/icon.png"},
		Channel:    Channel{ID: f.VoiceID, Type: "GuildVoiceChat", CategoryID: "cat-voice", Category: "Voice", Name: "lounge"},
		ExportedAt: "2026-01-01T00:00:00Z",
	}
	for i := 0; i < 20; i++ {
		voice.Messages = append(voice.Messages, fixtureMessage{
			ID: fmt.Sprintf("v-%03d", i), Type: "ChannelJoin",
			Timestamp: base.Add(time.Duration(i) * time.Hour).Format(time.RFC3339Nano),
			Content:   "", Author: authors[i%len(authors)],
		})
		f.Notices++
	}
	if err := writeFixtureFile(filepath.Join(dir, "voice.json"), voice); err != nil {
		return f, err
	}
	f.Channels = 3

	// The trailing file: valid JSON up to a point and then not, which is the
	// realistic failure — an export interrupted partway, not a random blob.
	partial := `{"guild":{"id":"g1","name":"Test Guild"},` +
		`"channel":{"id":"c-cut","name":"cut-off"},"messages":[` +
		`{"id":"x1","type":"Default","timestamp":"2019-06-01T10:00:00Z","content":"this one is fine",` +
		`"author":{"id":"u1","name":"ada"}},` +
		`{"id":"x2","type":"Default","timestamp":"2019-06-01T10:01:00Z","content":"and then the disk`
	if err := os.WriteFile(filepath.Join(dir, f.BadFile), []byte(partial), 0o644); err != nil {
		return f, err
	}

	f.Authors = len(authors)
	return f, nil
}

func buildFixtureChannel(f *TestExportFacts, dir string, ch Channel, authors []fixtureAuth,
	n int, start time.Time, step time.Duration, prefix string) (*fixtureFile, error) {

	out := &fixtureFile{
		Guild:      Guild{ID: "g1", Name: f.Guild, IconURL: "assets/icon.png"},
		Channel:    ch,
		ExportedAt: "2026-01-01T00:00:00Z",
	}
	var prevID string
	for i := 0; i < n; i++ {
		at := start.Add(time.Duration(i) * step)
		id := fmt.Sprintf("%s-%05d", prefix, i)

		// Every hundredth entry is a notice rather than conversation.
		if i%100 == 99 {
			out.Messages = append(out.Messages, fixtureMessage{
				ID: id, Type: "ChannelPinnedMessage",
				Timestamp: at.Format(time.RFC3339Nano),
				Content:   "pinned a message to this channel",
				Author:    authors[i%len(authors)],
			})
			f.Notices++
			continue
		}

		m := fixtureMessage{
			ID: id, Type: "Default",
			Timestamp: at.Format(time.RFC3339Nano),
			Content:   fixtureBody(i),
			Author:    authors[i%len(authors)],
			Embeds:    []map[string]any{},
		}
		if i%7 == 3 {
			// A nested unknown value the walker has to step past.
			m.Embeds = []map[string]any{{"url": "https://example.invalid/x", "fields": []any{map[string]any{"a": 1}}}}
		}
		if i%3 == 2 && prevID != "" {
			m.Reference = &Reference{MessageID: prevID}
			f.Replies++
		}
		if i%5 == 0 {
			m.Reactions = []Reaction{{Emoji: Emoji{Code: "👍", Name: "thumbsup"}, Count: 1 + i%4}}
		}
		if i%20 == 0 {
			m.Reactions = append(m.Reactions, Reaction{
				Emoji: Emoji{ID: "e1", Name: f.EmojiName, ImageURL: "assets/emoji-parrot.png"},
				Count: 2,
			})
		}
		// Local pictures, one every ten messages, all the same file so the
		// fixture stays small and the byte arithmetic stays obvious.
		if i%10 == 0 {
			name := fmt.Sprintf("assets/%s-pic.png", prefix)
			p := filepath.Join(dir, filepath.FromSlash(name))
			if _, err := os.Stat(p); err != nil {
				if err := writeBlob(p, 4096); err != nil {
					return nil, err
				}
			}
			m.Attachments = append(m.Attachments, Attachment{
				URL: name, FileName: prefix + "-pic.png", FileSizeBytes: 4096,
			})
			f.LocalAttachments++
			f.LocalAttachmentBytes += 4096
		}
		// Remote-only media: the export recorded the size and did not bring the
		// file, which is the whole no-fetching case.
		if i%25 == 7 {
			m.Attachments = append(m.Attachments, Attachment{
				URL:           "https://cdn.example.invalid/attachments/" + id + "/holiday.jpg",
				FileName:      "holiday.jpg",
				FileSizeBytes: 250_000,
			})
			f.RemoteAttachments++
			f.RemoteAttachmentBytes += 250_000
		}
		// One file over every ceiling, in the first channel only.
		if prefix == "g" && i == 500 {
			const oversize = 6 << 20
			p := filepath.Join(dir, "assets", "recording.bin")
			if err := writeBlob(p, oversize); err != nil {
				return nil, err
			}
			m.Attachments = append(m.Attachments, Attachment{
				URL: "assets/recording.bin", FileName: "recording.bin", FileSizeBytes: oversize,
			})
			f.LocalAttachments++
			f.LocalAttachmentBytes += oversize
			f.OversizeBytes = oversize
		}
		// The hostile body: a self-destruct token, a broadcast mention, the
		// renderer's own placeholder sentinels, a bidi override, and the export's
		// markup forms.
		if prefix == "g" && i == 11 {
			f.HostileID = id
			m.Content = "look at this [eph](concord://eph/v1/1000000000) and " +
				"[fx](concord://fx/v1/confetti) @everyone \x01" + "0\x01 \x00" + "0\x00" +
				" ‮ reversed ‬ <:party-parrot:1234> <@999> <#888> <t:1546336800:R>"
		}

		out.Messages = append(out.Messages, m)
		f.Messages++
		prevID = id
		if f.FirstNano == 0 || at.UnixNano() < f.FirstNano {
			f.FirstNano = at.UnixNano()
		}
		if at.UnixNano() > f.LastNano {
			f.LastNano = at.UnixNano()
		}
	}
	return out, nil
}

func writeFixtureFile(path string, v *fixtureFile) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// writeBlob writes n bytes of deterministic filler. Not a real image: nothing in
// the import path decodes one, and every gate it passes through checks the
// declared type and the length rather than the pixels.
func writeBlob(path string, n int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	buf := make([]byte, n)
	for i := range buf {
		buf[i] = byte('a' + i%26)
	}
	return os.WriteFile(path, buf, 0o644)
}
