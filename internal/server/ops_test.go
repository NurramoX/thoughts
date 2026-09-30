package server

import (
	"net/http"
	"testing"

	"github.com/NurramoX/thoughts/internal/api"
)

func getThought(t *testing.T, fs *fakeStore, id int64) api.Thought {
	t.Helper()
	thought, err := fs.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return thought
}

func wantETag(t *testing.T, resp *http.Response, etag string) {
	t.Helper()
	if got := resp.Header.Get("ETag"); got != etag {
		t.Fatalf("%s %s: ETag %q, want %q", resp.Request.Method, resp.Request.URL.Path, got, etag)
	}
}

func TestTagOperationsAreIdempotent(t *testing.T) {
	ts, fs := newTestServer(t)
	create(t, ts, `{"title":"a"}`)

	resp := do(t, ts, "PUT", "/thoughts/1/tags/rust", "")
	wantStatus(t, resp, http.StatusNoContent)
	wantETag(t, resp, `"2"`)
	resp = do(t, ts, "PUT", "/thoughts/1/tags/rust", "")
	wantStatus(t, resp, http.StatusNoContent)
	wantETag(t, resp, `"2"`)
	if tags := getThought(t, fs, 1).Tags; len(tags) != 1 || tags[0] != "rust" {
		t.Fatalf("tags %v", tags)
	}

	resp = do(t, ts, "DELETE", "/thoughts/1/tags/rust", "")
	wantStatus(t, resp, http.StatusNoContent)
	wantETag(t, resp, `"3"`)
	resp = do(t, ts, "DELETE", "/thoughts/1/tags/rust", "")
	wantStatus(t, resp, http.StatusNoContent)
	wantETag(t, resp, `"3"`)
}

func TestTagOperationsHonourIfMatch(t *testing.T) {
	ts, _ := newTestServer(t)
	create(t, ts, `{"title":"a"}`)
	p := wantProblem(t, do(t, ts, "PUT", "/thoughts/1/tags/x", "", "If-Match", `"5"`), http.StatusPreconditionFailed)
	if p.CurrentVersion != 1 {
		t.Fatalf("current_version %d", p.CurrentVersion)
	}
	wantStatus(t, do(t, ts, "PUT", "/thoughts/1/tags/x", "", "If-Match", `"1"`), http.StatusNoContent)
	wantProblem(t, do(t, ts, "DELETE", "/thoughts/1/tags/x", "", "If-Match", `"1"`), http.StatusPreconditionFailed)
	wantProblem(t, do(t, ts, "DELETE", "/thoughts/1/tags/x", "", "If-Match", `bad`), http.StatusBadRequest)
	wantProblem(t, do(t, ts, "PUT", "/thoughts/2/tags/x", ""), http.StatusNotFound)
	wantProblem(t, do(t, ts, "PUT", "/thoughts/two/tags/x", ""), http.StatusBadRequest)
}

func TestTagPathIsUnescaped(t *testing.T) {
	ts, fs := newTestServer(t)
	create(t, ts, `{"title":"a"}`)
	wantStatus(t, do(t, ts, "PUT", "/thoughts/1/tags/a%20b", ""), http.StatusNoContent)
	if tags := getThought(t, fs, 1).Tags; len(tags) != 1 || tags[0] != "a b" {
		t.Fatalf("tags %q", tags)
	}
}

func TestPutAttributeTakesTheBodyAsValue(t *testing.T) {
	ts, fs := newTestServer(t)
	create(t, ts, `{"title":"a"}`)

	resp := do(t, ts, "PUT", "/thoughts/1/attributes/effort", "small", "Content-Type", "text/plain; charset=utf-8")
	wantStatus(t, resp, http.StatusNoContent)
	wantETag(t, resp, `"2"`)
	// No Content-Type, and curl's form default, are both fine.
	wantStatus(t, do(t, ts, "PUT", "/thoughts/1/attributes/source", "shower thought"), http.StatusNoContent)
	wantStatus(t, do(t, ts, "PUT", "/thoughts/1/attributes/mood", "calm", "Content-Type", "application/x-www-form-urlencoded"), http.StatusNoContent)

	attrs := getThought(t, fs, 1).Attributes
	if attrs["effort"] != "small" || attrs["source"] != "shower thought" || attrs["mood"] != "calm" {
		t.Fatalf("attributes %v", attrs)
	}
}

func TestAttributeOperationErrors(t *testing.T) {
	ts, _ := newTestServer(t)
	create(t, ts, `{"title":"a"}`)
	wantProblem(t, do(t, ts, "PUT", "/thoughts/1/attributes/effort", "x", "Content-Type", "text/plain; charset=latin1"), http.StatusUnsupportedMediaType)
	wantProblem(t, do(t, ts, "PUT", "/thoughts/1/attributes/effort", "\xff"), http.StatusUnprocessableEntity)
	wantProblem(t, do(t, ts, "PUT", "/thoughts/1/attributes/effort", ""), http.StatusUnprocessableEntity)
	wantProblem(t, do(t, ts, "PUT", "/thoughts/1/attributes/effort", "x", "If-Match", `"4"`), http.StatusPreconditionFailed)
	wantProblem(t, do(t, ts, "DELETE", "/thoughts/1/attributes/status", ""), http.StatusUnprocessableEntity)
	wantProblem(t, do(t, ts, "DELETE", "/thoughts/3/attributes/effort", ""), http.StatusNotFound)
}

func TestDeleteAttribute(t *testing.T) {
	ts, fs := newTestServer(t)
	create(t, ts, `{"title":"a","attributes":{"effort":"small"}}`)
	resp := do(t, ts, "DELETE", "/thoughts/1/attributes/effort", "", "If-Match", `"1"`)
	wantStatus(t, resp, http.StatusNoContent)
	wantETag(t, resp, `"2"`)
	resp = do(t, ts, "DELETE", "/thoughts/1/attributes/effort", "")
	wantStatus(t, resp, http.StatusNoContent)
	wantETag(t, resp, `"2"`)
	if _, ok := getThought(t, fs, 1).Attributes["effort"]; ok {
		t.Fatal("effort still set")
	}
}
