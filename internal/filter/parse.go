package filter

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/NurramoX/thoughts/internal/api"
)

// Parse parses src. Periods are read in now's location, and relative
// instants count back from now. The empty (or all-whitespace) filter parses
// to nil. Every error is an *Error.
func Parse(src string, now time.Time) (Expr, error) {
	p := &parser{src: []rune(src), now: now}
	e, err := p.or()
	if err != nil {
		return nil, err
	}
	p.skipSpace()
	if !p.eof() { // or stops early only at a ')'
		return nil, p.errorf(p.i, "unmatched ')'")
	}
	return e, nil
}

type parser struct {
	src []rune
	i   int // index of the next rune
	now time.Time
}

// token is a WORD or STRING, unquoted and unescaped. pos holds the source
// index of each rune of s, so an error can point inside the token.
type token struct {
	s     string
	start int // source index of the token's first rune
	pos   []int
}

// or parses `and (OR and)*`. It returns nil when there is no term at all,
// which only the caller can judge.
func (p *parser) or() (Expr, error) {
	p.skipSpace()
	if p.atOr() {
		return nil, p.errorf(p.i, "'or' needs a term before it")
	}
	first, err := p.and()
	if err != nil || first == nil {
		return nil, err
	}
	terms := []Expr{first}
	for {
		p.skipSpace()
		if !p.atOr() {
			return newOr(terms), nil
		}
		orPos := p.i
		p.i += len("or")
		p.skipSpace()
		if p.atOr() {
			return nil, p.errorf(p.i, "'or' needs a term before it")
		}
		e, err := p.and()
		if err != nil {
			return nil, err
		}
		if e == nil {
			return nil, p.errorf(orPos, "'or' needs a term after it")
		}
		terms = append(terms, e)
	}
}

// and parses `term*`, stopping at the end, a ')' or an `or`.
func (p *parser) and() (Expr, error) {
	var terms []Expr
	for {
		p.skipSpace()
		if p.eof() || p.src[p.i] == ')' || p.atOr() {
			return newAnd(terms), nil
		}
		t, err := p.term()
		if err != nil {
			return nil, err
		}
		terms = append(terms, t)
	}
}

// newAnd returns the And of terms, lifting the terms of nested Ands, or the
// single term as is, or nil for none.
func newAnd(terms []Expr) Expr {
	if len(terms) <= 1 {
		return first(terms)
	}
	var flat []Expr
	for _, t := range terms {
		if a, ok := t.(And); ok {
			flat = append(flat, a.Terms...)
		} else {
			flat = append(flat, t)
		}
	}
	return And{flat}
}

// newOr is newAnd for Or.
func newOr(terms []Expr) Expr {
	if len(terms) <= 1 {
		return first(terms)
	}
	var flat []Expr
	for _, t := range terms {
		if o, ok := t.(Or); ok {
			flat = append(flat, o.Terms...)
		} else {
			flat = append(flat, t)
		}
	}
	return Or{flat}
}

func first(terms []Expr) Expr {
	if len(terms) == 0 {
		return nil
	}
	return terms[0]
}

func (p *parser) term() (Expr, error) {
	start := p.i
	switch p.src[p.i] {
	case '-':
		p.i++
		if p.eof() || unicode.IsSpace(p.src[p.i]) || p.src[p.i] == ')' || p.atOr() {
			return nil, p.errorf(start, "'-' needs a term right after it")
		}
		x, err := p.term()
		if err != nil {
			return nil, err
		}
		return Not{x}, nil
	case '(':
		p.i++
		e, err := p.or()
		if err != nil {
			return nil, err
		}
		p.skipSpace()
		if p.eof() {
			return nil, p.errorf(start, "unclosed '('")
		}
		if e == nil {
			return nil, p.errorf(p.i, "empty parentheses")
		}
		p.i++ // the ')'
		return e, nil
	case ':':
		return nil, p.errorf(start, "missing key before ':'")
	case ',':
		return nil, p.errorf(start, "unexpected ','; quote a text term that contains ','")
	case '"':
		t, err := p.string()
		if err != nil {
			return nil, err
		}
		if !p.eof() && p.src[p.i] == ':' {
			return nil, p.errorf(p.i, "unexpected ':'; a key can't be quoted")
		}
		return p.text(TextAny, t)
	}
	w := p.word()
	if !p.eof() && p.src[p.i] == ':' {
		return p.keyTerm(api.LowerLabel(w.s))
	}
	return p.text(TextAny, w)
}

// keyTerm parses `':' value (',' value)*` after key, turning any-of into
// an Or of single-value terms.
func (p *parser) keyTerm(key string) (Expr, error) {
	var values []token
	for sep := ':'; ; sep = ',' {
		p.i++ // the ':' or ','
		v, err := p.value(sep)
		if err != nil {
			return nil, err
		}
		values = append(values, v)
		if p.eof() || p.src[p.i] != ',' {
			break
		}
	}
	if !p.eof() && p.src[p.i] == ':' {
		return nil, p.errorf(p.i, "unexpected ':'; quote a value that contains ':'")
	}
	terms := make([]Expr, len(values))
	for i, v := range values {
		t, err := p.keyed(key, v)
		if err != nil {
			return nil, err
		}
		terms[i] = t
	}
	return newOr(terms), nil
}

// value reads the WORD or STRING after sep. A WORD can't start with '-' or
// be the word or (in any case): such a value needs quotes.
func (p *parser) value(sep rune) (token, error) {
	if p.eof() || !isWordRune(p.src[p.i]) && p.src[p.i] != '"' {
		return token{}, p.errorf(p.i, "expected a value after '%c'", sep)
	}
	if p.src[p.i] == '"' {
		return p.string()
	}
	w := p.word()
	if strings.HasPrefix(w.s, "-") {
		return token{}, p.errorf(w.start, "a value that starts with '-' needs quotes; quote it: %q", w.s)
	}
	if strings.EqualFold(w.s, "or") {
		return token{}, p.errorf(w.start, "the word or as a value needs quotes; quote it: %q", w.s)
	}
	return w, nil
}

// keyed makes the term for key:v.
func (p *parser) keyed(key string, v token) (Expr, error) {
	if v.s == "" {
		return nil, p.errorf(v.start, "empty value after '%s:'", key)
	}
	switch key {
	case "tag":
		return Tag{api.LowerLabel(v.s)}, nil
	case "id":
		return p.id(v)
	case "has":
		k := api.LowerLabel(v.s)
		switch k {
		case "id", "title", "body", "has":
			return nil, p.errorf(v.start, "has:%s is not a presence test; use has:tag or has:<attribute key>", k)
		}
		return Has{k}, nil
	case "title":
		return Text{TextTitle, v.s}, nil
	case "body":
		return Text{TextBody, v.s}, nil
	case "created":
		return p.date(key, Created, v)
	case "updated":
		return p.date(key, Updated, v)
	}
	return Attr{key, v.s}, nil
}

// id reads v as an id: a plain decimal number.
func (p *parser) id(v token) (Expr, error) {
	n, ok := api.ParseID(v.s)
	if !ok {
		return nil, p.errorf(v.start, "id:%s is not an id; an id is a plain decimal number", v.s)
	}
	return ID{n}, nil
}

// text makes a text term, which can't be empty: FTS5 rejects an empty phrase.
func (p *parser) text(f TextField, t token) (Expr, error) {
	if t.s == "" {
		return nil, p.errorf(t.start, "empty text term")
	}
	return Text{f, t.s}, nil
}

func (p *parser) eof() bool { return p.i >= len(p.src) }

func (p *parser) skipSpace() {
	for !p.eof() && unicode.IsSpace(p.src[p.i]) {
		p.i++
	}
}

// atOr reports whether the next word is the OR keyword: `or` in any case,
// not used as a key.
func (p *parser) atOr() bool {
	j := p.i
	for j < len(p.src) && isWordRune(p.src[j]) {
		j++
	}
	if !strings.EqualFold(string(p.src[p.i:j]), "or") {
		return false
	}
	return j == len(p.src) || p.src[j] != ':'
}

// isWordRune reports whether r can appear in a WORD.
func isWordRune(r rune) bool {
	switch r {
	case '(', ')', ':', ',', '"':
		return false
	}
	return !unicode.IsSpace(r)
}

// word reads a WORD, which may be empty.
func (p *parser) word() token {
	t := token{start: p.i}
	for !p.eof() && isWordRune(p.src[p.i]) {
		t.pos = append(t.pos, p.i)
		p.i++
	}
	t.s = string(p.src[t.start:p.i])
	return t
}

// string reads a STRING at its opening quote.
func (p *parser) string() (token, error) {
	t := token{start: p.i}
	var b strings.Builder
	p.i++
	for {
		if p.eof() {
			return token{}, p.errorf(t.start, "unclosed string")
		}
		r := p.src[p.i]
		if r == '"' {
			if p.i+1 < len(p.src) && p.src[p.i+1] == '"' {
				b.WriteRune('"')
				t.pos = append(t.pos, p.i)
				p.i += 2
				continue
			}
			p.i++
			t.s = b.String()
			return t, nil
		}
		b.WriteRune(r)
		t.pos = append(t.pos, p.i)
		p.i++
	}
}

// errorf returns an *Error at source index i.
func (p *parser) errorf(i int, format string, args ...any) *Error {
	return &Error{Position: i + 1, Msg: fmt.Sprintf(format, args...)}
}
