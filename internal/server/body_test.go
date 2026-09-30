package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/NurramoX/thoughts/internal/api"
)

const markdown = "text/markdown; charset=utf-8"

func TestGetBodyIsByteExact(t *testing.T) {
	ts, _ := newTestServer(t)
	body := "# Title\r\n\n  trailing  \n\x00\n"
	create(t, ts, `{"title":"a","body":"# Title\r\n\n  trailing  \n\u0000\n"}`)

	resp := do(t, ts, "GET", "/thoughts/1/body", "")
	wantStatus(t, resp, http.StatusOK)
	if ct := resp.Header.Get("Content-Type"); ct != markdown {
		t.Fatalf("Content-Type %q", ct)
	}
	if resp.Header.Get("ETag") != `"1"` {
		t.Fatalf("ETag %q", resp.Header.Get("ETag"))
	}
	if got := readBody(t, resp); got != body {
		t.Fatalf("body %q, want %q", got, body)
	}

	head := do(t, ts, "HEAD", "/thoughts/1/body", "")
	wantStatus(t, head, http.StatusOK)
	if head.Header.Get("ETag") != `"1"` || head.ContentLength != int64(len(body)) || readBody(t, head) != "" {
		t.Fatalf("HEAD: ETag %q, length %d", head.Header.Get("ETag"), head.ContentLength)
	}
}

func TestGetBodyErrors(t *testing.T) {
	ts, _ := newTestServer(t)
	create(t, ts, `{"title":"a"}`)
	wantProblem(t, do(t, ts, "GET", "/thoughts/2/body", ""), http.StatusNotFound)
	wantProblem(t, do(t, ts, "GET", "/thoughts/x/body", ""), http.StatusBadRequest)
	resp := do(t, ts, "GET", "/thoughts/1/body", "", "If-None-Match", `"1"`)
	wantStatus(t, resp, http.StatusNotModified)
	if resp.Header.Get("ETag") != `"1"` {
		t.Fatal("304 without ETag")
	}
}

func TestPutBody(t *testing.T) {
	ts, _ := newTestServer(t)
	create(t, ts, `{"title":"a"}`)

	resp := do(t, ts, "PUT", "/thoughts/1/body", "new\n", "Content-Type", markdown, "If-Match", `"1"`)
	wantStatus(t, resp, http.StatusNoContent)
	if resp.Header.Get("ETag") != `"2"` {
		t.Fatalf("ETag %q", resp.Header.Get("ETag"))
	}
	if got := readBody(t, do(t, ts, "GET", "/thoughts/1/body", "")); got != "new\n" {
		t.Fatalf("body %q", got)
	}

	// Content-Type parameters and case are read loosely; the charset must be UTF-8.
	wantStatus(t, do(t, ts, "PUT", "/thoughts/1/body", "x", "Content-Type", "Text/Markdown", "If-Match", "*"), http.StatusNoContent)
	wantStatus(t, do(t, ts, "PUT", "/thoughts/1/body", "", "Content-Type", "text/markdown;charset=UTF-8", "If-Match", "*"), http.StatusNoContent)
	if got := readBody(t, do(t, ts, "GET", "/thoughts/1/body", "")); got != "" {
		t.Fatalf("an empty PUT should empty the body, got %q", got)
	}
}

func TestPutBodyErrors(t *testing.T) {
	ts, _ := newTestServer(t)
	create(t, ts, `{"title":"a"}`)
	for _, tc := range []struct {
		name    string
		body    string
		headers []string
		code    int
	}{
		{"no If-Match", "x", []string{"Content-Type", markdown}, http.StatusPreconditionRequired},
		{"stale", "x", []string{"Content-Type", markdown, "If-Match", `"9"`}, http.StatusPreconditionFailed},
		{"no Content-Type", "x", []string{"If-Match", "*"}, http.StatusUnsupportedMediaType},
		{"text/plain", "x", []string{"Content-Type", "text/plain", "If-Match", "*"}, http.StatusUnsupportedMediaType},
		{"latin-1", "x", []string{"Content-Type", "text/markdown; charset=iso-8859-1", "If-Match", "*"}, http.StatusUnsupportedMediaType},
		{"invalid UTF-8", "\xff\xfe", []string{"Content-Type", markdown, "If-Match", "*"}, http.StatusUnprocessableEntity},
		{"too large", strings.Repeat("a", api.MaxBody+1), []string{"Content-Type", markdown, "If-Match", "*"}, http.StatusRequestEntityTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantProblem(t, do(t, ts, "PUT", "/thoughts/1/body", tc.body, tc.headers...), tc.code)
		})
	}
	wantStatus(t, do(t, ts, "PUT", "/thoughts/1/body", strings.Repeat("a", api.MaxBody), "Content-Type", markdown, "If-Match", "*"), http.StatusNoContent)
}
