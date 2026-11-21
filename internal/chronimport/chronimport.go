// Package chronimport reads channel-export JSON archives — the de-facto shape
// produced by widely used chat-export tools — and turns them into the material
// a guild can carry as a chronicle.
//
// # The shape it expects
//
// An export is a DIRECTORY of JSON files, one per exported channel (a big
// channel is often split across several files, each carrying the same channel
// header and a slice of its history). Every file looks like this, and every
// field in it is optional as far as this package is concerned:
//
//	{
//	  "guild":   {"id": "…", "name": "Some Community", "iconUrl": "…"},
//	  "channel": {"id": "…", "type": "GuildTextChat", "categoryId": "…",
//	              "category": "General", "name": "general", "topic": "…"},
//	  "messages": [
//	    {"id": "…", "type": "Default",
//	     "timestamp": "2019-04-02T18:21:03.117+00:00",
//	     "timestampEdited": null,
//	     "content": "hello",
//	     "author": {"id": "…", "name": "ada", "nickname": "Ada",
//	                "avatarUrl": "…"},
//	     "attachments": [{"url": "…", "fileName": "cat.png",
//	                      "fileSizeBytes": 20480}],
//	     "reactions":   [{"emoji": {"id": "…", "code": "👍", "name": "thumbsup",
//	                                "imageUrl": "…"}, "count": 3}],
//	     "reference":   {"messageId": "…"}}
//	  ]
//	}
//
// The parser is deliberately TOLERANT, because an export is a file somebody
// else's tool wrote, possibly years ago, possibly from a fork of that tool, and
// the alternative to tolerance is a decade of history that will not import
// because one field was renamed. Unknown fields are ignored; missing fields take
// their zero value; a message with no parseable timestamp or no id is counted as
// malformed and skipped rather than fataling the file; a file that is not JSON
// at all is counted and skipped the same way. Only two things are actually
// required of a message: an id and a timestamp that some known layout parses.
//
// # It never fetches anything
//
// An export made "with assets" carries its media as files on disk and points at
// them with relative paths. An export made without assets points at a remote CDN
// instead. THIS PACKAGE NEVER FETCHES A REMOTE URL, and neither does the
// importer built on it. That is a stance, not an omission:
//
//   - An import is a bulk operation over somebody else's data. Turning it into
//     thousands of requests to a third party would tell that third party the
//     whole shape of what is being moved, from an IP address that has nothing to
//     do with the account the export came from.
//   - The links rot. A chronicle is meant to still be readable in ten years; a
//     signed archive full of URLs that 404 is a promise the format cannot keep.
//   - It would make the importer's output depend on the network, so two runs
//     over the same directory could disagree.
//
// A remote-only attachment is therefore COUNTED in the scan — it carries
// fileSizeBytes, so the size report is honest about what is not coming — and
// imported as a short placeholder line naming the file and its size. If the user
// wants the media, they re-export with assets and import again.
//
// # Path safety
//
// A local asset path comes out of the export file, which means it is untrusted
// input naming a file on the importing machine. Every path is resolved against
// the export directory, refused if it escapes it or passes through a symlink,
// and opened without following links (see localAssetPath and OpenLocalAsset),
// so an export cannot talk the importer into sealing /etc/shadow into a
// guild's history.
package chronimport

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Guild is the export's own idea of the community it came from. Only Name is
// used, as the source label on the manifest.
type Guild struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	IconURL string `json:"iconUrl"`
}

// Channel is one exported channel's header. CategoryID may be absent even when
// Category (the name) is present, and vice versa; the importer treats the name
// as authoritative because that is what it matches against a real guild.
type Channel struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	CategoryID string `json:"categoryId"`
	Category   string `json:"category"`
	Name       string `json:"name"`
	Topic      string `json:"topic"`
}

// Author is a message's writer as the export names them. Nickname is the
// per-community override where the tool recorded one; Name is the account name.
type Author struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatarUrl"`
}

// Display is the name to show: the community nickname where there was one,
// otherwise the account name, otherwise the id so an author is never nameless.
func (a Author) Display() string {
	for _, s := range []string{a.Nickname, a.Name, a.ID} {
		if t := strings.TrimSpace(s); t != "" {
			return t
		}
	}
	return "unknown"
}

// Key identifies an author across files. The id where the export has one (names
// change, ids do not), else the display name.
func (a Author) Key() string {
	if a.ID != "" {
		return "id:" + a.ID
	}
	return "name:" + a.Display()
}

// Attachment is one file on a message. URL is either a relative path into the
// export's asset directory or a remote link; FileSizeBytes is what the tool
// recorded and is present in both modes, which is what lets the scan report the
// true size of an export whose media is not actually there.
type Attachment struct {
	URL           string `json:"url"`
	FileName      string `json:"fileName"`
	FileSizeBytes int64  `json:"fileSizeBytes"`
}

// Emoji is a reaction's emoji. A unicode reaction carries Code (or Name) and no
// ID; a custom one carries an ID and an ImageURL, which is the only reason the
// importer can bring custom emoji across at all — with assets, that URL is a
// file on disk.
type Emoji struct {
	ID         string `json:"id"`
	Code       string `json:"code"`
	Name       string `json:"name"`
	ImageURL   string `json:"imageUrl"`
	IsAnimated bool   `json:"isAnimated"`
}

// Custom reports whether this is a community-specific uploaded emoji rather than
// a unicode one.
func (e Emoji) Custom() bool { return e.ID != "" }

// Label is how the emoji appears in a reaction tally: the literal character for
// a unicode emoji, the :shortcode: form for a custom one.
func (e Emoji) Label() string {
	if !e.Custom() {
		if c := strings.TrimSpace(e.Code); c != "" {
			return c
		}
		return strings.TrimSpace(e.Name)
	}
	if n := strings.TrimSpace(e.Name); n != "" {
		return ":" + n + ":"
	}
	return ":" + e.ID + ":"
}

// Reaction is one emoji's tally on a message. The people who reacted are not
// carried even when the export lists them: they are mostly not members of the
// guild the history is landing in, and their names would double the archive for
// a detail nobody scrolling 2014 is looking for.
type Reaction struct {
	Emoji Emoji `json:"emoji"`
	Count int   `json:"count"`
}

// Reference is a reply's pointer at what it answers.
type Reference struct {
	MessageID string `json:"messageId"`
}

// Message is one exported message. Timestamps stay strings here and are parsed
// by At(), because a message whose stamp does not parse is a message to skip,
// not a file to abandon.
type Message struct {
	ID              string       `json:"id"`
	Type            string       `json:"type"`
	Timestamp       string       `json:"timestamp"`
	TimestampEdited string       `json:"timestampEdited"`
	Content         string       `json:"content"`
	Author          Author       `json:"author"`
	Attachments     []Attachment `json:"attachments"`
	Reactions       []Reaction   `json:"reactions"`
	Reference       *Reference   `json:"reference"`
}

// UnmarshalJSON reads a message the way the package doc promises: tolerantly.
// An id or a timestamp written as a JSON number (an epoch, a snowflake) is
// taken as one; anything else of the wrong type is an error for THIS message
// only, which walkMessages counts and steps over.
func (m *Message) UnmarshalJSON(b []byte) error {
	type plain Message
	var aux struct {
		plain
		ID        json.RawMessage `json:"id"`
		Timestamp json.RawMessage `json:"timestamp"`
	}
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}
	*m = Message(aux.plain)
	m.ID = scalarText(aux.ID)
	m.Timestamp = scalarText(aux.Timestamp)
	if n, err := strconv.ParseFloat(m.Timestamp, 64); err == nil && len(aux.Timestamp) > 0 && aux.Timestamp[0] != '"' {
		m.Timestamp = epochStamp(n)
	}
	return nil
}

// scalarText is a JSON string's text, or a number's digits as written; "" for
// anything else (null, an object, an array).
func scalarText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s
		}
		return ""
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	return ""
}

// epochStamp turns a numeric timestamp into the RFC 3339 form the rest of the
// package reads. Seconds, milliseconds and microseconds are told apart by
// size: no conversation happened before 2001 in milliseconds-as-seconds.
func epochStamp(n float64) string {
	var t time.Time
	switch {
	case n >= 1e15:
		t = time.UnixMicro(int64(n))
	case n >= 1e12:
		t = time.UnixMilli(int64(n))
	default:
		sec := math.Floor(n)
		t = time.Unix(int64(sec), int64((n-sec)*1e9))
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// timeLayouts are the stamp forms seen in the wild, tried in order. RFC3339 and
// its nanosecond form cover current tools; the two space-separated forms cover
// older ones and hand-edited files; the date-only form covers a stripped export.
// All of them are parsed as written, so an offset in the file is honoured and a
// stamp without one is read as UTC.
var timeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// parseStamp reads one export timestamp. Reports ok=false rather than an error
// because every caller's response is the same — count it and move on.
func parseStamp(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "null" {
		return time.Time{}, false
	}
	for _, l := range timeLayouts {
		if t, err := time.Parse(l, s); err == nil {
			// A stamp before 1980 or after 2200 is not a date, it is a field that
			// held something else. Nanoseconds are what the chronicle indexes on,
			// and a zero or negative one fails the manifest's own validation.
			if y := t.Year(); y < 1980 || y > 2200 {
				return time.Time{}, false
			}
			return t, true
		}
	}
	return time.Time{}, false
}

// At is the message's instant in unix nanoseconds, or ok=false when the export
// gave no readable stamp.
func (m Message) At() (int64, bool) {
	t, ok := parseStamp(m.Timestamp)
	if !ok {
		return 0, false
	}
	return t.UnixNano(), true
}

// Skippable reports whether a message is one of the join/pin/boost notices every
// export is full of. They are not conversation, they reference members who will
// never exist in the destination guild, and importing them means a reader
// scrolling a decade of history spends a third of it on "X joined".
func (m Message) Skippable() bool {
	switch m.Type {
	case "", "Default", "Reply", "ThreadStarterMessage":
		return false
	}
	return true
}

// Header is what a file says about itself before its messages start.
type Header struct {
	Guild   Guild   `json:"guild"`
	Channel Channel `json:"channel"`
	// Path is the file this header came from, absolute.
	Path string `json:"-"`
}

// ErrNotAnExport marks a file this package could not read as an export at all —
// not JSON, or JSON with no messages array. Callers count these; nothing treats
// one as fatal, because one bad file in a directory of four hundred must not
// cost the other three hundred and ninety-nine.
var ErrNotAnExport = fmt.Errorf("chronimport: not a channel export file")

// ErrTruncated marks a file that stopped being JSON partway through its
// messages: cut short by a download, a full disk, a crash of the exporting
// tool. Everything before the break was handed to the caller and counts; the
// file is not one that "could not be opened".
var ErrTruncated = fmt.Errorf("chronimport: the file ends partway through its messages")

// Walk streams one export file: head once, then msg for every message in it, in
// file order.
//
// THE MEMORY RULE. Files run to hundreds of megabytes and a phone has to survive
// one, so nothing here ever holds more than a single message: the decoder is
// driven token by token, the messages array is consumed element by element, and
// any unknown value — including a nested object of unknown depth — is walked past
// rather than materialised. A caller that retains what msg hands it is the only
// way to make an import allocate proportionally to the file.
//
// msg may return a non-nil error to stop early; that error is returned as-is,
// which is how the importer's cancellation works.
//
// The count of messages that were present but unusable (no id, no readable
// stamp) is returned so the caller can report skipped-malformed honestly.
func Walk(path string, head func(Header) error, msg func(*Message) error) (malformed int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return walkReader(f, path, head, msg)
}

func walkReader(r io.Reader, path string, head func(Header) error, msg func(*Message) error) (malformed int64, err error) {
	dec := json.NewDecoder(r)
	dec.UseNumber()

	t, err := dec.Token()
	if err != nil {
		return 0, ErrNotAnExport
	}
	if d, ok := t.(json.Delim); !ok || d != '{' {
		return 0, ErrNotAnExport
	}

	h := Header{Path: path}
	headSent := false
	sendHead := func() error {
		if headSent || head == nil {
			headSent = true
			return nil
		}
		headSent = true
		return head(h)
	}

	sawMessages := false
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return malformed, ErrNotAnExport
		}
		key, _ := kt.(string)
		switch key {
		case "guild":
			if err := dec.Decode(&h.Guild); err != nil {
				return malformed, ErrNotAnExport
			}
		case "channel":
			if err := dec.Decode(&h.Channel); err != nil {
				return malformed, ErrNotAnExport
			}
		case "messages":
			// The header is complete by the time messages start in every export
			// this package has seen; if a tool ever puts it after, the caller
			// still gets it — just late, which is why sendHead is idempotent.
			if err := sendHead(); err != nil {
				return malformed, err
			}
			sawMessages = true
			n, err := walkMessages(dec, msg)
			malformed += n
			if err != nil {
				return malformed, err
			}
		default:
			if err := skipValue(dec); err != nil {
				return malformed, ErrNotAnExport
			}
		}
	}
	if !sawMessages {
		return malformed, ErrNotAnExport
	}
	// A file whose messages array came before its header still owes the caller
	// one; and a file with an empty array owes it too.
	if err := sendHead(); err != nil {
		return malformed, err
	}
	return malformed, nil
}

// walkMessages consumes the messages array one element at a time.
func walkMessages(dec *json.Decoder, msg func(*Message) error) (malformed int64, err error) {
	t, err := dec.Token()
	if err != nil {
		return 0, ErrNotAnExport
	}
	if d, ok := t.(json.Delim); !ok || d != '[' {
		// Not an array. Walk past whatever it is and report the file unusable.
		return 0, ErrNotAnExport
	}
	// ONE message reused across the whole array. Decoding into a fresh value per
	// message would be correct but would hand the garbage collector a million
	// short-lived objects on a real import; zeroing one is free.
	var m Message
	for dec.More() {
		m = Message{}
		if err := dec.Decode(&m); err != nil {
			// A value of the wrong type (content as a number, an object where a
			// string goes) is one bad message: the decoder has read the whole
			// element before judging it, so it is positioned at the next one.
			// This used to end the walk, and the other messages of the file
			// vanished while the scan said none were malformed.
			var te *json.UnmarshalTypeError
			if errors.As(err, &te) {
				malformed++
				continue
			}
			// Anything else is the JSON itself breaking off, and the decoder
			// cannot find its feet again after that.
			return malformed, ErrTruncated
		}
		if m.ID == "" {
			malformed++
			continue
		}
		if _, ok := m.At(); !ok {
			malformed++
			continue
		}
		if msg != nil {
			if err := msg(&m); err != nil {
				return malformed, err
			}
		}
	}
	if _, err := dec.Token(); err != nil { // the closing ]
		return malformed, ErrTruncated
	}
	return malformed, nil
}

// skipValue walks past one JSON value without materialising it. Delimiters are
// counted rather than the value decoded, so an unknown key holding a nested
// megabyte costs a few tokens' worth of memory instead of a megabyte.
func skipValue(dec *json.Decoder) error {
	t, err := dec.Token()
	if err != nil {
		return err
	}
	d, ok := t.(json.Delim)
	if !ok || (d != '{' && d != '[') {
		return nil
	}
	depth := 1
	for depth > 0 {
		t, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := t.(json.Delim); ok {
			if d == '{' || d == '[' {
				depth++
			} else {
				depth--
			}
		}
	}
	return nil
}

// ExportFiles lists the export files in a directory, sorted, so two runs over
// the same directory agree on order and therefore on output. Only the top level
// is listed: the asset directories sitting beside the JSON hold thousands of
// images and walking them would be the slowest part of a scan that never needs
// to know they exist until an attachment names one.
func ExportFiles(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		if !strings.EqualFold(filepath.Ext(e.Name()), ".json") {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	// os.ReadDir already sorts by filename; being explicit about relying on it.
	return out, nil
}

// isRemoteURL reports whether an attachment URL points somewhere on the network
// rather than at a file the export brought with it.
func isRemoteURL(u string) bool {
	u = strings.TrimSpace(u)
	if u == "" {
		return false
	}
	if strings.HasPrefix(u, "//") {
		return true
	}
	i := strings.Index(u, ":")
	if i <= 0 {
		return false
	}
	scheme := strings.ToLower(u[:i])
	// A Windows drive letter ("C:\…") is not a scheme. Everything else with a
	// colon before the first slash is treated as one, which errs towards calling
	// a path remote — the safe direction, since remote means "do not open it".
	if len(scheme) == 1 {
		return false
	}
	return !strings.ContainsAny(scheme, `/\`)
}

// localAssetPath resolves an attachment's URL to a real file inside the export
// directory, or reports ok=false.
//
// This is the security boundary of the whole importer. The URL is a string a
// stranger's tool wrote into a file, and the caller is about to read whatever it
// names and seal it into a guild's permanent history. So: remote URLs are never
// opened, absolute paths and paths that climb out are refused, and a symlink
// anywhere below the export root — file or directory, pointing out or back in —
// is refused outright. "../../.ssh/id_ed25519" has to be a miss, not a blob
// every member of the guild can fetch, and so does "d/id_ed25519" when the
// archive extractor restored "d" as a link to the home directory.
//
// Refusing every link rather than resolving and re-checking is what keeps this
// simple enough to trust: with no links below the root, the lexically cleaned
// path IS the file, so containment is a string property. No real export needs
// a link; an extractor that restores one is restoring something the export
// tool did not write.
//
// This is the check; OpenLocalAsset is the read. Between a check and an open
// somebody can swap a directory for a link, so anything that reads the bytes
// goes through OpenLocalAsset, which walks the same path again without
// following links at the moment it opens it.
func localAssetPath(root, raw string) (string, bool) {
	rootReal, rels := assetCandidates(root, raw)
	for _, rel := range rels {
		if p, ok := checkBeneath(rootReal, rel); ok {
			return p, true
		}
	}
	return "", false
}

// OpenLocalAsset opens the file an asset URL names inside the export directory
// for reading, or reports ok=false. The file is opened one path component at a
// time without following symlinks (see openBeneath), so what is read is what
// was checked, and it is a regular file: a FIFO, a device or a directory is
// refused. The file's Name is its path under the root, for the extension.
func OpenLocalAsset(root, raw string) (*os.File, bool) {
	rootReal, rels := assetCandidates(root, raw)
	for _, rel := range rels {
		if f, err := openBeneath(rootReal, rel); err == nil {
			return f, true
		}
	}
	return nil, false
}

// assetCandidates resolves the export root (which the owner chose, and which may
// itself be reached through a link) and lists the relative, cleaned paths an
// asset URL could mean, none of which is absolute or climbs out of the root.
func assetCandidates(root, raw string) (string, []string) {
	raw = strings.TrimSpace(raw)
	if raw == "" || isRemoteURL(raw) {
		return "", nil
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", nil
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", nil
	}
	// Exports percent-encode spaces and non-ASCII in asset paths. Try the
	// decoded form first and fall back to the literal one, because a filename
	// that genuinely contains a percent sign exists too.
	cands := []string{raw}
	if dec, err := url.PathUnescape(raw); err == nil && dec != raw {
		cands = append([]string{dec}, cands...)
	}
	var rels []string
	for _, c := range cands {
		c = filepath.FromSlash(c)
		if filepath.IsAbs(c) || filepath.VolumeName(c) != "" {
			// An absolute path in an export is never something we brought with
			// us. Refused outright rather than checked for containment: the only
			// way it could pass is by naming a file inside the export directory,
			// which the relative form already covers.
			continue
		}
		c = filepath.Clean(c)
		if c == "." || c == ".." || strings.HasPrefix(c, ".."+string(filepath.Separator)) {
			continue
		}
		rels = append(rels, c)
	}
	return rootReal, rels
}

// checkBeneath walks rel below root one component at a time with Lstat, and
// accepts it only if no component is a link, every one but the last is a
// directory, and the last is a regular file.
func checkBeneath(root, rel string) (string, bool) {
	parts := strings.Split(rel, string(filepath.Separator))
	p := root
	for i, part := range parts {
		p = filepath.Join(p, part)
		st, err := os.Lstat(p)
		if err != nil || st.Mode()&os.ModeSymlink != 0 {
			return "", false
		}
		if i < len(parts)-1 && !st.IsDir() {
			return "", false
		}
		if i == len(parts)-1 && !st.Mode().IsRegular() {
			return "", false
		}
	}
	if !withinRoot(root, p) {
		return "", false
	}
	return p, true
}

// OpenLocalAttachment opens an attachment's file inside the export, or reports
// ok=false — which is the answer for a remote-only export, for a file the user
// deleted after exporting, and for anything trying to name a path outside the
// directory.
func OpenLocalAttachment(root string, a Attachment) (*os.File, bool) {
	return OpenLocalAsset(root, a.URL)
}

// OpenLocalAvatar opens an author's portrait the same way.
func OpenLocalAvatar(root string, a Author) (*os.File, bool) {
	return OpenLocalAsset(root, a.AvatarURL)
}

// OpenLocalEmoji opens a custom emoji's image the same way. The stat carries
// the URL it was found at so the importer does not have to hold the whole
// reaction that named it.
func OpenLocalEmoji(root string, e EmojiStat) (*os.File, bool) {
	return OpenLocalAsset(root, e.ImageURL)
}

// withinRoot reports whether p is root itself or sits under it.
func withinRoot(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
