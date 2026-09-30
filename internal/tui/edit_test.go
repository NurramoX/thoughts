package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const mine = "My edited body.\n"

// editorWrites makes $VISUAL replace the temp file with text, as a user
// saving in the editor would.
func editorWrites(t *testing.T, text string) {
	t.Helper()
	src := filepath.Join(t.TempDir(), "mine.md")
	if err := os.WriteFile(src, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VISUAL", "cp "+src)
}

func TestEditWritesTheBodyGuardedByThePreviewVersion(t *testing.T) {
	for _, key := range []string{"e", "enter"} {
		t.Run(key, func(t *testing.T) {
			fc := threeThoughts()
			editorWrites(t, mine)
			h := newHarness(t, fc, "")
			h.press(key)
			if want := []string{`PUT 1 body if-match "1"`}; !slices.Equal(fc.writes(), want) {
				t.Fatalf("writes = %v, want %v", fc.writes(), want)
			}
			if fc.thought(1).Body != mine {
				t.Errorf("body = %q", fc.thought(1).Body)
			}
			if h.selected() != 1 {
				t.Errorf("selected %d, want to stay on 1", h.selected())
			}
			if v := h.view(); !strings.Contains(v, "My edited body.") || !strings.Contains(v, "v2") {
				t.Errorf("preview not refreshed:\n%s", v)
			}
			if h.m.edit != nil || len(h.m.aborted) != 0 {
				t.Errorf("round-trip left state behind")
			}
		})
	}
}

func TestEditWritesNothingWhenUnchangedOrTheEditorFails(t *testing.T) {
	for _, visual := range []string{"true", "false"} {
		t.Run(visual, func(t *testing.T) {
			fc := threeThoughts()
			t.Setenv("VISUAL", visual)
			h := newHarness(t, fc, "")
			h.press("e")
			if len(fc.writes()) != 0 {
				t.Errorf("writes = %v", fc.writes())
			}
			if h.m.edit != nil || len(h.m.aborted) != 0 {
				t.Errorf("round-trip left state behind")
			}
		})
	}
}

// conflicted is a harness whose `e` has just hit a 412: thought 1 moved to
// version 2 after its preview loaded.
func conflicted(t *testing.T) *harness {
	t.Helper()
	fc := threeThoughts()
	editorWrites(t, mine)
	h := newHarness(t, fc, "")
	fc.bump(1, "Their body.\n")
	h.press("e")
	if !strings.Contains(h.view(), "thought 1 changed: you had version 1, it is now 2 · [o]verwrite / [r]e-edit / [a]bort") {
		t.Fatalf("no 412 prompt:\n%s", h.view())
	}
	fc.calls = nil
	return h
}

func TestEditConflictOverwrite(t *testing.T) {
	h := conflicted(t)
	h.press("o")
	if want := []string{`PUT 1 body if-match *`}; !slices.Equal(h.fc.writes(), want) {
		t.Errorf("writes = %v, want %v", h.fc.writes(), want)
	}
	if h.fc.thought(1).Body != mine {
		t.Errorf("body = %q", h.fc.thought(1).Body)
	}
	if h.m.edit != nil || len(h.m.aborted) != 0 {
		t.Errorf("round-trip left state behind")
	}
}

func TestEditConflictReEdit(t *testing.T) {
	h := conflicted(t)
	h.press("r")
	v := h.view()
	if !strings.Contains(v, "Their body.") || !strings.Contains(v, "their version 2") {
		t.Fatalf("their body is not shown:\n%s", v)
	}
	if len(h.fc.writes()) != 0 {
		t.Fatalf("re-edit wrote before reopening: %v", h.fc.writes())
	}
	h.press("enter") // the editor reopens on the user's text, which they save
	if want := []string{`PUT 1 body if-match "2"`}; !slices.Equal(h.fc.writes(), want) {
		t.Errorf("writes = %v, want %v", h.fc.writes(), want)
	}
	if h.fc.thought(1).Body != mine {
		t.Errorf("body = %q", h.fc.thought(1).Body)
	}
	if h.m.edit != nil || len(h.m.aborted) != 0 {
		t.Errorf("round-trip left state behind")
	}
}

func TestEditConflictReEditThatConflictsAgain(t *testing.T) {
	h := conflicted(t)
	h.press("r")
	h.fc.bump(1, "Even newer.\n")
	h.press("enter")
	if !strings.Contains(h.view(), "you had version 2, it is now 3") {
		t.Errorf("no second 412 prompt:\n%s", h.view())
	}
}

func TestEditConflictAbortKeepsTheTextForStdout(t *testing.T) {
	for _, keys := range [][]string{{"a"}, {"r", "a"}, {"ctrl+c"}} {
		t.Run(strings.Join(keys, ","), func(t *testing.T) {
			h := conflicted(t)
			h.press(keys...)
			if len(h.fc.writes()) != 0 {
				t.Errorf("writes = %v", h.fc.writes())
			}
			if len(h.m.aborted) != 1 || string(h.m.aborted[0]) != mine {
				t.Errorf("aborted = %q, want the user's text", h.m.aborted)
			}
			if h.m.edit != nil {
				t.Errorf("session left open")
			}
			_ = h.view() // the final frame renders without the session
		})
	}
}

func TestEditWriteFailureKeepsTheTextForStdout(t *testing.T) {
	fc := threeThoughts()
	editorWrites(t, mine)
	fc.failNext["PUT 1 body"] = problem(413, "too large")
	h := newHarness(t, fc, "")
	h.press("e")
	if len(h.m.aborted) != 1 || string(h.m.aborted[0]) != mine {
		t.Errorf("aborted = %q, want the user's text", h.m.aborted)
	}
	if !strings.Contains(h.view(), "too large") {
		t.Errorf("error not shown:\n%s", h.view())
	}
}
