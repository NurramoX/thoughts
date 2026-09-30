package editor

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
)

// The open sessions. While there is at least one, SIGINT and SIGTERM remove
// every temp file and then take their default action, so no text is left
// behind however the process ends.
var (
	mu       sync.Mutex
	sessions = map[*Session]bool{}
	signals  chan os.Signal
)

func register(s *Session) {
	mu.Lock()
	defer mu.Unlock()
	if len(sessions) == 0 {
		signals = make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
		go onSignal(signals)
	}
	sessions[s] = true
}

func unregister(s *Session) {
	mu.Lock()
	defer mu.Unlock()
	if !sessions[s] {
		return
	}
	delete(sessions, s)
	if len(sessions) == 0 {
		signal.Stop(signals)
		close(signals)
		signals = nil
	}
}

// onSignal cleans up on the first signal, then re-raises it with the default
// disposition so the process ends the way it was asked to.
func onSignal(c <-chan os.Signal) {
	sig, ok := <-c
	if !ok {
		return
	}
	CloseAll()
	signal.Reset(sig)
	syscall.Kill(os.Getpid(), sig.(syscall.Signal))
}

// CloseAll closes every open Session. Signal handlers call it before exit.
func CloseAll() {
	mu.Lock()
	open := make([]*Session, 0, len(sessions))
	for s := range sessions {
		open = append(open, s)
	}
	mu.Unlock()
	for _, s := range open {
		s.Close()
	}
}
