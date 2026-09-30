package cli

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/NurramoX/thoughts/internal/client"
	"github.com/NurramoX/thoughts/internal/daemon"
	"github.com/NurramoX/thoughts/internal/home"
	"github.com/NurramoX/thoughts/internal/launchd"
	"github.com/NurramoX/thoughts/skill"
)

// installWait is how long install waits for the daemon it started.
const installWait = 10 * time.Second

func runDaemon(a *app, in *input) int {
	if len(in.args) > 0 {
		return a.usagef(in.verb, "too many arguments")
	}
	h, err := home.Resolve()
	if err != nil {
		return a.fail(err)
	}
	ctx, stop := signal.NotifyContext(a.ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := daemon.Run(ctx, h, a.stderr); err != nil {
		return a.fail(err)
	}
	return exitOK
}

// launchdVerb checks what install, uninstall, start and stop share: no
// arguments and no THOUGHTS_HOME.
func (a *app) launchdVerb(in *input) (launchd.Agent, int) {
	if len(in.args) > 0 {
		return launchd.Agent{}, a.usagef(in.verb, "too many arguments")
	}
	if os.Getenv(home.EnvVar) != "" {
		return launchd.Agent{}, a.usage("%s refuses to run while %s is set; launchd never uses it", in.verb.name, home.EnvVar)
	}
	ag, err := agent()
	if err != nil {
		return ag, a.fail(err)
	}
	return ag, exitOK
}

func runInstall(a *app, in *input) int {
	ag, code := a.launchdVerb(in)
	if code != exitOK {
		return code
	}
	h, err := home.Resolve()
	if err != nil {
		return a.fail(err)
	}
	exe, err := os.Executable()
	if err != nil {
		return a.fail(err)
	}
	steps := []func() error{
		func() error { return os.MkdirAll(h.Dir, 0o700) },
		func() error { return os.MkdirAll(ag.LogDir(), 0o755) },
		func() error { return os.MkdirAll(filepath.Dir(ag.PlistPath()), 0o755) },
		func() error { return os.WriteFile(ag.PlistPath(), ag.Plist(exe), 0o644) },
		func() error { return ag.Enable(a.ctx) },
		func() error {
			if ag.Loaded(a.ctx) {
				return ag.Bootout(a.ctx)
			}
			return nil
		},
		func() error { return ag.Bootstrap(a.ctx) },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return a.fail(err)
		}
	}
	a.home = h
	a.c = client.New(h.Socket())
	code, ok := a.await(installWait)
	if !ok {
		a.errorf("installed but not responding, see %s", ag.LogPath())
		return exitUnreachable
	}
	if code != exitOK {
		return code
	}
	fmt.Fprintf(a.stdout, "socket  %s\ndb      %s\nlog     %s\n", h.Socket(), h.DB(), ag.LogPath())
	for _, s := range skill.All() {
		dir := skillDir(ag, s.Name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return a.fail(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), s.Markdown, 0o644); err != nil {
			return a.fail(err)
		}
		fmt.Fprintf(a.stdout, "skill   %s\n", dir)
	}
	return exitOK
}

// skillDir is where Claude Code finds the named skill.
func skillDir(ag launchd.Agent, name string) string {
	return filepath.Join(ag.UserHome, ".claude", "skills", name)
}

func runUninstall(a *app, in *input) int {
	ag, code := a.launchdVerb(in)
	if code != exitOK {
		return code
	}
	if ag.Loaded(a.ctx) {
		if err := ag.Bootout(a.ctx); err != nil {
			return a.fail(err)
		}
	}
	if err := os.Remove(ag.PlistPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return a.fail(err)
	}
	for _, s := range skill.All() {
		if err := os.RemoveAll(skillDir(ag, s.Name)); err != nil {
			return a.fail(err)
		}
	}
	h, err := home.Resolve()
	if err != nil {
		return a.fail(err)
	}
	fmt.Fprintf(a.stdout, "the thoughts remain in %s\nthe logs remain in %s\n", h.Dir, ag.LogDir())
	return exitOK
}

func runStart(a *app, in *input) int {
	ag, code := a.launchdVerb(in)
	if code != exitOK {
		return code
	}
	if _, err := os.Stat(ag.PlistPath()); err != nil {
		a.errorf("not installed (no %s): run `thought install`", ag.PlistPath())
		return exitInternal
	}
	if ag.Loaded(a.ctx) {
		return exitOK
	}
	if err := ag.Bootstrap(a.ctx); err != nil {
		return a.fail(err)
	}
	return exitOK
}

func runStop(a *app, in *input) int {
	ag, code := a.launchdVerb(in)
	if code != exitOK {
		return code
	}
	if !ag.Loaded(a.ctx) {
		return exitOK
	}
	if err := ag.Bootout(a.ctx); err != nil {
		return a.fail(err)
	}
	return exitOK
}
