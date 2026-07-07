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
