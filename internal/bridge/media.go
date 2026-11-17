package bridge

import (
	"time"

	"github.com/ZahakJ/concord/internal/identity"
	"github.com/ZahakJ/concord/internal/mediatok"
	"github.com/ZahakJ/concord/internal/store"
)

// MediaItemView is one gallery entry as the UI draws it: the message it
// belongs to, and the attachment read out of that message's body (the same
// fields lib/attachments.js parses from a token, so a tile can be opened
// through the feed's own fetch paths).
type MediaItemView struct {
	MessageID  string `json:"messageId"`
	ChannelID  string `json:"channelId"`
	Sender     string `json:"sender"`
	SenderName string `json:"senderName"`
	Sent       string `json:"sent"`
	Expires    int64  `json:"expires,omitempty"` // unix seconds
	Ord        int    `json:"ord"`
	Kind       int    `json:"kind"` // 1 photo, 2 video, 3 file
	BlobID     string `json:"blobId"`
	Keys       string `json:"keys"`
	Subtype    string `json:"subtype,omitempty"`
	W          int    `json:"w,omitempty"`
	H          int    `json:"h,omitempty"`
	Spoiler    bool   `json:"spoiler,omitempty"`
	Name       string `json:"name,omitempty"`
	Desc       string `json:"desc,omitempty"`
	Size       int64  `json:"size,omitempty"`
	Mime       string `json:"mime,omitempty"`
}

// MediaPageView is one page and where the next starts ("" at the end).
type MediaPageView struct {
	Items []MediaItemView `json:"items"`
	Next  string          `json:"next"`
}

// MediaPage is one page of a conversation's gallery: tab 1 photos and
// videos, tab 2 files. Blocked senders' items are left out, as their
// messages are in the feed.
func (b *Bridge) MediaPage(channelID string, tab int, cursor string, limit int) (MediaPageView, error) {
	svc, err := b.service()
	if err != nil {
		return MediaPageView{}, err
	}
	items, next, err := svc.MediaPage(channelID, mediatok.Tab(tab), cursor, limit)
	if err != nil {
		return MediaPageView{}, err
	}
	return MediaPageView{Items: mediaViews(svc, items), Next: next}, nil
}

// mediaViews turns a page into what the UI draws, leaving out blocked
// senders' items as the feed leaves out their messages.
func mediaViews(blk senderBlocker, items []store.MediaItem) []MediaItemView {
	out := make([]MediaItemView, 0, len(items))
	for _, m := range items {
		if blk.SenderBlocked(m.Sender) {
			continue
		}
		it := m.Item
		out = append(out, MediaItemView{
			MessageID:  m.MessageID,
			ChannelID:  m.ChannelID,
			Sender:     identity.FingerprintOf(m.Sender),
			SenderName: m.SenderName,
			Sent:       time.Unix(0, m.Sent).UTC().Format("2006-01-02T15:04:05.000000000Z07:00"),
			Expires:    m.Expires,
			Ord:        it.Ord,
			Kind:       int(it.Kind),
			BlobID:     it.BlobID,
			Keys:       it.Keys,
			Subtype:    it.Subtype,
			W:          it.W,
			H:          it.H,
			Spoiler:    it.Flags&mediatok.FlagSpoiler != 0,
			Name:       it.Name,
			Desc:       it.Desc,
			Size:       it.Size,
			Mime:       it.Mime,
		})
	}
	return out
}

// EnsureMedia makes sure a gallery item's blob is on this device: "local",
// "fetched", or "metered" (not fetched: the connection is billed and the
// person has not asked for this one).
func (b *Bridge) EnsureMedia(channelID, blobID string, allowMetered bool) (string, error) {
	svc, err := b.service()
	if err != nil {
		return "", err
	}
	return svc.EnsureMedia(channelID, blobID, allowMetered)
}
