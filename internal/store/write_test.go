package store

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NurramoX/thoughts/internal/api"
)

var none api.Precondition

// seed creates one thought with a tag, an attribute and a body, then moves the
// clock on, so that a real change shows in updated_at.
func seed(t *testing.T) (Store, *clock, api.Thought) {
	t.Helper()
	s, c := open(t)
	thought := create(t, s, api.CreateRequest{
		Title:      "Borrow checker",
		Body:       "body\n",
		Tags:       []string{"rust"},
		Attributes: map[string]string{"effort": "Small"},
	})
	c.Advance(time.Minute)
	return s, c, thought
}

// A write is a real change or a no-op, and version and updated_at follow.
func TestVersionMovesOnlyOnARealChange(t *testing.T) {
	type write func(s Store, id int64) (int64, error)
	patch := func(p api.Patch) write {
		return func(s Store, id int64) (int64, error) {
			thought, err := s.Patch(ctx, id, none, p)
			return thought.Version, err
		}
	}
	tags := func(ts ...string) *[]string { return &ts }
	for _, tc := range []struct {
		name   string
		write  write
		change bool
	}{
		{"same title", patch(api.Patch{Title: str("  Borrow checker ")}), false},
		{"new title", patch(api.Patch{Title: str("Borrow checker 2")}), true},
		{"same body", patch(api.Patch{Body: str("body\n")}), false},
		{"body without its newline", patch(api.Patch{Body: str("body")}), true},
		{"same tag set", patch(api.Patch{Tags: tags("RUST", "rust")}), false},
		{"new tag set", patch(api.Patch{Tags: tags("go")}), true},
		{"empty tag set", patch(api.Patch{Tags: tags()}), true},
		{"same attribute", patch(api.Patch{Attributes: map[string]*string{"effort": str(" Small")}}), false},
		{"attribute in another case", patch(api.Patch{Attributes: map[string]*string{"effort": str("small")}}), true},
		{"same status, other case", patch(api.Patch{Attributes: map[string]*string{"status": str("RAW")}}), false},
		{"remove an absent key", patch(api.Patch{Attributes: map[string]*string{"size": nil}}), false},
		{"remove a key", patch(api.Patch{Attributes: map[string]*string{"effort": nil}}), true},
		{"empty patch", patch(api.Patch{}), false},
		{"PutBody same", func(s Store, id int64) (int64, error) { return s.PutBody(ctx, id, none, []byte("body\n")) }, false},
		{"PutBody new", func(s Store, id int64) (int64, error) { return s.PutBody(ctx, id, none, []byte("")) }, true},
		{"PutTag present", func(s Store, id int64) (int64, error) { return s.PutTag(ctx, id, none, "Rust") }, false},
		{"PutTag new", func(s Store, id int64) (int64, error) { return s.PutTag(ctx, id, none, "go") }, true},
		{"DeleteTag absent", func(s Store, id int64) (int64, error) { return s.DeleteTag(ctx, id, none, "go") }, false},
		{"DeleteTag present", func(s Store, id int64) (int64, error) { return s.DeleteTag(ctx, id, none, "RUST") }, true},
		{"PutAttribute same", func(s Store, id int64) (int64, error) { return s.PutAttribute(ctx, id, none, "Effort", "Small ") }, false},
		{"PutAttribute new value", func(s Store, id int64) (int64, error) { return s.PutAttribute(ctx, id, none, "effort", "large") }, true},
		{"PutAttribute new key", func(s Store, id int64) (int64, error) { return s.PutAttribute(ctx, id, none, "size", "xl") }, true},
		{"PutAttribute status", func(s Store, id int64) (int64, error) { return s.PutAttribute(ctx, id, none, "status", "Done") }, true},
		{"DeleteAttribute absent", func(s Store, id int64) (int64, error) { return s.DeleteAttribute(ctx, id, none, "size") }, false},
		{"DeleteAttribute present", func(s Store, id int64) (int64, error) { return s.DeleteAttribute(ctx, id, none, "EFFORT") }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, c, before := seed(t)
			version, err := tc.write(s, before.ID)
			if err != nil {
				t.Fatal(err)
			}
			after := get(t, s, before.ID)
			wantVersion, wantUpdated := before.Version, before.UpdatedAt
			if tc.change {
				wantVersion++
				wantUpdated = api.Time{Time: c.Now().UTC().Truncate(time.Millisecond)}
			}
			if version != wantVersion || after.Version != wantVersion {
				t.Errorf("version: returned %d, stored %d, want %d", version, after.Version, wantVersion)
			}
			if !after.UpdatedAt.Equal(wantUpdated.Time) {
				t.Errorf("updated_at = %v, want %v", after.UpdatedAt, wantUpdated)
			}
			if !after.CreatedAt.Equal(before.CreatedAt.Time) {
				t.Errorf("created_at moved to %v", after.CreatedAt)
			}
		})
	}
}

func TestPatchAppliesEverythingAsOneVersion(t *testing.T) {
	s, c, before := seed(t)
	got, err := s.Patch(ctx, before.ID, api.Precondition{Version: 1}, api.Patch{
		Title:      str(" New title "),
		Body:       str("new body"),
		Tags:       &[]string{"b", "A"},
		Attributes: map[string]*string{"effort": nil, "size": str(" XL "), "status": str("Active")},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := api.Thought{
		Meta: api.Meta{
			ID:         before.ID,
			Title:      "New title",
			Tags:       []string{"a", "b"},
			Attributes: map[string]string{"size": "XL", "status": "active"},
			Version:    2,
			CreatedAt:  before.CreatedAt,
			UpdatedAt:  api.Time{Time: c.Now().UTC().Truncate(time.Millisecond)},
		},
		Body: "new body",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Patch = %+v\nwant    %+v", got, want)
	}
}

// Nothing of a rejected patch is applied.
func TestRejectedPatchChangesNothing(t *testing.T) {
	s, _, before := seed(t)
	many := make([]string, 0)
	for i := range 128 {
		many = append(many, "k"+strings.Repeat("x", i))
	}
	attrs := map[string]*string{}
	for _, k := range many {
		attrs[k] = str("v")
	}
	for name, p := range map[string]api.Patch{
		"remove status":          {Title: str("changed"), Attributes: map[string]*string{"status": nil}},
		"invalid title":          {Title: str(""), Body: str("changed")},
		"over the attribute cap": {Title: str("changed"), Attributes: attrs},
		"reserved key":           {Title: str("changed"), Attributes: map[string]*string{"title": str("x")}},
		"key given twice":        {Attributes: map[string]*string{"a": str("x"), "A": nil}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := s.Patch(ctx, before.ID, none, p)
			wantInvalid(t, err)
			if after := get(t, s, before.ID); !reflect.DeepEqual(after, before) {
				t.Errorf("thought changed:\n%+v\nwas\n%+v", after, before)
			}
		})
	}
}

func TestStatusCannotBeRemoved(t *testing.T) {
	s, _, thought := seed(t)
	_, err := s.DeleteAttribute(ctx, thought.ID, none, "Status")
	wantInvalid(t, err)
}

func TestSingleWritesValidate(t *testing.T) {
	s, _, thought := seed(t)
	_, err := s.PutTag(ctx, thought.ID, none, "no spaces")
	wantInvalid(t, err)
	_, err = s.DeleteTag(ctx, thought.ID, none, "")
	wantInvalid(t, err)
	_, err = s.PutAttribute(ctx, thought.ID, none, "has", "x")
	wantInvalid(t, err)
	_, err = s.PutAttribute(ctx, thought.ID, none, "k", "")
	wantInvalid(t, err)
	_, err = s.PutAttribute(ctx, thought.ID, none, "status", "later")
	wantInvalid(t, err)
	_, err = s.PutBody(ctx, thought.ID, none, []byte{0xff})
	wantInvalid(t, err)
}

func TestTagAndAttributeCapsHoldForSingleWrites(t *testing.T) {
	s, _ := open(t)
	var tags []string
	attrs := map[string]string{}
	for i := range 128 {
		tags = append(tags, "t"+strings.Repeat("x", i))
	}
	for i := range 127 {
		attrs["k"+strings.Repeat("x", i)] = "v"
	}
	thought := create(t, s, api.CreateRequest{Title: "full", Tags: tags, Attributes: attrs})

	_, err := s.PutTag(ctx, thought.ID, none, "one-more")
	wantInvalid(t, err)
	_, err = s.PutAttribute(ctx, thought.ID, none, "one-more", "v")
	wantInvalid(t, err)
	// Present ones are no-ops, and replacing a value is fine.
	if _, err := s.PutTag(ctx, thought.ID, none, "t"); err != nil {
		t.Error(err)
	}
	if _, err := s.PutAttribute(ctx, thought.ID, none, "k", "w"); err != nil {
		t.Error(err)
	}
	if got := get(t, s, thought.ID); len(got.Tags) != 128 || len(got.Attributes) != 128 {
		t.Errorf("%d tags, %d attributes", len(got.Tags), len(got.Attributes))
	}
}

func TestPreconditions(t *testing.T) {
	type write func(s Store, id int64, pre api.Precondition) error
	for name, w := range map[string]write{
		"Patch": func(s Store, id int64, pre api.Precondition) error {
			_, err := s.Patch(ctx, id, pre, api.Patch{Title: str("x")})
			return err
		},
		"PutBody": func(s Store, id int64, pre api.Precondition) error {
			_, err := s.PutBody(ctx, id, pre, []byte("x"))
			return err
		},
		"PutTag": func(s Store, id int64, pre api.Precondition) error {
			_, err := s.PutTag(ctx, id, pre, "x")
			return err
		},
		"DeleteTag": func(s Store, id int64, pre api.Precondition) error {
			_, err := s.DeleteTag(ctx, id, pre, "rust")
			return err
		},
		"PutAttribute": func(s Store, id int64, pre api.Precondition) error {
			_, err := s.PutAttribute(ctx, id, pre, "x", "y")
			return err
		},
		"DeleteAttribute": func(s Store, id int64, pre api.Precondition) error {
			_, err := s.DeleteAttribute(ctx, id, pre, "effort")
			return err
		},
		"Delete": func(s Store, id int64, pre api.Precondition) error {
			return s.Delete(ctx, id, pre)
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, _, thought := seed(t)
			if _, err := s.PutTag(ctx, thought.ID, none, "bump"); err != nil { // version 2
				t.Fatal(err)
			}
			wantStale(t, w(s, thought.ID, api.Precondition{Version: 1}), 2)
			// A stale write, even one that would be a no-op, changes nothing.
			if got := get(t, s, thought.ID); got.Version != 2 {
				t.Fatalf("version = %d after a stale write", got.Version)
			}
			if err := w(s, 99, api.Precondition{Version: 1}); !errors.Is(err, ErrNotFound) {
				t.Errorf("unknown id: err = %v, want ErrNotFound", err)
			}
			if err := w(s, thought.ID, api.Precondition{Version: 2}); err != nil {
				t.Errorf("current version: %v", err)
			}
		})
		t.Run(name+" forced", func(t *testing.T) {
			s, _, thought := seed(t)
			if err := w(s, thought.ID, api.Precondition{Version: 7, Force: true}); err != nil {
				t.Errorf("forced: %v", err)
			}
		})
		t.Run(name+" unguarded", func(t *testing.T) {
			s, _, thought := seed(t)
			if err := w(s, thought.ID, none); err != nil {
				t.Errorf("unguarded: %v", err)
			}
		})
	}
}

// Concurrent writers serialise on the one connection; none is lost.
func TestConcurrentWritesSerialise(t *testing.T) {
	s, _, thought := seed(t)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			if _, err := s.PutTag(ctx, thought.ID, none, "t"+strconv.Itoa(i)); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if got := get(t, s, thought.ID); got.Version != 21 || len(got.Tags) != 21 {
		t.Errorf("version %d with %d tags, want 21 and 21", got.Version, len(got.Tags))
	}
}

func TestTimestampsAreUTCMilliseconds(t *testing.T) {
	s, c := open(t)
	c.Set(time.Date(2026, 1, 2, 3, 4, 5, 999_999_999, time.FixedZone("CEST", 2*3600)))
	thought := create(t, s, api.CreateRequest{Title: "t"})
	if got := thought.CreatedAt.String(); got != "2026-01-02T01:04:05.999Z" {
		t.Errorf("created_at = %s", got)
	}
}

func TestDeleteIsHardAndCascades(t *testing.T) {
	s, _, thought := seed(t)
	create(t, s, api.CreateRequest{Title: "other", Tags: []string{"go"}})
	if err := s.Delete(ctx, thought.ID, none); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, thought.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after delete: err = %v", err)
	}
	if err := s.Delete(ctx, thought.ID, none); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: err = %v", err)
	}
	if _, err := s.PutTag(ctx, thought.ID, none, "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("PutTag after delete: err = %v", err)
	}
	// Its tags and attributes left the vocabulary with it.
	tags, err := s.Tags(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := []api.TagCount{{Tag: "go", Count: 1}}; !reflect.DeepEqual(tags, want) {
		t.Errorf("Tags = %v, want %v", tags, want)
	}
	keys, err := s.Attributes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := []api.KeyCount{{Key: "status", Count: 1}}; !reflect.DeepEqual(keys, want) {
		t.Errorf("Attributes = %v, want %v", keys, want)
	}
}
