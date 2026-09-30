package tui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/NurramoX/thoughts/internal/client"
)

func TestOpensWithTheOpenThoughtsSortedByUpdated(t *testing.T) {
	fc := newFake(testNow)
	fc.add("Borrow checker notes", time.Hour, "rust")
	fc.add("Garden planner", 48*time.Hour)

	h := newHarness(t, fc, "")

	want := client.ListParams{Filter: DefaultFilter, Sort: "updated", Order: "desc"}
	if len(fc.params) != 1 || fc.params[0] != want {
		t.Fatalf("list params = %+v, want one call with %+v", fc.params, want)
	}
	v := h.view()
	for _, s := range []string{DefaultFilter, "Borrow checker notes", "Garden planner", "rust", "1/2", "? help"} {
		if !strings.Contains(v, s) {
			t.Errorf("view lacks %q:\n%s", s, v)
		}
	}
	if !slices.Equal(h.rowIDs(), []int64{1, 2}) || h.selected() != 1 {
		t.Errorf("rows %v, selected %d", h.rowIDs(), h.selected())
	}
}

func TestOpensWithTheGivenFilter(t *testing.T) {
	fc := newFake(testNow)
	fc.add("One", time.Hour)
	newHarness(t, fc, "tag:rust")
	if fc.params[0].Filter != "tag:rust" {
		t.Fatalf("filter = %q", fc.params[0].Filter)
	}
}
