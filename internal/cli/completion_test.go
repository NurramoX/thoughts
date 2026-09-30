package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCompleteVerbs(t *testing.T) {
	r := run("", "complete", "verbs", "re")
	r.expect(t, 0, "replace\nreview\n", "")
	r = run("", "complete", "verbs")
	if strings.Contains(r.out, "complete\n") || !strings.HasPrefix(r.out, "add\nshow\n") {
		t.Errorf("got %q", r.out)
	}
}

func TestCompleteFromTheDaemon(t *testing.T) {
	e := newEnv(t)
	e.api.add("Borrow checker", "", []string{"rust", "go"}, map[string]string{"effort": "small"})
	e.api.add("Other", "", []string{"release"}, nil)
	run("", "complete", "tags", "r").expect(t, 0, "release\nrust\n", "")
	run("", "complete", "keys").expect(t, 0, "effort\nstatus\n", "")
	run("", "complete", "values", "effort", "--", "s").expect(t, 0, "small\n", "")
	run("", "complete", "values", "status").expect(t, 0, "raw\nactive\ndone\ndropped\n", "")
	run("", "complete", "ids").expect(t, 0, "2\tOther\n1\tBorrow checker\n", "")
	if got := e.api.requests(); got[len(got)-1] != "GET /thoughts?limit=50&order=desc&sort=updated" {
		t.Errorf("request %q", got[len(got)-1])
	}
}

func TestCompleteIsSilentWhenTheDaemonIsDown(t *testing.T) {
	t.Setenv("THOUGHTS_HOME", "")
	t.Setenv("HOME", shortDir(t))
	fakeLaunchctl(t, true)
	start := time.Now()
	run("", "complete", "tags").expect(t, 0, "", "")
	if time.Since(start) > time.Second {
		t.Error("retried")
	}
	run("", "complete", "values", "status", "d").expect(t, 0, "done\ndropped\n", "")
}

func TestCompleteUsage(t *testing.T) {
	for _, args := range [][]string{{"complete"}, {"complete", "files"}, {"complete", "values"}, {"complete", "tags", "a", "b"}} {
		if r := run("", args...); r.code != 2 {
			t.Errorf("%q: %+v", args, r)
		}
	}
}

func TestCompletionScripts(t *testing.T) {
	for _, sh := range []string{"fish", "zsh", "bash"} {
		r := run("", "completion", sh)
		if r.code != 0 || !strings.Contains(r.out, "thought complete") {
			t.Errorf("%s: %+v", sh, r)
			continue
		}
		if _, err := exec.LookPath(sh); err != nil {
			continue
		}
		p := filepath.Join(t.TempDir(), "thought."+sh)
		writeFile(t, p, r.out)
		if out, err := exec.Command(sh, "-n", p).CombinedOutput(); err != nil {
			t.Errorf("%s -n: %v %s", sh, err, out)
		}
	}
	if r := run("", "completion", "tcsh"); r.code != 2 {
		t.Errorf("tcsh: %+v", r)
	}
}

// stubThought puts on PATH an `thought` that answers `thought complete` with canned
// candidates, so the scripts' own logic is what gets tested.
func stubThought(t *testing.T) string {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "thought"), `#!/bin/sh
shift
for prefix; do :; done
case "$1" in
verbs) printf 'add\nshow\ntag\n' ;;
tags) printf 'rust\nrelease\n' ;;
keys) printf 'effort\nstatus\n' ;;
values) printf 'v-%s\n' "$2" ;;
ids) printf '42\tBorrow checker\n' ;;
esac | grep "^$prefix"
`)
	os.Chmod(filepath.Join(dir, "thought"), 0o755)
	return dir
}

var completionCases = []struct {
	line string
	want []string
}{
	{"thought ", []string{"add", "show", "tag"}},
	{"thought tag ", []string{"42"}},
	{"thought tag 42 ", []string{"rust", "release"}},
	{"thought tag --version 3 42 r", []string{"rust", "release"}},
	{"thought set 42 eff", []string{"effort="}},
	{"thought set 42 effort=", []string{"effort=v-effort"}},
	{"thought status 42 ", []string{"v-status"}},
	{"thought status 42 done ", nil},
	{"thought add T -t ", []string{"rust", "release"}},
	{"thought show 1 ", []string{"42"}},
	{"thought ls ", nil},
	{"thought help ", []string{"add", "show", "tag"}},
	{"thought ls --sort ", []string{"updated", "created", "title", "rank"}},
}

func TestFishCompletion(t *testing.T) {
	if _, err := exec.LookPath("fish"); err != nil {
		t.Skip("no fish")
	}
	script := filepath.Join(t.TempDir(), "thought.fish")
	writeFile(t, script, run("", "completion", "fish").out)
	path := stubThought(t) + ":" + os.Getenv("PATH")
	for _, tc := range completionCases {
		cmd := exec.Command("fish", "--no-config", "-c", "source $argv[1]; complete -C $argv[2]", script, tc.line)
		cmd.Env = append(os.Environ(), "PATH="+path)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%q: %v %s", tc.line, err, out)
		}
		var got []string
		for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if l != "" {
				got = append(got, strings.SplitN(l, "\t", 2)[0])
			}
		}
		if !sameSet(got, tc.want) {
			t.Errorf("%q: got %q, want %q", tc.line, got, tc.want)
		}
	}
}

func TestBashCompletion(t *testing.T) {
	script := filepath.Join(t.TempDir(), "thought.bash")
	writeFile(t, script, run("", "completion", "bash").out)
	path := stubThought(t) + ":" + os.Getenv("PATH")
	for _, tc := range completionCases {
		// Bash replaces only the part of the word after the last = or :.
		want := slices.Clone(tc.want)
		if strings.HasSuffix(tc.line, "=") {
			for i, w := range want {
				want[i] = strings.TrimPrefix(w, "effort=")
			}
		}
		cmd := exec.Command("/bin/bash", "-c", `source "$1"; COMP_LINE=$2; COMP_POINT=${#COMP_LINE}; _thought; printf '%s\n' "${COMPREPLY[@]}"`, "bash", script, tc.line)
		cmd.Env = append(os.Environ(), "PATH="+path)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%q: %v %s", tc.line, err, out)
		}
		got := strings.Fields(string(out))
		if !sameSet(got, want) {
			t.Errorf("%q: got %q, want %q", tc.line, got, want)
		}
	}
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}
