package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/NurramoX/thoughts/internal/api"
)

func TestAdd(t *testing.T) {
	e := newEnv(t)
	run("", "add", "Borrow", "checker", "-t", "rust", "-s", "effort=small=ish", "-t", "lang").expect(t, 0, "1\n", "")
	got := e.api.get(1)
	if got.Title != "Borrow checker" || !reflect.DeepEqual(got.Tags, []string{"lang", "rust"}) ||
		got.Attributes["effort"] != "small=ish" || got.Body != "" {
		t.Errorf("stored %+v", got)
	}
}

func TestAddNeverSniffsStdin(t *testing.T) {
	e := newEnv(t)
	run("ignored", "add", "T").expect(t, 0, "1\n", "")
	if b := e.api.get(1).Body; b != "" {
		t.Errorf("body %q", b)
	}
	run("from stdin\n", "add", "T", "--body-file", "-").expect(t, 0, "2\n", "")
	if b := e.api.get(2).Body; b != "from stdin\n" {
		t.Errorf("body %q", b)
	}
	p := filepath.Join(t.TempDir(), "b.md")
	os.WriteFile(p, []byte("no newline"), 0o644)
	run("", "add", "--body-file="+p, "T").expect(t, 0, "3\n", "")
	if b := e.api.get(3).Body; b != "no newline" {
		t.Errorf("body %q", b)
	}
}

func TestAddJSON(t *testing.T) {
	newEnv(t)
	run("", "add", "T", "--json").expect(t, 0, `{"id":1,"version":1}`+"\n", "")
}

func TestAddUsage(t *testing.T) {
	e := newEnv(t)
	for _, args := range [][]string{
		{"add"},
		{"add", "T", "-s", "effort"},
		{"add", "T", "-s", "effort="},
		{"add", "T", "-x"},
		{"add", "T", "-t"},
		{"add", "T", "--edit=yes"},
	} {
		if r := run("", args...); r.code != 2 || r.out != "" || !strings.HasPrefix(r.err, "thought add: ") {
			t.Errorf("%q: got %+v", args, r)
		}
	}
	if n := len(e.api.requests()); n != 0 {
		t.Errorf("%d requests sent", n)
	}
}

func TestAddRejected(t *testing.T) {
	newEnv(t)
	run("", "add", "T", "-s", "status=later").expect(t, 5, "", "thought: Unprocessable Entity: unknown status 'later'\n")
	r := run("", "add", "T", "-s", "status=later", "--json")
	if r.code != 5 || !strings.HasPrefix(r.err, `{"detail":"unknown status 'later'"`) {
		t.Errorf("got %+v", r)
	}
}

func TestShow(t *testing.T) {
	e := newEnv(t)
	e.api.add("First", "body one\n", []string{"a", "b"}, map[string]string{"effort": "small", "area": "cli"})
	e.api.add("Second", "no newline", nil, nil)
	r := run("", "show", "1", "2")
	created := when(api.Time{Time: e.api.get(1).CreatedAt.Time})
	created2 := when(api.Time{Time: e.api.get(2).CreatedAt.Time})
	want := "" +
		"title       First\n" +
		"id          1\n" +
		"status      raw\n" +
		"tags        a b\n" +
		"attributes  area=cli\n" +
		"            effort=small\n" +
		"version     1\n" +
		"created     " + created + "\n" +
		"updated     " + created + "\n" +
		"\n" +
		"body one\n" +
		"---\n" +
		"title       Second\n" +
		"id          2\n" +
		"status      raw\n" +
		"version     1\n" +
		"created     " + created2 + "\n" +
		"updated     " + created2 + "\n" +
		"\n" +
		"no newline\n"
	r.expect(t, 0, want, "")
}

func TestShowBatchContinuesPastFailures(t *testing.T) {
	e := newEnv(t)
	e.api.add("A", "", nil, nil)
	r := run("", "show", "9", "1", "8", "--json")
	if r.code != 3 {
		t.Errorf("code %d", r.code)
	}
	lines := strings.Split(strings.TrimSuffix(r.out, "\n"), "\n")
	if len(lines) != 1 || !strings.HasPrefix(lines[0], `{"id":1,"title":"A",`) {
		t.Errorf("stdout %q", r.out)
	}
	if n := strings.Count(r.err, "\n"); n != 2 {
		t.Errorf("stderr %q", r.err)
	}
}

func TestShowIDs(t *testing.T) {
	newEnv(t)
	for _, id := range []string{"x", "0", "-1", "+1", "01", "1.0"} {
		if r := run("", "show", "--", id); r.code != 2 {
			t.Errorf("%q: got %+v", id, r)
		}
	}
	if r := run("", "show"); r.code != 2 {
		t.Errorf("no id: got %+v", r)
	}
}

func TestBodyIsByteExact(t *testing.T) {
	e := newEnv(t)
	for _, b := range []string{"", "x", "x\n", "x\n\n", "\n"} {
		i := e.api.add("T", b, nil, nil)
		r := run("", "body", itoa(i.ID))
		r.expect(t, 0, b, "")
	}
	run("", "body", "99").expect(t, 3, "", "thought: Not Found: no thought 99\n")
}

func TestWrite(t *testing.T) {
	e := newEnv(t)
	e.api.add("T", "old", nil, nil)
	if r := run("new", "write", "1"); r.code != 2 {
		t.Errorf("without --version: %+v", r)
	}
	if r := run("new", "write", "1", "--version", "1", "--force"); r.code != 2 {
		t.Errorf("both: %+v", r)
	}
	if r := run("new", "write", "1", "--version", "x"); r.code != 2 {
		t.Errorf("bad version: %+v", r)
	}
	run("new\n\n", "write", "1", "--version", "1", "--json").expect(t, 0, `{"id":1,"version":2}`+"\n", "")
	if b := e.api.get(1).Body; b != "new\n\n" {
		t.Errorf("body %q", b)
	}
	run("x", "write", "1", "--version", "1").expect(t, 4, "", "thought 1 changed: you had version 1, it is now 2\n")
	run("", "write", "1", "--force").expect(t, 0, "", "")
	if b := e.api.get(1).Body; b != "" {
		t.Errorf("empty input wrote %q", b)
	}
	got := e.api.requests()
	if last := got[len(got)-1]; last != "PUT /thoughts/1/body *" {
		t.Errorf("last request %q", last)
	}
}

func TestWriteStaleJSON(t *testing.T) {
	e := newEnv(t)
	e.api.add("T", "old", nil, nil)
	r := run("x", "write", "1", "--version", "5", "--json")
	if r.code != 4 || r.out != "" || !strings.Contains(r.err, `"current_version":1`) || strings.Contains(r.err, "changed:") {
		t.Errorf("got %+v", r)
	}
}

func TestTitle(t *testing.T) {
	e := newEnv(t)
	e.api.add("Old", "", nil, nil)
	run("", "title", "1", "New", "", "title", "--json").expect(t, 0, `{"id":1,"version":2}`+"\n", "")
	if got := e.api.get(1).Title; got != "New  title" {
		t.Errorf("title %q", got)
	}
	reqs := e.api.requests()
	if reqs[1] != "GET /thoughts/1" || reqs[2] != `PATCH /thoughts/1 "1"` {
		t.Errorf("requests %q", reqs)
	}
	run("", "title", "1", "X", "--version", "1").expect(t, 4, "", "thought 1 changed: you had version 1, it is now 2\n")
	run("", "title", "1", "Y", "--force").expect(t, 0, "", "")
	if got := e.api.requests(); got[len(got)-2] != "GET /" || got[len(got)-1] != "PATCH /thoughts/1 *" {
		t.Errorf("--force must skip the GET: %q", got)
	}
}

func TestFlagsAnywhereAndDoubleDash(t *testing.T) {
	e := newEnv(t)
	run("", "add", "--json", "-t", "x", "--", "-5", "--json").expect(t, 0, `{"id":1,"version":1}`+"\n", "")
	if got := e.api.get(1).Title; got != "-5 --json" {
		t.Errorf("title %q", got)
	}
}

func TestRm(t *testing.T) {
	e := newEnv(t)
	e.api.add("A", "", nil, nil)
	e.api.add("B", "", nil, nil)
	if r := run("", "rm", "1"); r.code != 2 || !strings.Contains(r.err, "-y") {
		t.Errorf("without terminal: %+v", r)
	}
	if len(e.api.requests()) != 0 {
		t.Error("refusal reached the server")
	}
	r := run("", "rm", "1", "5", "2", "-y")
	if r.code != 3 || r.out != "" {
		t.Errorf("got %+v", r)
	}
	if e.api.get(1).ID != 0 || e.api.get(2).ID != 0 {
		t.Error("not deleted")
	}
	reqs := e.api.requests()
	if !reflect.DeepEqual(reqs[len(reqs)-2:], []string{"GET /thoughts/2", `DELETE /thoughts/2 "1"`}) {
		t.Errorf("requests %q", reqs)
	}
}

func TestRmPrompts(t *testing.T) {
	e := newEnv(t)
	e.api.add("Keep me", "", nil, nil)
	e.api.add("Drop me", "", nil, nil)
	terminal(t)
	run("n\ny\n", "rm", "1", "2").expect(t, 0, "", `delete thought 1 "Keep me"? [y/N] delete thought 2 "Drop me"? [y/N] `)
	if e.api.get(1).ID != 1 || e.api.get(2).ID != 0 {
		t.Error("wrong thought deleted")
	}
}

func TestRmForce(t *testing.T) {
	e := newEnv(t)
	e.api.add("A", "", nil, nil)
	run("", "rm", "1", "-y", "--force").expect(t, 0, "", "")
	if got := e.api.requests(); !reflect.DeepEqual(got, []string{"GET /", "DELETE /thoughts/1 *"}) {
		t.Errorf("requests %q", got)
	}
}

// terminal makes stdin and stdout count as a terminal for one test.
func terminal(t *testing.T) {
	old := isTerminal
	isTerminal = func(any) bool { return true }
	t.Cleanup(func() { isTerminal = old })
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
