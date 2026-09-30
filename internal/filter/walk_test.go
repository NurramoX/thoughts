package filter

import (
	"reflect"
	"testing"
)

func TestRanked(t *testing.T) {
	tests := []struct {
		src  string
		want []Text
	}{
		{"", nil},
		{"tag:rust", nil},
		{"borrow", []Text{text("borrow")}},
		{"title:borrow", []Text{{TextTitle, "borrow"}}},
		{`borrow "borrow checker" tag:rust body:x`, []Text{text("borrow"), text("borrow checker"), {TextBody, "x"}}},
		{"(a b) c", []Text{text("a"), text("b"), text("c")}},
		{"a or b", nil},
		{"-a", nil},
		{"a -b (c or d)", []Text{text("a")}},
		{"title:a,b", nil},
		{"x title:a,b", []Text{text("x")}},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			e, err := Parse(tt.src, now)
			if err != nil {
				t.Fatal(err)
			}
			if got := Ranked(e); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Ranked(%q) = %#v, want %#v", tt.src, got, tt.want)
			}
		})
	}
}

func TestRankedNestedAnd(t *testing.T) {
	e := And{[]Expr{text("a"), And{[]Expr{text("b"), Not{text("c")}}}}}
	want := []Text{text("a"), text("b")}
	if got := Ranked(e); !reflect.DeepEqual(got, want) {
		t.Errorf("Ranked = %#v, want %#v", got, want)
	}
}

func TestVocabulary(t *testing.T) {
	tests := []struct {
		src        string
		tags, keys []string
	}{
		{"", nil, nil},
		{"borrow title:x id:3 created:2026", nil, nil},
		{"tag:Rust", []string{"rust"}, nil},
		{"tag:rust,go -tag:rust (x or tag:c)", []string{"c", "go", "rust"}, nil},
		{"effort:small Source:x -effort:big", nil, []string{"effort", "source"}},
		{"status:raw,active -updated:90d..", nil, nil},
		{"has:status has:tag has:created has:updated", nil, nil},
		{"has:effort -has:zeal tag:a", []string{"a"}, []string{"effort", "zeal"}},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			e, err := Parse(tt.src, now)
			if err != nil {
				t.Fatal(err)
			}
			tags, keys := Vocabulary(e)
			if !reflect.DeepEqual(tags, tt.tags) || !reflect.DeepEqual(keys, tt.keys) {
				t.Errorf("Vocabulary(%q) = %q, %q, want %q, %q", tt.src, tags, keys, tt.tags, tt.keys)
			}
		})
	}
}

func TestCaret(t *testing.T) {
	tests := []struct {
		src  string
		pos  int
		want string
	}{
		{"(a b", 1, "(a b\n^"},
		{"a)", 2, "a)\n ^"},
		{"tag:", 5, "tag:\n    ^"},      // one past the end
		{"日本語 )", 5, "日本語 )\n       ^"}, // wide characters take two columns
		{"café )", 6, "café )\n     ^"},
		{"a\tb)", 4, "a\tb)\n \t ^"},
	}
	for _, tt := range tests {
		if got := Caret(tt.src, tt.pos); got != tt.want {
			t.Errorf("Caret(%q, %d) = %q, want %q", tt.src, tt.pos, got, tt.want)
		}
	}
}
