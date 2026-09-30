package store

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/NurramoX/thoughts/internal/api"
)

// clock is a fake clock that only moves when told to.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock {
	return &clock{t: time.Date(2026, 9, 22, 14, 3, 7, 412_345_678, time.UTC)}
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func (c *clock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

var ctx = context.Background()

// open opens a store on a fresh database and closes it with the test.
func open(t *testing.T) (Store, *clock) {
	t.Helper()
	c := newClock()
	s, err := Open(filepath.Join(t.TempDir(), "thoughts.db"), c.Now)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return s, c
}

func create(t *testing.T, s Store, req api.CreateRequest) api.Thought {
	t.Helper()
	thought, err := s.Create(ctx, req)
	if err != nil {
		t.Fatalf("Create(%+v): %v", req, err)
	}
	return thought
}

func get(t *testing.T, s Store, id int64) api.Thought {
	t.Helper()
	thought, err := s.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get(%d): %v", id, err)
	}
	return thought
}

func wantInvalid(t *testing.T, err error) {
	t.Helper()
	var inv *InvalidError
	if !errors.As(err, &inv) {
		t.Fatalf("err = %v, want *InvalidError", err)
	}
}

func wantStale(t *testing.T, err error, current int64) {
	t.Helper()
	var stale *StaleError
	if !errors.As(err, &stale) {
		t.Fatalf("err = %v, want *StaleError", err)
	}
	if stale.Current != current {
		t.Fatalf("StaleError.Current = %d, want %d", stale.Current, current)
	}
}

func str(s string) *string { return &s }

func ts(s string) api.Time {
	t, err := time.Parse(api.TimeLayout, s)
	if err != nil {
		panic(err)
	}
	return api.Time{Time: t}
}
