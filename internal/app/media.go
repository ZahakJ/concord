package app

import (
	"fmt"

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
