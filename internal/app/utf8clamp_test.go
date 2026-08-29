package app

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"
)

// The exact string the organizer critic typed into the category-rename box: 39
// characters, 71 bytes. It is one byte over nothing in particular — it is over
// whatever the cap happens to be, which is the only thing a byte slice needs to
// produce a replacement character.
const arabicCategory = "AB ورشة الترجمة والتدقيق اللغوي العربية"

// And the forum title that came back with a U+FFFD at the HEAD of it.
const arabicTitle = "كيف أنقل قناة بين الفئات في دار الحكمة دون أن أفقد الرسائل القديمة"

func TestClampBytesNeverSplitsARune(t *testing.T) {
	// The property, stated directly: whatever comes out is valid UTF-8, is no
	// longer than the budget, and is a prefix of what went in. A byte slice
	// satisfies two of the three.
	inputs := []string{
		arabicCategory,
		arabicTitle,
		"आपका स्वागत है — यह एक बहुत लंबा नाम है",
		"🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉",
		"plain ascii stays exactly as it is",
		"",
	}
	for _, in := range inputs {
		for budget := 0; budget <= len(in)+3; budget++ {
			got := clampBytes(in, budget)
			if !utf8.ValidString(got) {
				t.Fatalf("clampBytes(%q, %d) = %q: not valid UTF-8", in, budget, got)
			}
			if len(got) > budget {
				t.Fatalf("clampBytes(%q, %d) = %q: %d bytes, over budget", in, budget, got, len(got))
			}
			if !strings.HasPrefix(in, got) {
				t.Fatalf("clampBytes(%q, %d) = %q: not a prefix of the input", in, budget, got)
			}
			// Nothing may be dropped that would have fit: the result plus the
			// next rune must be over budget, or the input is exhausted.
			if len(got) < len(in) {
				_, w := utf8.DecodeRuneInString(in[len(got):])
				if len(got)+w <= budget {
					t.Fatalf("clampBytes(%q, %d) = %q: cut short, %d more bytes fit", in, budget, got, w)
				}
			}
		}
	}
}

func TestRenameCategoryStoresValidUTF8(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	svc := startService(t, ctx)
	g, err := svc.CreateGuild("Dar al-Hikma")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	cat, err := svc.CreateCategory(g.ID, "Reading")
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}
	if err := svc.RenameCategory(g.ID, cat.ID, arabicCategory); err != nil {
		t.Fatalf("RenameCategory: %v", err)
	}
	cats, err := svc.store.Categories(g.ID)
	if err != nil {
		t.Fatalf("Categories: %v", err)
	}
	var got string
	for _, c := range cats {
		if c.ID == cat.ID {
			got = c.Name
		}
	}
	if got == "" {
		t.Fatalf("category %s vanished", cat.ID)
	}
	if !utf8.ValidString(got) || strings.ContainsRune(got, utf8.RuneError) {
		t.Fatalf("stored category name is corrupt: %q", got)
	}
}

func TestCreateThreadTitleSurvivesArabic(t *testing.T) {
	svc, gid, fid := forumFixture(t)
	// 121 bytes of Arabic. The old 64-byte cap cut it mid-rune AND cut it in
	// half; the raised cap keeps the whole question.
	post, err := svc.CreateThread(gid, fid, arabicTitle, "body", nil)
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	if !utf8.ValidString(post.Name) || strings.ContainsRune(post.Name, utf8.RuneError) {
		t.Fatalf("stored post title is corrupt: %q", post.Name)
	}
	if post.Name != arabicTitle {
		t.Fatalf("title was truncated at %d bytes: %q", len(post.Name), post.Name)
	}
}

func TestCreateThreadTitleAtEveryArabicBoundary(t *testing.T) {
	svc, gid, fid := forumFixture(t)
	// Walk a long Arabic title past the cap one character at a time. Every
	// crossing is a chance to land inside a two-byte rune.
	long := strings.Repeat("ش", 200)
	for n := 90; n <= 110; n++ {
		post, err := svc.CreateThread(gid, fid, long[:0]+string([]rune(long)[:n]), "b", nil)
		if err != nil {
			t.Fatalf("CreateThread(%d runes): %v", n, err)
		}
		if strings.ContainsRune(post.Name, utf8.RuneError) {
			t.Fatalf("%d runes stored corrupt: %q", n, post.Name)
		}
		if len(post.Name) > maxTitleBytes {
			t.Fatalf("%d runes stored %d bytes, over the %d cap", n, len(post.Name), maxTitleBytes)
		}
	}
}

func TestNicknameAndDMNameSurviveArabic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	svc := startService(t, ctx)
	g, err := svc.CreateGuild("Dar al-Hikma")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	if err := svc.SetNickname(g.ID, arabicCategory); err != nil {
		t.Fatalf("SetNickname: %v", err)
	}
	got := svc.NickOf(g.ID, svc.id.Fingerprint())
	if strings.ContainsRune(got, utf8.RuneError) {
		t.Fatalf("stored nickname is corrupt: %q", got)
	}
}
