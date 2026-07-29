package slug

import (
	"strings"
	"testing"
)

func TestMake(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"simple", "Acme", "acme"},
		{"spaces become hyphens", "Acme Corp", "acme-corp"},

		{"punctuation is collapsed", "Acme, Inc.", "acme-inc"},
		{"runs collapse to one hyphen", "a   ///   b", "a-b"},
		{"surrounding hyphens trimmed", "  --Acme--  ", "acme"},
		{"digits survive", "v2 API", "v2-api"},
		{"non-latin falls back", "日本語", Fallback},
		{"punctuation only falls back", "!!!", Fallback},
		{"empty falls back", "", Fallback},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Make(tc.in); got != tc.want {
				t.Errorf("Make(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestMake_OutputIsAlwaysURLAndHeaderSafe(t *testing.T) {
	inputs := []string{
		`../../etc/passwd`,
		`name" ; rm -rf /`,
		"line\nbreak",
		"tab\there",
		`a/b\c`,
		`%2e%2e%2f`,
	}

	for _, in := range inputs {
		got := Make(in)
		if strings.ContainsAny(got, `/\."'; `+"\n\r\t") {
			t.Errorf("Make(%q) = %q, which is not safe for a URL path or a header", in, got)
		}
	}
}

func TestMakeUnique(t *testing.T) {
	seen := make(map[string]struct{}, 200)
	for range 200 {
		got, err := MakeUnique("Acme Corp")
		if err != nil {
			t.Fatalf("MakeUnique: %v", err)
		}
		if !strings.HasPrefix(got, "acme-corp-") {
			t.Fatalf("MakeUnique = %q, want it to keep the readable prefix", got)
		}
		if _, dup := seen[got]; dup {
			t.Fatalf("MakeUnique produced a duplicate: %q", got)
		}
		seen[got] = struct{}{}
	}
}
