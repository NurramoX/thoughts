package daemon

import (
	"errors"
	"os"
	"syscall"
)

// lock is a held flock on daemon.lock.
type lock struct{ f *os.File }

// acquire takes flock(LOCK_EX|LOCK_NB) on path, creating the file if needed.
// It returns ErrAlreadyRunning when another process holds it.
func acquire(path string) (*lock, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrAlreadyRunning
		}
		return nil, err
	}
	return &lock{f: f}, nil
}

// release unlocks and closes the file, which stays in the home.
func (l *lock) release() {
	syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	l.f.Close()
}
