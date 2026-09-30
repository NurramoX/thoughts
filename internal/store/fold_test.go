package store

import (
	"testing"
	"unicode"
)

// Every rune of a simple-folding orbit must fold to the same rune, or equal
// values would compare unequal in filters.
func TestFoldIsConstantOnEveryOrbit(t *testing.T) {
	for r := rune(0); r <= unicode.MaxRune; r++ {
		want := foldRune(r)
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			if got := foldRune(f); got != want {
				t.Fatalf("fold(%U) = %U but fold(%U) = %U", r, want, f, got)
			}
		}
	}
}

func TestFold(t *testing.T) {
	for in, want := range map[string]string{
		"Small":    "small",
		"STRASSE":  "strasse",
		"Straße":   "straße", // simple folding never expands ß
		"ẞ":        "ß",
		"ΣΊΣΥΦΟΣ":  "σίσυφοσ", // Σ, σ and final ς share one fold
		"σίσυφος":  "σίσυφοσ",
		"İstanbul": "İstanbul", // İ has no simple fold
		"ıi":       "ıi",
		"\u212a":   "k", // Kelvin sign
	} {
		if got := fold(in); got != want {
			t.Errorf("fold(%q) = %q, want %q", in, got, want)
		}
	}
}
