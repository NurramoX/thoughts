package daemon

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/NurramoX/thoughts/internal/home"
)

// shortHome returns a home under /tmp whose socket path stays well under the
// 104-byte limit, which t.TempDir() on macOS can exceed. The directory itself
// is not created.
func shortHome(t *testing.T) home.Home {
	t.Helper()
	parent, err := os.MkdirTemp("/tmp", "thought")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(parent) })
	return home.Home{Dir: filepath.Join(parent, "h"), Custom: true}
}

func mode(t *testing.T, path string) fs.FileMode {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// holdLock takes daemon.lock the way another daemon would.
func holdLock(t *testing.T, h home.Home) {
	t.Helper()
	f, err := os.OpenFile(h.Lock(), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
}

// running is a daemon started by start.
type running struct {
	cancel context.CancelFunc
	done   chan error
	client *http.Client
}

// start runs the daemon and waits until it answers on its socket.
func start(t *testing.T, h home.Home) *running {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	r := &running{cancel: cancel, done: make(chan error, 1)}
	go func() { r.done <- Run(ctx, h, io.Discard) }()
	t.Cleanup(func() {
		cancel()
		<-r.done
	})
	r.client = &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", h.Socket())
		},
	}}
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := r.client.Get("http://thoughts/")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET / = %d", resp.StatusCode)
			}
			return r
		}
		select {
		case err := <-r.done:
			r.done <- err // for the cleanup
			t.Fatalf("daemon exited before serving: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("daemon not answering: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// stop cancels the daemon and returns what Run returned. It is safe to call
// from any goroutine.
func (r *running) stop() error {
	r.cancel()
	r.client.CloseIdleConnections()
	select {
	case err := <-r.done:
		r.done <- err // for the cleanup
		return err
	case <-time.After(10 * time.Second):
		return errors.New("daemon did not stop")
	}
}

func TestSecondInstanceTouchesNothing(t *testing.T) {
	h := shortHome(t)
	if err := os.Mkdir(h.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	holdLock(t, h)

	err := Run(t.Context(), h, io.Discard)
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("Run = %v, want ErrAlreadyRunning", err)
	}
	if m := mode(t, h.Dir); m != 0o755 {
		t.Fatalf("home mode changed to %o", m)
	}
	if exists(h.Socket()) || exists(h.DB()) {
		t.Fatal("a refused daemon created the socket or the database")
	}
}

// homeWithSocketPath is a home under a short parent whose socket path is
// exactly n bytes long.
func homeWithSocketPath(t *testing.T, n int) home.Home {
	t.Helper()
	parent := shortHome(t).Dir
	pad := n - len(parent) - len("/") - len("/thoughts.sock")
	h := home.Home{Dir: filepath.Join(parent, strings.Repeat("d", pad))}
	if len(h.Socket()) != n {
		t.Fatalf("socket path is %d bytes, want %d", len(h.Socket()), n)
	}
	return h
}

// macOS's sun_path holds 104 bytes including the terminating NUL, so 103 is
// the longest path that binds.
func TestRefusesASocketPathOf104Bytes(t *testing.T) {
	h := homeWithSocketPath(t, 104)
	err := Run(t.Context(), h, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "103") {
		t.Fatalf("Run = %v, want a socket path error", err)
	}
	if exists(h.Dir) {
		t.Fatal("a refused daemon created its home")
	}
}

// The tests below need the real store.Open.

func TestServesOnASocketPathOf103Bytes(t *testing.T) {
	h := homeWithSocketPath(t, 103)
	start(t, h)
	if !exists(h.Socket()) {
		t.Fatal("no socket")
	}
}

func TestServesWithPrivateModes(t *testing.T) {
	h := shortHome(t)
	if err := os.Mkdir(h.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	start(t, h)
	if m := mode(t, h.Dir); m != 0o700 {
		t.Fatalf("home mode %o, want 700", m)
	}
	if m := mode(t, h.Socket()); m != 0o600 {
		t.Fatalf("socket mode %o, want 600", m)
	}
}

func TestCreatesItsHome(t *testing.T) {
	h := shortHome(t)
	start(t, h)
	if m := mode(t, h.Dir); m != 0o700 {
		t.Fatalf("home mode %o, want 700", m)
	}
}

func TestLockIsHeldWhileRunning(t *testing.T) {
	h := shortHome(t)
	start(t, h)
	if err := Run(t.Context(), h, io.Discard); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second Run = %v, want ErrAlreadyRunning", err)
	}
	if !exists(h.Socket()) {
		t.Fatal("the refused daemon removed the running one's socket")
	}
}

func TestReplacesStaleSocket(t *testing.T) {
	h := shortHome(t)
	if err := os.Mkdir(h.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", h.Socket())
	if err != nil {
		t.Fatal(err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	ln.Close()

	start(t, h)
}

func TestShutdownCleansUp(t *testing.T) {
	h := shortHome(t)
	r := start(t, h)
	if err := r.stop(); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	if exists(h.Socket()) {
		t.Fatal("socket left behind")
	}
	if !exists(h.DB()) {
		t.Fatal("no database")
	}
	if fi, err := os.Stat(h.DB() + "-wal"); err == nil && fi.Size() != 0 {
		t.Fatalf("WAL not checkpointed: %d bytes", fi.Size())
	}
	// The lock is free again.
	holdLock(t, h)
}

func TestShutdownDrainsInFlightRequests(t *testing.T) {
	h := shortHome(t)
	r := start(t, h)
	resp, err := r.client.Post("http://thoughts/thoughts", "application/json", strings.NewReader(`{"title":"a"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /thoughts = %d", resp.StatusCode)
	}

	// A request whose body is still arriving when shutdown starts is
	// served once the body is complete.
	conn, err := net.Dial("unix", h.Socket())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	head := "PUT /thoughts/1/body HTTP/1.1\r\nHost: x\r\nContent-Type: text/markdown\r\n" +
		"If-Match: *\r\nContent-Length: 4\r\nConnection: close\r\n\r\nbo"
	if _, err := io.WriteString(conn, head); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond) // let the server read the headers
	stopped := make(chan error, 1)
	go func() { stopped <- r.stop() }()
	time.Sleep(100 * time.Millisecond)
	if _, err := io.WriteString(conn, "dy"); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(conn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), "HTTP/1.1 204") {
		t.Fatalf("in-flight request got %q", got)
	}
	if err := <-stopped; err != nil {
		t.Fatalf("Run = %v", err)
	}
}

func TestShutdownEndsEventStreams(t *testing.T) {
	h := shortHome(t)
	r := start(t, h)
	resp, err := r.client.Get("http://thoughts/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /events = %d", resp.StatusCode)
	}
	began := time.Now()
	if err := r.stop(); err != nil {
		t.Fatalf("Run = %v", err)
	}
	if took := time.Since(began); took >= drainTimeout {
		t.Errorf("shutdown took %v: it waited out the drain for the stream", took)
	}
	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Errorf("the stream did not end cleanly: %v", err)
	}
}
