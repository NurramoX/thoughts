package filter

import "slices"

// Ranked returns the text terms that are top-level AND terms of e: the ones
// that are ranked and snippeted. It returns nil when there are none. A bare
// Text as the whole filter counts, nested Ands are looked through, and
// nothing under an Or or a Not counts.
func Ranked(e Expr) []Text {
	switch n := e.(type) {
	case Text:
		return []Text{n}
	case And:
		var ts []Text
		for _, t := range n.Terms {
			ts = append(ts, Ranked(t)...)
		}
		return ts
	}
	return nil
}

// Vocabulary returns the tags and attribute keys that e mentions, each
// sorted and deduplicated, for the vocabulary hint. Keys include those of
// Has terms except "tag", "created" and "updated"; "status" is never
// returned.
func Vocabulary(e Expr) (tags, keys []string) {
	var walk func(Expr)
	walk = func(e Expr) {
		switch n := e.(type) {
		case And:
			for _, t := range n.Terms {
				walk(t)
			}
		case Or:
			for _, t := range n.Terms {
				walk(t)
			}
		case Not:
			walk(n.X)
		case Tag:
			tags = append(tags, n.Tag)
		case Attr:
			keys = append(keys, n.Key)
		case Has:
			keys = append(keys, n.Key)
		}
	}
	walk(e)
	keys = slices.DeleteFunc(keys, func(k string) bool {
		switch k {
		case "status", "tag", "created", "updated":
			return true
		}
		return false
	})
	return sortedSet(tags), sortedSet(keys)
}

// sortedSet sorts and deduplicates s, returning nil when it is empty.
func sortedSet(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	slices.Sort(s)
	return slices.Compact(s)
}
