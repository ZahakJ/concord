package bridge

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestMediaSupportDefaultsToInline(t *testing.T) {
	saved := MediaMissing
	t.Cleanup(func() { MediaMissing = saved })
	MediaMissing = nil
	var b Bridge
	if got := b.MediaSupport(); !got.Inline || got.Missing != nil {
		t.Fatalf("no shell check: got %+v, want inline", got)
	}
}

func TestMediaSupportReportsMissingSink(t *testing.T) {
	saved := MediaMissing
	t.Cleanup(func() { MediaMissing = saved })
	MediaMissing = func() []string { return []string{"autoaudiosink"} }
	var b Bridge
	got, err := b.Dispatch("MediaSupport", nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(got)
	var v MediaSupportView
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if v.Inline || !reflect.DeepEqual(v.Missing, []string{"autoaudiosink"}) {
		t.Fatalf("sink missing: got %s", raw)
	}
	MediaMissing = func() []string { return nil }
	if got := b.MediaSupport(); !got.Inline {
		t.Fatalf("sink present: got %+v, want inline", got)
	}
}
