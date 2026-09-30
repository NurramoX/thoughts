package filter

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// dateForms is the hint shown with a value that is not a date.
const dateForms = "use 2026, 2026-09, 2026-09-01, today, a quoted RFC 3339 instant, or a range such as 2026-01..2026-06 or 90d.."

// date makes the Date term for key:v, where key names field. Each end of v
// resolves to a half-open interval: a period to its extent, and an instant
// (RFC 3339 or relative) to the millisecond it names.
func (p *parser) date(key string, field DateField, v token) (Expr, error) {
	i := strings.Index(v.s, "..")
	if i < 0 {
		if err := p.dateWordCase(v.s, v.start); err != nil {
			return nil, err
		}
		if _, ok := p.relative(v.s); ok {
			return nil, p.errorf(v.start,
				"%s:%s needs a range: write %s:%s.. for since then, or %s:..%s for before then",
				key, v.s, key, v.s, key, v.s)
		}
		from, to, ok := p.end(v.s)
		if !ok {
			return nil, p.errorf(v.start, "%s:%s is not a date; %s", key, v.s, dateForms)
		}
		return Date{field, from, to}, nil
	}

	a, b := v.s[:i], v.s[i+len(".."):]
	if a == "" && b == "" {
		return nil, p.errorf(v.start, "%s:.. is not a date; a range needs at least one end", key)
	}
	d := Date{Field: field}
	if a != "" {
		if err := p.dateWordCase(a, v.pos[0]); err != nil {
			return nil, err
		}
		from, _, ok := p.end(a)
		if !ok {
			return nil, p.errorf(v.pos[0], "%q is not a date; %s", a, dateForms)
		}
		d.From = from
	}
	if b != "" {
		bPos := v.pos[utf8.RuneCountInString(v.s[:i+len("..")])]
		if err := p.dateWordCase(b, bPos); err != nil {
			return nil, err
		}
		_, to, ok := p.end(b)
		if !ok {
			return nil, p.errorf(bPos, "%q is not a date; %s", b, dateForms)
		}
		d.To = to
	}
	return d, nil
}

// dateWordCase returns an error at source index i when s is a date word,
// today or a relative instant, spelled other than lowercase.
func (p *parser) dateWordCase(s string, i int) error {
	lower := strings.ToLower(s)
	if lower == s {
		return nil
	}
	if _, ok := p.relative(lower); ok || lower == "today" {
		return p.errorf(i, "%s is not a date; date words are lowercase: write %s", s, lower)
	}
	return nil
}

// end resolves one end of a range, or a whole period, to [from, to).
func (p *parser) end(s string) (from, to time.Time, ok bool) {
	if t, ok := p.relative(s); ok {
		return t, t.Add(time.Millisecond), true
	}
	return p.period(s)
}

// period resolves a period to [from, to) in now's location. A full RFC 3339
// instant is the period of the millisecond it names.
func (p *parser) period(s string) (from, to time.Time, ok bool) {
	loc := p.now.Location()
	if s == "today" {
		y, m, d := p.now.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, loc), time.Date(y, m, d+1, 0, 0, 0, 0, loc), true
	}
	for _, f := range []struct {
		shape, layout string
		next          func(time.Time) time.Time
	}{
		{"dddd", "2006", func(t time.Time) time.Time { return t.AddDate(1, 0, 0) }},
		{"dddd-dd", "2006-01", func(t time.Time) time.Time { return t.AddDate(0, 1, 0) }},
		{"dddd-dd-dd", "2006-01-02", func(t time.Time) time.Time { return t.AddDate(0, 0, 1) }},
	} {
		if !hasShape(s, f.shape) {
			continue
		}
		t, err := time.ParseInLocation(f.layout, s, loc)
		if err != nil {
			return time.Time{}, time.Time{}, false
		}
		return t, f.next(t), true
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	t = t.Truncate(time.Millisecond)
	return t, t.Add(time.Millisecond), true
}

// hasShape reports whether s matches shape, where 'd' stands for an ASCII
// digit and any other byte for itself.
func hasShape(s, shape string) bool {
	if len(s) != len(shape) {
		return false
	}
	for i := range len(s) {
		if shape[i] == 'd' {
			if s[i] < '0' || s[i] > '9' {
				return false
			}
		} else if s[i] != shape[i] {
			return false
		}
	}
	return true
}

// relative resolves a relative instant (`90d`, `2w`, `6m`, `1y`, lowercase
// only): that long
// before now, counted in calendar units in now's location.
func (p *parser) relative(s string) (time.Time, bool) {
	if len(s) < 2 || len(s) > 6 { // at most five digits
		return time.Time{}, false
	}
	digits := s[:len(s)-1]
	if !hasShape(digits, strings.Repeat("d", len(digits))) {
		return time.Time{}, false
	}
	n, _ := strconv.Atoi(digits)
	switch s[len(s)-1] {
	case 'd':
		return p.now.AddDate(0, 0, -n), true
	case 'w':
		return p.now.AddDate(0, 0, -7*n), true
	case 'm':
		return p.now.AddDate(0, -n, 0), true
	case 'y':
		return p.now.AddDate(-n, 0, 0), true
	}
	return time.Time{}, false
}
