package app

import (
	"fmt"
	"strings"

	"github.com/ZahakJ/concord/internal/mediatok"
	"github.com/ZahakJ/concord/internal/store"
)

// The gallery: every photo, video and file a conversation holds on this
// device, a page at a time (store/media.go has the index and its rules).

// MediaPage returns one page of a conversation's gallery tab, newest first,
// after cursor ("" = the newest end), and the cursor of the next page ("" at
// the end).
func (s *Service) MediaPage(channelID string, tab mediatok.Tab, cursor string, limit int) ([]store.MediaItem, string, error) {
	if tab != mediatok.TabVisual && tab != mediatok.TabFiles {
		return nil, "", fmt.Errorf("app: no such gallery tab")
	}
	items, next, err := s.store.MediaPage(channelID, tab, store.ParseMediaCursor(cursor), limit)
	if err != nil {
		return nil, "", err
	}
	return items, next.String(), nil
}

// RecentMax caps the attach menu's "Recent" strip.
const RecentMax = 12

// recentScanMax bounds how many index rows one RecentMedia call reads while
// skipping items it may not offer, so a history full of left guilds cannot
// turn opening the attach menu into a scan of the whole index.
const recentScanMax = 480

// RecentMedia is the attach menu's "Recent" strip: at most limit (≤
// RecentMax) photos, videos and files this account sent or received in any
// conversation it still belongs to, newest first. On top of the gallery's own
// rules (store.RecentMedia: live, not hidden, not expired, signed) it leaves
// out conversations this device no longer tracks (a guild left, a channel
// deleted), guilds it was removed or banned from, 1:1 DMs it hid, blocked
// senders, spoilered items and voice clips.
//
// Nothing here gives the page a new capability: each item carries the blob
// id and key its message already held, which the page could read from the
// feed. A re-send goes through SendAttachment/SendFile like a picked file,
// which seal the bytes as a new blob under a new key (sealBlob).
func (s *Service) RecentMedia(limit int) ([]store.MediaItem, error) {
	if limit <= 0 || limit > RecentMax {
		limit = RecentMax
	}
	var out []store.MediaItem
	cur := store.MediaCursor{}
	for scanned := 0; scanned < recentScanMax; {
		page, next, err := s.store.RecentMedia(cur, 4*RecentMax)
		if err != nil {
			return nil, err
		}
		scanned += 4 * RecentMax
		for _, m := range page {
			if s.recentOffers(m) {
				out = append(out, m)
				if len(out) == limit {
					return out, nil
				}
			}
		}
		if next.MessageID == "" {
			break
		}
		cur = next
	}
	return out, nil
}

// recentOffers reports whether the Recent strip may offer a gallery item.
func (s *Service) recentOffers(m store.MediaItem) bool {
	if m.Item.Flags&mediatok.FlagSpoiler != 0 || strings.HasPrefix(m.Item.Mime, "audio/") {
		return false
	}
	if s.SenderBlocked(m.Sender) {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	gid, ok := s.channelToGuild[m.ChannelID]
	return ok && s.evicted[gid] == "" && !s.hiddenDMs[gid]
}

// MediaLocal reports whether a blob's ciphertext is already on this device.
// The Recent strip draws a thumbnail only then: opening the attach menu must
// not ask other conversations' members for anything.
func (s *Service) MediaLocal(blobID string) bool {
	return blobIDRe.MatchString(blobID) && s.store.HasAttachment(blobID)
}

// Media blob states, as EnsureMedia reports them.
const (
	MediaLocal   = "local"   // the ciphertext is on this device
	MediaFetched = "fetched" // it was not, and a member just served it
	MediaMetered = "metered" // it is not, and the connection is billed: ask first
)

// EnsureMedia makes sure a gallery item's blob is on this device, fetching
// it from the conversation's members when it is not. Nothing is decrypted:
// it is the step before a picture can be opened. On a metered connection it
// fetches only when allowMetered says the person asked for this one (a tap);
// tiles that merely scroll into view do not spend the data plan.
func (s *Service) EnsureMedia(channelID, blobID string, allowMetered bool) (string, error) {
	if !blobIDRe.MatchString(blobID) {
		return "", fmt.Errorf("app: bad attachment id")
	}
	if s.store.HasAttachment(blobID) {
		return MediaLocal, nil
	}
	if !allowMetered && s.host.Metered() {
		return MediaMetered, nil
	}
	if _, err, _ := s.attachFlight.Do(blobID, func() (any, error) {
		return s.attachmentCiphertext(channelID, blobID)
	}); err != nil {
		return "", err // "not found" when nobody reachable holds it
	}
	return MediaFetched, nil
}
