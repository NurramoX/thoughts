package server

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/NurramoX/thoughts/internal/api"
)

// eventServer is a test server whose streams end before it closes, as the
// daemon's do on shutdown.
func eventServer(t *testing.T) (*httptest.Server, *Handler) {
	t.Helper()
	h := New(newFakeStore(), func() time.Time { return testNow })
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	t.Cleanup(h.Close) // runs first
	return ts, h
}

// subscribe opens GET /events and returns a reader of its events.
func subscribe(t *testing.T, ts *httptest.Server) func() (api.Change, bool) {
	t.Helper()
	resp := do(t, ts, "GET", "/events", "")
	wantStatus(t, resp, http.StatusOK)
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type %q", ct)
	}
	r := bufio.NewReader(resp.Body)
	return func() (api.Change, bool) {
		t.Helper()
		line, err := r.ReadString('\n')
		if err != nil {
			return api.Change{}, false
		}
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			t.Fatalf("event line %q", line)
		}
		if blank, _ := r.ReadString('\n'); blank != "\n" {
			t.Fatalf("event not ended by a blank line: %q", blank)
		}
		var c api.Change
		if err := json.Unmarshal([]byte(data), &c); err != nil {
			t.Fatal(err)
		}
		return c, true
	}
}

func TestEventsReportEverySuccessfulWrite(t *testing.T) {
	ts, _ := eventServer(t)
	next := subscribe(t, ts)
	for _, step := range []struct {
		method, path, body string
		headers            []string
		want               api.Change
	}{
		{"POST", "/thoughts", `{"title":"a"}`, nil, api.Change{ID: 1, Version: 1}},
		{"PATCH", "/thoughts/1", `{"title":"b"}`, []string{"If-Match", "*"}, api.Change{ID: 1, Version: 2}},
		{"PUT", "/thoughts/1/body", "text", []string{"If-Match", "*", "Content-Type", markdownType}, api.Change{ID: 1, Version: 3}},
		{"PUT", "/thoughts/1/tags/x", "", nil, api.Change{ID: 1, Version: 4}},
		{"DELETE", "/thoughts/1/tags/x", "", nil, api.Change{ID: 1, Version: 5}},
		{"PUT", "/thoughts/1/attributes/k", "v", nil, api.Change{ID: 1, Version: 6}},
		{"DELETE", "/thoughts/1/attributes/k", "", nil, api.Change{ID: 1, Version: 7}},
		// Failed writes are not changes.
		{"PUT", "/thoughts/1/tags/y", "", []string{"If-Match", `"1"`}, api.Change{}},
		{"PUT", "/thoughts/9/tags/y", "", nil, api.Change{}},
		{"DELETE", "/thoughts/1", "", []string{"If-Match", "*"}, api.Change{ID: 1, Deleted: true}},
	} {
		resp := do(t, ts, step.method, step.path, step.body, step.headers...)
		if resp.StatusCode >= 300 {
			if step.want != (api.Change{}) {
				t.Fatalf("%s %s: status %d", step.method, step.path, resp.StatusCode)
			}
			continue
		}
		if got, ok := next(); !ok || got != step.want {
			t.Fatalf("%s %s: event %+v, want %+v", step.method, step.path, got, step.want)
		}
	}
}

func TestEventsReachEveryStream(t *testing.T) {
	ts, _ := eventServer(t)
	a, b := subscribe(t, ts), subscribe(t, ts)
	do(t, ts, "POST", "/thoughts", `{"title":"a"}`)
	for _, next := range []func() (api.Change, bool){a, b} {
		if got, ok := next(); !ok || got.ID != 1 {
			t.Errorf("event %+v", got)
		}
	}
}

func TestCloseEndsStreamsAndRefusesNewOnes(t *testing.T) {
	ts, h := eventServer(t)
	next := subscribe(t, ts)
	h.Close()
	if c, ok := next(); ok {
		t.Fatalf("stream still open after Close: %+v", c)
	}
	wantProblem(t, do(t, ts, "GET", "/events", ""), http.StatusServiceUnavailable)
}

func TestAStreamThatFallsBehindIsCutOff(t *testing.T) {
	b := newHub()
	slow, _ := b.subscribe()
	fast, _ := b.subscribe()
	for i := range streamBuffer + 1 {
		b.publish(api.Change{ID: int64(i + 1), Version: 1})
		<-fast
	}
	for range streamBuffer {
		if _, open := <-slow; !open {
			t.Fatal("buffered changes lost")
		}
	}
	if _, open := <-slow; open {
		t.Fatal("a full stream was not closed")
	}
	b.publish(api.Change{ID: 99, Version: 1})
	if c := <-fast; c.ID != 99 {
		t.Errorf("the other stream got %+v", c)
	}
	b.unsubscribe(slow) // already closed: no double close
}
