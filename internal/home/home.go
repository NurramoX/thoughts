// Package home locates the daemon's home directory and the files in it
// (spec §6). The daemon and the CLI read it the same way.
package home

import (
	"os"
	"path/filepath"
)

// EnvVar replaces the home root for tests and dev builds.
const EnvVar = "THOUGHTS_HOME"

// Label is the launchd job label.
const Label = "io.github.nurramox.thoughts"

// Home is a resolved home directory.
type Home struct {
	Dir string
	// Custom is true when THOUGHTS_HOME set Dir.
	Custom bool
}

// Resolve returns $THOUGHTS_HOME when set, otherwise
// ~/Library/Application Support/thoughts.
func Resolve() (Home, error) {
	if d := os.Getenv(EnvVar); d != "" {
		abs, err := filepath.Abs(d)
		if err != nil {
			return Home{}, err
		}
		return Home{Dir: abs, Custom: true}, nil
	}
	u, err := os.UserHomeDir()
	if err != nil {
		return Home{}, err
	}
	return Home{Dir: filepath.Join(u, "Library", "Application Support", "thoughts")}, nil
}

func (h Home) DB() string     { return filepath.Join(h.Dir, "thoughts.db") }
func (h Home) Socket() string { return filepath.Join(h.Dir, "thoughts.sock") }
func (h Home) Lock() string   { return filepath.Join(h.Dir, "daemon.lock") }
