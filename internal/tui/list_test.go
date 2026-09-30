package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/NurramoX/thoughts/internal/api"
	"github.com/NurramoX/thoughts/internal/client"
)

func TestActionsUpdateRowsInPlaceWithoutReordering(t *testing.T) {
	fc := threeThoughts()
	h := newHarness(t, fc, "")
	// The server would now leave thought 1 out and put 3 first; the snapshot
	// holds until a requery.
	fc.lists[DefaultFilter] = []int64{3, 2}
	h.press("d", "x")
	if !slices.Equal(h.rowIDs(), []int64{1, 2, 3}) {
		t.Fatalf("rows = %v after actions", h.rowIDs())
	}
	if h.m.rows[0].Attributes["status"] != "done" || h.m.rows[1].Attributes["status"] != "dropped" {
		t.Errorf("rows not updated in place: %+v", h.m.rows[:2])
	}
	h.press("ctrl+r")
	if !slices.Equal(h.rowIDs(), []int64{3, 2}) {
		t.Errorf("rows = %v after ctrl-r", h.rowIDs())
	}
}

func TestRequeryKeepsTheSelectedThought(t *testing.T) {
	fc := threeThoughts()
	h := newHarness(t, fc, "")
	h.press("j") // thought 2
	fc.lists[DefaultFilter] = []int64{3, 1, 2}
	h.press("ctrl+r")
	if h.selected() != 2 {
		t.Errorf("selected %d after requery, want 2", h.selected())
	}
}

func TestRequerySelectsTheTopRowWhenTheSelectedThoughtIsGone(t *testing.T) {
	fc := threeThoughts()
	h := newHarness(t, fc, "")
	h.press("j")
	fc.lists[DefaultFilter] = []int64{3, 1}
	h.press("ctrl+r")
	if h.selected() != 3 {
		t.Errorf("selected %d after requery, want 3", h.selected())
	}
}

func TestFilterBarFocusesWithTheCursorAtTheEnd(t *testing.T) {
	for _, key := range []string{"/", "f"} {
		h := newHarness(t, threeThoughts(), "tag:rust")
		h.press(key)
		h.typeText(" go")
		if got := h.m.bar.Value(); got != "tag:rust go" {
			t.Errorf("%s: bar = %q, want %q", key, got, "tag:rust go")
		}
	}
}

func TestTypingRequeriesAfterTheDebounce(t *testing.T) {
	fc := threeThoughts()
	h := newHarness(t, fc, "")
	h.press("/", "ctrl+u")
	fc.calls = nil

	// Three keystrokes: only the last one's tick requeries.
	var ticks []filterTickMsg
	for _, r := range "abc" {
		for _, msg := range h.step(keyMsg(string(r))) {
			if tick, ok := msg.(filterTickMsg); ok {
				ticks = append(ticks, tick)
			}
		}
	}
	if len(ticks) != 3 {
		t.Fatalf("got %d debounce ticks, want 3", len(ticks))
	}
	for _, tick := range ticks[:2] {
		h.send(tick)
	}
	if n := fc.countCalls("LIST"); n != 0 {
		t.Fatalf("stale ticks requeried %d times", n)
	}
	h.send(ticks[2])
	if !slices.Equal(fc.calls[:1], []string{`LIST "abc"`}) {
		t.Errorf("calls = %v, want one LIST of \"abc\"", fc.calls)
	}
}

func TestMovingTheSelectionWhileTyping(t *testing.T) {
	h := newHarness(t, threeThoughts(), "")
	h.press("/")
	h.press("ctrl+n", "down", "ctrl+p")
	if h.selected() != 2 {
		t.Errorf("selected %d, want 2", h.selected())
	}
	h.press("up")
	if h.selected() != 1 {
		t.Errorf("selected %d, want 1", h.selected())
	}
	if h.m.bar.Value() != DefaultFilter {
		t.Errorf("moving typed into the bar: %q", h.m.bar.Value())
	}
}

func TestLeavingTheBarKeepsTheFilterAndReturnsToTheList(t *testing.T) {
	for _, key := range []string{"enter", "esc"} {
		t.Run(key, func(t *testing.T) {
			fc := threeThoughts()
			h := newHarness(t, fc, "")
			h.press("/", "ctrl+u")
			h.step(keyMsg("x")) // debounce still pending
			h.press(key)
			if h.m.bar.Value() != "x" {
				t.Errorf("bar = %q", h.m.bar.Value())
			}
			if last := fc.params[len(fc.params)-1]; last.Filter != "x" {
				t.Errorf("leaving the bar did not requery the pending filter: %+v", fc.params)
			}
			fc.calls = nil
			h.press("j")
			if h.selected() != 2 || h.m.bar.Value() != "x" {
				t.Errorf("j went to the bar, not the list")
			}
			if len(fc.writes()) != 0 {
				t.Errorf("writes = %v", fc.writes())
			}
		})
	}
}

func TestEmptyFilterListsEverything(t *testing.T) {
	fc := threeThoughts()
	h := newHarness(t, fc, "status:raw")
	fc.lists["status:raw"] = []int64{1}
	h.press("ctrl+r")
	h.press("/", "ctrl+u", "enter")
	if last := fc.params[len(fc.params)-1]; last.Filter != "" {
		t.Errorf("filter = %q, want empty", last.Filter)
	}
	if !slices.Equal(h.rowIDs(), []int64{1, 2, 3}) {
		t.Errorf("rows = %v", h.rowIDs())
	}
}

func TestAFilterErrorKeepsTheLastGoodListAndShowsACaret(t *testing.T) {
	fc := threeThoughts()
	h := newHarness(t, fc, "")
	bad := "status:(raw"
	p := problem(400, "unexpected (")
	p.Problem.Position = 8
	fc.listErr[bad] = p
	h.press("/", "ctrl+u")
	h.typeText(bad)
	if !slices.Equal(h.rowIDs(), []int64{1, 2, 3}) {
		t.Errorf("rows = %v, want the last good list", h.rowIDs())
	}
	v := h.view()
	if !strings.Contains(v, "unexpected (") {
		t.Errorf("view lacks the error:\n%s", v)
	}
	// filter.Caret puts ^ under rune 8.
	if !strings.Contains(v, "\n"+strings.Repeat(" ", 7)+"^") {
		t.Errorf("view lacks the caret under position 8:\n%s", v)
	}
}

func TestAnEmptyResultShowsTheVocabularyHint(t *testing.T) {
	fc := threeThoughts()
	fc.lists["tga:rust"] = []int64{}
	h := newHarness(t, fc, "tga:rust")
	v := h.view()
	if !strings.Contains(v, "no thoughts") {
		t.Errorf("view lacks the empty-result line:\n%s", v)
	}
	// hint.Vocabulary names the unknown key.
	if !strings.Contains(v, "'tga'") {
		t.Errorf("view lacks the vocabulary hint:\n%s", v)
	}
}

func TestOrderCyclesAndRequeries(t *testing.T) {
	fc := threeThoughts()
	h := newHarness(t, fc, "")
	h.m.shuffle = func(rs []row) { slices.Reverse(rs) }
	want := []struct{ name, sort, order string }{
		{"created", "created", "desc"},
		{"random", "", ""},
		{"updated", "updated", "desc"},
	}
	for _, w := range want {
		h.press("o")
		got := fc.params[len(fc.params)-1]
		if got != (client.ListParams{Filter: DefaultFilter, Sort: w.sort, Order: w.order}) {
			t.Errorf("%s: params = %+v", w.name, got)
		}
		if !strings.Contains(h.view(), "· "+w.name) {
			t.Errorf("status line lacks %q", w.name)
		}
		if w.name == "random" && !slices.Equal(h.rowIDs(), []int64{3, 2, 1}) {
			t.Errorf("random rows = %v, want the client-side shuffle", h.rowIDs())
		}
	}
}

func TestRowsShowTheAgeOfTheSortField(t *testing.T) {
	fc := threeThoughts()
	fc.thoughts[1].CreatedAt = api.Time{Time: testNow.Add(-72 * 3600e9)}
	h := newHarness(t, fc, "")
	if v := h.view(); !strings.Contains(v, "1h") {
		t.Errorf("updated order lacks the updated age:\n%s", v)
	}
	h.press("o") // created
	if v := h.view(); !strings.Contains(v, "3d") {
		t.Errorf("created order lacks the created age:\n%s", v)
	}
}

func TestAStaleListAnswerIsDropped(t *testing.T) {
	fc := threeThoughts()
	h := newHarness(t, fc, "")
	old := collect(h.m.requery())
	h.press("ctrl+r")
	fc.calls = nil
	for _, msg := range old {
		h.send(msg)
	}
	if h.m.listSeq != old[0].(listMsg).seq+1 {
		t.Fatal("test setup: expected one newer requery")
	}
	if !slices.Equal(h.rowIDs(), []int64{1, 2, 3}) {
		t.Errorf("rows = %v", h.rowIDs())
	}
}
