package hint_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/NurramoX/thoughts/internal/api"
	"github.com/NurramoX/thoughts/internal/client"
	"github.com/NurramoX/thoughts/internal/hint"
)

// vocab is a Client that knows only the vocabulary endpoints and counts how
// often they are called.
type vocab struct {
	client.Client
	tags  []api.TagCount
	keys  []api.KeyCount
	calls int
}

func (v *vocab) Tags(context.Context) ([]api.TagCount, error) {
	v.calls++
	return v.tags, nil
}

func (v *vocab) Attributes(context.Context) ([]api.KeyCount, error) {
	v.calls++
	return v.keys, nil
}

func TestVocabulary(t *testing.T) {
	c := &vocab{
		tags: []api.TagCount{{Tag: "rust", Count: 2}},
		keys: []api.KeyCount{{Key: "status", Count: 3}, {Key: "effort", Count: 1}},
	}
	cases := []struct {
		src  string
		want []string
	}{
		{"tag:rsut effort:small", []string{"no thoughts have the tag 'rsut'"}},
		{"tag:rust tga:x -has:efort", []string{"no thoughts have the key 'efort'", "no thoughts have the key 'tga'"}},
		{"tag:zz or tag:rust", []string{"no thoughts have the tag 'zz'"}},
		{"tag:rust effort:big status:raw", nil},
		{"borrow checker", nil},
		{"", nil},
		{"tag:(", nil}, // does not parse: no hint
	}
	for _, tc := range cases {
		got, err := hint.Vocabulary(context.Background(), c, tc.src)
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q: got %q, want %q", tc.src, got, tc.want)
		}
	}
}

func TestVocabularyAsksOnlyWhenNeeded(t *testing.T) {
	c := &vocab{}
	hint.Vocabulary(context.Background(), c, "borrow")
	if c.calls != 0 {
		t.Errorf("%d calls for a filter without tags or keys", c.calls)
	}
}
