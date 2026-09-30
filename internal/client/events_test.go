package client_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/NurramoX/thoughts/internal/api"
	"github.com/NurramoX/thoughts/internal/client"
)

func TestChangesReadsServerSentEvents(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/events" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, ": a comment\n\n"+
			"data: {\"id\":1,\"version\":2}\n\n"+
			"\n"+
			"data:{\"id\":3,\"deleted\":true}\r\n\r\n")
	})
	s, err := c.Changes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, want := range []api.Change{{ID: 1, Version: 2}, {ID: 3, Deleted: true}} {
		got, err := s.Next()
		if err != nil || got != want {
			t.Fatalf("Next = %+v, %v; want %+v", got, err, want)
		}
	}
	if _, err := s.Next(); !errors.Is(err, io.EOF) {
		t.Errorf("Next at the end = %v, want io.EOF", err)
	}
}

func TestChangesReportsAProblem(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, `{"title":"Service Unavailable","status":503,"detail":"shutting down"}`)
	})
	_, err := c.Changes(context.Background())
	if client.Status(err) != http.StatusServiceUnavailable {
		t.Fatalf("err = %v, want a 503", err)
	}
}

func TestChangesEndsWithItsContext(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	s, err := c.Changes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cancel()
	if _, err := s.Next(); err == nil {
		t.Fatal("Next after cancel returned no error")
	}
}
