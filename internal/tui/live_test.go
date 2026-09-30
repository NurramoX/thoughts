package tui

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/NurramoX/thoughts/internal/api"
	"github.com/NurramoX/thoughts/internal/client"
)

// change is the changedMsg for a write that left id at version.
func change(id, version int64) changedMsg {
	return changedMsg{change: api.Change{ID: id, Version: version}}
}

func TestANewThoughtJoinsTheListWhereTheServerPutsIt(t *testing.T) {
	fc := threeThoughts()
	h := newHarness(t, fc, "")
	h.press("j") // on 2
	id := fc.add("Fourth thought", 0)
	fc.lists[DefaultFilter] = []int64{id, 1, 2, 3}
	h.send(change(id, 1))
	if got, want := h.rowIDs(), []int64{4, 1, 2, 3}; !slices.Equal(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	if h.selected() != 2 {
		t.Errorf("selected %d, want to stay on 2", h.selected())
	}
	if v := h.view(); !strings.Contains(v, "Fourth thought") || !strings.Contains(v, "3/4") {
		t.Errorf("view:\n%s", v)
	}
}

func TestANewThoughtGoesAfterTheThoughtTheServerListsBeforeIt(t *testing.T) {
	fc := threeThoughts()
	h := newHarness(t, fc, "")
	id := fc.add("Fourth thought", 0)
	fc.lists[DefaultFilter] = []int64{1, 2, id, 3}
	h.send(change(id, 1))
	if got, want := h.rowIDs(), []int64{1, 2, 4, 3}; !slices.Equal(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
}

func TestANewThoughtOutsideTheFilterStaysOut(t *testing.T) {
	fc := threeThoughts()
	h := newHarness(t, fc, "")
	fc.lists[DefaultFilter] = []int64{1, 2, 3}
	id := fc.add("Done elsewhere", 0)
	h.send(change(id, 1))
	if got, want := h.rowIDs(), []int64{1, 2, 3}; !slices.Equal(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
}

func TestTheFirstThoughtInAnEmptyListIsSelectedAndPreviewed(t *testing.T) {
	fc := newFake(testNow)
	h := newHarness(t, fc, "")
	id := fc.add("Only thought", 0)
	h.send(change(id, 1))
	if h.selected() != id {
		t.Fatalf("selected %d, want %d", h.selected(), id)
	}
	if v := h.view(); !strings.Contains(v, "Body of Only thought.") {
		t.Errorf("preview not loaded:\n%s", v)
	}
	if h.m.hint != nil {
		t.Errorf("vocabulary hint still shown: %v", h.m.hint)
	}
}

func TestAChangeElsewhereUpdatesItsRowAndPreviewInPlace(t *testing.T) {
	fc := threeThoughts()
	h := newHarness(t, fc, "")
	fc.bump(1, "Rewritten elsewhere.\n")
	// Thought 1 no longer matches the filter; the snapshot keeps it anyway.
	fc.lists[DefaultFilter] = []int64{2, 3}
	h.send(change(1, 2))
	if got, want := h.rowIDs(), []int64{1, 2, 3}; !slices.Equal(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	if h.m.rows[0].Version != 2 {
		t.Errorf("row version %d, want 2", h.m.rows[0].Version)
	}
	if v := h.view(); !strings.Contains(v, "Rewritten elsewhere.") || !strings.Contains(v, "v2") {
		t.Errorf("preview not refreshed:\n%s", v)
	}
	if n := fc.countCalls("LIST"); n != 1 {
		t.Errorf("%d LIST calls, want only the first: a known row needs no requery", n)
	}
}

func TestADeleteElsewhereMarksTheRow(t *testing.T) {
	fc := threeThoughts()
	h := newHarness(t, fc, "")
	gets := fc.countCalls("GET")
	h.send(changedMsg{change: api.Change{ID: 1, Deleted: true}})
	if !h.m.rows[0].deleted || h.selected() != 1 {
		t.Fatalf("row 1 not marked deleted in place: %+v", h.m.rows[0])
	}
	if !strings.Contains(h.view(), "deleted") {
		t.Errorf("preview does not say deleted:\n%s", h.view())
	}
	if fc.countCalls("GET") != gets || fc.countCalls("LIST") != 1 {
		t.Errorf("a delete needs no calls: %v", fc.calls)
	}
}

func TestOwnWritesComeBackAsChangesThatNeedNothing(t *testing.T) {
	fc := threeThoughts()
	h := newHarness(t, fc, "")
	h.press("a") // thought 1 → version 2
	calls := len(fc.calls)
	h.send(change(1, 2))
	if extra := fc.calls[calls:]; len(extra) != 0 {
		t.Errorf("the echo of our own write cost %v", extra)
	}
}

func TestABurstOfChangesCostsOneRefresh(t *testing.T) {
	fc := threeThoughts()
	h := newHarness(t, fc, "")
	var ticks []tea.Msg
	for range 3 {
		id := fc.add("New", 0)
		ticks = append(ticks, h.step(change(id, 1))...)
	}
	for _, tick := range ticks {
		h.send(tick)
	}
	if n := fc.countCalls("LIST"); n != 2 {
		t.Errorf("%d LIST calls, want the first and one refresh", n)
	}
	if len(h.m.rows) != 6 {
		t.Errorf("rows = %v", h.rowIDs())
	}
}

func TestARefreshWaitsForTheRequeryOut(t *testing.T) {
	fc := threeThoughts()
	h := newHarness(t, fc, "")
	answer := h.step(keyMsg("ctrl+r")) // listed before the new thought exists
	id := fc.add("Fourth thought", 0)
	h.send(change(id, 1))
	if n := fc.countCalls("LIST"); n != 2 {
		t.Fatalf("%d LIST calls, want the refresh held back", n)
	}
	for _, msg := range answer {
		h.send(msg)
	}
	if n := fc.countCalls("LIST"); n != 3 {
		t.Errorf("%d LIST calls, want the refresh after the answer", n)
	}
	if !slices.Contains(h.rowIDs(), id) {
		t.Errorf("rows = %v, want the new thought", h.rowIDs())
	}
}

func TestARefreshListsTheSnapshotsQueryNotTheBar(t *testing.T) {
	fc := threeThoughts()
	bad := "status:(raw"
	fc.listErr[bad] = problem(400, "unexpected (")
	h := newHarness(t, fc, "")
	h.press("/")
	h.m.bar.SetValue(bad)
	h.press("enter") // a 400: the snapshot still answers the default filter
	h.send(resyncMsg{})
	if last := fc.params[len(fc.params)-1]; last.Filter != DefaultFilter {
		t.Errorf("refresh listed %q, want %q", last.Filter, DefaultFilter)
	}
}

func TestAResyncBringsInWhatWasMissed(t *testing.T) {
	fc := threeThoughts()
	h := newHarness(t, fc, "")
	fc.bump(2, "Changed while the stream was down.\n")
	id := fc.add("Fourth thought", 0)
	h.send(resyncMsg{})
	if got, want := h.rowIDs(), []int64{1, 2, 3, id}; !slices.Equal(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
	if h.m.rows[1].Version != 2 {
		t.Errorf("row 2 version %d, want 2", h.m.rows[1].Version)
	}
}

func TestAResyncReloadsAStalePreview(t *testing.T) {
	fc := threeThoughts()
	h := newHarness(t, fc, "")
	fc.bump(1, "Changed while the stream was down.\n")
	h.send(resyncMsg{})
	if v := h.view(); !strings.Contains(v, "Changed while the stream was down.") {
		t.Errorf("preview not refreshed:\n%s", v)
	}
}

func TestNewThoughtsAboveTheViewKeepItStill(t *testing.T) {
	fc := newFake(testNow)
	for i := range 60 {
		fc.add("Thought", time.Duration(i)*time.Minute)
	}
	h := newHarness(t, fc, "")
	h.press("G")
	sel, offset := h.m.sel, h.m.offset
	if offset == 0 {
		t.Fatal("the list does not scroll; the test needs more rows")
	}
	id := fc.add("Newest", 0)
	fc.lists[DefaultFilter] = append([]int64{id}, fc.order[:60]...)
	h.send(change(id, 1))
	if h.m.sel != sel+1 || h.m.offset != offset+1 {
		t.Errorf("sel, offset = %d, %d, want %d, %d", h.m.sel, h.m.offset, sel+1, offset+1)
	}
}

// streamClient serves watch the streams a test hands it, one per Changes
// call; with none ready, Changes fails.
type streamClient struct {
	client.Client
	streams chan *fakeStream
}

func (c *streamClient) Changes(ctx context.Context) (client.ChangeStream, error) {
	select {
	case s := <-c.streams:
		return s, nil
	default:
		return nil, &client.UnreachableError{Err: errors.New("no daemon")}
	}
}

// fakeStream yields its changes, then io.EOF.
type fakeStream struct {
	changes []api.Change
	closed  chan struct{}
}

func newStream(changes ...api.Change) *fakeStream {
	return &fakeStream{changes: changes, closed: make(chan struct{})}
}

func (s *fakeStream) Next() (api.Change, error) {
	if len(s.changes) == 0 {
		return api.Change{}, io.EOF
	}
	c := s.changes[0]
	s.changes = s.changes[1:]
	return c, nil
}

func (s *fakeStream) Close() error {
	close(s.closed)
	return nil
}

func TestWatchResyncsOnEveryConnectionAndRetries(t *testing.T) {
	c := &streamClient{streams: make(chan *fakeStream, 2)}
	first, second := newStream(api.Change{ID: 1, Version: 2}), newStream(api.Change{ID: 2, Deleted: true})
	c.streams <- first
	ctx, cancel := context.WithCancel(context.Background())
	msgs := make(chan tea.Msg)
	done := make(chan struct{})
	go func() {
		watch(ctx, c, func(m tea.Msg) { msgs <- m }, time.Millisecond)
		close(done)
	}()

	want := []tea.Msg{
		resyncMsg{}, change(1, 2),
		resyncMsg{}, changedMsg{change: api.Change{ID: 2, Deleted: true}},
	}
	for i, w := range want {
		if i == 2 {
			// The first stream has ended; the daemon is away until now.
			c.streams <- second
		}
		select {
		case got := <-msgs:
			if got != w {
				t.Fatalf("message %d = %#v, want %#v", i, got, w)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no message %d", i)
		}
	}
	<-first.closed

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("watch did not stop with its context")
	}
}
