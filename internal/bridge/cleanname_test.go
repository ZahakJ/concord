package bridge

import "testing"

// The exact bytes an older build stored: "AB ورشة الترجمة والتدقيق اللغوي العربية"
// cut at 64 with a raw slice, which landed inside the ع.
func TestCleanNameDropsTheTailOfASplitRune(t *testing.T) {
	corrupt := "AB ورشة الترجمة والتدقيق اللغوي الع\xd8"
	got := cleanName(corrupt)
	want := "AB ورشة الترجمة والتدقيق اللغوي الع"
	if got != want {
		t.Fatalf("cleanName(%q) = %q, want %q", corrupt, got, want)
	}
}

func TestCleanNameLeavesGoodNamesExactlyAlone(t *testing.T) {
	// Including a replacement character somebody typed on purpose: the string
	// is valid UTF-8, so nothing is touched.
	for _, s := range []string{
		"general", "ورشة الترجمة", "🎉 party", "", "what is � called?",
		"trailing space kept ",
	} {
		if got := cleanName(s); got != s {
			t.Errorf("cleanName(%q) = %q, want it untouched", s, got)
		}
	}
}

func TestCleanNameTrimsTheSpaceTheFragmentLeftBehind(t *testing.T) {
	if got := cleanName("Reading \xd8"); got != "Reading" {
		t.Fatalf("cleanName = %q, want %q", got, "Reading")
	}
}
