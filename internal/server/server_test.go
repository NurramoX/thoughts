package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/NurramoX/thoughts/internal/api"
)

// testNow is the server clock in tests.
var testNow = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

func newTestServer(t *testing.T) (*httptest.Server, *fakeStore) {
	t.Helper()
	fs := newFakeStore()
	ts := httptest.NewServer(New(fs, func() time.Time { return testNow }))
	t.Cleanup(ts.Close)
	return ts, fs
}

// do sends a request; headers are name, value pairs.
func do(t *testing.T, ts *httptest.Server, method, path, body string, headers ...string) *http.Response {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, ts.URL+path, r)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decoding %T: %v", v, err)
	}
	return v
}

func wantStatus(t *testing.T, resp *http.Response, code int) {
	t.Helper()
	if resp.StatusCode != code {
		t.Fatalf("%s %s: status %d, want %d", resp.Request.Method, resp.Request.URL.Path, resp.StatusCode, code)
	}
}

// wantProblem checks the status and that the body is a problem document with
// that status, and returns it.
func wantProblem(t *testing.T, resp *http.Response, code int) api.Problem {
	t.Helper()
	wantStatus(t, resp, code)
	if ct := resp.Header.Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("Content-Type %q, want application/problem+json", ct)
	}
	p := decode[api.Problem](t, resp)
	if p.Status != code || p.Title == "" {
		t.Fatalf("problem %+v, want status %d and a title", p, code)
	}
	return p
}

// create captures a thought through the API and returns its envelope.
func create(t *testing.T, ts *httptest.Server, body string) api.Thought {
	t.Helper()
	resp := do(t, ts, "POST", "/thoughts", body, "Content-Type", "application/json")
	wantStatus(t, resp, http.StatusCreated)
	return decode[api.Thought](t, resp)
}

func TestServiceRoot(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := do(t, ts, "GET", "/", "")
	wantStatus(t, resp, http.StatusOK)
	if got := strings.TrimSpace(readBody(t, resp)); got != `{"service":"thoughts","api":1}` {
		t.Fatalf("GET / = %s", got)
	}
}

func TestUnmatchedRoutesAreProblems(t *testing.T) {
	ts, _ := newTestServer(t)
	wantProblem(t, do(t, ts, "GET", "/nope", ""), http.StatusNotFound)
	wantProblem(t, do(t, ts, "GET", "/thoughts/1/nope", ""), http.StatusNotFound)
	resp := do(t, ts, "POST", "/", "")
	wantProblem(t, resp, http.StatusMethodNotAllowed)
	if resp.Header.Get("Allow") == "" {
		t.Fatal("405 without Allow")
	}
	wantProblem(t, do(t, ts, "PUT", "/thoughts", ""), http.StatusMethodNotAllowed)
}
