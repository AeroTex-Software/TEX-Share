package util

import (
	"strings"
	"testing"
)

func TestValidSlugAndNamespace(t *testing.T) {
	valid := []string{"a", "hello-world", "abc123", "a_b-c", "x9"}
	for _, s := range valid {
		if !ValidSlug(s) {
			t.Errorf("ValidSlug(%q) = false, want true", s)
		}
	}
	invalid := []string{"", "-leading", "trailing-", "Upper", "with space", "sl/ash", strings.Repeat("a", 65)}
	for _, s := range invalid {
		if ValidSlug(s) {
			t.Errorf("ValidSlug(%q) = true, want false", s)
		}
	}
	if !ValidNamespace("alice") || ValidNamespace("Alice") {
		t.Errorf("namespace validation incorrect")
	}
}

func TestValidUsername(t *testing.T) {
	for _, s := range []string{"abc", "alice", "a.b_c-d", "User123"} {
		if !ValidUsername(s) {
			t.Errorf("ValidUsername(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "ab", "-abc", "with space", "has/slash"} {
		if ValidUsername(s) {
			t.Errorf("ValidUsername(%q) = true, want false", s)
		}
	}
}

func TestValidHostname(t *testing.T) {
	for _, s := range []string{"localhost", "example.com", "sub.example.com", "a-b.co", "x1.y2.z3"} {
		if !ValidHostname(s) {
			t.Errorf("ValidHostname(%q) = false, want true", s)
		}
	}
	// Scheme, port and path are rejected.
	for _, s := range []string{"", "http://example.com", "example.com:8080", "example.com/path", "-bad.com", "bad-.com", strings.Repeat("a", 254)} {
		if ValidHostname(s) {
			t.Errorf("ValidHostname(%q) = true, want false", s)
		}
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Hello World":   "hello-world",
		"  Spaces  ":    "spaces",
		"Ünïcödé Tèxt":  "unicode-text",
		"a---b":         "a-b",
		"...":           "",
		"UPPER_case.md": "upper-case-md",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConstantEqual(t *testing.T) {
	if !ConstantEqual("token", "token") {
		t.Error("equal strings reported unequal")
	}
	if ConstantEqual("token", "tokeN") || ConstantEqual("a", "ab") {
		t.Error("unequal strings reported equal")
	}
}

func TestSanitizeFilename(t *testing.T) {
	if got := SanitizeFilename("../../etc/passwd"); strings.ContainsAny(got, "/\\") {
		t.Errorf("SanitizeFilename kept a path separator: %q", got)
	}
	if got := SanitizeFilename(""); got != "download" {
		t.Errorf("empty name = %q, want download", got)
	}
	if got := SanitizeFilename("a\x00b.txt"); strings.ContainsRune(got, 0) {
		t.Errorf("control character not stripped: %q", got)
	}
	if got := SanitizeFilename(strings.Repeat("x", 300) + ".txt"); len(got) > 180 || !strings.HasSuffix(got, ".txt") {
		t.Errorf("long name not truncated with extension: len=%d", len(got))
	}
}

func TestContentDisposition(t *testing.T) {
	cd := ContentDisposition("attachment", `weird"name.txt`)
	if !strings.HasPrefix(cd, "attachment;") {
		t.Errorf("missing kind: %q", cd)
	}
	if strings.Contains(cd, `"weird"name`) {
		t.Errorf("quote not escaped: %q", cd)
	}
	if !strings.Contains(cd, "filename*=UTF-8''") {
		t.Errorf("missing RFC5987 form: %q", cd)
	}
}

func TestHashTokenStable(t *testing.T) {
	if HashToken("abc") != HashToken("abc") {
		t.Error("HashToken is not deterministic")
	}
	if HashToken("abc") == HashToken("abd") {
		t.Error("HashToken collision on distinct input")
	}
	if len(HashToken("abc")) != 64 {
		t.Errorf("HashToken length = %d, want 64", len(HashToken("abc")))
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		0:                 "0 B",
		512:               "512 B",
		1024:              "1.0 KiB",
		1024 * 1024:       "1.0 MiB",
		1536 * 1024:       "1.5 MiB",
		3 * 1024 * 1024 * 1024: "3.0 GiB",
	}
	for in, want := range cases {
		if got := HumanBytes(in); got != want {
			t.Errorf("HumanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestRandomTokenUniqueness(t *testing.T) {
	a, b := RandomToken(32), RandomToken(32)
	if a == b {
		t.Fatal("RandomToken produced identical values")
	}
	if len(RandomHex(8)) != 16 {
		t.Fatalf("RandomHex(8) length = %d", len(RandomHex(8)))
	}
}
