package app

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/identity"
	"github.com/ZahakJ/concord/internal/version"
)

// A history archive: every guild and DM this device holds, sealed under a
// passphrase, so that losing every device does not have to mean losing the
// conversation.
//
// What it deliberately does NOT contain is as important as what it does. No
// identity seed and no MLS group state, for two separate reasons:
//
//   - The identity already has a recovery path (the mnemonic, and device
//     linking). Putting a second copy of it in a file people are encouraged to
//     copy around would widen the blast radius of that file for no gain.
//   - MLS group state includes this device's leaf PRIVATE key, and every linked
//     device is its own member with its own leaf. Restoring group state onto a
//     second device would put one member's private key on two machines, which
//     breaks forward secrecy and desynchronises epochs. An archive that did it
//     would be a correctness bug wearing the costume of a convenience.
//
// So a restore is "put my history back on a device that already has an identity
// and has already rejoined the groups" — not "become me". The two halves are
// recovered by different mechanisms on purpose.
const archiveVersion = 1

type archiveFile struct {
	Version int    `json:"version"`
	Created string `json:"created"` // RFC3339, informational only
	App     string `json:"app"`     // version that wrote it, for diagnosing odd files

	Guilds   []archiveGuild    `json:"guilds"`
	Messages []domain.Message  `json:"messages"`
	Saved    []string          `json:"saved,omitempty"`
	Blobs    map[string][]byte `json:"blobs,omitempty"` // attachment ciphertext by id
}

// archiveGuild records enough structure to make the messages readable again:
// which channel a message belongs to, and what that channel and guild were
// called. It is NOT used to recreate a guild — membership is cryptographic and
// cannot be restored from a file.
type archiveGuild struct {
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	Kind     string           `json:"kind,omitempty"`
	Channels []archiveChannel `json:"channels"`
}

type archiveChannel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

// ArchiveStats reports what an export wrote or an import took in.
type ArchiveStats struct {
	Guilds      int `json:"guilds"`
	Channels    int `json:"channels"`
	Messages    int `json:"messages"`
	Saved       int `json:"saved"`
	Attachments int `json:"attachments"`
	Skipped     int `json:"skipped"` // import: already present, left untouched
}

// ExportArchive builds the sealed archive. withAttachments carries the cached
// attachment blobs too, which is off by default because the cache is bounded at
// a gigabyte and evicted by least-recent use: including it makes the file large
// without making it complete.
func (s *Service) ExportArchive(passphrase string, withAttachments bool) ([]byte, ArchiveStats, error) {
	var st ArchiveStats
	if passphrase == "" {
		return nil, st, fmt.Errorf("app: an archive needs a passphrase")
	}

	af := archiveFile{Version: archiveVersion, Created: time.Now().UTC().Format(time.RFC3339), App: version.Version}

	s.mu.RLock()
	guilds := make([]*domain.Guild, 0, len(s.guilds))
	for _, g := range s.guilds {
		guilds = append(guilds, g)
	}
	s.mu.RUnlock()

	for _, g := range guilds {
		ag := archiveGuild{ID: g.ID, Name: g.Name, Kind: g.Kind}
		for _, c := range g.Channels {
			ag.Channels = append(ag.Channels, archiveChannel{ID: c.ID, Name: c.Name, Type: c.Type})
			// limit 0 means "everything": an archive that stopped at a page
			// would be the same trap the Markdown export fell into.
			msgs, err := s.store.Messages(c.ID, 0)
			if err != nil {
				return nil, st, fmt.Errorf("app: read %s: %w", c.Name, err)
			}
			af.Messages = append(af.Messages, msgs...)
			st.Channels++
		}
		af.Guilds = append(af.Guilds, ag)
		st.Guilds++
	}
	st.Messages = len(af.Messages)

	saved, err := s.store.SavedMessageIDs()
	if err != nil {
		return nil, st, err
	}
	for id := range saved {
		af.Saved = append(af.Saved, id)
	}
	st.Saved = len(af.Saved)

	if withAttachments {
		blobs, err := s.store.AttachmentBlobs()
		if err != nil {
			return nil, st, err
		}
		af.Blobs = blobs
		st.Attachments = len(blobs)
	}

	raw, err := json.Marshal(af)
	if err != nil {
		return nil, st, err
	}
	// Gzip before sealing: a chat archive is overwhelmingly text and compresses
	// hard, and compressing after encryption would achieve nothing.
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		return nil, st, err
	}
	if err := zw.Close(); err != nil {
		return nil, st, err
	}
	sealed, err := identity.SealWithPassphrase(passphrase, buf.Bytes())
	if err != nil {
		return nil, st, err
	}
	return sealed, st, nil
}

// ImportArchive merges a sealed archive into this device. It is ADDITIVE and
// never destructive: nothing already present is overwritten or removed, so
// running it twice is harmless and restoring a stale archive onto a live
// account cannot eat anything said since it was taken. There is no server copy
// to fall back on if it could.
func (s *Service) ImportArchive(sealed []byte, passphrase string) (ArchiveStats, error) {
	var st ArchiveStats
	plain, err := identity.OpenWithPassphrase(passphrase, sealed)
	if err != nil {
		return st, err // ErrWrongPassphrase reaches the UI intact
	}
	zr, err := gzip.NewReader(bytes.NewReader(plain))
	if err != nil {
		return st, fmt.Errorf("app: archive is not readable: %w", err)
	}
	defer zr.Close()
	raw, err := io.ReadAll(zr)
	if err != nil {
		return st, fmt.Errorf("app: archive is truncated: %w", err)
	}
	var af archiveFile
	if err := json.Unmarshal(raw, &af); err != nil {
		return st, fmt.Errorf("app: archive is not readable: %w", err)
	}
	if af.Version > archiveVersion {
		return st, fmt.Errorf("app: this archive was written by a newer version (%d); upgrade first", af.Version)
	}

	// Only channels this device actually has are restored into. An archive can
	// name a guild you have since left or never rejoined, and inventing local
	// rows for it would produce history hanging off nothing.
	known := map[string]bool{}
	s.mu.RLock()
	for _, g := range s.guilds {
		for _, c := range g.Channels {
			known[c.ID] = true
		}
	}
	s.mu.RUnlock()

	for _, m := range af.Messages {
		if !known[m.ChannelID] {
			st.Skipped++
			continue
		}
		// An author signature that survived the export round trip is kept, and one
		// that does not check out is dropped rather than the row: this is the
		// operator restoring their own file, so the import is not the place to
		// argue about it. What matters is the invariant every other path relies
		// on — a stored signature is one that verified — because these rows are
		// re-served to peers by history sync, and a signature that fails there is
		// a refusal, not a shrug.
		if len(m.Sig) > 0 && !verifyMessageSig(m) {
			m.Sig = nil
		}
		// SaveMessage is ON CONFLICT(id) DO NOTHING, which is exactly the
		// additive semantics wanted: an id already here wins, untouched.
		added, err := s.store.SaveMessage(m)
		if err != nil {
			return st, err
		}
		if !added {
			st.Skipped++
			continue
		}
		st.Messages++
		if m.Pinned {
			_ = s.store.SetPinned(m.ID, true)
		}
		for emoji, fprs := range m.Reactions {
			for _, f := range fprs {
				_ = s.store.AddReaction(m.ID, f, emoji)
			}
		}
	}

	for _, id := range af.Saved {
		if err := s.store.SaveBookmark(id, "", time.Now().UnixNano()); err == nil {
			st.Saved++
		}
	}
	for id, ct := range af.Blobs {
		if _, ok, _ := s.store.GetAttachment(id); ok {
			continue
		}
		if err := s.store.SaveAttachment(id, ct); err == nil {
			st.Attachments++
		}
	}

	s.emitGuildUpdate()
	return st, nil
}
