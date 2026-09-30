package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/NurramoX/thoughts/internal/client"
	"github.com/NurramoX/thoughts/skill"
)

// launchdHome gives the test its own $HOME, no THOUGHTS_HOME, and a fake
// launchctl whose bootstrap starts a fake daemon in the default home.
func launchdHome(t *testing.T, loaded bool) (home string, calls *[]string) {
	t.Helper()
	home = shortDir(t)
	t.Setenv("HOME", home)
	t.Setenv("THOUGHTS_HOME", "")
	calls = new([]string)
	old := launchctl
	launchctl = func(_ context.Context, args ...string) ([]byte, error) {
		*calls = append(*calls, strings.Join(args, " "))
		switch args[0] {
		case "print":
			if !loaded {
				return nil, errors.New("exit status 113")
			}
		case "bootstrap":
			dir := filepath.Join(home, "Library", "Application Support", "thoughts")
			os.MkdirAll(dir, 0o700) // as the daemon does
			startFake(t, dir)
			loaded = true
		case "bootout":
			loaded = false
		}
		return nil, nil
	}
	t.Cleanup(func() { launchctl = old })
	return home, calls
}

var service = "gui/" + strconv.Itoa(os.Getuid()) + "/io.github.nurramox.thoughts"

func TestInstall(t *testing.T) {
	home, calls := launchdHome(t, false)
	app := filepath.Join(home, "Library", "Application Support", "thoughts")
	plist := filepath.Join(home, "Library", "LaunchAgents", "io.github.nurramox.thoughts.plist")
	log := filepath.Join(home, "Library", "Logs", "thoughts", "daemon.log")
	skills := filepath.Join(home, ".claude", "skills")
	run("", "install").expect(t, 0, ""+
		"socket  "+app+"/thoughts.sock\n"+
		"db      "+app+"/thoughts.db\n"+
		"log     "+log+"\n"+
		"skill   "+skills+"/new-thought\n"+
		"skill   "+skills+"/thought\n", "")
	want := []string{
		"enable " + service,
		"print " + service,
		"bootstrap gui/" + strconv.Itoa(os.Getuid()) + " " + plist,
	}
	if !reflect.DeepEqual(*calls, want) {
		t.Errorf("calls\n%q\nwant\n%q", *calls, want)
	}
	b, err := os.ReadFile(plist)
	exe, _ := os.Executable()
	if err != nil || !strings.Contains(string(b), "<string>"+exe+"</string>") || !strings.Contains(string(b), log) {
		t.Errorf("plist %s %v", b, err)
	}
	for _, s := range skill.All() {
		if b, _ := os.ReadFile(filepath.Join(skills, s.Name, "SKILL.md")); string(b) != string(s.Markdown) {
			t.Errorf("skill %s %q", s.Name, b)
		}
	}
	if fi, err := os.Stat(app); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("home %v %v", fi, err)
	}
}

func TestReinstallBootsOutFirst(t *testing.T) {
	_, calls := launchdHome(t, true)
	if r := run("", "install"); r.code != 0 {
		t.Fatalf("got %+v", r)
	}
	if (*calls)[2] != "bootout "+service || (*calls)[3] != "print "+service || !strings.HasPrefix((*calls)[4], "bootstrap ") {
		t.Errorf("calls %q", *calls)
	}
}

func TestUninstall(t *testing.T) {
	home, calls := launchdHome(t, true)
	plist := filepath.Join(home, "Library", "LaunchAgents", "io.github.nurramox.thoughts.plist")
	os.MkdirAll(filepath.Dir(plist), 0o755)
	writeFile(t, plist, "")
	skills := filepath.Join(home, ".claude", "skills")
	for _, name := range []string{"thought", "new-thought"} {
		os.MkdirAll(filepath.Join(skills, name), 0o755)
		writeFile(t, filepath.Join(skills, name, "SKILL.md"), "")
	}
	app := filepath.Join(home, "Library", "Application Support", "thoughts")
	run("", "uninstall").expect(t, 0, "the thoughts remain in "+app+"\nthe logs remain in "+filepath.Join(home, "Library", "Logs", "thoughts")+"\n", "")
	if !reflect.DeepEqual(*calls, []string{"print " + service, "bootout " + service, "print " + service}) {
		t.Errorf("calls %q", *calls)
	}
	for _, p := range []string{plist, filepath.Join(skills, "thought"), filepath.Join(skills, "new-thought")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s remains", p)
		}
	}
}

func TestStartStop(t *testing.T) {
	home, calls := launchdHome(t, false)
	r := run("", "start")
	if r.code == 0 || !strings.Contains(r.err, "run `thought install`") {
		t.Errorf("start without plist: %+v", r)
	}
	plist := filepath.Join(home, "Library", "LaunchAgents", "io.github.nurramox.thoughts.plist")
	os.MkdirAll(filepath.Dir(plist), 0o755)
	writeFile(t, plist, "")
	run("", "start").expect(t, 0, "", "")
	run("", "stop").expect(t, 0, "", "")
	run("", "stop").expect(t, 0, "", "")
	want := []string{
		"print " + service,
		"bootstrap gui/" + strconv.Itoa(os.Getuid()) + " " + plist,
		"print " + service,
		"bootout " + service,
		"print " + service,
		"print " + service,
	}
	if !reflect.DeepEqual(*calls, want) {
		t.Errorf("calls\n%q\nwant\n%q", *calls, want)
	}
}

func TestLaunchdVerbsRefuseCustomHome(t *testing.T) {
	calls := fakeLaunchctl(t, true)
	t.Setenv("THOUGHTS_HOME", shortDir(t))
	for _, v := range []string{"install", "uninstall", "start", "stop"} {
		if r := run("", v); r.code != 2 || !strings.Contains(r.err, "THOUGHTS_HOME") {
			t.Errorf("%s: %+v", v, r)
		}
	}
	if len(*calls) != 0 {
		t.Errorf("calls %q", *calls)
	}
}

func TestDaemonTakesNoArguments(t *testing.T) {
	if r := run("", "daemon", "x"); r.code != 2 {
		t.Errorf("got %+v", r)
	}
}

func TestReview(t *testing.T) {
	e := newEnv(t)
	var got []string
	old := runTUI
	runTUI = func(_ context.Context, _ client.Client, filter string) ([][]byte, error) {
		got = append(got, filter)
		return [][]byte{[]byte("mine\n"), []byte("also")}, nil
	}
	t.Cleanup(func() { runTUI = old })
	run("").expect(t, 0, "mine\nalso", "")
	run("", "review", "tag:rust", "-", "x").expect(t, 0, "mine\nalso", "")
	if !reflect.DeepEqual(got, []string{"", "tag:rust - x"}) {
		t.Errorf("filters %q", got)
	}
	e.api.api = 3
	if r := run("", "review"); r.code != 8 || len(got) != 2 {
		t.Errorf("mismatch reached the TUI: %+v", r)
	}
}
