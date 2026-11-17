package bridge

import (
	"testing"

	"github.com/ZahakJ/concord/internal/mediatok"
	"github.com/ZahakJ/concord/internal/store"
)

// A blocked member's pictures leave the gallery as their messages leave the
// feed, and everyone else's keep their order and their fields.
func TestMediaViewsLeaveOutBlockedSenders(t *testing.T) {
	items := []store.MediaItem{
		{MessageID: "1", Sender: []byte("alice"), Sent: 3, Item: mediatok.Item{Kind: mediatok.Photo, BlobID: "b1", W: 4, H: 3, Flags: mediatok.FlagSpoiler}},
		{MessageID: "2", Sender: []byte("mallory"), Sent: 2, Item: mediatok.Item{Kind: mediatok.Photo, BlobID: "b2"}},
		{MessageID: "3", Sender: []byte("bob"), Sent: 1, Item: mediatok.Item{Ord: 2, Kind: mediatok.Video, BlobID: "b3", Mime: "video/mp4", Size: 9}},
	}
	got := mediaViews(blockList{"mallory": true}, items)
	if len(got) != 2 || got[0].MessageID != "1" || got[1].MessageID != "3" {
		t.Fatalf("views %+v", got)
	}
	if !got[0].Spoiler || got[0].W != 4 || got[0].Kind != 1 {
		t.Fatalf("photo view %+v", got[0])
	}
	if got[1].Ord != 2 || got[1].Kind != 2 || got[1].Mime != "video/mp4" || got[1].Size != 9 || got[1].Spoiler {
		t.Fatalf("video view %+v", got[1])
	}
	if all := mediaViews(blockList{}, items); len(all) != 3 {
		t.Fatalf("unblocked: %d views", len(all))
	}
}
