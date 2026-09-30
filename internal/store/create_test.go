package store

import (
	"errors"
	"reflect"
	"testing"

	"github.com/NurramoX/thoughts/internal/api"
)

func TestCreateThenGet(t *testing.T) {
	s, _ := open(t)
	thought := create(t, s, api.CreateRequest{
		Title:      "  Borrow checker ergonomics ",
		Body:       "# notes\r\nbyte-exact \x00 body\n",
		Tags:       []string{"Rust", "lang", "rust"},
		Attributes: map[string]string{"Effort": "  Small ", "status": "Active"},
	})
	want := api.Thought{
		Meta: api.Meta{
			ID:         1,
			Title:      "Borrow checker ergonomics",
			Tags:       []string{"lang", "rust"},
			Attributes: map[string]string{"effort": "Small", "status": "active"},
			Version:    1,
			CreatedAt:  ts("2026-09-22T14:03:07.412Z"),
			UpdatedAt:  ts("2026-09-22T14:03:07.412Z"),
		},
		Body: "# notes\r\nbyte-exact \x00 body\n",
	}
	if !reflect.DeepEqual(thought, want) {
		t.Errorf("Create = %+v\nwant     %+v", thought, want)
	}
	if got := get(t, s, thought.ID); !reflect.DeepEqual(got, want) {
		t.Errorf("Get = %+v\nwant  %+v", got, want)
	}
}

func TestCreateDefaults(t *testing.T) {
	s, _ := open(t)
	thought := create(t, s, api.CreateRequest{Title: "t"})
	if thought.Body != "" || thought.Tags == nil || len(thought.Tags) != 0 {
		t.Errorf("defaults: %+v", thought)
	}
	if !reflect.DeepEqual(thought.Attributes, map[string]string{"status": "raw"}) {
		t.Errorf("Attributes = %v, want status raw", thought.Attributes)
	}
}

func TestIDsAreNeverReused(t *testing.T) {
	s, _ := open(t)
	create(t, s, api.CreateRequest{Title: "one"})
	two := create(t, s, api.CreateRequest{Title: "two"})
	if err := s.Delete(ctx, two.ID, api.Precondition{}); err != nil {
		t.Fatal(err)
	}
	if three := create(t, s, api.CreateRequest{Title: "three"}); three.ID != 3 {
		t.Errorf("id after delete = %d, want 3", three.ID)
	}
}

func TestGetUnknown(t *testing.T) {
	s, _ := open(t)
	if _, err := s.Get(ctx, 99); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
