package cli

import (
	"net/http"
	"strings"
	"testing"
)

func TestLsTable(t *testing.T) {
	e := newEnv(t)
	e.api.add("Borrow checker", "Config files\n  could be checked", []string{"rust"}, nil)
	e.api.add("Thoughts CLI", "", []string{"go", "cli"}, map[string]string{"status": "active"})
	run("", "ls").expect(t, 0, ""+
		"2  active  Thoughts CLI    cli,go\n"+
		"1  raw     Borrow checker  rust\n", "")
	run("", "ls", "borrow").expect(t, 0, "1  raw  Borrow checker  rust  Config files could be checked\n", "")
	run("", "ls", "--limit", "1").expect(t, 0, "2  active  Thoughts CLI  cli,go\n", "2 thoughts\n")
}

func TestLsColour(t *testing.T) {
	e := newEnv(t)
	e.api.add("A", "", []string{"x"}, nil)
	terminal(t)
	run("", "ls").expect(t, 0, "1  \x1b[33mraw\x1b[0m  A  \x1b[2mx\x1b[0m\n", "")
	t.Setenv("NO_COLOR", "1")
	run("", "ls").expect(t, 0, "1  raw  A  x\n", "")
}

func TestLsFilterAndParams(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 5; i++ {
		e.api.add("T", "", []string{"x"}, nil)
	}
	run("", "ls", "tag:x", "--limit", "2", "--offset=1", "--sort", "created", "--asc", "-q").expect(t, 0, "2\n3\n", "")
	reqs := e.api.requests()
	if got := reqs[len(reqs)-1]; got != "GET /thoughts?filter=tag%3Ax&limit=2&offset=1&order=asc&sort=created" {
		t.Errorf("request %q", got)
	}
	n := len(e.api.requests())
	run("", "ls", "--", "-tag:x").expect(t, 0, "", "")
	if got := e.api.requests(); got[n+1] != "GET /thoughts?filter=-tag%3Ax" {
		t.Errorf("request %q", got[n+1])
	}
}

func TestLsTakesNegatedTermsBare(t *testing.T) {
	e := newEnv(t)
	n := len(e.api.requests())
	run("", "ls", "status:raw,active", "-updated:90d..", "-q").expect(t, 0, "", "")
	if got := e.api.requests(); got[n+1] != "GET /thoughts?filter=status%3Araw%2Cactive+-updated%3A90d.." {
		t.Errorf("request %q", got[n+1])
	}
	if r := run("", "ls", "--sotr", "title"); r.code != 2 {
		t.Errorf("unknown long flag: got %+v", r)
	}
}

func TestLsUsage(t *testing.T) {
	e := newEnv(t)
	for _, args := range [][]string{
		{"ls", "-q", "--json"},
		{"ls", "--sort", "size"},
		{"ls", "--asc", "--desc"},
		{"ls", "--limit", "0"},
		{"ls", "--offset", "-1"},
		{"ls", "--limit", "x"},
	} {
		if r := run("", args...); r.code != 2 || r.out != "" {
			t.Errorf("%q: got %+v", args, r)
		}
	}
	if len(e.api.requests()) != 0 {
		t.Error("usage error reached the server")
	}
}

func TestLsJSON(t *testing.T) {
	e := newEnv(t)
	e.api.add("A", "", nil, nil)
	r := run("", "ls", "--json")
	if r.code != 0 || !strings.HasPrefix(r.out, `{"thoughts":[{"id":1,"title":"A","tags":[],"attributes":{"status":"raw"},"version":1,`) ||
		!strings.HasSuffix(r.out, `"total":1}`+"\n") || strings.Count(r.out, "\n") != 1 {
		t.Errorf("got %+v", r)
	}
}

func TestLsFilterError(t *testing.T) {
	newEnv(t)
	// Needs filter.Caret.
	run("", "ls", "tag:x", "((").expect(t, 6, "", "thought: Bad Request: unexpected '('\ntag:x ((\n       ^\n")
	r := run("", "ls", "((", "--json")
	if r.code != 6 || !strings.HasPrefix(r.err, `{"detail":"unexpected '('","position":2,`) {
		t.Errorf("json: %+v", r)
	}
}

func TestLsEmptyGivesTheVocabularyHint(t *testing.T) {
	e := newEnv(t)
	e.api.add("A", "", []string{"rust"}, nil)
	// Needs filter.Parse and filter.Vocabulary.
	run("", "ls", "tag:rsut", "status:raw").expect(t, 0, "", "no thoughts have the tag 'rsut'\n")
	run("", "ls", "tag:rust", "effort:big", "-q").expect(t, 0, "", "no thoughts have the key 'effort'\n")
}

func TestTagsAndAttrs(t *testing.T) {
	e := newEnv(t)
	e.api.add("A", "", []string{"rust", "go"}, map[string]string{"effort": "small"})
	e.api.add("B", "", []string{"rust"}, map[string]string{"effort": "large"})
	run("", "tags").expect(t, 0, "go    1\nrust  2\n", "")
	run("", "tags", "--json").expect(t, 0, `[{"tag":"go","count":1},{"tag":"rust","count":2}]`+"\n", "")
	run("", "attrs").expect(t, 0, "effort  2\nstatus  2\n", "")
	run("", "attrs", "effort", "--json").expect(t, 0, `[{"value":"large","count":1},{"value":"small","count":1}]`+"\n", "")
	if r := run("", "attrs", "a", "b"); r.code != 2 {
		t.Errorf("two keys: %+v", r)
	}
}

func TestLabels(t *testing.T) {
	e := newEnv(t)
	e.api.add("A", "", nil, nil)
	run("", "tag", "1", "b", "a").expect(t, 0, "", "")
	run("", "untag", "1", "b", "--json").expect(t, 0, `{"id":1,"version":4}`+"\n", "")
	run("", "set", "1", "effort=small", "url=a=b").expect(t, 0, "", "")
	run("", "unset", "1", "effort").expect(t, 0, "", "")
	run("", "status", "1", "done", "--json").expect(t, 0, `{"id":1,"version":8}`+"\n", "")
	got := e.api.get(1)
	if strings.Join(got.Tags, " ") != "a" || got.Attributes["url"] != "a=b" || got.Attributes["status"] != "done" || len(got.Attributes) != 2 {
		t.Errorf("thought %+v", got)
	}
	for _, r := range e.api.requests() {
		if strings.HasSuffix(r, `"`) || strings.HasSuffix(r, "*") {
			t.Errorf("unguarded verb sent If-Match: %q", r)
		}
	}
}

func TestLabelsStopAtFirstFailure(t *testing.T) {
	e := newEnv(t)
	e.api.add("A", "", nil, nil)
	r := run("", "set", "1", "a=1", "status=later", "b=2")
	if r.code != 5 || e.api.get(1).Attributes["b"] != "" || e.api.get(1).Attributes["a"] != "1" {
		t.Errorf("got %+v, thought %+v", r, e.api.get(1))
	}
	run("", "unset", "1", "status").expect(t, 5, "", "thought: Unprocessable Entity: status cannot be removed\n")
}

func TestLabelsUsage(t *testing.T) {
	e := newEnv(t)
	for _, args := range [][]string{
		{"tag", "1"},
		{"tag", "x", "a"},
		{"set", "1", "k=v", "effort="},
		{"set", "1", "novalue"},
		{"status", "1"},
		{"status", "1", "done", "raw"},
		{"tag", "1", "a", "--force"},
	} {
		if r := run("", args...); r.code != 2 {
			t.Errorf("%q: got %+v", args, r)
		}
	}
	if r := run("", "set", "1", "effort="); !strings.Contains(r.err, "unset") {
		t.Errorf("empty value should point at unset: %q", r.err)
	}
	if len(e.api.requests()) != 0 {
		t.Error("usage error reached the server")
	}
}

func TestLabelsWithVersion(t *testing.T) {
	e := newEnv(t)
	e.api.add("A", "", nil, nil)
	run("", "tag", "1", "a", "b", "--version", "1", "--json").expect(t, 0, `{"id":1,"version":3}`+"\n", "")
	reqs := e.api.requests()
	if reqs[1] != `PUT /thoughts/1/tags/a "1"` || reqs[2] != `PUT /thoughts/1/tags/b "2"` {
		t.Errorf("requests %q", reqs)
	}
	run("", "tag", "1", "c", "--version", "1").expect(t, 4, "", "thought 1 changed: you had version 1, it is now 3\n")
}

func TestReplace(t *testing.T) {
	e := newEnv(t)
	e.api.add("A", "one two one\nthree", nil, nil)
	run("", "replace", "1", "two", "2", "--json").expect(t, 0, `{"id":1,"version":2}`+"\n", "")
	if b := e.api.get(1).Body; b != "one 2 one\nthree" {
		t.Errorf("body %q", b)
	}
	r := run("", "replace", "1", "one", "1")
	if r.code != 5 || !strings.Contains(r.err, "occurs 2 times") {
		t.Errorf("two matches: %+v", r)
	}
	r = run("", "replace", "1", "zzz", "1")
	if r.code != 5 || !strings.Contains(r.err, "occurs 0 times") {
		t.Errorf("no match: %+v", r)
	}
	run("", "replace", "1", "one", "", "--all").expect(t, 0, "", "")
	if b := e.api.get(1).Body; b != " 2 \nthree" {
		t.Errorf("body %q", b)
	}
	if r := run("", "replace", "1", "", "x"); r.code != 2 {
		t.Errorf("empty old: %+v", r)
	}
}

func TestReplaceFiles(t *testing.T) {
	e := newEnv(t)
	e.api.add("A", "a\nb\n", nil, nil)
	dir := t.TempDir()
	old, repl := dir+"/old", dir+"/new"
	writeFile(t, old, "a\n")
	writeFile(t, repl, "c\n\n")
	run("", "replace", "1", "--old-file", old, "--new-file", repl).expect(t, 0, "", "")
	if b := e.api.get(1).Body; b != "c\n\nb\n" {
		t.Errorf("body %q", b)
	}
	run("", "replace", "1", "b\n", "--new-file", repl).expect(t, 0, "", "")
	if b := e.api.get(1).Body; b != "c\n\nc\n\n" {
		t.Errorf("body %q", b)
	}
	if r := run("", "replace", "1", "x", "--old-file", old, "--new-file", repl); r.code != 2 {
		t.Errorf("extra argument: %+v", r)
	}
}

// concurrent changes the thought behind the CLI's back on the first n body PUTs.
func concurrent(e *env, n int) *int {
	count := new(int)
	e.api.hook = func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/body") && *count < n {
			*count++
			e.api.mu.Lock()
			i := e.api.thoughts[1]
			i.Body += "!"
			e.api.touch(i)
			e.api.mu.Unlock()
		}
		return false
	}
	return count
}

func TestReplaceRetries(t *testing.T) {
	e := newEnv(t)
	e.api.add("A", "x", nil, nil)
	concurrent(e, 3)
	run("", "replace", "1", "x", "y").expect(t, 0, "", "")
	if b := e.api.get(1).Body; b != "y!!!" {
		t.Errorf("body %q", b)
	}
}

func TestReplaceGivesUpAfterThreeRetries(t *testing.T) {
	e := newEnv(t)
	e.api.add("A", "x", nil, nil)
	n := concurrent(e, 4)
	run("", "replace", "1", "x", "y").expect(t, 4, "", "thought 1 changed: you had version 4, it is now 5\n")
	if *n != 4 {
		t.Errorf("%d attempts", *n)
	}
}

func TestReplaceWithVersionDoesNotRetry(t *testing.T) {
	e := newEnv(t)
	e.api.add("A", "x", nil, nil)
	n := concurrent(e, 4)
	run("", "replace", "1", "x", "y", "--version", "1").expect(t, 4, "", "thought 1 changed: you had version 1, it is now 2\n")
	if *n != 1 {
		t.Errorf("%d attempts", *n)
	}
}
