package app

import (
	"strings"
	"testing"
)

func TestCleanDisplayName(t *testing.T) {
	for _, bad := range []string{"", "   ", "​​​", "‮‬", "ㅤ", "⠀", "́"} {
		if _, err := cleanDisplayName(bad); err == nil {
			t.Errorf("invisible name %q accepted", bad)
		}
	}
	cases := map[string]string{
		"  Ada  ":    "Ada",
		"‮evil.exe‬": "evil.exe",
		"می‌خواهم":   "می‌خواهم", // the ZWNJ a Persian name needs stays
		"🦊":          "🦊",
	}
	for in, want := range cases {
		got, err := cleanDisplayName(in)
		if err != nil || got != want {
			t.Errorf("cleanDisplayName(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestTypeable(t *testing.T) {
	if Typeable("​​") || Typeable("") || Typeable("A​da") {
		t.Fatal("an untypeable name was called typeable")
	}
	if !Typeable("Ada Tester") || !Typeable("أمين") || !Typeable("José") {
		t.Fatal("an ordinary name was called untypeable")
	}
}

// The bio limit is refused, not cut: 600 Arabic characters are 1200 bytes.
func TestSetProfileRefusesLongBioAndInvisibleName(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a service")
	}
	svc := startServiceInDir(t, t.Context(), t.TempDir())
	if err := svc.SetProfile(Profile{Name: "Ada", Bio: strings.Repeat("ع", 600)}); err == nil {
		t.Fatal("an over-long bio was accepted")
	}
	if err := svc.SetProfile(Profile{Name: "Ada", Bio: strings.Repeat("ع", 300)}); err != nil {
		t.Fatalf("a bio at the limit was refused: %v", err)
	}
	if got := svc.SelfProfile().Bio; len([]rune(got)) != 300 {
		t.Fatalf("bio stored with %d characters, want 300", len([]rune(got)))
	}
	if err := svc.SetProfile(Profile{Name: "​​"}); err == nil {
		t.Fatal("an invisible name was accepted")
	}
	if svc.DisplayName() != "Ada" {
		t.Fatalf("refused edit changed the name: %q", svc.DisplayName())
	}
}
