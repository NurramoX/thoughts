package cli

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// editorScript makes $EDITOR a shell script with body, and $TMPDIR a fresh
// directory that must be empty again when the test ends.
func editorScript(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "ed")
	writeFile(t, p, "#!/bin/sh\n"+body)
	os.Chmod(p, 0o755)
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", p)
	tmp := filepath.Join(dir, "tmp")
	os.Mkdir(tmp, 0o700)
	t.Setenv("TMPDIR", tmp)
	t.Cleanup(func() {
		if left, _ := os.ReadDir(tmp); len(left) > 0 {
			t.Errorf("temp files left behind: %v", left)
		}
	})
}

const appendPlus = `printf + >> "$1"`

func TestEdit(t *testing.T) {
	e := newEnv(t)
	e.api.add("A", "base", nil, nil)
	editorScript(t, appendPlus)
	run("", "edit", "1", "--json").expect(t, 0, `{"id":1,"version":2}`+"\n", "")
	if b := e.api.get(1).Body; b != "base+" {
		t.Errorf("body %q", b)
	}
	if got := e.api.requests(); got[len(got)-1] != `PUT /thoughts/1/body "1"` {
		t.Errorf("requests %q", got)
	}
}

func TestEditUnchangedWritesNothing(t *testing.T) {
	e := newEnv(t)
	e.api.add("A", "base", nil, nil)
	editorScript(t, "true")
	run("", "edit", "1").expect(t, 0, "", "")
	for _, r := range e.api.requests() {
		if strings.HasPrefix(r, "PUT") {
			t.Errorf("wrote: %q", r)
		}
	}
}

func TestEditorFailureWritesNothing(t *testing.T) {
	e := newEnv(t)
	e.api.add("A", "base", nil, nil)
	editorScript(t, appendPlus+"; exit 3")
	r := run("", "edit", "1")
	if r.code != 1 || !strings.Contains(r.err, "nothing written") {
		t.Errorf("got %+v", r)
	}
	if b := e.api.get(1).Body; b != "base" {
		t.Errorf("body %q", b)
	}
}

func TestEditStaleWithoutTerminalAborts(t *testing.T) {
	e := newEnv(t)
	e.api.add("A", "base", nil, nil)
	editorScript(t, appendPlus)
	concurrent(e, 1)
	run("", "edit", "1").expect(t, 4, "base+", "thought 1 changed: you had version 1, it is now 2\n")
	if b := e.api.get(1).Body; b != "base!" {
		t.Errorf("body %q", b)
	}
}

func TestEditStaleOverwrite(t *testing.T) {
	e := newEnv(t)
	e.api.add("A", "base", nil, nil)
	editorScript(t, appendPlus)
	concurrent(e, 1)
	terminal(t)
	run("o\n", "edit", "1").expect(t, 0, "", "thought 1 changed: you had version 1, it is now 2\n[o]verwrite, [r]e-edit, [a]bort? ")
	if b := e.api.get(1).Body; b != "base+" {
		t.Errorf("body %q", b)
	}
	if got := e.api.requests(); got[len(got)-1] != "PUT /thoughts/1/body *" {
		t.Errorf("requests %q", got)
	}
}

func TestEditStaleReEdit(t *testing.T) {
	e := newEnv(t)
	e.api.add("A", "base", nil, nil)
	editorScript(t, appendPlus)
	concurrent(e, 1)
	terminal(t)
	run("?\nr\n", "edit", "1", "--json").expect(t, 0, `{"id":1,"version":3}`+"\n",
		`{"current_version":2,"detail":"thought 1 is at version 2","status":412,"title":"Precondition Failed"}`+"\n"+
			"[o]verwrite, [r]e-edit, [a]bort? [o]verwrite, [r]e-edit, [a]bort? "+
			"--- thought 1, version 2, as it is now:\nbase!\n")
	if b := e.api.get(1).Body; b != "base++" {
		t.Errorf("body %q", b)
	}
	if got := e.api.requests(); got[len(got)-1] != `PUT /thoughts/1/body "2"` {
		t.Errorf("requests %q", got)
	}
}

func TestEditStaleAbort(t *testing.T) {
	e := newEnv(t)
	e.api.add("A", "base", nil, nil)
	editorScript(t, appendPlus)
	concurrent(e, 1)
	terminal(t)
	run("a\n", "edit", "1").expect(t, 4, "base+", "thought 1 changed: you had version 1, it is now 2\n[o]verwrite, [r]e-edit, [a]bort? ")
}

func TestEditRejectedKeepsTheText(t *testing.T) {
	e := newEnv(t)
	e.api.add("A", "base", nil, nil)
	editorScript(t, appendPlus)
	e.api.hook = func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method == "PUT" {
			problem(w, 413, "too big", nil)
			return true
		}
		return false
	}
	run("", "edit", "1").expect(t, 5, "base+", "thought: Request Entity Too Large: too big\n")
}

func TestEditNotFound(t *testing.T) {
	newEnv(t)
	editorScript(t, appendPlus)
	run("", "edit", "9").expect(t, 3, "", "thought: Not Found: no thought 9\n")
}

func TestAddEdit(t *testing.T) {
	e := newEnv(t)
	editorScript(t, `printf 'written\n' >> "$1"`)
	body := filepath.Join(t.TempDir(), "seed")
	writeFile(t, body, "seed\n")
	run("", "add", "T", "--edit", "--body-file", body).expect(t, 0, "1\n", "")
	if b := e.api.get(1).Body; b != "seed\nwritten\n" {
		t.Errorf("body %q", b)
	}
}

func TestAddEditUnchangedCreatesNothing(t *testing.T) {
	e := newEnv(t)
	editorScript(t, "true")
	r := run("", "add", "T", "--edit")
	if r.code != 1 || r.out != "" || !strings.Contains(r.err, "nothing created") {
		t.Errorf("got %+v", r)
	}
	if e.api.get(1).ID != 0 {
		t.Error("created")
	}
}
