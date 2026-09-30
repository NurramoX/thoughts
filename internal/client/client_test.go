package client_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/NurramoX/thoughts/internal/api"
	"github.com/NurramoX/thoughts/internal/client"
)

// serve runs h on a Unix socket in a short /tmp directory (macOS caps socket
// paths at 104 bytes) and returns a Client for it.
func serve(t *testing.T, h http.HandlerFunc) client.Client {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "idc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h}
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	return client.New(sock)
}

func TestPing(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		w.Write([]byte(`{"service":"thoughts","api":1}`))
	})
	got, err := c.Ping(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != (api.Service{Service: "thoughts", API: 1}) {
		t.Errorf("got %+v", got)
	}
}

func TestUnreachable(t *testing.T) {
	c := client.New(filepath.Join(t.TempDir(), "missing.sock"))
	_, err := c.Ping(context.Background())
	var ue *client.UnreachableError
	if !errors.As(err, &ue) {
		t.Fatalf("got %T %v, want *UnreachableError", err, err)
	}
}
