package launchd_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/NurramoX/thoughts/internal/launchd"
)

const wantPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>io.github.nurramox.thoughts</string>
	<key>ProgramArguments</key>
	<array>
		<string>/opt/b&amp;w/thought</string>
		<string>daemon</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>StandardOutPath</key>
	<string>/Users/me/Library/Logs/thoughts/daemon.log</string>
	<key>StandardErrorPath</key>
	<string>/Users/me/Library/Logs/thoughts/daemon.log</string>
	<key>ExitTimeOut</key>
	<integer>10</integer>
</dict>
</plist>
`

func TestPlist(t *testing.T) {
	a := launchd.Agent{UserHome: "/Users/me", UID: 501}
	got := string(a.Plist("/opt/b&w/thought"))
	if got != wantPlist {
		t.Errorf("got\n%s", got)
	}
	if _, err := exec.LookPath("plutil"); err == nil {
		p := filepath.Join(t.TempDir(), "x.plist")
		os.WriteFile(p, []byte(got), 0o644)
		if out, err := exec.Command("plutil", "-lint", p).CombinedOutput(); err != nil {
			t.Errorf("plutil: %s", out)
		}
	}
}

func TestPaths(t *testing.T) {
	a := launchd.Agent{UserHome: "/Users/me", UID: 501}
	if got := a.PlistPath(); got != "/Users/me/Library/LaunchAgents/io.github.nurramox.thoughts.plist" {
		t.Errorf("PlistPath %s", got)
	}
	if got := a.LogPath(); got != "/Users/me/Library/Logs/thoughts/daemon.log" {
		t.Errorf("LogPath %s", got)
	}
}

func TestLaunchctlCalls(t *testing.T) {
	var calls []string
	loaded := false
	lingers := 0 // prints that still find the job after bootout
	a := launchd.Agent{UserHome: "/Users/me", UID: 501, Ctl: func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		switch args[0] {
		case "print":
			if lingers > 0 {
				lingers--
			} else if !loaded {
				return nil, errors.New("exit status 113")
			}
		case "bootout":
			loaded, lingers = false, 2
		}
		return nil, nil
	}}
	ctx := context.Background()
	if a.Loaded(ctx) {
		t.Error("loaded")
	}
	loaded = true
	if !a.Loaded(ctx) {
		t.Error("not loaded")
	}
	a.Enable(ctx)
	a.Bootstrap(ctx)
	a.Bootout(ctx)
	want := []string{
		"print gui/501/io.github.nurramox.thoughts",
		"print gui/501/io.github.nurramox.thoughts",
		"enable gui/501/io.github.nurramox.thoughts",
		"bootstrap gui/501 /Users/me/Library/LaunchAgents/io.github.nurramox.thoughts.plist",
		"bootout gui/501/io.github.nurramox.thoughts",
		"print gui/501/io.github.nurramox.thoughts",
		"print gui/501/io.github.nurramox.thoughts",
		"print gui/501/io.github.nurramox.thoughts",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("calls\n%q\nwant\n%q", calls, want)
	}
}
