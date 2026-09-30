package editor_test

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/NurramoX/thoughts/internal/editor"
)

// script writes an executable shell script and returns its path.
func script(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "ed")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func open(t *testing.T, id int64, body string) *editor.Session {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())
	s, err := editor.Open(id, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenWritesTheBodyPrivately(t *testing.T) {
	s := open(t, 42, "hello\n")
	b, err := os.ReadFile(s.Path())
	if err != nil || string(b) != "hello\n" {
		t.Fatalf("got %q %v", b, err)
	}
	fi, _ := os.Stat(s.Path())
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("file mode %v", fi.Mode().Perm())
	}
	di, _ := os.Stat(filepath.Dir(s.Path()))
	if di.Mode().Perm() != 0o700 {
		t.Errorf("dir mode %v", di.Mode().Perm())
	}
	name := filepath.Base(s.Path())
	if !strings.HasPrefix(name, "thought-42-") || !strings.HasSuffix(name, ".md") {
		t.Errorf("name %q", name)
	}
	if got, want := filepath.Dir(filepath.Dir(s.Path())), os.Getenv("TMPDIR"); got != filepath.Clean(want) {
		t.Errorf("not under $TMPDIR: %s", s.Path())
	}
}

func TestEditorPreference(t *testing.T) {
	visual := script(t, `for f; do :; done; printf visual >> "$f"`)
	ed := script(t, `printf editor >> "$1"`)
	cases := []struct {
		visual, editor, want string
	}{
		{visual, ed, "visual"},
		{"", ed, "editor"},
		{visual + " --flag", "", "visual"}, // may carry arguments
	}
	for _, tc := range cases {
		s := open(t, 1, "")
		t.Setenv("VISUAL", tc.visual)
		t.Setenv("EDITOR", tc.editor)
		if out, err := s.Command().CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		text, changed, err := s.Result()
		if err != nil || !changed || string(text) != tc.want {
			t.Errorf("got %q %v %v, want %q", text, changed, err, tc.want)
		}
	}
}

func TestDefaultsToVi(t *testing.T) {
	s := open(t, 1, "")
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	cmd := s.Command()
	if !strings.Contains(strings.Join(cmd.Args, " "), "vi ") {
		t.Errorf("args %q", cmd.Args)
	}
}

func TestPathWithSpaces(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "a b"))
	os.Mkdir(os.Getenv("TMPDIR"), 0o700)
	s, err := editor.Open(3, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	t.Setenv("VISUAL", script(t, `printf y >> "$1"`))
	if out, err := s.Command().CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if text, _, _ := s.Result(); string(text) != "xy" {
		t.Errorf("got %q", text)
	}
}

func TestUnchanged(t *testing.T) {
	s := open(t, 1, "same")
	t.Setenv("VISUAL", "true")
	s.Command().Run()
	text, changed, err := s.Result()
	if err != nil || changed || string(text) != "same" {
		t.Errorf("got %q %v %v", text, changed, err)
	}
}

func TestRebase(t *testing.T) {
	s := open(t, 1, "base")
	t.Setenv("VISUAL", script(t, `printf mine > "$1"`))
	s.Command().Run()
	s.Rebase([]byte("mine"))
	if _, changed, _ := s.Result(); changed {
		t.Error("changed against the new base")
	}
	s.Rebase([]byte("theirs"))
	if text, changed, _ := s.Result(); !changed || string(text) != "mine" {
		t.Errorf("got %q %v", text, changed)
	}
}

func TestCloseRemovesEverything(t *testing.T) {
	s := open(t, 1, "x")
	dir := filepath.Dir(s.Path())
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("dir remains: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestCloseAll(t *testing.T) {
	a := open(t, 1, "a")
	b := open(t, 2, "b")
	editor.CloseAll()
	for _, s := range []*editor.Session{a, b} {
		if _, err := os.Stat(s.Path()); !os.IsNotExist(err) {
			t.Errorf("%s remains", s.Path())
		}
	}
}

// TestSignalRemovesTheFile runs a child that opens a session and waits, then
// signals it.
func TestSignalRemovesTheFile(t *testing.T) {
	if os.Getenv("EDITOR_TEST_CHILD") == "1" {
		s, err := editor.Open(7, []byte("x"))
		if err != nil {
			os.Exit(3)
		}
		fmt.Println(s.Path())
		time.Sleep(10 * time.Second)
		os.Exit(0)
	}
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestSignalRemovesTheFile$")
			cmd.Env = append(os.Environ(), "EDITOR_TEST_CHILD=1", "TMPDIR="+t.TempDir())
			out, _ := cmd.StdoutPipe()
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			path, err := bufio.NewReader(out).ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			path = strings.TrimSpace(path)
			cmd.Process.Signal(sig)
			err = cmd.Wait()
			ws, _ := cmd.ProcessState.Sys().(syscall.WaitStatus)
			if !ws.Signaled() || ws.Signal() != sig {
				t.Errorf("child ended with %v, want killed by %v", err, sig)
			}
			if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
				t.Errorf("%s remains", path)
			}
		})
	}
}
