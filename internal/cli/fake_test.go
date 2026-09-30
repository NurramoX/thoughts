package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NurramoX/thoughts/internal/api"
)

// fakeAPI is an in-memory stand-in for the daemon, good enough for the CLI:
// it keeps thoughts, honours If-Match, and understands a toy filter (words of
// the form tag:x and key:v, anything else matched against titles, and a "(("
// that is a parse error).
type fakeAPI struct {
	mu       sync.Mutex
	api      int
	thoughts map[int64]*api.Thought
	next     int64
	clock    time.Time
	// hook, when set, sees every request first and may answer it.
	hook func(w http.ResponseWriter, r *http.Request) bool
	// log is every request as "METHOD /path If-Match".
	log []string
	srv *http.Server
}

// startFake serves a fake API at <dir>/thoughts.sock.
func startFake(t *testing.T, dir string) *fakeAPI {
	t.Helper()
	f, err := serveFake(t, dir)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// serveFake is startFake for goroutines, which must not call t.Fatal.
func serveFake(t *testing.T, dir string) (*fakeAPI, error) {
	f := &fakeAPI{api: api.APIVersion, thoughts: map[int64]*api.Thought{}, clock: time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)}
	l, err := net.Listen("unix", filepath.Join(dir, "thoughts.sock"))
	if err != nil {
		return nil, err
	}
	f.srv = &http.Server{Handler: f}
	go f.srv.Serve(l)
	t.Cleanup(func() { f.srv.Close() })
	return f, nil
}

// shortDir is a fresh directory under /tmp: macOS caps socket paths at 104
// bytes, which t.TempDir() can exceed.
func shortDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", "idt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

// add stores a thought directly and returns it.
func (f *fakeAPI) add(title, body string, tags []string, attrs map[string]string) *api.Thought {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.create(title, body, tags, attrs)
}

func (f *fakeAPI) create(title, body string, tags []string, attrs map[string]string) *api.Thought {
	f.next++
	f.clock = f.clock.Add(time.Minute)
	a := map[string]string{"status": "raw"}
	for k, v := range attrs {
		a[k] = v
	}
	if tags == nil {
		tags = []string{}
	}
	slices.Sort(tags)
	thought := &api.Thought{Meta: api.Meta{
		ID: f.next, Title: title, Tags: tags, Attributes: a, Version: 1,
		CreatedAt: api.Time{Time: f.clock}, UpdatedAt: api.Time{Time: f.clock},
	}, Body: body}
	f.thoughts[thought.ID] = thought
	return thought
}

// get returns a copy of a thought.
func (f *fakeAPI) get(id int64) api.Thought {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.thoughts[id]
	if i == nil {
		return api.Thought{}
	}
	return *i
}

func (f *fakeAPI) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.log)
}

func problem(w http.ResponseWriter, status int, detail string, extra map[string]any) {
	doc := map[string]any{"title": http.StatusText(status), "status": status}
	if detail != "" {
		doc["detail"] = detail
	}
	for k, v := range extra {
		doc[k] = v
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(doc)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// touch records a real change.
func (f *fakeAPI) touch(i *api.Thought) {
	f.clock = f.clock.Add(time.Minute)
	i.Version++
	i.UpdatedAt = api.Time{Time: f.clock}
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if f.hook != nil && f.hook(w, r) {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log = append(f.log, strings.TrimSpace(r.Method+" "+r.URL.RequestURI()+" "+r.Header.Get("If-Match")))

	parts := strings.Split(strings.Trim(r.URL.EscapedPath(), "/"), "/")
	for i, p := range parts {
		parts[i], _ = unescape(p)
	}
	switch {
	case r.URL.Path == "/":
		writeJSON(w, 200, api.Service{Service: "thoughts", API: f.api})
	case r.URL.Path == "/thoughts" && r.Method == "POST":
		var req api.CreateRequest
		json.NewDecoder(r.Body).Decode(&req)
		if strings.TrimSpace(req.Title) == "" {
			problem(w, 422, "title is required", nil)
			return
		}
		if s, ok := req.Attributes["status"]; ok && !slices.Contains(api.Statuses, s) {
			problem(w, 422, "unknown status '"+s+"'", nil)
			return
		}
		i := f.create(req.Title, req.Body, req.Tags, req.Attributes)
		w.Header().Set("ETag", api.ETag(i.Version))
		writeJSON(w, 201, i)
	case r.URL.Path == "/thoughts":
		f.list(w, r)
	case parts[0] == "tags":
		counts := map[string]int{}
		for _, i := range f.thoughts {
			for _, t := range i.Tags {
				counts[t]++
			}
		}
		var out []api.TagCount
		for _, k := range sortedKeys(counts) {
			out = append(out, api.TagCount{Tag: k, Count: counts[k]})
		}
		writeJSON(w, 200, nonNil(out))
	case parts[0] == "attributes" && len(parts) == 1:
		counts := map[string]int{}
		for _, i := range f.thoughts {
			for k := range i.Attributes {
				counts[k]++
			}
		}
		var out []api.KeyCount
		for _, k := range sortedKeys(counts) {
			out = append(out, api.KeyCount{Key: k, Count: counts[k]})
		}
		writeJSON(w, 200, nonNil(out))
	case parts[0] == "attributes":
		counts := map[string]int{}
		for _, i := range f.thoughts {
			if v, ok := i.Attributes[parts[1]]; ok {
				counts[v]++
			}
		}
		var out []api.ValueCount
		for _, k := range sortedKeys(counts) {
			out = append(out, api.ValueCount{Value: k, Count: counts[k]})
		}
		writeJSON(w, 200, nonNil(out))
	case parts[0] == "thoughts":
		id, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			problem(w, 400, "bad id", nil)
			return
		}
		i := f.thoughts[id]
		if i == nil {
			problem(w, 404, fmt.Sprintf("no thought %d", id), nil)
			return
		}
		f.thought(w, r, i, parts[2:])
	default:
		problem(w, 404, "", nil)
	}
}

func unescape(s string) (string, error) {
	r, err := http.NewRequest("GET", "http://x/"+s, nil)
	if err != nil {
		return s, err
	}
	return strings.TrimPrefix(r.URL.Path, "/"), nil
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func sortedKeys(m map[string]int) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// precondition checks If-Match; required says whether a missing one is 428.
func precondition(w http.ResponseWriter, r *http.Request, i *api.Thought, required bool) bool {
	h := r.Header.Get("If-Match")
	switch {
	case h == "":
		if required {
			problem(w, 428, "If-Match is required", nil)
			return false
		}
	case h == "*":
	default:
		v, _ := api.ParseETag(h)
		if v != i.Version {
			problem(w, 412, fmt.Sprintf("thought %d is at version %d", i.ID, i.Version), map[string]any{"current_version": i.Version})
			return false
		}
	}
	return true
}

func (f *fakeAPI) thought(w http.ResponseWriter, r *http.Request, i *api.Thought, rest []string) {
	done := func() {
		w.Header().Set("ETag", api.ETag(i.Version))
		w.WriteHeader(204)
	}
	switch {
	case len(rest) == 0 && r.Method == "GET":
		if r.Header.Get("If-None-Match") == api.ETag(i.Version) {
			w.WriteHeader(304)
			return
		}
		w.Header().Set("ETag", api.ETag(i.Version))
		writeJSON(w, 200, i)
	case len(rest) == 0 && r.Method == "PATCH":
		if !precondition(w, r, i, true) {
			return
		}
		var p api.Patch
		json.NewDecoder(r.Body).Decode(&p)
		if p.Title != nil && *p.Title != i.Title {
			i.Title = *p.Title
			f.touch(i)
		}
		w.Header().Set("ETag", api.ETag(i.Version))
		writeJSON(w, 200, api.PatchedThought{Meta: i.Meta})
	case len(rest) == 0 && r.Method == "DELETE":
		if !precondition(w, r, i, true) {
			return
		}
		delete(f.thoughts, i.ID)
		w.WriteHeader(204)
	case rest[0] == "body" && r.Method == "GET":
		w.Header().Set("ETag", api.ETag(i.Version))
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		io.WriteString(w, i.Body)
	case rest[0] == "body" && r.Method == "PUT":
		if !precondition(w, r, i, true) {
			return
		}
		b, _ := io.ReadAll(r.Body)
		if string(b) != i.Body {
			i.Body = string(b)
			f.touch(i)
		}
		done()
	case rest[0] == "tags":
		if !precondition(w, r, i, false) {
			return
		}
		t := rest[1]
		has := slices.Contains(i.Tags, t)
		if r.Method == "PUT" && !has {
			i.Tags = append(i.Tags, t)
			slices.Sort(i.Tags)
			f.touch(i)
		}
		if r.Method == "DELETE" && has {
			i.Tags = slices.DeleteFunc(i.Tags, func(s string) bool { return s == t })
			f.touch(i)
		}
		done()
	case rest[0] == "attributes":
		if !precondition(w, r, i, false) {
			return
		}
		k := rest[1]
		if r.Method == "PUT" {
			b, _ := io.ReadAll(r.Body)
			v := string(b)
			if k == "status" && !slices.Contains(api.Statuses, v) {
				problem(w, 422, "unknown status '"+v+"'", nil)
				return
			}
			if i.Attributes[k] != v {
				i.Attributes[k] = v
				f.touch(i)
			}
		} else {
			if k == "status" {
				problem(w, 422, "status cannot be removed", nil)
				return
			}
			if _, ok := i.Attributes[k]; ok {
				delete(i.Attributes, k)
				f.touch(i)
			}
		}
		done()
	default:
		problem(w, 405, "", nil)
	}
}

func (f *fakeAPI) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	src := q.Get("filter")
	if p := strings.Index(src, "(("); p >= 0 {
		problem(w, 400, "unexpected '('", map[string]any{"position": len([]rune(src[:p])) + 2})
		return
	}
	words := strings.Fields(src)
	ranked := false
	var out []api.Meta
	for _, i := range f.thoughts {
		ok := true
		for _, word := range words {
			k, v, isAttr := strings.Cut(word, ":")
			switch {
			case isAttr && k == "tag":
				ok = ok && slices.Contains(i.Tags, v)
			case isAttr:
				ok = ok && i.Attributes[k] == v
			default:
				ranked = true
				ok = ok && strings.Contains(strings.ToLower(i.Title), strings.ToLower(word))
			}
		}
		if ok {
			m := i.Meta
			if ranked {
				m.Snippet = i.Body
			}
			out = append(out, m)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].UpdatedAt.After(out[b].UpdatedAt.Time) })
	if q.Get("order") == "asc" {
		slices.Reverse(out)
	}
	total := len(out)
	if o, _ := strconv.Atoi(q.Get("offset")); o > 0 {
		out = out[min(o, len(out)):]
	}
	if l, _ := strconv.Atoi(q.Get("limit")); l > 0 {
		out = out[:min(l, len(out))]
	}
	writeJSON(w, 200, api.List{Thoughts: nonNil(out), Total: total})
}
