package server

import (
	"context"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/NurramoX/thoughts/internal/api"
	"github.com/NurramoX/thoughts/internal/store"
)

// fakeStore is a minimal in-memory store.Store that keeps the interface's
// promises the server relies on: versions advance only on a real change,
// Preconditions are checked when not zero, status defaults to raw and cannot
// be removed, and bodies over api.MaxBody are ErrTooLarge. It does no other
// validation beyond an empty title. List ignores the filter and records the
// Query it was given.
type fakeStore struct {
	mu        sync.Mutex
	thoughts  map[int64]*api.Thought
	next      int64
	lastQuery store.Query
	listed    bool
}

func newFakeStore() *fakeStore { return &fakeStore{thoughts: map[int64]*api.Thought{}, next: 1} }

var fakeNow = api.Time{Time: time.Date(2026, 9, 22, 14, 3, 7, 412e6, time.UTC)}

func (f *fakeStore) Create(_ context.Context, req api.CreateRequest) (api.Thought, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.TrimSpace(req.Title) == "" {
		return api.Thought{}, &store.InvalidError{Msg: "title is empty"}
	}
	if len(req.Body) > api.MaxBody {
		return api.Thought{}, store.ErrTooLarge
	}
	attrs := map[string]string{api.StatusKey: api.StatusRaw}
	maps.Copy(attrs, req.Attributes)
	tags := slices.Clone(req.Tags)
	if tags == nil {
		tags = []string{}
	}
	slices.Sort(tags)
	thought := &api.Thought{
		Meta: api.Meta{ID: f.next, Title: req.Title, Tags: tags, Attributes: attrs,
			Version: 1, CreatedAt: fakeNow, UpdatedAt: fakeNow},
		Body: req.Body,
	}
	f.thoughts[thought.ID] = thought
	f.next++
	return clone(thought), nil
}

func (f *fakeStore) Get(_ context.Context, id int64) (api.Thought, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	thought, ok := f.thoughts[id]
	if !ok {
		return api.Thought{}, store.ErrNotFound
	}
	return clone(thought), nil
}

func (f *fakeStore) List(_ context.Context, q store.Query) (api.List, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastQuery, f.listed = q, true
	var ids []int64
	for id := range f.thoughts {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	l := api.List{Total: len(ids)}
	for _, id := range ids {
		l.Thoughts = append(l.Thoughts, clone(f.thoughts[id]).Meta)
	}
	return l, nil
}

// write finds the thought, checks pre, applies change and advances the version
// when change reports a real change.
func (f *fakeStore) write(id int64, pre api.Precondition, change func(*api.Thought) (bool, error)) (int64, error) {
	thought, ok := f.thoughts[id]
	if !ok {
		return 0, store.ErrNotFound
	}
	if !pre.Force && pre.Version > 0 && pre.Version != thought.Version {
		return 0, &store.StaleError{Current: thought.Version}
	}
	changed, err := change(thought)
	if err != nil {
		return 0, err
	}
	if changed {
		thought.Version++
	}
	return thought.Version, nil
}

func (f *fakeStore) Patch(_ context.Context, id int64, pre api.Precondition, p api.Patch) (api.Thought, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, err := f.write(id, pre, func(thought *api.Thought) (bool, error) {
		changed := false
		if p.Title != nil && *p.Title != thought.Title {
			if strings.TrimSpace(*p.Title) == "" {
				return false, &store.InvalidError{Msg: "title is empty"}
			}
			thought.Title, changed = *p.Title, true
		}
		if p.Body != nil && *p.Body != thought.Body {
			thought.Body, changed = *p.Body, true
		}
		if p.Tags != nil {
			tags := slices.Sorted(slices.Values(*p.Tags))
			if !slices.Equal(tags, thought.Tags) {
				thought.Tags, changed = tags, true
			}
		}
		for k, v := range p.Attributes {
			c, err := setAttr(thought, k, v)
			if err != nil {
				return false, err
			}
			changed = changed || c
		}
		return changed, nil
	})
	if err != nil {
		return api.Thought{}, err
	}
	return clone(f.thoughts[id]), nil
}

func setAttr(thought *api.Thought, k string, v *string) (bool, error) {
	old, had := thought.Attributes[k]
	if v == nil {
		if k == api.StatusKey {
			return false, &store.InvalidError{Msg: "status cannot be removed"}
		}
		delete(thought.Attributes, k)
		return had, nil
	}
	if *v == "" {
		return false, &store.InvalidError{Msg: "empty value"}
	}
	thought.Attributes[k] = *v
	return !had || old != *v, nil
}

func (f *fakeStore) PutBody(_ context.Context, id int64, pre api.Precondition, body []byte) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.write(id, pre, func(thought *api.Thought) (bool, error) {
		if len(body) > api.MaxBody {
			return false, store.ErrTooLarge
		}
		changed := thought.Body != string(body)
		thought.Body = string(body)
		return changed, nil
	})
}

func (f *fakeStore) PutTag(_ context.Context, id int64, pre api.Precondition, tag string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.write(id, pre, func(thought *api.Thought) (bool, error) {
		if slices.Contains(thought.Tags, tag) {
			return false, nil
		}
		thought.Tags = append(thought.Tags, tag)
		slices.Sort(thought.Tags)
		return true, nil
	})
}

func (f *fakeStore) DeleteTag(_ context.Context, id int64, pre api.Precondition, tag string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.write(id, pre, func(thought *api.Thought) (bool, error) {
		i := slices.Index(thought.Tags, tag)
		if i < 0 {
			return false, nil
		}
		thought.Tags = slices.Delete(thought.Tags, i, i+1)
		return true, nil
	})
}

func (f *fakeStore) PutAttribute(_ context.Context, id int64, pre api.Precondition, key, value string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.write(id, pre, func(thought *api.Thought) (bool, error) {
		return setAttr(thought, key, &value)
	})
}

func (f *fakeStore) DeleteAttribute(_ context.Context, id int64, pre api.Precondition, key string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.write(id, pre, func(thought *api.Thought) (bool, error) {
		return setAttr(thought, key, nil)
	})
}

func (f *fakeStore) Delete(_ context.Context, id int64, pre api.Precondition) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.write(id, pre, func(*api.Thought) (bool, error) { return false, nil }); err != nil {
		return err
	}
	delete(f.thoughts, id)
	return nil
}

func (f *fakeStore) Tags(context.Context) ([]api.TagCount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	counts := map[string]int{}
	for _, thought := range f.thoughts {
		for _, t := range thought.Tags {
			counts[t]++
		}
	}
	var out []api.TagCount
	for _, t := range slices.Sorted(maps.Keys(counts)) {
		out = append(out, api.TagCount{Tag: t, Count: counts[t]})
	}
	return out, nil
}

func (f *fakeStore) Attributes(context.Context) ([]api.KeyCount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	counts := map[string]int{}
	for _, thought := range f.thoughts {
		for k := range thought.Attributes {
			counts[k]++
		}
	}
	var out []api.KeyCount
	for _, k := range slices.Sorted(maps.Keys(counts)) {
		out = append(out, api.KeyCount{Key: k, Count: counts[k]})
	}
	return out, nil
}

func (f *fakeStore) AttributeValues(_ context.Context, key string) ([]api.ValueCount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	counts := map[string]int{}
	for _, thought := range f.thoughts {
		if v, ok := thought.Attributes[key]; ok {
			counts[v]++
		}
	}
	var out []api.ValueCount
	for _, v := range slices.Sorted(maps.Keys(counts)) {
		out = append(out, api.ValueCount{Value: v, Count: counts[v]})
	}
	return out, nil
}

func (f *fakeStore) Close() error { return nil }

func clone(thought *api.Thought) api.Thought {
	c := *thought
	c.Tags = slices.Clone(thought.Tags)
	c.Attributes = maps.Clone(thought.Attributes)
	return c
}
