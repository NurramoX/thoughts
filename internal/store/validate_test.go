package store

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/NurramoX/thoughts/internal/api"
)

func TestCreateRejectsInvalidContent(t *testing.T) {
	tags := func(n int) []string {
		var out []string
		for i := range n {
			out = append(out, "t"+strings.Repeat("x", i))
		}
		return out
	}
	attrs := func(n int) map[string]string {
		out := map[string]string{}
		for i := range n {
			out["k"+strings.Repeat("x", i)] = "v"
		}
		return out
	}
	for name, req := range map[string]api.CreateRequest{
		"empty title":             {Title: ""},
		"blank title":             {Title: " \t "},
		"title over 400 chars":    {Title: strings.Repeat("é", 401)},
		"multi-line title":        {Title: "one\ntwo"},
		"title with a control":    {Title: "bell\a"},
		"title with a separator":  {Title: "one two"},
		"title not UTF-8":         {Title: "bad \xff"},
		"body not UTF-8":          {Title: "t", Body: "bad \xc3"},
		"empty tag":               {Title: "t", Tags: []string{""}},
		"tag with a space":        {Title: "t", Tags: []string{"a b"}},
		"tag starting with -":     {Title: "t", Tags: []string{"-a"}},
		"tag starting with _":     {Title: "t", Tags: []string{"_a"}},
		"non-ASCII tag":           {Title: "t", Tags: []string{"café"}},
		"Kelvin-sign tag":         {Title: "t", Tags: []string{"K"}},
		"tag over 128 bytes":      {Title: "t", Tags: []string{strings.Repeat("a", 129)}},
		"129 tags":                {Title: "t", Tags: tags(129)},
		"invalid key":             {Title: "t", Attributes: map[string]string{"a.b": "v"}},
		"key over 128 bytes":      {Title: "t", Attributes: map[string]string{strings.Repeat("a", 129): "v"}},
		"empty value":             {Title: "t", Attributes: map[string]string{"k": ""}},
		"blank value":             {Title: "t", Attributes: map[string]string{"k": "  "}},
		"value over 2000 bytes":   {Title: "t", Attributes: map[string]string{"k": strings.Repeat("a", 2001)}},
		"tab-only value":          {Title: "t", Attributes: map[string]string{"k": "\t \t"}},
		"value with an LF":        {Title: "t", Attributes: map[string]string{"k": "a\nb"}},
		"value with a CR":         {Title: "t", Attributes: map[string]string{"k": "a\rb"}},
		"value with a VT":         {Title: "t", Attributes: map[string]string{"k": "a\vb"}},
		"value with an FF":        {Title: "t", Attributes: map[string]string{"k": "a\fb"}},
		"value with a NEL":        {Title: "t", Attributes: map[string]string{"k": "a\u0085b"}},
		"value with an LS":        {Title: "t", Attributes: map[string]string{"k": "a\u2028b"}},
		"value with a PS":         {Title: "t", Attributes: map[string]string{"k": "a\u2029b"}},
		"value not UTF-8":         {Title: "t", Attributes: map[string]string{"k": "\xff"}},
		"unknown status":          {Title: "t", Attributes: map[string]string{"status": "maybe"}},
		"key given twice":         {Title: "t", Attributes: map[string]string{"k": "a", "K": "b"}},
		"128 attributes + status": {Title: "t", Attributes: attrs(128)},
	} {
		t.Run(name, func(t *testing.T) {
			s, _ := open(t)
			_, err := s.Create(ctx, req)
			wantInvalid(t, err)
		})
	}
	for _, k := range api.ReservedKeys {
		t.Run("reserved key "+k, func(t *testing.T) {
			s, _ := open(t)
			_, err := s.Create(ctx, api.CreateRequest{Title: "t", Attributes: map[string]string{strings.ToUpper(k): "v"}})
			wantInvalid(t, err)
		})
	}
}

func TestValuesMayHoldTabsAndOtherNonBreakingCharacters(t *testing.T) {
	s, _ := open(t)
	thought := create(t, s, api.CreateRequest{Title: "t", Attributes: map[string]string{
		"cols":  "\ta\tb\t",
		"bell":  "ring\a",
		"esc":   "\x1b[1m",
		"nbsp":  "a\u00a0b",
		"state": "wip",
	}})
	want := map[string]string{"cols": "a\tb", "bell": "ring\a", "esc": "\x1b[1m", "nbsp": "a\u00a0b", "state": "wip", "status": "raw"}
	if !reflect.DeepEqual(thought.Attributes, want) {
		t.Errorf("Attributes = %q, want %q", thought.Attributes, want)
	}
}

func TestCreateAcceptsTheLimits(t *testing.T) {
	s, _ := open(t)
	tags := []string{strings.Repeat("z", 128), "a0-_"}
	for i := range 126 {
		tags = append(tags, "t"+strings.Repeat("x", i))
	}
	attrs := map[string]string{"status": "done", strings.Repeat("k", 128): "v"}
	for i := range 126 {
		attrs["k"+strings.Repeat("x", i)] = strings.Repeat("v", 2000)
	}
	thought := create(t, s, api.CreateRequest{
		Title:      strings.Repeat("é", 400),
		Body:       strings.Repeat("a", api.MaxBody),
		Tags:       tags,
		Attributes: attrs,
	})
	if len(thought.Tags) != 128 || len(thought.Attributes) != 128 || len(thought.Body) != api.MaxBody {
		t.Errorf("got %d tags, %d attributes, %d body bytes", len(thought.Tags), len(thought.Attributes), len(thought.Body))
	}
}

func TestBodyOverTheLimitIsTooLarge(t *testing.T) {
	s, _ := open(t)
	big := strings.Repeat("a", api.MaxBody+1)
	if _, err := s.Create(ctx, api.CreateRequest{Title: "t", Body: big}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("Create: err = %v, want ErrTooLarge", err)
	}
	thought := create(t, s, api.CreateRequest{Title: "t"})
	if _, err := s.PutBody(ctx, thought.ID, api.Precondition{}, []byte(big)); !errors.Is(err, ErrTooLarge) {
		t.Errorf("PutBody: err = %v, want ErrTooLarge", err)
	}
	if _, err := s.Patch(ctx, thought.ID, api.Precondition{}, api.Patch{Body: &big}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("Patch: err = %v, want ErrTooLarge", err)
	}
}
