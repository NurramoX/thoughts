package store

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/NurramoX/thoughts/internal/api"
	"github.com/NurramoX/thoughts/internal/filter"
)

// compiled is a Filter as SQL over thought i. Every value is a bound parameter.
type compiled struct {
	// match is the FTS5 query of the ranked text terms, "" when there are
	// none. It must be AND-ed at the top of a query that joins thought_fts.
	match string
	where string
	args  []any
}

// compile turns e into SQL. The ranked text terms (filter.Ranked) are left
// to the caller's MATCH on the joined thought_fts; every other text term, under
// an Or or a Not, becomes an IN over its own MATCH subquery.
func compile(e filter.Expr) (compiled, error) {
	c := compiler{ranked: filter.Ranked(e)}
	where, err := c.expr(e, true)
	if err != nil {
		return compiled{}, err
	}
	phrases := make([]string, len(c.ranked))
	for i, t := range c.ranked {
		phrases[i] = ftsPhrase(t)
	}
	return compiled{match: strings.Join(phrases, " AND "), where: where, args: c.args}, nil
}

type compiler struct {
	ranked []filter.Text
	args   []any
}

func (c *compiler) bind(sql string, args ...any) string {
	c.args = append(c.args, args...)
	return sql
}

// expr compiles e. top holds while e is reached from the root through And
// nodes only: there a ranked text term is covered by the caller's MATCH.
func (c *compiler) expr(e filter.Expr, top bool) (string, error) {
	switch e := e.(type) {
	case nil:
		return "1", nil
	case filter.And:
		return c.join(e.Terms, " AND ", top)
	case filter.Or:
		return c.join(e.Terms, " OR ", false)
	case filter.Not:
		if t, ok := e.X.(filter.Text); ok {
			return c.bind(`i.id NOT IN (SELECT rowid FROM thought_fts WHERE thought_fts MATCH ?)`, ftsPhrase(t)), nil
		}
		x, err := c.expr(e.X, false)
		return "NOT (" + x + ")", err
	case filter.Text:
		if top && slices.Contains(c.ranked, e) {
			return "1", nil
		}
		return c.bind(`i.id IN (SELECT rowid FROM thought_fts WHERE thought_fts MATCH ?)`, ftsPhrase(e)), nil
	case filter.Attr:
		return c.bind(`i.id IN (SELECT thought_id FROM thought_attribute WHERE key = ? AND value_folded = ?)`, e.Key, fold(e.Value)), nil
	case filter.Tag:
		return c.bind(`i.id IN (SELECT thought_id FROM thought_tag WHERE tag = ?)`, e.Tag), nil
	case filter.ID:
		return c.bind(`i.id = ?`, e.ID), nil
	case filter.Has:
		switch e.Key {
		case "tag":
			return `i.id IN (SELECT thought_id FROM thought_tag)`, nil
		case "created", "updated":
			return "1", nil
		}
		return c.bind(`i.id IN (SELECT thought_id FROM thought_attribute WHERE key = ?)`, e.Key), nil
	case filter.Date:
		return c.date(e)
	}
	return "", fmt.Errorf("filter node %T cannot be compiled", e)
}

func (c *compiler) join(terms []filter.Expr, op string, top bool) (string, error) {
	if len(terms) == 0 {
		return "1", nil
	}
	parts := make([]string, len(terms))
	for i, t := range terms {
		p, err := c.expr(t, top)
		if err != nil {
			return "", err
		}
		parts[i] = "(" + p + ")"
	}
	return strings.Join(parts, op), nil
}

// date tests a timestamp against [From, To). Stored timestamps are whole
// milliseconds, so both bounds round up to the millisecond.
func (c *compiler) date(d filter.Date) (string, error) {
	var col string
	switch d.Field {
	case filter.Created:
		col = "i.created_at"
	case filter.Updated:
		col = "i.updated_at"
	default:
		return "", fmt.Errorf("unknown date field %d", d.Field)
	}
	var conds []string
	if !d.From.IsZero() {
		conds = append(conds, c.bind(col+" >= ?", stampCeil(d.From)))
	}
	if !d.To.IsZero() {
		conds = append(conds, c.bind(col+" < ?", stampCeil(d.To)))
	}
	if len(conds) == 0 {
		return "1", nil
	}
	return strings.Join(conds, " AND "), nil
}

// stampCeil renders t as a stored timestamp, rounded up to the millisecond.
func stampCeil(t time.Time) string {
	t = t.UTC()
	if r := t.Truncate(time.Millisecond); !r.Equal(t) {
		t = r.Add(time.Millisecond)
	}
	return t.Format(api.TimeLayout)
}

// ftsPhrase renders a text term as one quoted FTS5 phrase, inner quotes
// doubled, scoped to its column when it has one.
func ftsPhrase(t filter.Text) string {
	phrase := `"` + strings.ReplaceAll(t.Phrase, `"`, `""`) + `"`
	switch t.Field {
	case filter.TextTitle:
		return "{title} : " + phrase
	case filter.TextBody:
		return "{body} : " + phrase
	}
	return phrase
}
