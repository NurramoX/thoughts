package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestPreviewShowsTheHeaderAndRenderedBody(t *testing.T) {
	fc := threeThoughts()
	fc.thoughts[1].Attributes["effort"] = "small"
	fc.thoughts[1].Body = "# Heading\n\nSome **bold** words.\n"
	h := newHarness(t, fc, "")
	v := h.view()
	for _, s := range []string{"First thought", "#1 · raw · rust", "effort=small", "created 1h · updated 1h · v1", "Heading", "Some bold words."} {
		if !strings.Contains(v, s) {
			t.Errorf("preview lacks %q:\n%s", s, v)
		}
	}
	if strings.Contains(v, "**bold**") {
		t.Errorf("body not rendered:\n%s", v)
	}
}

func TestMTogglesRawMarkdown(t *testing.T) {
	fc := threeThoughts()
	fc.thoughts[1].Body = "Some **bold** words.\n"
	h := newHarness(t, fc, "")
	h.press("m")
	if !strings.Contains(h.view(), "Some **bold** words.") {
		t.Errorf("raw view lacks the markdown:\n%s", h.view())
	}
	h.press("m")
	if strings.Contains(h.view(), "**bold**") {
		t.Errorf("m did not toggle back")
	}
}

func TestPreviewIsDebouncedAndRevalidated(t *testing.T) {
	fc := threeThoughts()
	h := newHarness(t, fc, "")
	if !slices.Equal(fc.calls, []string{`LIST "` + DefaultFilter + `"`, "GET 1"}) {
		t.Fatalf("calls = %v", fc.calls)
	}
	fc.calls = nil

	// Moving quickly past thought 2 fetches only thought 3.
	var ticks []tea.Msg
	ticks = append(ticks, h.step(keyMsg("j"))...)
	ticks = append(ticks, h.step(keyMsg("j"))...)
	for _, tick := range ticks {
		h.send(tick)
	}
	if !slices.Equal(fc.calls, []string{"GET 3"}) {
		t.Errorf("calls = %v, want only GET 3", fc.calls)
	}

	// Back on thought 1: its cached preview is revalidated.
	fc.calls = nil
	h.press("g")
	if !slices.Equal(fc.calls, []string{"GET 1 if-none-match 1"}) {
		t.Errorf("calls = %v, want a revalidation", fc.calls)
	}

	// A change elsewhere shows on the next revalidation, and the row follows.
	fc.bump(3, "New body of three.\n")
	fc.thoughts[3].Title = "Third, renamed"
	h.press("G")
	if v := h.view(); !strings.Contains(v, "New body of three.") || !strings.Contains(v, "v2") {
		t.Errorf("preview not refreshed:\n%s", v)
	}
	if h.m.rows[2].Title != "Third, renamed" {
		t.Errorf("row title = %q", h.m.rows[2].Title)
	}
}

func TestPreviewOfADeletedThought(t *testing.T) {
	fc := threeThoughts()
	h := newHarness(t, fc, "")
	delete(fc.thoughts, 2)
	h.press("j")
	if !strings.Contains(h.view(), "deleted") {
		t.Errorf("view lacks the deletion:\n%s", h.view())
	}
	if !slices.Equal(h.rowIDs(), []int64{1, 2, 3}) {
		t.Errorf("the row disappeared before a requery")
	}
}

func TestPreviewScrolls(t *testing.T) {
	fc := threeThoughts()
	var body strings.Builder
	for i := range 100 {
		fmt.Fprintf(&body, "line %d\n\n", i)
	}
	fc.thoughts[1].Body = body.String()
	h := newHarness(t, fc, "")
	h.press("m") // raw, so lines are predictable
	h.press("J", "J", "J")
	if h.m.vp.YOffset() != 3 {
		t.Errorf("offset %d after JJJ, want 3", h.m.vp.YOffset())
	}
	h.press("K")
	if h.m.vp.YOffset() != 2 {
		t.Errorf("offset %d after K, want 2", h.m.vp.YOffset())
	}
	h.press("ctrl+d")
	if h.m.vp.YOffset() <= 2 {
		t.Errorf("ctrl-d did not scroll")
	}
	h.press("ctrl+u", "ctrl+u")
	if h.m.vp.YOffset() != 0 {
		t.Errorf("ctrl-u did not scroll back: %d", h.m.vp.YOffset())
	}
	h.press("j", "k")
	if h.m.vp.YOffset() != 0 {
		t.Errorf("a new selection keeps the scroll")
	}
}

func TestNarrowTerminalsShowOnePaneAndTabSwitches(t *testing.T) {
	h := newHarness(t, threeThoughts(), "")
	h.send(tea.WindowSizeMsg{Width: 79, Height: 20})
	v := h.view()
	if !strings.Contains(v, "First thought") || strings.Contains(v, "created 1h") || strings.Contains(v, "│") {
		t.Errorf("narrow view is not the list alone:\n%s", v)
	}
	h.press("tab")
	v = h.view()
	if !strings.Contains(v, "created 1h") || strings.Contains(v, DefaultFilter) {
		t.Errorf("tab did not switch to the preview:\n%s", v)
	}
	h.press("n") // keys still act on the list
	if h.selected() != 2 || !strings.Contains(h.view(), "Second thought") {
		t.Errorf("preview pane does not follow the selection:\n%s", h.view())
	}
	h.press("tab")
	if !strings.Contains(h.view(), DefaultFilter) {
		t.Errorf("tab did not switch back")
	}
	for _, line := range strings.Split(h.view(), "\n") {
		if w := len([]rune(line)); w > 79 {
			t.Errorf("line wider than the terminal (%d): %q", w, line)
		}
	}
}

func TestWideLayoutProportions(t *testing.T) {
	h := newHarness(t, threeThoughts(), "")
	for _, c := range []struct{ width, list int }{{80, 32}, {120, 48}, {200, 80}} {
		h.send(tea.WindowSizeMsg{Width: c.width, Height: 20})
		lines := strings.Split(h.view(), "\n")
		sep := strings.Index(lines[0], "│")
		if got := len([]rune(lines[0][:max(sep, 0)])); sep < 0 || got != c.list {
			t.Errorf("width %d: list is %d columns, want %d", c.width, got, c.list)
		}
		for _, line := range lines {
			if w := len([]rune(line)); w != c.width {
				t.Errorf("width %d: line is %d wide: %q", c.width, w, line)
				break
			}
		}
	}
}

func TestListScrollsToKeepTheSelectionVisible(t *testing.T) {
	fc := newFake(testNow)
	for i := range 40 {
		fc.add(fmt.Sprintf("Thought number %d", i+1), time.Duration(i+1)*time.Minute)
	}
	h := newHarness(t, fc, "")
	h.send(tea.WindowSizeMsg{Width: 100, Height: 12})
	h.press("G")
	v := h.view()
	if !strings.Contains(v, "▸") || !strings.Contains(v, "Thought number 40") {
		t.Errorf("the last row is not shown:\n%s", v)
	}
	if !strings.Contains(v, "40/40") {
		t.Errorf("status line lacks 40/40:\n%s", v)
	}
}
