package chronimport

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/bits"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The scan is the dry run: a complete, read-only pass over an export that
// answers "what is in here and what would importing it cost" without writing a
// byte, opening a socket, or holding more than one message in memory.
//
// It exists because the alternative is asking somebody to commit to an
// irreversible operation over a directory they have not looked at since the day
// they downloaded it. A decade of a community is not a thing to import blind:
// the sizes decide whether it fits, the date range decides where to cut, and the
// per-channel table is where somebody notices the channel they did not mean to
// bring.
//
// Everything the estimator later needs comes from HERE, so that moving a policy
// slider re-does arithmetic rather than re-reading a hundred gigabytes. That is
// the whole reason the tallies below are shaped the way they are.

// Kind classifies an attachment for the policy's include-images / include-video
// / include-other toggles. Deliberately coarse: the point is to let somebody say
// "text and pictures, not the videos", not to build a MIME database.
type Kind int

const (
	KindImage Kind = iota
	KindVideo
	KindOther
	kindCount
)

// KindName is the wire name of a kind, for the report and the policy.
func KindName(k Kind) string {
	switch k {
	case KindImage:
		return "image"
	case KindVideo:
		return "video"
	}
	return "other"
}

var imageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
	".bmp": true, ".avif": true, ".heic": true, ".heif": true,
	".tif": true, ".tiff": true, ".svg": true,
}

var videoExts = map[string]bool{
	".mp4": true, ".webm": true, ".mov": true, ".mkv": true, ".avi": true,
	".m4v": true, ".mpg": true, ".mpeg": true, ".wmv": true, ".flv": true,
}

// KindOf classifies by filename extension, falling back to the URL's own path
// when the export recorded no filename. Extension rather than sniffing because
// the scan must not open a single asset file: an export with two hundred
// thousand attachments would otherwise spend its whole runtime in stat and read
// on files the user may exclude a moment later.
func KindOf(a Attachment) Kind {
	name := a.FileName
	if name == "" {
		name = a.URL
		if i := strings.IndexAny(name, "?#"); i >= 0 {
			name = name[:i]
		}
	}
	switch ext := strings.ToLower(filepath.Ext(name)); {
	case imageExts[ext]:
		return KindImage
	case videoExts[ext]:
		return KindVideo
	}
	return KindOther
}

// The report histogram's buckets, in the shape a person reads. Their boundaries
// are the ones that matter to a decision: 64 KiB is "free", 512 KiB is "fine",
// 5 MiB is the ceiling on an inline image, and above that is where an import
// starts costing real disk.
const (
	ClassTiny   = iota // ≤ 64 KiB
	ClassSmall         // ≤ 512 KiB
	ClassMedium        // ≤ 5 MiB
	ClassLarge         // > 5 MiB
	sizeClasses
)

// SizeClassNames labels the histogram for the UI.
var SizeClassNames = [sizeClasses]string{"≤64KiB", "≤512KiB", "≤5MiB", ">5MiB"}

func sizeClassOf(n int64) int {
	switch {
	case n <= 64<<10:
		return ClassTiny
	case n <= 512<<10:
		return ClassSmall
	case n <= 5<<20:
		return ClassMedium
	}
	return ClassLarge
}

// The ESTIMATOR's histogram is a different, finer thing from the report's, and
// the difference is the reason both exist. A tier cut is "at most N bytes per
// attachment" for an N the user types, so four buckets would answer "how much of
// a 512 KiB–5 MiB pile is under 1 MiB?" with a shrug.
//
// It is a floating-point histogram: three bits of mantissa under each power of
// two, so every bucket is an eighth of an octave wide and the worst a straddled
// one can be wrong by is an eighth of what it holds. Plain powers of two were
// tried first and are not enough — an export with one six-megabyte recording in
// it and a five-megabyte ceiling put a quarter of that file into the projection,
// which is a 190% error on a channel whose other media is a few kilobytes.
//
// 200 buckets covers a single byte to a hundred and twenty-eight megabytes, well
// past the twenty-five the live path will seal. It costs 3 kinds × 200 buckets ×
// 2 counters × 8 bytes ≈ 9.6 KiB per channel, which is nothing against a scan
// that has just read gigabytes.
const (
	tierMantissaBits = 3
	tierSubBuckets   = 1 << tierMantissaBits
	tierBuckets      = 25 * tierSubBuckets
)

// tierBucketOf places a size. Sizes below one sub-bucket get a bucket each,
// which keeps the indexing contiguous with the mantissa form above them.
func tierBucketOf(size int64) int {
	if size < 0 {
		size = 0
	}
	if size < tierSubBuckets {
		return int(size)
	}
	oct := bits.Len64(uint64(size)) - 1
	mant := int((size >> (oct - tierMantissaBits)) & (tierSubBuckets - 1))
	b := (oct-tierMantissaBits+1)*tierSubBuckets + mant
	if b >= tierBuckets {
		return tierBuckets - 1
	}
	return b
}

// tierBucketBounds is the half-open size range a bucket covers. The last bucket
// is open-ended — everything above the histogram's top piles into it — and is
// reported as one doubling so a ceiling inside it still interpolates to
// something rather than to a division by zero.
func tierBucketBounds(b int) (lo, hi int64) {
	if b < tierSubBuckets {
		return int64(b), int64(b) + 1
	}
	oct := b/tierSubBuckets + tierMantissaBits - 1
	mant := int64(b % tierSubBuckets)
	width := int64(1) << (oct - tierMantissaBits)
	lo = (tierSubBuckets + mant) * width
	return lo, lo + width
}

// MonthStat is one month of one channel. Months rather than days because the
// date-range slider has to stay instant over a decade of history: a hundred and
// twenty buckets per channel is arithmetic, three thousand six hundred and fifty
// is a table nobody wants to ship to a frontend. The cost is that a range
// ending mid-month is estimated as the whole month's proportion — see
// EstimateChatImport, which prorates it.
type MonthStat struct {
	// Nano is the first nanosecond of the month, UTC.
	Nano int64 `json:"nano"`
	// FirstNano and LastNano are the OBSERVED span inside this month — the first
	// and last message actually in it. The proration is against these rather
	// than against the calendar month, which matters at the ends of a range: a
	// channel whose history starts at nine in the morning on the first would
	// otherwise be projected to have nine hours of traffic before its own first
	// message, and "a range ending before anything was said" would import
	// twenty-one messages that do not exist.
	FirstNano             int64 `json:"firstNano"`
	LastNano              int64 `json:"lastNano"`
	Messages              int64 `json:"messages"`
	ContentBytes          int64 `json:"contentBytes"`
	Replies               int64 `json:"replies"`
	Reactions             int64 `json:"reactions"`
	LocalAttachments      int64 `json:"localAttachments"`
	LocalAttachmentBytes  int64 `json:"localAttachmentBytes"`
	RemoteAttachments     int64 `json:"remoteAttachments"`
	RemoteAttachmentBytes int64 `json:"remoteAttachmentBytes"`

	// Local is this month's on-disk media, tallied by kind and size bucket.
	//
	// It lives HERE, per month, rather than once per channel, and that placement
	// is the whole reason the estimate is accurate. The tier cut is by file size
	// and the range cut is by time; composing them as two independent fractions
	// of a channel's total is wrong precisely when the two correlate, which is
	// the ordinary case — a channel where somebody posted a six-megabyte
	// recording in 2019 and nothing but small pictures since projected a third of
	// the media it would actually seal for any range that excluded 2019.
	//
	// Sparse, because it has to be: most months hold a handful of distinct
	// (kind, bucket) pairs and many hold none, so a dense two-hundred-bucket
	// histogram per month would be megabytes of zeroes. Off the wire — the
	// frontend gets the four-bucket report histogram, and the estimator that
	// needs this runs in the same process as the scan.
	Local []AttachTally `json:"-"`
}

// AttachTally is one (kind, size bucket) cell of a month's local media.
type AttachTally struct {
	Kind   Kind
	Bucket int
	Count  int64
	Bytes  int64
}

// ChannelStats is one exported channel, merged across however many files carry
// it — a big channel is routinely split into a file per year, and a per-file
// table would be a table of files rather than of channels.
type ChannelStats struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type,omitempty"`
	Category   string `json:"category,omitempty"`
	CategoryID string `json:"categoryId,omitempty"`
	Topic      string `json:"topic,omitempty"`
	// Files are the export files this channel's history came from, relative to
	// the export directory, sorted.
	Files []string `json:"files"`

	Messages  int64 `json:"messages"`
	Malformed int64 `json:"malformed"`
	// Notices counts the join/pin/boost system entries, which are excluded from
	// Messages: they are counted so the totals reconcile against a file the user
	// might open, not because anybody wants them imported.
	Notices   int64 `json:"notices"`
	Replies   int64 `json:"replies"`
	Reactions int64 `json:"reactions"`

	FirstNano    int64 `json:"firstNano"`
	LastNano     int64 `json:"lastNano"`
	ContentBytes int64 `json:"contentBytes"`

	Attachments           int64 `json:"attachments"`
	AttachmentBytes       int64 `json:"attachmentBytes"`
	LocalAttachments      int64 `json:"localAttachments"`
	LocalAttachmentBytes  int64 `json:"localAttachmentBytes"`
	RemoteAttachments     int64 `json:"remoteAttachments"`
	RemoteAttachmentBytes int64 `json:"remoteAttachmentBytes"`

	Months []MonthStat `json:"months"`
}

// AuthorStat is one name in the top-N table.
type AuthorStat struct {
	Name     string `json:"name"`
	Messages int64  `json:"messages"`
	// HasAvatar reports that the export brought this author's portrait as a file
	// on disk, so an import could carry the face across.
	HasAvatar bool `json:"hasAvatar"`
	// AvatarBytes is what that portrait would cost inside the manifest, or 0
	// when it is too big to ride one at all.
	AvatarBytes int64 `json:"avatarBytes,omitempty"`
}

// MaxAvatarBytes mirrors app.maxChronicleAvatarBytes: the ceiling on one author
// portrait carried inline in a manifest. Duplicated for the same reason
// MaxManifestBytes is, and held to the same rule — the app-side test asserts
// they agree.
const MaxAvatarBytes = 8 << 10

// manifestAvatarCost is what a portrait of n bytes on disk would occupy in the
// index once it is a base64 data URI, or 0 when that is over the ceiling and the
// importer would drop it. Base64 costs four bytes for every three, and the
// "data:image/jpeg;base64," prefix is twenty-three.
func manifestAvatarCost(n int64) int64 {
	cost := (n+2)/3*4 + 24
	if cost > MaxAvatarBytes {
		return 0
	}
	return cost
}

// EmojiStat is one custom emoji seen in the export's reactions.
type EmojiStat struct {
	Name string `json:"name"`
	ID   string `json:"id,omitempty"`
	// Sanitized is Name folded into the guild-emoji charset, or "" when nothing
	// usable survives — which is how the wizard can say "3 emoji cannot be
	// imported" before anything happens.
	Sanitized string `json:"sanitized,omitempty"`
	Uses      int64  `json:"uses"`
	// Local reports the image is a file in the export rather than a CDN link.
	// The importer refuses to fetch, so an emoji without one cannot come across.
	Local bool  `json:"local"`
	Bytes int64 `json:"bytes,omitempty"`
	// ImageURL is what the export pointed at, kept so the importer can resolve
	// the file again without re-reading the reaction that named it. Not on the
	// wire: it is a path on the importing machine, and the wizard has no use
	// for it.
	ImageURL string `json:"-"`
}

// FileError names a file the scan could not read, so the report can say which
// rather than only how many.
type FileError struct {
	File   string `json:"file"`
	Reason string `json:"reason"`
}

// TopAuthorsLimit is how many names the report's author table carries. The full
// count is in Authors; the table is for reading, and a list of nine thousand
// names is not.
const TopAuthorsLimit = 25

// Stats is the whole dry run. Every number in it is derived from a complete
// pass, so the totals reconcile: an import that says it will bring 412,003
// messages will bring 412,003 messages or explain why not.
type Stats struct {
	Dir string `json:"dir"`
	// Key fingerprints the directory's contents by name, size and modification
	// time. Two scans with equal keys are guaranteed by construction to produce
	// equal Stats, which is what makes caching one safe.
	Key string `json:"key"`
	// Guild is the community name the export recorded, used as the default
	// source label on the manifest.
	Guild string `json:"guild"`

	Files      int         `json:"files"`
	FileErrors []FileError `json:"fileErrors,omitempty"`

	Channels []ChannelStats `json:"channels"`

	Messages  int64 `json:"messages"`
	Malformed int64 `json:"malformed"`
	Notices   int64 `json:"notices"`
	Replies   int64 `json:"replies"`
	Reactions int64 `json:"reactions"`

	FirstNano    int64 `json:"firstNano"`
	LastNano     int64 `json:"lastNano"`
	ContentBytes int64 `json:"contentBytes"`

	Authors    int          `json:"authors"`
	TopAuthors []AuthorStat `json:"topAuthors"`
	// Portraits is how many authors brought a face the manifest could carry, and
	// PortraitBytes what those faces cost inside it once base64 has added its
	// third. They are separate numbers because the index is otherwise a few
	// hundred bytes per channel and a few dozen per name — portraits are the only
	// elastic part of it, and the only reason an index ever approaches its
	// ceiling on an export of ordinary size.
	Portraits     int   `json:"portraits"`
	PortraitBytes int64 `json:"portraitBytes"`

	Attachments           int64 `json:"attachments"`
	AttachmentBytes       int64 `json:"attachmentBytes"`
	LocalAttachments      int64 `json:"localAttachments"`
	LocalAttachmentBytes  int64 `json:"localAttachmentBytes"`
	RemoteAttachments     int64 `json:"remoteAttachments"`
	RemoteAttachmentBytes int64 `json:"remoteAttachmentBytes"`

	// Histogram is the four-bucket size distribution of every attachment in the
	// export, local and remote alike — the remote ones carry their recorded size,
	// which is exactly why they belong in it: "you are leaving 40 GB behind" is
	// the sentence the number exists to make possible.
	Histogram      [sizeClasses]int64 `json:"histogram"`
	LocalHistogram [sizeClasses]int64 `json:"localHistogram"`

	Emoji []EmojiStat `json:"emoji,omitempty"`

	// Elapsed is how long the pass took, in milliseconds. Shown because a scan
	// of a very large export is a wait somebody should be told about, and
	// because it is the honest input to "this import will take a while".
	Elapsed int64 `json:"elapsedMs"`
}

// ChannelByID finds a channel's stats.
func (st *Stats) ChannelByID(id string) (*ChannelStats, bool) {
	for i := range st.Channels {
		if st.Channels[i].ID == id {
			return &st.Channels[i], true
		}
	}
	return nil, false
}

// ScanChatExport walks an export directory and reports what is in it.
//
// Read-only and offline, both absolutely: it opens the JSON files, and it stats
// (never reads) the asset files an attachment names, so that "is the media
// actually here" can be answered without a byte of it being loaded. Nothing is
// written and nothing is fetched.
//
// The result is deterministic for a given directory state — files in sorted
// order, channels in first-seen order, authors ranked with ties broken by name —
// which is what makes Key a sound cache key.
func ScanChatExport(dir string) (*Stats, error) {
	started := time.Now()
	files, err := ExportFiles(dir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("chronimport: %s holds no .json export files", dir)
	}
	key, err := dirKey(dir, files)
	if err != nil {
		return nil, err
	}

	st := &Stats{Dir: dir, Key: key, Files: len(files)}

	// Channels keyed by the export's own channel id, falling back to the name
	// when a tool wrote none — two files of the same nameless channel must still
	// merge, or the destination guild ends up with #general and #general.
	byChannel := map[string]*ChannelStats{}
	order := []string{}
	// Per-channel month tallies, folded into ChannelStats.Months at the end so
	// the hot loop is a map write rather than a sorted-slice insert. tallies is
	// the same for each month's sparse media histogram, keyed kind<<32|bucket.
	months := map[string]map[int64]*MonthStat{}
	tallies := map[string]map[int64]map[int64]*AttachTally{}

	authors := map[string]*AuthorStat{}
	emoji := map[string]*EmojiStat{}

	for _, path := range files {
		rel, _ := filepath.Rel(dir, path)
		var cur *ChannelStats
		var curMonths map[int64]*MonthStat
		var curTallies map[int64]map[int64]*AttachTally

		head := func(h Header) error {
			if st.Guild == "" {
				st.Guild = strings.TrimSpace(h.Guild.Name)
			}
			ckey := h.Channel.ID
			if ckey == "" {
				ckey = "name:" + h.Channel.Name
			}
			c, ok := byChannel[ckey]
			if !ok {
				c = &ChannelStats{
					ID: ckey, Name: strings.TrimSpace(h.Channel.Name),
					Type:       strings.TrimSpace(h.Channel.Type),
					Category:   strings.TrimSpace(h.Channel.Category),
					CategoryID: strings.TrimSpace(h.Channel.CategoryID),
					Topic:      strings.TrimSpace(h.Channel.Topic),
				}
				if c.Name == "" {
					c.Name = "channel-" + shortHash(ckey)
				}
				byChannel[ckey] = c
				order = append(order, ckey)
				months[ckey] = map[int64]*MonthStat{}
				tallies[ckey] = map[int64]map[int64]*AttachTally{}
			}
			c.Files = append(c.Files, rel)
			cur, curMonths, curTallies = c, months[ckey], tallies[ckey]
			return nil
		}

		msg := func(m *Message) error {
			nano, _ := m.At() // Walk guarantees this parses
			if m.Skippable() {
				cur.Notices++
				return nil
			}
			cur.Messages++
			cur.ContentBytes += int64(len(m.Content))
			if cur.FirstNano == 0 || nano < cur.FirstNano {
				cur.FirstNano = nano
			}
			if nano > cur.LastNano {
				cur.LastNano = nano
			}
			ms := monthBucket(curMonths, nano)
			msKey := ms.Nano
			ms.Messages++
			ms.ContentBytes += int64(len(m.Content))
			if ms.FirstNano == 0 || nano < ms.FirstNano {
				ms.FirstNano = nano
			}
			if nano > ms.LastNano {
				ms.LastNano = nano
			}

			if m.Reference != nil && m.Reference.MessageID != "" {
				cur.Replies++
				ms.Replies++
			}
			for _, r := range m.Reactions {
				cur.Reactions++
				ms.Reactions++
				if r.Emoji.Custom() {
					tallyEmoji(emoji, dir, r.Emoji, int64(max64(int64(r.Count), 1)))
				}
			}

			a := &m.Author
			ak := a.Key()
			as, ok := authors[ak]
			if !ok {
				as = &AuthorStat{Name: a.Display()}
				// Resolved once per author, never per message: a portrait lookup
				// stats a file, and doing it a million times for four faces is
				// how a scan becomes an hour.
				if p, ok := localAssetPath(dir, a.AvatarURL); ok {
					if fi, err := os.Stat(p); err == nil && fi.Size() > 0 {
						as.HasAvatar = true
						as.AvatarBytes = manifestAvatarCost(fi.Size())
					}
				}
				authors[ak] = as
			}
			as.Messages++

			for i := range m.Attachments {
				at := m.Attachments[i]
				size := at.FileSizeBytes
				kind := KindOf(at)
				p, local := localAssetPath(dir, at.URL)
				if local {
					// The recorded size can disagree with the file (a tool that
					// re-encoded, a truncated download). The file on disk is what
					// an import would actually seal, so it is what is counted.
					if fi, err := os.Stat(p); err == nil {
						size = fi.Size()
					}
				}
				if size < 0 {
					size = 0
				}
				cur.Attachments++
				cur.AttachmentBytes += size
				st.Histogram[sizeClassOf(size)]++
				if local {
					cur.LocalAttachments++
					cur.LocalAttachmentBytes += size
					st.LocalHistogram[sizeClassOf(size)]++
					ms.LocalAttachments++
					ms.LocalAttachmentBytes += size
					tallyAttachment(curTallies, msKey, kind, size)
				} else {
					cur.RemoteAttachments++
					cur.RemoteAttachmentBytes += size
					ms.RemoteAttachments++
					ms.RemoteAttachmentBytes += size
				}
			}
			return nil
		}

		bad, err := Walk(path, head, msg)
		if cur != nil {
			cur.Malformed += bad
		}
		st.Malformed += bad
		if err != nil {
			// A file that could not be read at all, or one that was truncated
			// partway. Whatever was read before the failure is kept: half a
			// channel of real history is worth more than a report that pretends
			// the file was empty, and the reason is named so somebody can go and
			// look at it.
			st.FileErrors = append(st.FileErrors, FileError{File: rel, Reason: err.Error()})
		}
	}

	for _, k := range order {
		c := byChannel[k]
		sort.Strings(c.Files)
		c.Files = dedupeStrings(c.Files)
		c.Months = foldMonths(months[k], tallies[k])
		st.Channels = append(st.Channels, *c)

		st.Messages += c.Messages
		st.Notices += c.Notices
		st.Replies += c.Replies
		st.Reactions += c.Reactions
		st.ContentBytes += c.ContentBytes
		st.Attachments += c.Attachments
		st.AttachmentBytes += c.AttachmentBytes
		st.LocalAttachments += c.LocalAttachments
		st.LocalAttachmentBytes += c.LocalAttachmentBytes
		st.RemoteAttachments += c.RemoteAttachments
		st.RemoteAttachmentBytes += c.RemoteAttachmentBytes
		if c.FirstNano != 0 && (st.FirstNano == 0 || c.FirstNano < st.FirstNano) {
			st.FirstNano = c.FirstNano
		}
		if c.LastNano > st.LastNano {
			st.LastNano = c.LastNano
		}
	}

	st.Authors = len(authors)
	st.TopAuthors = topAuthors(authors)
	for _, a := range authors {
		if a.AvatarBytes > 0 {
			st.Portraits++
			st.PortraitBytes += a.AvatarBytes
		}
	}
	st.Emoji = foldEmoji(emoji)
	if st.Guild == "" {
		st.Guild = filepath.Base(strings.TrimRight(dir, string(filepath.Separator)))
	}
	st.Elapsed = time.Since(started).Milliseconds()
	return st, nil
}

// monthBucket finds or creates the month a nanosecond falls in.
func monthBucket(m map[int64]*MonthStat, nano int64) *MonthStat {
	t := time.Unix(0, nano).UTC()
	start := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC).UnixNano()
	ms, ok := m[start]
	if !ok {
		ms = &MonthStat{Nano: start}
		m[start] = ms
	}
	return ms
}

// tallyAttachment records one on-disk file in a month's sparse histogram.
func tallyAttachment(t map[int64]map[int64]*AttachTally, month int64, k Kind, size int64) {
	byCell, ok := t[month]
	if !ok {
		byCell = map[int64]*AttachTally{}
		t[month] = byCell
	}
	bucket := tierBucketOf(size)
	key := int64(k)<<32 | int64(bucket)
	cell, ok := byCell[key]
	if !ok {
		cell = &AttachTally{Kind: k, Bucket: bucket}
		byCell[key] = cell
	}
	cell.Count++
	cell.Bytes += size
}

func foldMonths(m map[int64]*MonthStat, t map[int64]map[int64]*AttachTally) []MonthStat {
	out := make([]MonthStat, 0, len(m))
	for _, v := range m {
		ms := *v
		if cells := t[ms.Nano]; len(cells) > 0 {
			ms.Local = make([]AttachTally, 0, len(cells))
			for _, c := range cells {
				ms.Local = append(ms.Local, *c)
			}
			// Sorted so two scans of one directory produce identical slices.
			sort.Slice(ms.Local, func(i, j int) bool {
				if ms.Local[i].Kind != ms.Local[j].Kind {
					return ms.Local[i].Kind < ms.Local[j].Kind
				}
				return ms.Local[i].Bucket < ms.Local[j].Bucket
			})
		}
		out = append(out, ms)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Nano < out[j].Nano })
	return out
}

// tallyEmoji records one use of a custom emoji, checking once whether its image
// is a file the export brought with it.
func tallyEmoji(m map[string]*EmojiStat, dir string, e Emoji, uses int64) {
	key := e.ID
	if key == "" {
		key = e.Name
	}
	es, ok := m[key]
	if !ok {
		es = &EmojiStat{Name: strings.TrimSpace(e.Name), ID: e.ID}
		if es.Name == "" {
			es.Name = e.ID
		}
		es.Sanitized = SanitizeEmojiName(es.Name)
		es.ImageURL = e.ImageURL
		if p, ok := localAssetPath(dir, e.ImageURL); ok {
			if fi, err := os.Stat(p); err == nil {
				es.Local, es.Bytes = true, fi.Size()
			}
		}
		m[key] = es
	}
	es.Uses += uses
}

func foldEmoji(m map[string]*EmojiStat) []EmojiStat {
	if len(m) == 0 {
		return nil
	}
	out := make([]EmojiStat, 0, len(m))
	for _, v := range m {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Uses != out[j].Uses {
			return out[i].Uses > out[j].Uses
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func topAuthors(m map[string]*AuthorStat) []AuthorStat {
	out := make([]AuthorStat, 0, len(m))
	for _, v := range m {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Messages != out[j].Messages {
			return out[i].Messages > out[j].Messages
		}
		return out[i].Name < out[j].Name // deterministic ties
	})
	if len(out) > TopAuthorsLimit {
		out = out[:TopAuthorsLimit]
	}
	return out
}

// dirKey fingerprints the export directory: every file's name, size and
// modification time. Not a content hash — hashing a hundred gigabytes to decide
// whether to re-read a hundred gigabytes saves nothing — which means a file
// rewritten within a filesystem timestamp tick with the same length would go
// unnoticed. That is the standard build-system trade and the same one `make`
// takes; the cache it guards is a size report, not a correctness boundary, and
// the import itself always re-reads.
func dirKey(dir string, files []string) (string, error) {
	h := sha256.New()
	fmt.Fprintf(h, "v1\x00%s\x00", dir)
	for _, f := range files {
		fi, err := os.Stat(f)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%d\x00%d\x00", filepath.Base(f), fi.Size(), fi.ModTime().UnixNano())
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// CurrentKey recomputes a directory's fingerprint, so a caller holding a cached
// Stats can tell whether it is still true without re-scanning.
func CurrentKey(dir string) (string, error) {
	files, err := ExportFiles(dir)
	if err != nil {
		return "", err
	}
	return dirKey(dir, files)
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:4])
}

func dedupeStrings(in []string) []string {
	if len(in) < 2 {
		return in
	}
	out := in[:1]
	for _, s := range in[1:] {
		if s != out[len(out)-1] {
			out = append(out, s)
		}
	}
	return out
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
