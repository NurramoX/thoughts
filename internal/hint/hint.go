// Package hint is the vocabulary hint (spec §7): when a Filter selects
// nothing, name the tags and keys it mentions that no thought carries, so a typo
// is visible. Shared by `thought ls` and the Review TUI.
package hint

import (
	"context"
	"fmt"
	"time"

	"github.com/NurramoX/thoughts/internal/client"
	"github.com/NurramoX/thoughts/internal/filter"
)

// Vocabulary parses src locally (for the hint only; the server stays the
// authority) and checks its tags and keys against /tags and /attributes. It
// returns one line per unknown name, e.g. "no thoughts have the key 'tga'", or
// nil when every name exists or src does not parse. Unknown tags come first,
// then unknown keys, each in sorted order.
func Vocabulary(ctx context.Context, c client.Client, src string) ([]string, error) {
	e, err := filter.Parse(src, time.Now())
	if err != nil {
		return nil, nil
	}
	tags, keys := filter.Vocabulary(e)
	var lines []string
	if len(tags) > 0 {
		known, err := c.Tags(ctx)
		if err != nil {
			return nil, err
		}
		have := map[string]bool{}
		for _, t := range known {
			have[t.Tag] = true
		}
		lines = append(lines, missing("tag", tags, have)...)
	}
	if len(keys) > 0 {
		known, err := c.Attributes(ctx)
		if err != nil {
			return nil, err
		}
		have := map[string]bool{}
		for _, k := range known {
			have[k.Key] = true
		}
		lines = append(lines, missing("key", keys, have)...)
	}
	return lines, nil
}

func missing(kind string, names []string, have map[string]bool) []string {
	var lines []string
	for _, n := range names {
		if !have[n] {
			lines = append(lines, fmt.Sprintf("no thoughts have the %s '%s'", kind, n))
		}
	}
	return lines
}
