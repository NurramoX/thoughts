package store

import (
	"strings"
	"unicode"

	"modernc.org/sqlite"
)

// foldCollation orders text by its fold, so sorting ignores case beyond
// ASCII, which SQLite's NOCASE does not.
const foldCollation = "fold"

func init() {
	sqlite.MustRegisterCollationUtf8(foldCollation, func(a, b string) int {
		return strings.Compare(fold(a), fold(b))
	})
}

// fold maps s under Unicode simple case folding (one rune to one rune), so
// that two strings are equal after fold exactly when strings.EqualFold says
// they are. Unlike full folding, ß stays ß (it equals only ẞ), and Turkish
// İ and ı equal nothing but themselves.
func fold(s string) string { return strings.Map(foldRune, s) }

// foldRune maps r to one fixed member of its simple-folding orbit (the runes
// unicode.SimpleFold cycles through): its lowercase form, which is what
// CaseFolding.txt maps to for nearly every rune, or r itself when the
// lowercase form lies outside the orbit (İ lowercases to i, which does not
// simply fold to İ).
func foldRune(r rune) rune {
	lower := unicode.ToLower(unicode.ToUpper(r))
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		if f == lower {
			return lower
		}
	}
	return r
}
