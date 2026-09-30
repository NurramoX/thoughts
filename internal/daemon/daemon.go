// Package daemon runs the server in the foreground (spec §6): the home
// directory, the single-instance lock, the Unix socket and orderly shutdown.
// It knows nothing about launchd.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/NurramoX/thoughts/internal/home"
	"github.com/NurramoX/thoughts/internal/server"
	"github.com/NurramoX/thoughts/internal/store"
)

// ErrAlreadyRunning: another process holds daemon.lock.
var ErrAlreadyRunning = errors.New("already running")

// maxSocketPath is the longest socket path that binds on macOS: sun_path
// holds 104 bytes including the terminating NUL.
const maxSocketPath = 103

// drainTimeout is how long shutdown waits for in-flight requests before
// closing their connections.
const drainTimeout = 5 * time.Second

// Run serves until ctx is cancelled, then shuts down in the spec's order and
// returns nil. It installs no signal handlers: the caller cancels ctx on
// SIGTERM or SIGINT (the `thought daemon` verb uses signal.NotifyContext).
//
// Start-up order: refuse an over-long socket path, create the home, take
// daemon.lock (ErrAlreadyRunning if another process holds it, having touched
// nothing), chmod the home 0700, open the store, replace a stale socket,
// bind, chmod the socket 0600 and serve. The store opens before the socket
// is bound, so a daemon that cannot open its database never answers.
//
// Lifecycle events and errors go to log; there is no access log.
func Run(ctx context.Context, h home.Home, log io.Writer) error {
	logger := newLogger(log)
	sock := h.Socket()
	if len(sock) > maxSocketPath {
		return fmt.Errorf("socket path %s is %d bytes, over the %d that bind on macOS", sock, len(sock), maxSocketPath)
	}
	if err := os.MkdirAll(h.Dir, 0o700); err != nil {
		return err
	}
	lock, err := acquire(h.Lock())
	if err != nil {
		return err
	}
	defer lock.release()
	if err := os.Chmod(h.Dir, 0o700); err != nil {
		return err
	}
	return serve(ctx, h, logger)
}

func newLogger(w io.Writer) *log.Logger {
	return log.New(w, "thought daemon: ", log.LstdFlags)
}

// serve runs the store and the server while the caller holds the lock.
func serve(ctx context.Context, h home.Home, logger *log.Logger) error {
	st, err := store.Open(h.DB(), time.Now)
	if err != nil {
		return fmt.Errorf("opening the store: %w", err)
	}
	ln, err := listen(h.Socket())
	if err != nil {
		st.Close()
		return err
	}

	handler := server.New(st, time.Now)
	srv := &http.Server{Handler: handler, ErrorLog: logger}
	srv.RegisterOnShutdown(handler.Close)
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	logger.Printf("listening on %s", h.Socket())

	var runErr error
	select {
	case <-ctx.Done():
		logger.Print("shutting down")
	case err := <-served:
		runErr = fmt.Errorf("serving: %w", err)
		logger.Print(runErr)
	}

	// Shutdown closes the listener first, then drains.
	drain, cancel := context.WithTimeout(context.Background(), drainTimeout)
	defer cancel()
	if err := srv.Shutdown(drain); err != nil {
		logger.Printf("drain: %v; closing the remaining connections", err)
		srv.Close()
	}
	if err := st.Close(); err != nil {
		logger.Printf("closing the store: %v", err)
		runErr = errors.Join(runErr, err)
	}
	if err := os.Remove(h.Socket()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		logger.Printf("removing the socket: %v", err)
	}
	if runErr == nil {
		logger.Print("stopped")
	}
	return runErr
}

// listen binds the Unix socket at path, first removing a stale one left by a
// daemon that died (only the lock holder gets here), and makes it 0600. The
// listener leaves the file in place on close, so shutdown removes it only
// after the store is closed.
func listen(path string) (*net.UnixListener, error) {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("removing a stale socket: %w", err)
	}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	ln.SetUnlinkOnClose(false)
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		os.Remove(path)
		return nil, err
	}
	return ln, nil
}
