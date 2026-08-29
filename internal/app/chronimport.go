package app

import (
	"encoding/base64"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZahakJ/concord/internal/chronimport"
	"github.com/ZahakJ/concord/internal/domain"
)

// Importing somebody else's community into a guild.
//
// internal/chronimport is the pure half: it reads the export format, counts what
// is in it, and projects what a policy would cost, and it touches nothing but
// the files it was pointed at. THIS file is the half that has consequences —
// creating channels, sealing blobs, signing a manifest and putting an archive in
// front of every member — and its shape follows from three facts about that.
//
// IT TAKES MINUTES. A real import is gigabytes read, thousands of files sealed
// and a manifest signed at the end. That cannot be an RPC somebody waits on, so
// ImportChatExport starts a job and returns an id, progress arrives on the event
// stream, and ChronicleImportStatus answers "how far". One at a time per
// service, enforced by a mutex, because two imports would race on channel
// creation and produce a guild with two of everything.
//
// IT IS OWNER-ONLY, and for the reason AttachChronicle is: a chronicle is a
// claim about a guild's past signed by whoever runs it, and everything this file
// does ends in AttachChronicle, which checks the gate again. The check here is
// the early one, so an hour of work is not spent before the refusal.
//
// IT IS OFFLINE. Not one byte is fetched. See the package doc on
// internal/chronimport for why that is a stance rather than an omission; the
// consequence here is that an attachment the export did not bring becomes a line
// of text naming what is missing, and never a request to a third party.
//
// A note on what it does NOT do. The messages are not delivered into channels —
// nothing is sent, nothing is gossiped as a message, no member's unread count
// moves. A decade of history lands as an ARCHIVE that sits above the live
// scrollback, which is the whole point of the chronicle format: importing four
// hundred thousand messages as four hundred thousand real messages would put
// them through MLS one at a time and leave every member's database the size of
// the export.

const (
	// chatImportProgressEvery is how often a running import reports itself.
	// Every five hundred messages rather than on a timer: the work is uneven —
	// a channel of one-word messages runs a hundred times faster than one full
	// of images — and a bar that moves with the work is the honest one.
	chatImportProgressEvery = 500

	// maxImportedReactionsPerMessage bounds the reaction map on one archived
	// message. An export is a file somebody else wrote, and a thousand entries
	// on one message would be a cheap way to make a chunk that no peer can
	// serve.
	maxImportedReactionsPerMessage = 32

	// chatImportJobRetention is how long a finished job's result stays
	// answerable. Long enough for a client that was closed when the import
	// finished to come back and read the outcome.
	chatImportJobRetention = 30 * time.Minute
)

// ChatImportPhase names what a running import is doing, for the progress line.
type ChatImportPhase = string

const (
	PhaseScanning  ChatImportPhase = "scanning"
	PhaseStructure ChatImportPhase = "structure"
	PhaseEmoji     ChatImportPhase = "emoji"
	PhaseReading   ChatImportPhase = "reading"
	PhaseSealing   ChatImportPhase = "sealing"
	PhaseAttaching ChatImportPhase = "attaching"
	PhaseDone      ChatImportPhase = "done"
	PhaseFailed    ChatImportPhase = "failed"
)

// ChatImportProgress is one beat of a running import.
type ChatImportProgress struct {
	JobID   string `json:"jobId"`
	GuildID string `json:"guildId"`
	Phase   string `json:"phase"`
	// Channel is the source channel being read, or "" for phases that are not
	// per-channel.
	Channel string `json:"channel,omitempty"`
	Done    int64  `json:"done"`
	Total   int64  `json:"total"`
}

// ChatImportResult is what an import actually did, as opposed to what the
// estimate said it would. Every number here is counted, not projected.
type ChatImportResult struct {
	ChronicleID string `json:"chronicleId"`
	Source      string `json:"source"`

	Imported         int64 `json:"imported"`
	SkippedByPolicy  int64 `json:"skippedByPolicy"`
	SkippedMalformed int64 `json:"skippedMalformed"`
	// SkippedNotices is the join/pin/boost chatter, never imported and counted
	// separately so the arithmetic against the export's own total closes.
	SkippedNotices int64 `json:"skippedNotices"`

	AttachmentsSealed     int64 `json:"attachmentsSealed"`
	AttachmentBytesSealed int64 `json:"attachmentBytesSealed"`
	// Placeholders is every attachment the archive names and does not hold: the
	// remote-only ones, the ones the policy's tier excluded, and the ones whose
	// file turned out to be unreadable.
	Placeholders int64 `json:"placeholders"`

	Channels          int `json:"channels"`
	ChannelsCreated   int `json:"channelsCreated"`
	ChannelsReused    int `json:"channelsReused"`
	CategoriesCreated int `json:"categoriesCreated"`
	Authors           int `json:"authors"`
	EmojiImported     int `json:"emojiImported"`
	EmojiSkipped      int `json:"emojiSkipped"`

	Chunks        int   `json:"chunks"`
	ChunkBytes    int64 `json:"chunkBytes"`
	ManifestBytes int64 `json:"manifestBytes"`
}

// ChatImportStatus is what the polling RPC answers.
type ChatImportStatus struct {
	JobID    string            `json:"jobId"`
	GuildID  string            `json:"guildId"`
	Dir      string            `json:"dir"`
	Phase    string            `json:"phase"`
	Channel  string            `json:"channel,omitempty"`
	Done     int64             `json:"done"`
	Total    int64             `json:"total"`
	Running  bool              `json:"running"`
	Error    string            `json:"error,omitempty"`
	Started  int64             `json:"started"`
	Finished int64             `json:"finished,omitempty"`
	Result   *ChatImportResult `json:"result,omitempty"`
}

// chatImportJob is the live state of one import. Every field is guarded by
// Service.importMu — including the ones the worker writes, which is why the
// worker's progress helper takes the lock rather than writing through.
type chatImportJob struct {
	id      string
	guildID string
	dir     string
	phase   string
	channel string
	done    int64
	total   int64
	running bool
	err     string
	started time.Time
	ended   time.Time
	result  *ChatImportResult
}

func (j *chatImportJob) status() ChatImportStatus {
	st := ChatImportStatus{
		JobID: j.id, GuildID: j.guildID, Dir: j.dir, Phase: j.phase,
		Channel: j.channel, Done: j.done, Total: j.total,
		Running: j.running, Error: j.err, Started: j.started.UnixMilli(),
		Result: j.result,
	}
	if !j.ended.IsZero() {
		st.Finished = j.ended.UnixMilli()
	}
	return st
}

// OnChatImport registers a callback fired on every beat of a running import.
func (s *Service) OnChatImport(fn func(ChatImportProgress)) {
	s.mu.Lock()
	s.onChatImport = append(s.onChatImport, fn)
	s.mu.Unlock()
}

func (s *Service) emitChatImport(p ChatImportProgress) {
	s.mu.RLock()
	cbs := append([]func(ChatImportProgress){}, s.onChatImport...)
	s.mu.RUnlock()
	for _, cb := range cbs {
		cb(p)
	}
}

// ScanChatExport reads an export directory and reports what is in it, without
// writing or fetching anything.
//
// The result is cached against the directory's fingerprint (names, sizes,
// modification times), because the wizard's whole flow is scan once and then
// re-estimate on every slider drag, and a re-scan of a hundred gigabytes per
// keystroke is not a flow. A directory that changed underneath us re-scans.
func (s *Service) ScanChatExport(dir string) (*chronimport.Stats, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, fmt.Errorf("app: which export directory?")
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("app: %s is not a directory this device can read", dir)
	}
	if st, ok := s.cachedScan(dir); ok {
		return st, nil
	}
	st, err := chronimport.ScanChatExport(dir)
	if err != nil {
		return nil, err
	}
	s.importMu.Lock()
	// One directory's scan, not a growing pile: a wizard looks at one export.
	// Keeping every directory ever scanned would hold a report the size of the
	// export's channel table for as long as the process lives.
	s.scanCache = map[string]*chronimport.Stats{dir: st}
	s.importMu.Unlock()
	return st, nil
}

// cachedScan returns a still-valid cached scan for a directory.
func (s *Service) cachedScan(dir string) (*chronimport.Stats, bool) {
	s.importMu.Lock()
	st, ok := s.scanCache[dir]
	s.importMu.Unlock()
	if !ok {
		return nil, false
	}
	key, err := chronimport.CurrentKey(dir)
	if err != nil || key != st.Key {
		return nil, false
	}
	return st, true
}

// EstimateChatImport projects what importing a directory under a policy would
// cost. Pure arithmetic over a cached scan — no file is opened unless the
// directory has never been scanned or has changed since it was.
func (s *Service) EstimateChatImport(dir string, policy chronimport.Policy) (chronimport.Estimate, error) {
	st, err := s.ScanChatExport(dir)
	if err != nil {
		return chronimport.Estimate{}, err
	}
	return chronimport.EstimateChatImport(st, chronimport.NormalizePolicy(policy)), nil
}

// ImportChatExport starts an import and returns its job id.
//
// Owner-only, and refused while another import is running. The work happens on
// its own goroutine; progress arrives as chronicle-import events and the outcome
// is readable from ChronicleImportStatus for half an hour afterwards.
func (s *Service) ImportChatExport(guildID, dir string, policy chronimport.Policy) (string, error) {
	dir = strings.TrimSpace(dir)
	self := s.id.Fingerprint()
	if !s.IsGuildOwner(guildID, self) {
		return "", fmt.Errorf("app: only the guild's owner can import an archive")
	}
	s.mu.RLock()
	_, tracked := s.guilds[guildID]
	s.mu.RUnlock()
	if !tracked {
		return "", fmt.Errorf("app: unknown guild %s", guildID)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("app: %s is not a directory this device can read", dir)
	}

	policy = chronimport.NormalizePolicy(policy)

	s.importMu.Lock()
	if s.importJob != nil && s.importJob.running {
		running := s.importJob.id
		s.importMu.Unlock()
		return "", fmt.Errorf("app: an import is already running (job %s); wait for it to finish", running)
	}
	job := &chatImportJob{
		id: domain.NewID(), guildID: guildID, dir: dir,
		phase: PhaseScanning, running: true, started: time.Now(),
	}
	s.importJob = job
	s.importMu.Unlock()

	go s.runChatImport(job, policy)
	return job.id, nil
}

// ChronicleImportStatus answers where an import got to. An empty job id asks
// about the most recent one, which is what a client that reconnected mid-import
// needs.
func (s *Service) ChronicleImportStatus(jobID string) (ChatImportStatus, error) {
	s.importMu.Lock()
	defer s.importMu.Unlock()
	if s.importJob == nil {
		return ChatImportStatus{}, nil
	}
	if jobID != "" && s.importJob.id != jobID {
		return ChatImportStatus{}, fmt.Errorf("app: no import with that id")
	}
	if !s.importJob.running && !s.importJob.ended.IsZero() &&
		time.Since(s.importJob.ended) > chatImportJobRetention {
		return ChatImportStatus{}, nil
	}
	return s.importJob.status(), nil
}

// setPhase records where the import is and tells anybody watching.
func (s *Service) setPhase(job *chatImportJob, phase, channel string, done, total int64) {
	s.importMu.Lock()
	job.phase, job.channel, job.done, job.total = phase, channel, done, total
	guildID := job.guildID
	s.importMu.Unlock()
	s.emitChatImport(ChatImportProgress{
		JobID: job.id, GuildID: guildID, Phase: phase,
		Channel: channel, Done: done, Total: total,
	})
}

// finish closes a job out, successfully or otherwise, and emits the last beat.
func (s *Service) finish(job *chatImportJob, res *ChatImportResult, err error) {
	s.importMu.Lock()
	job.running = false
	job.ended = time.Now()
	job.result = res
	if err != nil {
		job.phase, job.err = PhaseFailed, err.Error()
	} else {
		job.phase = PhaseDone
		if res != nil {
			job.done, job.total = res.Imported, res.Imported
		}
	}
	phase, done, total, guildID := job.phase, job.done, job.total, job.guildID
	s.importMu.Unlock()
	s.emitChatImport(ChatImportProgress{
		JobID: job.id, GuildID: guildID, Phase: phase, Done: done, Total: total,
	})
	s.emitGuildUpdate()
}

// runChatImport is the whole pipeline, on its own goroutine.
func (s *Service) runChatImport(job *chatImportJob, policy chronimport.Policy) {
	res, err := s.importChatExport(job, policy)
	s.finish(job, res, err)
}

func (s *Service) importChatExport(job *chatImportJob, policy chronimport.Policy) (*ChatImportResult, error) {
	guildID, dir := job.guildID, job.dir

	s.setPhase(job, PhaseScanning, "", 0, 0)
	stats, err := s.ScanChatExport(dir)
	if err != nil {
		return nil, err
	}
	est := chronimport.EstimateChatImport(stats, policy)
	if est.Messages == 0 {
		return nil, fmt.Errorf("app: that policy would import no messages at all")
	}
	// Refuse early what the format cannot hold, rather than after an hour of
	// reading. Both ceilings are the manifest's, and both have the same fix:
	// narrow the policy, or import in several passes.
	if est.ManifestOverCap {
		return nil, fmt.Errorf(
			"app: that import's index would be about %d KB, over the %d KB an archive index can be; "+
				"narrow the date range or leave some channels out and import them separately",
			est.ManifestBytes>>10, maxChronicleManifestBytes>>10)
	}
	if est.ChannelsOverCap {
		return nil, fmt.Errorf(
			"app: that import names %d channels, over the %d one archive can index; "+
				"leave some out and import them as a second archive",
			len(est.Channels), maxChronicleChannels)
	}
	if est.Chunks > maxChronicleChunks {
		return nil, fmt.Errorf(
			"app: that import would need about %d pages, over the %d an archive can hold; "+
				"narrow the date range or leave some channels out",
			est.Chunks, maxChronicleChunks)
	}

	// ---- structure ----
	s.setPhase(job, PhaseStructure, "", 0, int64(len(stats.Channels)))
	plan, structRes, err := s.buildImportStructure(guildID, stats, policy, job)
	if err != nil {
		return nil, err
	}

	// ---- emoji ----
	emojiIn, emojiSkip := 0, 0
	if policy.IncludeEmoji {
		s.setPhase(job, PhaseEmoji, "", 0, int64(len(stats.Emoji)))
		emojiIn, emojiSkip = s.importCustomEmoji(guildID, dir, stats, job)
	}

	// ---- reading ----
	acc := &importAccumulator{
		dir:      dir,
		policy:   policy,
		plan:     plan,
		seen:     map[string]int{},
		imported: map[string]bool{},
		byChannel: make(map[string][]chronicleMessage,
			len(plan.channels)),
	}
	s.setPhase(job, PhaseReading, "", 0, est.Messages)
	if err := s.readChatExport(job, stats, acc, est.Messages); err != nil {
		return nil, err
	}
	if len(acc.byChannel) == 0 {
		return nil, fmt.Errorf("app: nothing survived that policy; there is no archive to attach")
	}

	// ---- sealing ----
	s.setPhase(job, PhaseSealing, "", 0, 0)
	source := policy.Source
	if source == "" {
		name := chronimport.SanitizeName(stats.Guild)
		if name == "" {
			name = "an imported community"
		}
		source = fmt.Sprintf("%s, imported %s", name, time.Now().UTC().Format("2 January 2006"))
	}
	source = clampBytes(source, maxChronicleSourceLen)
	desc := policy.Description
	desc = clampBytes(desc, maxChronicleDescLen)

	channels := acc.manifestChannels()
	raw, chunks, err := s.buildChronicle(guildID, source, desc, channels,
		acc.authors, acc.byChannel, acc.sealed, acc.sealedBytes)
	if err != nil {
		return nil, err
	}

	// ---- attaching ----
	s.setPhase(job, PhaseAttaching, "", 0, int64(len(chunks)))
	if err := s.AttachChronicle(guildID, raw, chunks); err != nil {
		return nil, err
	}

	var chunkBytes int64
	for _, ct := range chunks {
		chunkBytes += int64(len(ct))
	}
	return &ChatImportResult{
		ChronicleID:           chronicleIDOf(raw),
		Source:                source,
		Imported:              acc.imported64(),
		SkippedByPolicy:       acc.skippedPolicy,
		SkippedMalformed:      stats.Malformed,
		SkippedNotices:        stats.Notices,
		AttachmentsSealed:     acc.sealed64(),
		AttachmentBytesSealed: acc.sealedBytes,
		Placeholders:          acc.placeholders,
		Channels:              len(channels),
		ChannelsCreated:       structRes.created,
		ChannelsReused:        structRes.reused,
		CategoriesCreated:     structRes.categories,
		Authors:               len(acc.authors),
		EmojiImported:         emojiIn,
		EmojiSkipped:          emojiSkip,
		Chunks:                len(chunks),
		ChunkBytes:            chunkBytes,
		ManifestBytes:         int64(len(raw)),
	}, nil
}

// ---- structure ----

// plannedChannel is one source channel and where its history landed.
type plannedChannel struct {
	source chronimport.ChannelStats
	ctype  string
	mapped string // the real channel id in this guild
	// history is false for a voice channel: it is created so the guild looks
	// like the community it came from, and it carries nothing, because an
	// export of a voice channel is join and leave notices and no conversation.
	history bool
}

type importPlan struct {
	channels []plannedChannel
	bySource map[string]*plannedChannel
}

type structureResult struct{ created, reused, categories int }

// buildImportStructure creates the categories and channels the import needs and
// works out where each source channel's history goes.
//
// MATCHING IS BY NAME, and reuse is the default. Somebody importing their old
// community into a guild they already made has already created #general, and an
// importer that made a second one would be telling them their setup was wrong.
// The comparison is case-insensitive and whitespace-folded because "General"
// and "general" are the same room to everybody except a string comparison.
func (s *Service) buildImportStructure(guildID string, stats *chronimport.Stats,
	policy chronimport.Policy, job *chatImportJob) (*importPlan, structureResult, error) {

	var res structureResult
	plan := &importPlan{bySource: map[string]*plannedChannel{}}

	// The categories the included channels ask for, in first-seen order.
	existingCats, _ := s.Categories(guildID)
	catByName := map[string]string{}
	for _, c := range existingCats {
		catByName[foldName(c.Name)] = c.ID
	}

	included := chronimport.IncludedChannels(stats, policy)
	for i := range included {
		c := included[i]
		catID := ""
		if name := chronimport.SanitizeName(c.Category); name != "" {
			id, ok := catByName[foldName(name)]
			if !ok {
				cat, err := s.CreateCategory(guildID, name)
				if err != nil {
					return nil, res, fmt.Errorf("app: could not create the category %q: %w", name, err)
				}
				id = cat.ID
				catByName[foldName(name)] = id
				res.categories++
			}
			catID = id
		}

		ctype := chronimport.ChannelTypeOf(c.Type)
		name := chronimport.SanitizeName(c.Name)
		if name == "" {
			name = "imported"
		}
		mapped, created, err := s.channelForImport(guildID, name, ctype, catID)
		if err != nil {
			return nil, res, err
		}
		if created {
			res.created++
		} else {
			res.reused++
		}
		pc := plannedChannel{source: c, ctype: ctype, mapped: mapped, history: ctype != "voice"}
		plan.channels = append(plan.channels, pc)
		s.setPhase(job, PhaseStructure, name, int64(i+1), int64(len(included)))
	}
	for i := range plan.channels {
		plan.bySource[plan.channels[i].source.ID] = &plan.channels[i]
	}
	return plan, res, nil
}

// channelForImport finds a channel of this name and type in the guild, or makes
// one. Type is part of the match because a forum board and a voice room may
// legitimately share a name — the live UI already allows that — and landing a
// decade of text in a voice channel because the names agreed would be worse than
// creating a second row.
func (s *Service) channelForImport(guildID, name, ctype, categoryID string) (id string, created bool, err error) {
	want := foldName(name)
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var match string
	if ok {
		for _, ch := range g.Channels {
			// A forum POST is a channel too, and it is never what somebody meant
			// by "#general already exists".
			if ch.Parent != "" {
				continue
			}
			if foldName(ch.Name) == want && ch.ChannelType() == ctype {
				match = ch.ID
				break
			}
		}
	}
	s.mu.RUnlock()
	if !ok {
		return "", false, fmt.Errorf("app: unknown guild %s", guildID)
	}
	if match != "" {
		return match, false, nil
	}
	ch, err := s.CreateChannel(guildID, name, ctype, categoryID)
	if err != nil {
		return "", false, fmt.Errorf("app: could not create the channel %q: %w", name, err)
	}
	return ch.ID, true, nil
}

func foldName(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// ---- emoji ----

// importCustomEmoji brings across the custom emoji the export's reactions used,
// where the export brought the picture with it.
//
// Everything that fails is counted rather than fataling: an emoji is decoration,
// and losing a decade of history because one 300 KiB GIF was over the limit
// would be an absurd trade. The three ways one fails — no local file, a name
// that folds to nothing usable, an image the guild's own gate refuses — are all
// reported as one number, because the user's response to all three is the same.
func (s *Service) importCustomEmoji(guildID, dir string, stats *chronimport.Stats, job *chatImportJob) (imported, skipped int) {
	for i, e := range stats.Emoji {
		s.setPhase(job, PhaseEmoji, e.Name, int64(i+1), int64(len(stats.Emoji)))
		if !e.Local || e.Sanitized == "" {
			skipped++
			continue
		}
		uri, ok := s.emojiDataURI(dir, e)
		if !ok {
			skipped++
			continue
		}
		if err := s.AddCustomEmoji(guildID, e.Sanitized, uri); err != nil {
			skipped++
			continue
		}
		imported++
	}
	return imported, skipped
}

// emojiDataURI reads an emoji's image off disk and builds the data URI the guild
// emoji path takes, re-running that path's own validation before offering it.
func (s *Service) emojiDataURI(dir string, e chronimport.EmojiStat) (string, bool) {
	// Bounded before the read, not after: the point of a cap is not to allocate
	// the thing you are about to refuse.
	if e.Bytes <= 0 || e.Bytes > maxEmojiBytes {
		return "", false
	}
	p, ok := chronimport.LocalEmojiPath(dir, e)
	if !ok {
		return "", false
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return "", false
	}
	sub := imageSubtypeOf(p)
	if sub == "" {
		return "", false
	}
	uri := "data:image/" + sub + ";base64," + base64.StdEncoding.EncodeToString(raw)
	if !validEmojiImage(uri) {
		return "", false
	}
	return uri, true
}

// imageSubtypeOf maps a filename onto the four raster subtypes every image path
// in this application accepts. Extension rather than content sniffing to match
// what the rest of the codebase does with a data URI's declared type — and a
// file whose extension lies fails validEmojiImage or the renderer, not a
// security boundary, because the subtype only ever lands inside a data: URI
// whose charset is already constrained.
func imageSubtypeOf(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "png"
	case ".jpg", ".jpeg":
		return "jpeg"
	case ".gif":
		return "gif"
	case ".webp":
		return "webp"
	}
	return ""
}

// ---- reading ----

// importAccumulator holds the growing archive as the export is read.
//
// It holds the whole import in memory, and that is a real ceiling worth naming:
// the manifest caps an archive at 4096 pages of at most a thousand messages, so
// roughly four million messages is the format's own limit and a few hundred
// megabytes of Go structs is the practical one. The SCAN is the part that had to
// stream, because that is the part that runs against a directory nobody has
// measured yet; by the time this runs, the estimate has already refused anything
// the format could not hold. Sealing per channel as the read progresses would
// lower the ceiling further and is the obvious next move if it ever bites — it
// was not done here because buildChronicle is the one place the signing, sealing
// and size rules live, and splitting them to save memory nobody has run out of
// would be trading a real invariant for a hypothetical one.
type importAccumulator struct {
	dir    string
	policy chronimport.Policy
	plan   *importPlan

	byChannel map[string][]chronicleMessage
	authors   []chronicleAuthor
	seen      map[string]int // author key -> index in authors
	// imported is the set of source message ids that made it in, so a reply
	// whose target did not can drop its reference rather than dangle.
	imported map[string]bool
	// overflow is the index of the shared stand-in author, minted only if the
	// export has more distinct names than a manifest can carry.
	overflow int

	count         int64
	skippedPolicy int64
	sealed        int
	sealedBytes   int64
	placeholders  int64
}

func (a *importAccumulator) imported64() int64 { return a.count }
func (a *importAccumulator) sealed64() int64   { return int64(a.sealed) }

// manifestChannels is the archive's channel table: the planned channels that
// actually carry history, in the scan's order, with every name held to the
// manifest's own limits. One implementation rather than one here and one in the
// tests — a table built two ways is a table where only one of them is the one
// that gets validated.
func (a *importAccumulator) manifestChannels() []chronicleChannel {
	var out []chronicleChannel
	for _, ch := range a.plan.channels {
		if len(a.byChannel[ch.source.ID]) == 0 {
			continue
		}
		out = append(out, chronicleChannel{
			ID:     ch.source.ID,
			Name:   chronimport.SanitizeName(ch.source.Name),
			Type:   ch.ctype,
			Topic:  chronimport.SanitizeName(ch.source.Topic),
			Mapped: ch.mapped,
		})
	}
	return out
}

// authorIndex resolves an author to its manifest index, minting one on first
// sight. Beyond the manifest's ceiling every further name collapses into one
// stand-in: a manifest that overflowed would be refused whole, and an archive
// where the eight-thousand-and-first person is "someone" is enormously better
// than no archive.
// avatar is a THUNK, not a string, and that is not fussiness: resolving a
// portrait stats a file and may read it, and calling it per message rather than
// per author would put a million filesystem round trips into a million-message
// import for four distinct faces.
func (a *importAccumulator) authorIndex(au chronimport.Author, avatar func() string) int {
	k := au.Key()
	if i, ok := a.seen[k]; ok {
		return i
	}
	if len(a.authors) >= maxChronicleAuthors-1 {
		if a.overflow == 0 {
			a.authors = append(a.authors, chronicleAuthor{Name: "someone"})
			a.overflow = len(a.authors) - 1
		}
		a.seen[k] = a.overflow
		return a.overflow
	}
	name := chronimport.SanitizeName(au.Display())
	if name == "" {
		name = "someone"
	}
	a.authors = append(a.authors, chronicleAuthor{Name: name, Avatar: avatar()})
	i := len(a.authors) - 1
	a.seen[k] = i
	return i
}

// readChatExport is the second streaming pass: the one that builds the archive.
// Same walker as the scan, same memory discipline, one message at a time.
func (s *Service) readChatExport(job *chatImportJob, stats *chronimport.Stats,
	acc *importAccumulator, total int64) error {

	// Files grouped by the channel they belong to, so the archive is assembled
	// channel by channel and the progress line names a room rather than a file.
	for i := range acc.plan.channels {
		pc := &acc.plan.channels[i]
		if !pc.history {
			continue
		}
		for _, rel := range pc.source.Files {
			path := filepath.Join(acc.dir, rel)
			_, err := chronimport.Walk(path, nil, func(m *chronimport.Message) error {
				s.appendImported(acc, pc, m)
				if acc.count%chatImportProgressEvery == 0 {
					s.setPhase(job, PhaseReading, pc.source.Name, acc.count, total)
				}
				return nil
			})
			if err != nil {
				// The scan already reported this file; a truncated file yields
				// what it had and the import carries on, because refusing the
				// whole community over one bad file is the wrong trade and the
				// result counts exactly what it kept.
				continue
			}
		}
	}
	s.setPhase(job, PhaseReading, "", acc.count, total)
	return nil
}

// appendImported turns one exported message into an archived one, or counts why
// it did not.
func (s *Service) appendImported(acc *importAccumulator, pc *plannedChannel, m *chronimport.Message) {
	if m.Skippable() {
		return // counted by the scan as a notice, never imported
	}
	nano, ok := m.At()
	if !ok {
		return // counted by the scan as malformed
	}
	if !acc.policy.InRange(nano) {
		acc.skippedPolicy++
		return
	}

	content := chronimport.SanitizeContent(m.Content)
	tokens, notes := s.importAttachments(acc, m)
	if len(notes) > 0 {
		if content != "" {
			content += "\n"
		}
		content += strings.Join(notes, "\n")
	}
	// A message with no text, no picture and no reaction is a message the export
	// recorded and nobody wrote — an embed-only row, a deleted body. Dropping it
	// keeps a decade of history from being a third empty.
	if content == "" && len(tokens) == 0 {
		acc.skippedPolicy++
		return
	}

	cm := chronicleMessage{
		ID:      m.ID,
		Author:  acc.authorIndex(m.Author, func() string { return s.importedAvatar(acc.dir, m.Author) }),
		Nano:    nano,
		Content: content,
		Attach:  tokens,
	}
	if m.Reference != nil && acc.imported[m.Reference.MessageID] {
		// Only a reference the archive can actually resolve. A reply pointing at
		// a message the policy left out would render as a dangling quote, which
		// is worse than a reply that simply reads as one.
		cm.ReplyTo = m.Reference.MessageID
	}
	if acc.policy.IncludeReactions && len(m.Reactions) > 0 {
		cm.Reactions = importedReactions(m.Reactions)
	}

	acc.byChannel[pc.source.ID] = append(acc.byChannel[pc.source.ID], cm)
	acc.imported[m.ID] = true
	acc.count++
}

// importedReactions folds an export's reaction list into the count map a chunk
// carries, bounded.
func importedReactions(rs []chronimport.Reaction) map[string]int {
	out := map[string]int{}
	for _, r := range rs {
		if len(out) >= maxImportedReactionsPerMessage {
			break
		}
		label := reactionLabel(r.Emoji)
		if label == "" || r.Count <= 0 {
			continue
		}
		out[label] += r.Count
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// reactionLabel is how one emoji is written into a chunk.
//
// A CUSTOM emoji has to go through the same name fold the emoji import itself
// uses, because the two have to name the same thing. The export calls it
// "Party-Parrot!"; importCustomEmoji creates a guild emoji called
// "party_parrot" (that charset is all a guild emoji name may contain); and
// SanitizeContent already rewrites "<:Party-Parrot:1234>" in a message BODY to
// ":party_parrot:". Leaving the reaction tally spelling it ":Party-Parrot:"
// meant the one place the picture was most likely to appear was the one place
// it never resolved — the pill rendered as a literal shortcode, in an archive
// nobody can edit, for as long as the archive exists.
func reactionLabel(e chronimport.Emoji) string {
	if !e.Custom() {
		return chronimport.SanitizeName(e.Label())
	}
	name := chronimport.SanitizeEmojiName(e.Name)
	if name == "" {
		// Nothing usable survives the fold, so there is no emoji this could
		// name. Dropping it beats a pill reading "::".
		return ""
	}
	return ":" + name + ":"
}

// importedAvatar returns an author's portrait as a data URI, or "" — which is
// the ordinary answer.
//
// No scaling: this application ships as one static binary with no image codec
// beyond what the standard library gives, and re-encoding a portrait would mean
// carrying one. So the rule is simple and the reason is stated where somebody
// will look for it — a portrait that is already small enough to ride the
// manifest rides it, and a bigger one is dropped rather than mangled. The names
// still import; only the faces are optional, which is exactly the trade
// buildChronicle already makes when an index will not fit.
func (s *Service) importedAvatar(dir string, au chronimport.Author) string {
	p, ok := chronimport.LocalAvatarPath(dir, au)
	if !ok {
		return ""
	}
	fi, err := os.Stat(p)
	// Base64 costs four bytes for every three, plus the ~22-byte prefix, so a
	// file over three quarters of the cap cannot possibly fit and is refused
	// before it is read.
	if err != nil || fi.Size() <= 0 || fi.Size() > (maxChronicleAvatarBytes/4)*3 {
		return ""
	}
	sub := imageSubtypeOf(p)
	if sub == "" {
		return ""
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	uri := "data:image/" + sub + ";base64," + base64.StdEncoding.EncodeToString(raw)
	if !validImageDataURI(uri, maxChronicleAvatarBytes) {
		return ""
	}
	return uri
}

// importAttachments seals the media the policy admits and writes a placeholder
// line for the media it does not.
//
// The placeholder is the whole self-containment stance made visible: an archive
// says what it holds and what it does not, in the message where the file was,
// rather than leaving a reader to wonder whether 2014 really had no pictures in
// it.
func (s *Service) importAttachments(acc *importAccumulator, m *chronimport.Message) (tokens, notes []string) {
	for i := range m.Attachments {
		at := m.Attachments[i]
		kind := chronimport.KindOf(at)
		path, local := chronimport.LocalAttachmentPath(acc.dir, at)
		size := at.FileSizeBytes
		if local {
			if fi, err := os.Stat(path); err == nil {
				size = fi.Size()
			} else {
				local = false
			}
		}
		if local && acc.policy.TakesAttachment(kind, size) && size > 0 && size <= maxFilePlain {
			if tok, ok := s.sealImportedFile(path, at, size); ok {
				tokens = append(tokens, tok)
				acc.sealed++
				acc.sealedBytes += size
				continue
			}
		}
		acc.placeholders++
		notes = append(notes, placeholderLine(at, size))
	}
	return tokens, notes
}

// placeholderLine is what stands in for a file the archive does not hold.
func placeholderLine(at chronimport.Attachment, size int64) string {
	name := chronimport.SanitizeName(at.FileName)
	if name == "" {
		name = "a file"
	}
	name = clampBytes(name, maxFilenameLen)
	if size <= 0 {
		return fmt.Sprintf("[attachment not exported: %s]", name)
	}
	return fmt.Sprintf("[attachment not exported: %s, %s]", name, humanBytes(size))
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/float64(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}

// sealImportedFile seals one asset into a blob and returns the token the archive
// carries. The blob is PINNED: this device performed the import, so its copy is
// the original, and letting the attachment LRU evict a picture out of an archive
// nobody else has a copy of would destroy it.
func (s *Service) sealImportedFile(path string, at chronimport.Attachment, size int64) (string, bool) {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return "", false
	}
	sub := imageSubtypeOf(path)
	// An image over the inline ceiling is still a file, and a file token renders
	// as a download card rather than being refused — which is what the live path
	// would do with the same bytes.
	if sub != "" && len(raw) > maxAttachmentPlain {
		sub = ""
	}
	blobID, keys, err := s.sealBlob(raw)
	if err != nil {
		return "", false
	}
	// Pinning is best-effort: a failure here means the picture is evictable,
	// not that the import failed.
	_ = s.store.PinAttachment(blobID, true)

	if sub != "" {
		return fmt.Sprintf("concord://attach/v1/%s/%s/%s/0x0", blobID, keys, sub), true
	}
	name := chronimport.SanitizeName(at.FileName)
	if name == "" {
		name = filepath.Base(path)
	}
	name = clampBytes(name, maxFilenameLen)
	mimeType := mime.TypeByExtension(filepath.Ext(path))
	if i := strings.Index(mimeType, ";"); i >= 0 {
		mimeType = mimeType[:i]
	}
	if !mimeRe.MatchString(mimeType) {
		mimeType = "application/octet-stream"
	}
	return fmt.Sprintf("concord://file/v1/%s/%s/%d/%s/%s", blobID, keys, size,
		b64url.EncodeToString([]byte(mimeType)), b64url.EncodeToString([]byte(name))), true
}
