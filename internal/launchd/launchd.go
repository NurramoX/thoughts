// Package launchd manages the daemon's LaunchAgent (spec §6): the plist and
// the launchctl calls behind `thought install`, `uninstall`, `start` and `stop`,
// and the "is the job loaded" question of the daemon diagnosis.
package launchd

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/NurramoX/thoughts/internal/home"
)

// Ctl runs launchctl with args and returns its combined output.
type Ctl func(ctx context.Context, args ...string) ([]byte, error)

// Launchctl is the real launchctl.
func Launchctl(ctx context.Context, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, "launchctl", args...).CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("launchctl %s: %v: %s", strings.Join(args, " "), err, bytes.TrimSpace(out))
	}
	return out, nil
}

// Agent is the thoughts LaunchAgent of one user.
type Agent struct {
	UserHome string // the user's home directory
	UID      int
	Ctl      Ctl
}

// PlistPath is ~/Library/LaunchAgents/<label>.plist.
func (a Agent) PlistPath() string {
	return filepath.Join(a.UserHome, "Library", "LaunchAgents", home.Label+".plist")
}

// LogDir is ~/Library/Logs/thoughts.
func (a Agent) LogDir() string { return filepath.Join(a.UserHome, "Library", "Logs", "thoughts") }

// LogPath is where the daemon's stdout and stderr go.
func (a Agent) LogPath() string { return filepath.Join(a.LogDir(), "daemon.log") }

func (a Agent) domain() string  { return "gui/" + strconv.Itoa(a.UID) }
func (a Agent) service() string { return a.domain() + "/" + home.Label }

// Loaded reports whether launchd knows the job (`launchctl print` succeeds).
func (a Agent) Loaded(ctx context.Context) bool {
	_, err := a.Ctl(ctx, "print", a.service())
	return err == nil
}

// Enable clears any disabled override for the job.
func (a Agent) Enable(ctx context.Context) error {
	_, err := a.Ctl(ctx, "enable", a.service())
	return err
}

// Bootstrap loads the plist into the user's GUI domain, which starts the
// daemon (RunAtLoad).
func (a Agent) Bootstrap(ctx context.Context) error {
	_, err := a.Ctl(ctx, "bootstrap", a.domain(), a.PlistPath())
	return err
}

// unloadWait bounds how long Bootout waits for launchd to drop the job: the
// plist's ExitTimeOut plus slack.
const unloadWait = 15 * time.Second

// Bootout unloads the job, stopping the daemon. launchctl returns before
// launchd has torn the job down, and a bootstrap in the meantime fails with
// "5: Input/output error", so Bootout waits until the job is gone.
func (a Agent) Bootout(ctx context.Context) error {
	if _, err := a.Ctl(ctx, "bootout", a.service()); err != nil {
		return err
	}
	deadline := time.Now().Add(unloadWait)
	for a.Loaded(ctx) {
		if time.Now().After(deadline) {
			return fmt.Errorf("launchd still has %s %v after bootout", a.service(), unloadWait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return nil
}

// Plist renders the job for the executable exe (symlinks not resolved).
func (a Agent) Plist(exe string) []byte {
	var b bytes.Buffer
	str := func(s string) string {
		var e bytes.Buffer
		xml.EscapeText(&e, []byte(s))
		return "<string>" + e.String() + "</string>"
	}
	fmt.Fprintf(&b, `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	%s
	<key>ProgramArguments</key>
	<array>
		%s
		<string>daemon</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>StandardOutPath</key>
	%s
	<key>StandardErrorPath</key>
	%s
	<key>ExitTimeOut</key>
	<integer>10</integer>
</dict>
</plist>
`, str(home.Label), str(exe), str(a.LogPath()), str(a.LogPath()))
	return b.Bytes()
}
