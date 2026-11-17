package mediatok

import (
	"encoding/json"
	"os"
	"testing"
)

type fixture struct {
	Keys  string `json:"keys"`
	Cases []struct {
		Name  string `json:"name"`
		Body  string `json:"body"`
		Items []struct {
			Ord     int    `json:"ord"`
			Kind    Kind   `json:"kind"`
			BlobID  string `json:"blobId"`
			Subtype string `json:"subtype"`
			W       int    `json:"w"`
			H       int    `json:"h"`
			Flags   int    `json:"flags"`
			Name    string `json:"name"`
			Desc    string `json:"desc"`
			Size    int64  `json:"size"`
			Mime    string `json:"mime"`
		} `json:"items"`
	} `json:"cases"`
}

// The shared fixture: the node test reads the same file through the feed's
// own parser, so the gallery and the feed cannot disagree on what a body
// holds.
func TestParseMatchesSharedFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/tokens.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx fixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	if len(fx.Cases) < 10 {
		t.Fatalf("fixture has %d cases", len(fx.Cases))
	}
	for _, c := range fx.Cases {
		got := Parse(c.Body)
		if len(got) != len(c.Items) {
			t.Errorf("%s: %d items, want %d: %+v", c.Name, len(got), len(c.Items), got)
			continue
		}
		for i, w := range c.Items {
			g := got[i]
			if g.Ord != w.Ord || g.Kind != w.Kind || g.BlobID != w.BlobID || g.Subtype != w.Subtype || g.W != w.W || g.H != w.H ||
				g.Flags != w.Flags || g.Name != w.Name || g.Desc != w.Desc || g.Size != w.Size || g.Mime != w.Mime {
				t.Errorf("%s: item %d = %+v, want %+v", c.Name, i, g, w)
			}
			if g.Keys != fx.Keys {
				t.Errorf("%s: item %d keys %q", c.Name, i, g.Keys)
			}
		}
	}
}

func TestAtChecksTheKind(t *testing.T) {
	body := "[file](concord://file/v1/" + repeat("d", 64) + "/" + repeat("K", 75) + "/5/" + "dmlkZW8vbXA0" + "/)"
	if it, ok := At(body, 0, Video); !ok || it.Mime != "video/mp4" {
		t.Fatalf("At(video) = %+v, %v", it, ok)
	}
	if _, ok := At(body, 0, Photo); ok {
		t.Fatal("a video answered as a photo")
	}
	if _, ok := At(body, 1, Video); ok {
		t.Fatal("an ordinal past the end answered")
	}
}

func repeat(s string, n int) string {
	out := ""
	for range n {
		out += s
	}
	return out
}
