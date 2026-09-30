package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	// Never touch the real ~/Library, ~/.claude or launchd.
	h, err := os.MkdirTemp("/tmp", "idh")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", h)
	launchctl = func(context.Context, ...string) ([]byte, error) {
		return nil, errors.New("launchctl disabled in tests")
	}
	isTerminal = func(any) bool { return false }
	code := m.Run()
	os.RemoveAll(h)
	os.Exit(code)
}

// env is one test's daemon home with a fake API serving in it.
type env struct {
	t   *testing.T
	dir string
	api *fakeAPI
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := shortDir(t)
	t.Setenv("THOUGHTS_HOME", dir)
	t.Setenv("NO_COLOR", "")
	return &env{t: t, dir: dir, api: startFake(t, dir)}
}

type result struct {
	code     int
	out, err string
}

func run(stdin string, args ...string) result {
	var out, errb bytes.Buffer
	code := Main(args, strings.NewReader(stdin), &out, &errb)
	return result{code, out.String(), errb.String()}
}

func (r result) expect(t *testing.T, code int, out, err string) {
	t.Helper()
	if r.code != code || r.out != out || r.err != err {
		t.Errorf("got code %d\nstdout %q\nstderr %q\nwant code %d\nstdout %q\nstderr %q", r.code, r.out, r.err, code, out, err)
	}
}

// fakeLaunchctl replaces launchctl for one test; loaded answers `print`.
func fakeLaunchctl(t *testing.T, loaded bool) *[]string {
	t.Helper()
	calls := new([]string)
	old := launchctl
	launchctl = func(_ context.Context, args ...string) ([]byte, error) {
		*calls = append(*calls, strings.Join(args, " "))
		if args[0] == "print" && !loaded {
			return nil, errors.New("exit status 113")
		}
		return nil, nil
	}
	t.Cleanup(func() { launchctl = old })
	return calls
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownVerb(t *testing.T) {
	r := run("", "frobnicate")
	if r.code != 2 || !strings.Contains(r.err, "unknown verb 'frobnicate'") || r.out != "" {
		t.Errorf("got %+v", r)
	}
}

func TestPing(t *testing.T) {
	newEnv(t)
	run("", "ping").expect(t, 0, "thoughts api 1\n", "")
}

func TestAPIMismatch(t *testing.T) {
	e := newEnv(t)
	e.api.api = 2
	e.api.add("T", "", nil, nil)
	r := run("", "show", "1")
	if r.code != 8 || !strings.Contains(r.err, "run `thought install`") || r.out != "" {
		t.Errorf("got %+v", r)
	}
	if got := e.api.requests(); len(got) != 1 || got[0] != "GET /" {
		t.Errorf("requests %q", got)
	}
}

func TestNoDaemonAtCustomHome(t *testing.T) {
	dir := shortDir(t)
	t.Setenv("THOUGHTS_HOME", dir)
	calls := fakeLaunchctl(t, true)
	run("", "ping").expect(t, 7, "", "thought: no daemon at "+dir+": run `thought daemon`\n")
	if len(*calls) != 0 {
		t.Errorf("launchctl called: %q", *calls)
	}
}

func TestNotInstalled(t *testing.T) {
	t.Setenv("THOUGHTS_HOME", "")
	calls := fakeLaunchctl(t, false)
	run("", "ping").expect(t, 7, "", "thought: not installed: run `thought install`\n")
	if want := "print gui/" + strconv.Itoa(os.Getuid()) + "/io.github.nurramox.thoughts"; len(*calls) != 1 || (*calls)[0] != want {
		t.Errorf("calls %q", *calls)
	}
}

func TestInstalledButNotResponding(t *testing.T) {
	t.Setenv("THOUGHTS_HOME", "")
	fakeLaunchctl(t, true)
	start := time.Now()
	log := os.Getenv("HOME") + "/Library/Logs/thoughts/daemon.log"
	run("", "ping").expect(t, 7, "", "thought: installed but not responding, see "+log+"\n")
	if d := time.Since(start); d < 2*time.Second || d > 3*time.Second {
		t.Errorf("gave up after %v", d)
	}
}

func TestInstalledAndComingUp(t *testing.T) {
	t.Setenv("THOUGHTS_HOME", "")
	fakeLaunchctl(t, true)
	sockDir := os.Getenv("HOME") + "/Library/Application Support/thoughts"
	os.MkdirAll(sockDir, 0o700)
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	go func() {
		time.Sleep(300 * time.Millisecond)
		if _, err := serveFake(t, sockDir); err != nil {
			t.Error(err)
		}
	}()
	run("", "ping").expect(t, 0, "thoughts api 1\n", "")
}
