package main

import "testing"

func TestSemverLess(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v1.2.3", "v1.2.4", true},
		{"v1.2.4", "v1.2.3", false},
		{"v1.2.3", "v1.2.3", false},        // equal
		{"v1.10.0", "v1.9.0", false},       // numeric, not lexical: 10 > 9
		{"v1.9.0", "v1.10.0", true},        // 9 < 10
		{"v2.0.0", "v1.9.9", false},        // major dominates
		{"v1.9.9", "v2.0.0", true},         // major dominates
		{"1.2.3", "1.2.4", true},           // v-prefix optional
		{"dev", "v1.0.0", false},           // dev never triggers
		{"v1.0.0", "dev", false},           // unparseable target
		{"v1.2.3-rc1", "v1.2.4", false},    // pre-release unsupported -> no update
		{"garbage", "also-garbage", false}, // both unparseable
	}
	for _, c := range cases {
		if got := semverLess(c.a, c.b); got != c.want {
			t.Errorf("semverLess(%q,%q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestIsSemver(t *testing.T) {
	for _, v := range []string{"v1.2.3", "1.0.0", "v0.4.9"} {
		if !isSemver(v) {
			t.Errorf("isSemver(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"dev", "", "v1.2", "v1.2.3.4", "v1.2.x", "v1.2.3-rc1"} {
		if isSemver(v) {
			t.Errorf("isSemver(%q) = true, want false", v)
		}
	}
}

func TestMatchAsset(t *testing.T) {
	// Versioned names (the release stamps the tag into the filename).
	assets := []assetRef{
		{"concord-desktop-linux-v0.6.0", "u/dlin"},
		{"concord-desktop-windows-v0.6.0.exe", "u/dwin"},
		{"concord-desktop-macos-v0.6.0.zip", "u/dmac"},
		{"concord-windows-v0.6.0.exe", "u/wwin"}, // zero-dep web build
		{"concord-linux-amd64-v0.6.0", "u/wlin"},
		{"SHA256SUMS", "u/sums"},
	}
	cases := []struct {
		goos, wantURL string
	}{
		{"windows", "u/dwin"}, // prefers desktop over web
		{"linux", "u/dlin"},
		{"darwin", "u/dmac"},
		{"plan9", ""}, // unsupported OS
	}
	for _, c := range cases {
		got, _ := matchAsset(c.goos, assets)
		if got != c.wantURL {
			t.Errorf("matchAsset(%q) = %q, want %q", c.goos, got, c.wantURL)
		}
	}
	// Falls back to the web build if no desktop asset exists for the OS.
	if got, _ := matchAsset("windows", []assetRef{{"concord-windows.exe", "u/w"}}); got != "u/w" {
		t.Errorf("fallback web match = %q, want u/w", got)
	}
	// No assets -> empty.
	if got, _ := matchAsset("linux", nil); got != "" {
		t.Errorf("empty assets = %q, want empty", got)
	}
}
