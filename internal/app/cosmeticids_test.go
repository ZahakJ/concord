package app

import (
	"context"
	"testing"

	"github.com/ZahakJ/concord/internal/store"
)

// Ids that name a member of every JavaScript object are refused wherever a
// cosmetic or preset id is admitted. The clients only ever look such ids up
// in a table, and no table entry has one of these names.
var reservedProbes = []string{
	"constructor", "__proto__", "tostring", "valueof", "hasownproperty",
	"isprototypeof", "propertyisenumerable", "tolocalestring",
	"__definegetter__", "__lookupgetter__",
}

func TestCosmeticIDValidatorsRefuseReservedNames(t *testing.T) {
	for _, id := range reservedProbes {
		if validFrame(id) {
			t.Errorf("validFrame(%q) = true", id)
		}
		if validEffect(id) {
			t.Errorf("validEffect(%q) = true", id)
		}
		if validID(id) {
			t.Errorf("validID(%q) = true", id)
		}
		if validBanner("preset:" + id) {
			t.Errorf("validBanner(preset:%q) = true", id)
		}
		if validPresetID(id) {
			t.Errorf("validPresetID(%q) = true", id)
		}
		if validStoryScene(presetPrefix + id) {
			t.Errorf("validStoryScene(preset:%q) = true", id)
		}
	}
	// Ordinary ids are untouched.
	for _, id := range []string{"spin", "orbit-cow", "galaxy", "gold", "tiara", "constructors", "my-constructor"} {
		if !validFrame(id) || !validID(id) || !validBanner("preset:"+id) || !validPresetID(id) {
			t.Errorf("an ordinary id %q was refused", id)
		}
	}
}

// A refused cosmetic id drops that FIELD, never the profile around it.
func TestProfileDropsOnlyTheRefusedField(t *testing.T) {
	p := Profile{
		Name:   "Ann",
		Color:  "#123456",
		Frame:  "constructor",
		Effect: "valueof",
		Style:  &Style{Dec: "constructor", Pal: "gold", Dc: "__proto__", Cf: "tostring", Speed: "fast"},
	}
	sanitizeProfileExtras(&p)
	if p.Frame != "" || p.Effect != "" {
		t.Fatalf("frame/effect kept: %q %q", p.Frame, p.Effect)
	}
	if p.Name != "Ann" || p.Color != "#123456" {
		t.Fatalf("the rest of the profile went with it: %+v", p)
	}
	if p.Style == nil || p.Style.Dec != "" || p.Style.Dc != "" || p.Style.Cf != "" || p.Style.Pal != "gold" || p.Style.Speed != "fast" {
		t.Fatalf("style not sanitised field by field: %+v", p.Style)
	}
}

func TestCustomEmojiNamesRefuseReservedNames(t *testing.T) {
	if !validEmojiName("party_parrot") {
		t.Error("an ordinary emoji name was refused")
	}
	for _, id := range []string{"constructor", "__proto__", "valueof"} {
		if validEmojiName(id) {
			t.Errorf("custom emoji name %q would be admitted", id)
		}
	}
}

// A profile learned from a peer, and one already in the store from an older
// build, are both held to the rule: learnProfile drops the field before it is
// kept, and Start drops it from a stored copy on load.
func TestStoredAndLearnedProfilesDropReservedIDs(t *testing.T) {
	dir := t.TempDir()
	start := func() *Service {
		svc, err := Start(context.Background(), Config{DataDir: dir, Passphrase: "test-pass", DisableMDNS: true})
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
		return svc
	}

	svc := start()
	const learned = "fpr-learned"
	svc.learnProfile(learned, Profile{Name: "Lea", Frame: "constructor", Banner: "preset:constructor", Style: &Style{Dec: "constructor", Pal: "gold"}})
	svc.mu.RLock()
	got := svc.profiles[learned]
	svc.mu.RUnlock()
	if got.Name != "Lea" || got.Frame != "" || got.Banner != "" || got.Style == nil || got.Style.Dec != "" || got.Style.Pal != "gold" {
		t.Fatalf("learned profile: %+v style %+v", got, got.Style)
	}

	// What an older build could have stored.
	const stored = "fpr-stored"
	if err := svc.store.SaveProfile(store.ProfileRow{
		Fingerprint: stored, Name: "Sam", Color: "#abcdef",
		Banner: "preset:constructor", Frame: "constructor", Effect: "constructor",
		Style: `{"dec":"constructor","pal":"gold","dc":"constructor"}`,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}

	svc = start()
	defer svc.Close()
	svc.mu.RLock()
	got = svc.profiles[stored]
	svc.mu.RUnlock()
	if got.Name != "Sam" || got.Color != "#abcdef" {
		t.Fatalf("the stored profile was dropped, not cleaned: %+v", got)
	}
	if got.Frame != "" || got.Effect != "" || got.Banner != "" {
		t.Fatalf("stored reserved ids survived a restart: frame %q effect %q banner %q", got.Frame, got.Effect, got.Banner)
	}
	if got.Style == nil || got.Style.Dec != "" || got.Style.Dc != "" || got.Style.Pal != "gold" {
		t.Fatalf("stored style: %+v", got.Style)
	}
}
