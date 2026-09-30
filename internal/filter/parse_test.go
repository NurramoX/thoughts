package filter

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// now is a fixed instant in a non-UTC zone with DST, so that periods and
// relative instants are visibly resolved in now's location.
var (
	berlin = mustLoad("Europe/Berlin")
	now    = time.Date(2026, 9, 23, 15, 4, 5, 0, berlin)
)

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

func text(s string) Text { return Text{TextAny, s} }

func TestParse(t *testing.T) {
	tests := []struct {
		src  string
		want Expr
	}{
		{"", nil},
		{"  \t ", nil},
		{"borrow", text("borrow")},
		{"  borrow  ", text("borrow")},
		{"foo-bar", text("foo-bar")},
		{"key=value", text("key=value")},
		{"café", text("café")},

		// Juxtaposition is AND.
		{"borrow checker", And{[]Expr{text("borrow"), text("checker")}}},

		// Strings.
		{`"borrow checker"`, text("borrow checker")},
		{`"say ""hi"""`, text(`say "hi"`)},
		{`""""`, text(`"`)},
		{`"a:b,c (d) -e or"`, text("a:b,c (d) -e or")},
		{`"or"`, text("or")},
		{`"-x"`, text("-x")},
		{`foo"bar"`, And{[]Expr{text("foo"), text("bar")}}},

		// or, in any case, binds loosest.
		{"a or b", Or{[]Expr{text("a"), text("b")}}},
		{"a OR b Or c", Or{[]Expr{text("a"), text("b"), text("c")}}},
		{"a b or c", Or{[]Expr{And{[]Expr{text("a"), text("b")}}, text("c")}}},
		{"a or b c", Or{[]Expr{text("a"), And{[]Expr{text("b"), text("c")}}}}},
		{"orange", text("orange")},

		// - binds tightest.
		{"-a", Not{text("a")}},
		{"-a b", And{[]Expr{Not{text("a")}, text("b")}}},
		{"--a", Not{Not{text("a")}}},
		{`-"a b"`, Not{text("a b")}},
		{"a-", text("a-")},

		// Parens group; nested And and Or flatten.
		{"(a)", text("a")},
		{"(a or b) c", And{[]Expr{Or{[]Expr{text("a"), text("b")}}, text("c")}}},
		{"-(a b)", Not{And{[]Expr{text("a"), text("b")}}}},
		{"(a b) c", And{[]Expr{text("a"), text("b"), text("c")}}},
		{"a or (b or c)", Or{[]Expr{text("a"), text("b"), text("c")}}},
		{"((a))", text("a")},
		{"a(b)", And{[]Expr{text("a"), text("b")}}},

		// Attributes: keys are lowercased, values kept as written.
		{"effort:small", Attr{"effort", "small"}},
		{"Effort:Small", Attr{"effort", "Small"}},
		{"status:raw", Attr{"status", "raw"}},
		{"STATUS:Raw", Attr{"status", "Raw"}},
		{`source:"shower thought"`, Attr{"source", "shower thought"}},
		{"source:shower thought", And{[]Expr{Attr{"source", "shower"}, text("thought")}}},
		{`url:"https://x.org/a,b"`, Attr{"url", "https://x.org/a,b"}},
		{`q:"say ""hi"""`, Attr{"q", `say "hi"`}},
		{"k:a-b", Attr{"k", "a-b"}},
		{"k:a,b-", Or{[]Expr{Attr{"k", "a"}, Attr{"k", "b-"}}}},
		{"k:orange", Attr{"k", "orange"}},

		// A value that starts with '-' or is the word or is quoted.
		{`n:"-1"`, Attr{"n", "-1"}},
		{`word:"or"`, Attr{"word", "or"}},
		{`word:"OR"`, Attr{"word", "OR"}},
		{`tag:a,"or","-b"`, Or{[]Expr{Tag{"a"}, Tag{"or"}, Tag{"-b"}}}},

		// A word right before ':' is a key, even the word or.
		{"or:x", Attr{"or", "x"}},
		{"OR:x", Attr{"or", "x"}},
		{"a or:x", And{[]Expr{text("a"), Attr{"or", "x"}}}},
		{"-or:x", Not{Attr{"or", "x"}}},
		{"or:x or y", Or{[]Expr{Attr{"or", "x"}, text("y")}}},
		{"status:raw,active", Or{[]Expr{Attr{"status", "raw"}, Attr{"status", "active"}}}},
		{`k:a,"b c",d`, Or{[]Expr{Attr{"k", "a"}, Attr{"k", "b c"}, Attr{"k", "d"}}}},
		{"-effort:small", Not{Attr{"effort", "small"}}},
		{"-status:done,dropped", Not{Or{[]Expr{Attr{"status", "done"}, Attr{"status", "dropped"}}}}},
		{"k:v(x)", And{[]Expr{Attr{"k", "v"}, text("x")}}},

		// Keys, tags and has: values are lowercased as the server lowercases a
		// label: ASCII letters only.
		{"ÉFFORT:Small", Attr{"Éffort", "Small"}},
		{"tag:RÜST", Tag{"rÜst"}},
		{"has:ÉFFORT", Has{"Éffort"}},
		{"tag:rust", Tag{"rust"}},
		{"TAG:Rust", Tag{"rust"}},
		{`tag:"Rust"`, Tag{"rust"}},
		{"tag:rust,go", Or{[]Expr{Tag{"rust"}, Tag{"go"}}}},

		// Ids.
		{"id:12", ID{12}},
		{"ID:7", ID{7}}, // keys match lowercased
		{`id:"12"`, ID{12}},
		{"id:1,2", Or{[]Expr{ID{1}, ID{2}}}},
		{"id:9223372036854775807", ID{9223372036854775807}},

		// Presence.
		{"has:effort", Has{"effort"}},
		{"has:Effort", Has{"effort"}},
		{"has:tag", Has{"tag"}},
		{"has:created", Has{"created"}},
		{"has:updated", Has{"updated"}},
		{"has:status", Has{"status"}},
		{"-has:effort", Not{Has{"effort"}}},
		{"has:effort,tag", Or{[]Expr{Has{"effort"}, Has{"tag"}}}},

		// Scoped text.
		{"title:borrow", Text{TextTitle, "borrow"}},
		{"Body:borrow", Text{TextBody, "borrow"}},
		{`title:"borrow checker"`, Text{TextTitle, "borrow checker"}},
		{"title:a,b", Or{[]Expr{Text{TextTitle, "a"}, Text{TextTitle, "b"}}}},

		// ADR 0002's example.
		{`tag:rust status:raw,active -has:effort "borrow checker"`, And{[]Expr{
			Tag{"rust"},
			Or{[]Expr{Attr{"status", "raw"}, Attr{"status", "active"}}},
			Not{Has{"effort"}},
			text("borrow checker"),
		}}},
		{"borrow checker tag:rust", And{[]Expr{text("borrow"), text("checker"), Tag{"rust"}}}},
		{"tag:a or tag:b,c", Or{[]Expr{Tag{"a"}, Tag{"b"}, Tag{"c"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			got, err := Parse(tt.src, now)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tt.src, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parse(%q)\n got %#v\nwant %#v", tt.src, got, tt.want)
			}
		})
	}
}

// FuzzParse checks that Parse never panics and that every error is an
// *Error pointing into src or just past its end.
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		`tag:rust status:raw,active -has:effort "borrow checker"`,
		"status:raw,active -updated:90d..",
		`(a or -b) title:"x ""y""" created:2026-09..7d`,
		`id:12,x has:id "ü`,
		"a or or ) ( - , :",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		_, err := Parse(src, now)
		if err == nil {
			return
		}
		pe, ok := err.(*Error)
		if !ok {
			t.Fatalf("Parse(%q) error is %T, want *Error", src, err)
		}
		if n := len([]rune(src)); pe.Position < 1 || pe.Position > n+1 {
			t.Fatalf("Parse(%q) error at %d, outside 1..%d", src, pe.Position, n+1)
		}
	})
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		src     string
		pos     int
		msgPart string
	}{
		{"(a b", 1, "unclosed '('"},
		{"x (a (b)", 3, "unclosed '('"},
		{"()", 2, "empty parentheses"},
		{"a ( )", 5, "empty parentheses"},
		{"a)", 2, "unmatched ')'"},
		{"(a)) b", 4, "unmatched ')'"},
		{")", 1, "unmatched ')'"},
		{`"abc`, 1, "unclosed string"},
		{`a "b""`, 3, "unclosed string"},
		{"-", 1, "'-'"},
		{"a -", 3, "'-'"},
		{"- a", 1, "'-'"},
		{"(a -)", 4, "'-'"},
		{"-or b", 1, "'-'"},
		{"or a", 1, "'or' needs a term before it"},
		{"  OR a", 3, "'or' needs a term before it"},
		{"a or", 3, "'or' needs a term after it"},
		{"a or )", 3, "'or' needs a term after it"},
		{"(a or) b", 4, "'or' needs a term after it"},
		{"(or a)", 2, "'or' needs a term before it"},
		{"a or or b", 6, "'or'"},
		{`""`, 1, "empty"},
		{`a ""`, 3, "empty"},
		{",a", 1, "','"},
		{"a ,", 3, "','"},
		{":a", 1, "missing key"},
		{`"a":b`, 4, "key"},
		{"tag:", 5, "value after ':'"},
		{"tag: rust", 5, "value after ':'"},
		{"tag:(rust)", 5, "value after ':'"},
		{"(tag:)", 6, "value after ':'"},
		{"tag:a,", 7, "value after ','"},
		{"tag:a, b", 7, "value after ','"},
		{"tag:a,,b", 7, "value after ','"},
		{"k:a:b", 4, "':'"},
		{`k:"a":b`, 6, "':'"},
		{"k:,a", 3, "value after ':'"},
		// A value that starts with '-' or is the word or needs quotes.
		{"n:-1", 3, `quote it: "-1"`},
		{"k:-", 3, `quote it: "-"`},
		{"word:or", 6, `quote it: "or"`},
		{"word:OR", 6, `quote it: "OR"`},
		{"tag:a,or", 7, `quote it: "or"`},
		{"tag:a,-b", 7, `quote it: "-b"`},
		{"tag:a,or b", 7, `quote it: "or"`},
		{"(k:or)", 4, `quote it: "or"`},
		{`effort:""`, 8, "empty"},
		{`tag:a,""`, 7, "empty"},
		{`title:""`, 7, "empty"},
		{`k:"abc`, 3, "unclosed string"},
		{"id:abc", 4, "id"},
		{"id:1,x2", 6, "id"},
		{"id:-1", 4, `quote it: "-1"`},
		{"id:+1", 4, "id"},
		{"id:1.5", 4, "id"},
		{"id:007", 4, "id"},
		{"id:9223372036854775808", 4, "id"},
		{"has:id", 5, "has:id"},
		{"has:TITLE", 5, "has:title"},
		{"has:body", 5, "has:body"},
		{"has:effort,has", 12, "has:has"},
		{"created:7d", 9, "created:7d.."},
		{"updated:90d", 9, "updated:..90d"},
		{"created:2026,7d", 14, "range"},
		{"created:..", 9, "range needs"},
		{"created:26", 9, "not a date"},
		{"created:2026-9", 9, "not a date"},
		{"created:2026-13", 9, "not a date"},
		{"created:2026-02-30", 9, "not a date"},
		{"created:+026", 9, "not a date"},
		{"created:-7d..", 9, `quote it: "-7d.."`},
		{"created:yesterday", 9, "not a date"},
		{"created:2026..abc", 15, "not a date"},
		{"created:abc..2026", 9, "not a date"},
		{"created:2026...2027", 15, "not a date"},
		{"created:1000000d..", 9, "not a date"},
		{`created:"2026-09-01T10:00Z"`, 9, "not a date"},
		{"created:2026-09-01T10:00:00Z", 22, "quote"},
		{`created:""`, 9, "empty"},
		{`updated:"2026..ü"`, 16, "not a date"},
		{`updated:"ü..2026"`, 10, "not a date"},
		// Positions count runes, not bytes.
		{"ünï:ö,", 7, "value after ','"},
		{"日本 id:1,語", 9, "id"},
		{"ünïcödé (a", 9, "unclosed '('"},
		{"日本語 )", 5, "unmatched ')'"},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			e, err := Parse(tt.src, now)
			if err == nil {
				t.Fatalf("Parse(%q) = %#v, want an error", tt.src, e)
			}
			pe, ok := err.(*Error)
			if !ok {
				t.Fatalf("Parse(%q) error is %T, want *Error", tt.src, err)
			}
			if pe.Position != tt.pos || !strings.Contains(pe.Msg, tt.msgPart) {
				t.Errorf("Parse(%q) = error at %d %q, want at %d containing %q",
					tt.src, pe.Position, pe.Msg, tt.pos, tt.msgPart)
			}
		})
	}
}
