package chronimport

import (
	"sort"
	"strings"
)

// The estimator is the reason the scan collects what it collects.
//
// A wizard that re-reads a hundred gigabytes every time somebody drags a date
// slider is a wizard nobody finishes. So the pass over the files happens ONCE
// and produces Stats; every policy question after that is arithmetic over
// Stats, in microseconds, with no file open and no allocation worth naming.
// EstimateChatImport is a pure function for exactly that reason — the same
// inputs give the same answer, it can be called on every keystroke, and it is
// trivially testable against a real import of the same fixture.
//
// It is an ESTIMATE, and the places it is approximate are named in the comments
// below rather than hidden. The contract is the one a progress bar makes: close
// enough to decide with, never presented as a promise. The test that keeps it
// honest imports the fixture for real and asserts the projection was within a
// fifth of the truth.

// GzipRatio is the compression the chronicle's chunk builder actually achieves
// on chat text: JSON Lines of chronicleMessage, gzipped at the default level.
//
// MEASURED, not guessed: 5.77x over real chunks sealed from the test corpus by
// the real builder. TestGzipRatioHoldsUp re-measures on every run and fails if
// the truth drifts more than a sixth away from this number, so it cannot quietly
// rot as the message shape changes — and if it does, the failure prints what to
// change it to.
//
// It is far above the ~3x gzip manages on ordinary English prose, and the reason
// is that a chunk is not prose. Every line repeats the same five one-character
// JSON keys; the nanosecond stamps inside one chunk share eleven leading digits;
// authors are integer indices rather than repeated display names; and a
// conversation genuinely does say the same things over and over. Three of those
// four are deliberate choices in the chunk format, and this is where they pay.
const GzipRatio = 5.7

// perMessagePlainBytes is the JSON Lines overhead of one archived message
// before its content: the braces, the five keys, a source id, an author index
// and a nineteen-digit nanosecond stamp. Content, replies, reactions and
// attachment tokens are added on top, each where it is incurred.
const perMessagePlainBytes = 62

// perReplyPlainBytes is `,"r":"<id>"` for a message that answers another.
const perReplyPlainBytes = 26

// perReactionPlainBytes is one entry of the reaction map: a short emoji key, a
// count, and the punctuation between them.
const perReactionPlainBytes = 14

// perAttachTokenPlainBytes is one concord://attach token: scheme, a 64-character
// content address, a 75-character key, subtype and dimensions, inside the
// markdown image syntax that carries it.
const perAttachTokenPlainBytes = 190

// perPlaceholderPlainBytes is the line that stands in for an attachment the
// export did not bring — see RemotePlaceholder.
const perPlaceholderPlainBytes = 64

// Manifest arithmetic. A chunk reference is a content address, a channel id,
// two stamps, two counts and a 56-byte key in base64; a channel entry is its id,
// name, type and topic; an author entry is a name.
const (
	perChunkRefBytes = 210
	perChannelBytes  = 130
	perAuthorBytes   = 32
	manifestBaseSize = 512
)

// Policy is what the user decides before an import runs: how much of the export
// to bring and how much of its media to pay for.
//
// The zero value imports TEXT ONLY — every channel, every date, no attachments,
// no reactions, no emoji. That is deliberate: a policy that arrived empty
// because a caller forgot a field should under-import rather than seal forty
// gigabytes of video into a guild. DefaultPolicy is the generous one, and it is
// what the wizard starts from.
type Policy struct {
	// FromNano and ToNano bound the import; 0 means unbounded on that side. The
	// range is inclusive of From and exclusive of To, so two adjacent ranges
	// tile without double-counting a message.
	FromNano int64 `json:"fromNano,omitempty"`
	ToNano   int64 `json:"toNano,omitempty"`

	// ExcludeChannels names source channel ids to leave out. An exclude list
	// rather than an include list because a channel the scan found and the
	// policy has never heard of must default to being imported: the alternative
	// silently drops history whenever the two disagree.
	ExcludeChannels []string `json:"excludeChannels,omitempty"`

	// MaxAttachmentBytes is the per-attachment ceiling; 0 means no ceiling.
	// Anything over it is left behind as a placeholder line, exactly like a
	// remote-only attachment, because from the reader's point of view they are
	// the same thing: a file the archive names and does not hold.
	MaxAttachmentBytes int64 `json:"maxAttachmentBytes,omitempty"`
	IncludeImages      bool  `json:"includeImages,omitempty"`
	IncludeVideo       bool  `json:"includeVideo,omitempty"`
	IncludeOther       bool  `json:"includeOther,omitempty"`

	IncludeReactions bool `json:"includeReactions,omitempty"`
	IncludeEmoji     bool `json:"includeEmoji,omitempty"`

	// Source overrides the manifest's source label. Empty means "the export's
	// own guild name, and the date it was imported".
	Source string `json:"source,omitempty"`
	// Description is free text carried on the manifest.
	Description string `json:"description,omitempty"`
}

// DefaultPolicy is the generous starting point the wizard offers: everything,
// with attachments capped at the size the live path caps an inline image at.
func DefaultPolicy() Policy {
	return Policy{
		MaxAttachmentBytes: 5 << 20,
		IncludeImages:      true,
		IncludeVideo:       true,
		IncludeOther:       true,
		IncludeReactions:   true,
		IncludeEmoji:       true,
	}
}

// wantsKind reports whether the policy takes attachments of a kind at all.
func (p Policy) wantsKind(k Kind) bool {
	switch k {
	case KindImage:
		return p.IncludeImages
	case KindVideo:
		return p.IncludeVideo
	}
	return p.IncludeOther
}

// TakesAttachments reports whether any media at all could be sealed.
func (p Policy) TakesAttachments() bool {
	return p.IncludeImages || p.IncludeVideo || p.IncludeOther
}

// TakesAttachment is the per-file decision, and the one the importer actually
// runs. Kept beside the estimator's arithmetic so the two cannot disagree about
// what the policy means.
func (p Policy) TakesAttachment(k Kind, size int64) bool {
	if !p.wantsKind(k) {
		return false
	}
	if p.MaxAttachmentBytes > 0 && size > p.MaxAttachmentBytes {
		return false
	}
	return true
}

// InRange reports whether an instant falls inside the policy's window.
func (p Policy) InRange(nano int64) bool {
	if p.FromNano > 0 && nano < p.FromNano {
		return false
	}
	if p.ToNano > 0 && nano >= p.ToNano {
		return false
	}
	return true
}

// excludes reports whether a source channel id is left out.
func (p Policy) excludes(id string) bool {
	for _, e := range p.ExcludeChannels {
		if e == id {
			return true
		}
	}
	return false
}

// ChannelEstimate is the projection for one channel.
type ChannelEstimate struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Messages        int64  `json:"messages"`
	Attachments     int64  `json:"attachments"`
	AttachmentBytes int64  `json:"attachmentBytes"`
	ChunkBytes      int64  `json:"chunkBytes"`
	Chunks          int64  `json:"chunks"`
	FirstNano       int64  `json:"firstNano,omitempty"`
	LastNano        int64  `json:"lastNano,omitempty"`
}

// Estimate is what the import would cost. Every byte figure is what lands on
// disk and travels between members, not what was read off the export.
type Estimate struct {
	Messages int64 `json:"messages"`
	// SkippedByPolicy is what the policy leaves behind — messages outside the
	// date range or in an excluded channel. Shown next to Messages because "I
	// am importing 40,000 of 412,000" is the sentence somebody needs before
	// they press the button.
	SkippedByPolicy int64 `json:"skippedByPolicy"`
	// Malformed is what the scan already found unusable. Restated here so the
	// three numbers add up to the export's own total.
	Malformed int64 `json:"malformed"`
	// Notices is the join/pin/boost chatter, never imported.
	Notices int64 `json:"notices"`

	Channels []ChannelEstimate `json:"channels"`
	Authors  int               `json:"authors"`
	Emoji    int               `json:"emoji"`

	// Attachments/AttachmentBytes are the files that will actually be sealed
	// into blobs on this device.
	Attachments     int64 `json:"attachments"`
	AttachmentBytes int64 `json:"attachmentBytes"`
	// Placeholders counts the attachments the archive will name and not hold:
	// remote-only ones, and local ones the tier excludes.
	Placeholders int64 `json:"placeholders"`
	// PlaceholderBytes is what those files WOULD have cost — the number that
	// makes "re-export with assets" an informed choice rather than a shrug.
	PlaceholderBytes int64 `json:"placeholderBytes"`

	Chunks        int64 `json:"chunks"`
	ChunkBytes    int64 `json:"chunkBytes"`
	ManifestBytes int64 `json:"manifestBytes"`
	// TotalBytes is chunks + manifest + sealed attachments: everything the
	// import writes.
	TotalBytes int64 `json:"totalBytes"`

	FirstNano int64 `json:"firstNano,omitempty"`
	LastNano  int64 `json:"lastNano,omitempty"`

	// Portraits is how many author faces the index would carry.
	Portraits int `json:"portraits,omitempty"`

	// ChannelsOverCap warns that the import names more channels than one archive
	// can index. It is a SEPARATE flag from ManifestOverCap because the advice
	// differs: an index over its byte ceiling usually wants a narrower date
	// range, and one over its channel ceiling wants the channels split across
	// several imports.
	ChannelsOverCap bool `json:"channelsOverCap,omitempty"`

	// ManifestOverCap warns that the index would not fit its own 384 KiB ceiling
	// — which happens on an export with thousands of channels or millions of
	// messages, and means the import must be split. Better said here, before
	// anything has been read, than by a failure after twenty minutes of work.
	ManifestOverCap bool `json:"manifestOverCap,omitempty"`
}

// MaxManifestBytes mirrors app.maxChronicleManifestBytes. Duplicated rather
// than imported because this package must not depend on the service — it is the
// pure half — and the number is a format constant that has to change in a
// coordinated way anyway. Exported so the app-side test can assert the two have
// not drifted apart, which is the only thing that makes duplicating a constant
// tolerable. MaxChannels mirrors app.maxChronicleChannels on the same terms.
const (
	MaxManifestBytes = 384 << 10
	MaxChannels      = 512
)

// EstimateChatImport projects what importing an export under a policy would
// produce. Pure: no files, no clock, no network, no mutation of stats.
func EstimateChatImport(stats *Stats, policy Policy) Estimate {
	var est Estimate
	if stats == nil {
		return est
	}
	est.Malformed = stats.Malformed
	est.Notices = stats.Notices
	est.Authors = stats.Authors

	if policy.IncludeEmoji {
		for _, e := range stats.Emoji {
			// Only an emoji whose image is a file in the export and whose name
			// survives folding can be imported; the rest are counted nowhere,
			// because promising one and skipping it is worse than not offering.
			if e.Local && e.Sanitized != "" && e.Bytes > 0 && e.Bytes <= maxEmojiPlainBytes {
				est.Emoji++
			}
		}
	}

	var totalPlain int64
	for i := range stats.Channels {
		c := &stats.Channels[i]
		if policy.excludes(c.ID) {
			est.SkippedByPolicy += c.Messages
			continue
		}
		// A voice channel imports as structure and carries no history; counting
		// its messages as "skipped by policy" would be a lie, since there are
		// none to skip.
		if ChannelTypeOf(c.Type) == "voice" {
			est.SkippedByPolicy += c.Messages
			continue
		}

		ce, plain := estimateChannel(c, policy)
		est.SkippedByPolicy += c.Messages - ce.Messages
		if ce.Messages == 0 {
			continue
		}
		totalPlain += plain

		est.Messages += ce.Messages
		est.Attachments += ce.Attachments
		est.AttachmentBytes += ce.AttachmentBytes
		est.Chunks += ce.Chunks
		est.ChunkBytes += ce.ChunkBytes
		if est.FirstNano == 0 || (ce.FirstNano != 0 && ce.FirstNano < est.FirstNano) {
			est.FirstNano = ce.FirstNano
		}
		if ce.LastNano > est.LastNano {
			est.LastNano = ce.LastNano
		}
		est.Placeholders += placeholdersFor(c, policy)
		est.PlaceholderBytes += placeholderBytesFor(c, policy)
		est.Channels = append(est.Channels, ce)
	}

	est.ManifestBytes = manifestBaseSize +
		int64(len(est.Channels))*perChannelBytes +
		int64(est.Authors)*perAuthorBytes +
		stats.PortraitBytes +
		est.Chunks*perChunkRefBytes
	est.Portraits = stats.Portraits
	est.ManifestOverCap = est.ManifestBytes > MaxManifestBytes
	est.ChannelsOverCap = len(est.Channels) > MaxChannels
	est.TotalBytes = est.ChunkBytes + est.ManifestBytes + est.AttachmentBytes
	return est
}

// maxEmojiPlainBytes is the decoded ceiling on a custom emoji. app's cap is
// 256 KiB measured on the whole base64 data URI, so the raw file has to be
// smaller than that by base64's four-thirds — 190 KiB is the largest file that
// certainly fits, with the ~22-byte prefix rounded generously.
const maxEmojiPlainBytes = 190 << 10

// estimateChannel projects one channel and returns its chunk plaintext, which
// the caller needs for nothing but is kept separate to make the arithmetic
// readable.
func estimateChannel(c *ChannelStats, p Policy) (ChannelEstimate, int64) {
	ce := ChannelEstimate{ID: c.ID, Name: c.Name}

	// The date filter runs over the month buckets. A month that straddles an
	// endpoint is PRORATED by the fraction of it inside the range, on the
	// assumption that a month's traffic is spread through it — wrong for any one
	// month, right on average, and bounded by one month's worth of error at each
	// end of a range that is usually years long.
	var msgs, contentBytes, replies, reactions, localCount float64
	var takenCount, takenBytes float64
	takes := p.TakesAttachments()
	for _, m := range c.Months {
		f := monthOverlap(m, p)
		if f <= 0 {
			continue
		}
		msgs += f * float64(m.Messages)
		contentBytes += f * float64(m.ContentBytes)
		replies += f * float64(m.Replies)
		reactions += f * float64(m.Reactions)
		localCount += f * float64(m.LocalAttachments)
		if takes {
			// THE TIER IS APPLIED INSIDE THE MONTH, not as a channel-wide
			// fraction of it. Composing the two cuts as independent fractions is
			// wrong exactly when file size and date correlate, which is the
			// ordinary case: one big recording in an early year dragged the
			// projection for every later range down by more than half.
			cn, cb := tierTake(m.Local, p)
			takenCount += f * cn
			takenBytes += f * cb
		}
	}
	ce.Messages = int64(msgs + 0.5)
	if ce.Messages <= 0 {
		return ce, 0
	}
	ce.FirstNano, ce.LastNano = c.FirstNano, c.LastNano
	if p.FromNano > ce.FirstNano {
		ce.FirstNano = p.FromNano
	}
	if p.ToNano > 0 && p.ToNano < ce.LastNano {
		ce.LastNano = p.ToNano
	}

	ce.Attachments = int64(takenCount + 0.5)
	ce.AttachmentBytes = int64(takenBytes + 0.5)

	plain := int64(contentBytes) +
		ce.Messages*perMessagePlainBytes +
		int64(replies)*perReplyPlainBytes +
		ce.Attachments*perAttachTokenPlainBytes
	if p.IncludeReactions {
		plain += int64(reactions) * perReactionPlainBytes
	}
	// Every attachment the archive names and does not hold costs a line of text
	// instead of a blob.
	placeholders := int64(localCount+0.5) - ce.Attachments
	if placeholders < 0 {
		placeholders = 0
	}
	plain += (placeholders + remoteInRange(c, p)) * perPlaceholderPlainBytes

	ce.ChunkBytes = int64(float64(plain) / GzipRatio)
	ce.Chunks = chunkCount(ce.Messages, plain)
	return ce, plain
}

// chunkCount mirrors sealChronicleChunks' soft split: a chunk closes at a
// thousand messages or half a megabyte of plaintext, whichever comes first.
func chunkCount(messages, plain int64) int64 {
	byCount := (messages + 999) / 1000
	bySize := (plain + (512 << 10) - 1) / (512 << 10)
	n := byCount
	if bySize > n {
		n = bySize
	}
	if n < 1 {
		n = 1
	}
	return n
}

// monthOverlap is the fraction of one month's traffic that falls inside the
// policy's window: 1 for a month wholly inside, 0 for one wholly outside, and
// the proportion for one that straddles an endpoint.
//
// The proportion is taken against the month's OBSERVED span — its first and last
// message — not against the calendar month, so a range that ends before a
// channel said anything projects nothing rather than a slice of an empty
// fortnight.
func monthOverlap(m MonthStat, p Policy) float64 {
	lo, hi := m.FirstNano, m.LastNano
	if lo <= 0 {
		return 0
	}
	if hi <= lo {
		// One message, or several in the same nanosecond: there is no span to
		// take a proportion of, so it is in or it is out.
		if p.InRange(lo) {
			return 1
		}
		return 0
	}
	span := hi - lo
	if p.FromNano > lo {
		lo = p.FromNano
	}
	if p.ToNano > 0 && p.ToNano-1 < hi {
		hi = p.ToNano - 1
	}
	if hi < lo {
		return 0
	}
	f := float64(hi-lo) / float64(span)
	if f > 1 {
		return 1
	}
	return f
}

// tierTake is how much of one month's local media a policy's tier admits, by
// count and by bytes.
//
// The histogram is three bits of mantissa under each power of two, so a ceiling
// that lands inside a bucket is ambiguous over a range an eighth of an octave
// wide. It is resolved by assuming that bucket's files are spread evenly through
// it — the same assumption the month proration makes, wrong in the same harmless
// way, and now confined to a bucket narrow enough that being wrong about it
// barely moves the answer.
func tierTake(cells []AttachTally, p Policy) (count, bytes float64) {
	for _, a := range cells {
		if !p.wantsKind(a.Kind) {
			continue
		}
		share := bucketShare(a.Bucket, p.MaxAttachmentBytes)
		if share <= 0 {
			continue
		}
		count += share * float64(a.Count)
		bytes += share * float64(a.Bytes)
	}
	return count, bytes
}

// bucketShare is how much of one histogram bucket a per-file ceiling admits.
func bucketShare(bucket int, maxBytes int64) float64 {
	if maxBytes <= 0 {
		return 1
	}
	lo, hi := tierBucketBounds(bucket)
	switch {
	case maxBytes >= hi:
		return 1
	case maxBytes <= lo:
		return 0
	}
	return float64(maxBytes-lo) / float64(hi-lo)
}

// remoteInRange is the count of attachments the export did not bring, inside the
// policy's date window.
func remoteInRange(c *ChannelStats, p Policy) int64 {
	var n float64
	for _, m := range c.Months {
		if f := monthOverlap(m, p); f > 0 {
			n += f * float64(m.RemoteAttachments)
		}
	}
	return int64(n + 0.5)
}

// placeholdersFor and placeholderBytesFor count what the archive will name and
// not hold: every attachment the tier excluded, plus every one the export never
// brought.
func placeholdersFor(c *ChannelStats, p Policy) int64 {
	var local, taken float64
	takes := p.TakesAttachments()
	for _, m := range c.Months {
		f := monthOverlap(m, p)
		if f <= 0 {
			continue
		}
		local += f * float64(m.LocalAttachments)
		if takes {
			cn, _ := tierTake(m.Local, p)
			taken += f * cn
		}
	}
	n := int64(local-taken+0.5) + remoteInRange(c, p)
	if n < 0 {
		return 0
	}
	return n
}

func placeholderBytesFor(c *ChannelStats, p Policy) int64 {
	var local, taken, remote float64
	takes := p.TakesAttachments()
	for _, m := range c.Months {
		f := monthOverlap(m, p)
		if f <= 0 {
			continue
		}
		local += f * float64(m.LocalAttachmentBytes)
		remote += f * float64(m.RemoteAttachmentBytes)
		if takes {
			_, cb := tierTake(m.Local, p)
			taken += f * cb
		}
	}
	n := int64(local - taken + remote + 0.5)
	if n < 0 {
		return 0
	}
	return n
}

// IncludedChannels lists, in scan order, the source channels a policy would
// import. Shared by the estimator and the importer so the two agree on what
// "included" means.
func IncludedChannels(stats *Stats, p Policy) []ChannelStats {
	var out []ChannelStats
	for i := range stats.Channels {
		c := stats.Channels[i]
		if p.excludes(c.ID) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// NormalizePolicy bounds a policy that arrived over the wire: an inverted date
// range is straightened rather than refused (it is a slider, and somebody will
// drag one past the other), the exclude list is deduplicated and sorted so two
// equal policies produce equal estimates, and a negative ceiling becomes no
// ceiling.
func NormalizePolicy(p Policy) Policy {
	if p.FromNano > 0 && p.ToNano > 0 && p.ToNano < p.FromNano {
		p.FromNano, p.ToNano = p.ToNano, p.FromNano
	}
	if p.MaxAttachmentBytes < 0 {
		p.MaxAttachmentBytes = 0
	}
	if len(p.ExcludeChannels) > 0 {
		seen := map[string]bool{}
		out := p.ExcludeChannels[:0]
		for _, e := range p.ExcludeChannels {
			if e == "" || seen[e] {
				continue
			}
			seen[e] = true
			out = append(out, e)
		}
		sort.Strings(out)
		p.ExcludeChannels = out
	}
	p.Source = strings.TrimSpace(p.Source)
	p.Description = strings.TrimSpace(p.Description)
	return p
}
