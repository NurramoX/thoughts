package cli

import (
	"strings"
	"testing"
)

func TestHelp(t *testing.T) {
	r := run("", "help")
	if r.code != 0 || r.err != "" || !strings.Contains(r.out, "  replace    replace text in a thought's body\n") {
		t.Errorf("got %+v", r)
	}
	if strings.Contains(r.out, "complete ") {
		t.Error("hidden verb listed")
	}
	if r2 := run("", "--help"); r2 != r {
		t.Errorf("--help differs: %+v", r2)
	}
	if r := run("", "help", "nope"); r.code != 2 {
		t.Errorf("unknown verb: %+v", r)
	}
}

func TestEveryVerbHasHelpWithAnExample(t *testing.T) {
	for _, v := range verbs {
		r := run("", "help", v.name)
		if r.code != 0 || !strings.HasPrefix(r.out, "usage: thought "+v.name) || !strings.Contains(r.out, "\nexample:\n  ") ||
			!strings.Contains(r.out, "thought ") {
			t.Errorf("%s: %+v", v.name, r)
		}
		if r2 := run("", v.name, "x", "-h"); r2 != r {
			t.Errorf("%s -h differs from help %s", v.name, v.name)
		}
	}
}

func TestHelpAfterDoubleDashIsAnArgument(t *testing.T) {
	e := newEnv(t)
	run("", "add", "--", "-h").expect(t, 0, "1\n", "")
	if e.api.get(1).Title != "-h" {
		t.Errorf("title %q", e.api.get(1).Title)
	}
}

func TestVersion(t *testing.T) {
	e := newEnv(t)
	run("", "version").expect(t, 0, "thought dev (api 1)\ndaemon api 1\n", "")
	e.api.api = 2
	run("", "version").expect(t, 0, "thought dev (api 1)\ndaemon api 2\n",
		"thought: warning: the daemon speaks api 2, this thought speaks api 1: run `thought install`\n")
	t.Setenv("THOUGHTS_HOME", shortDir(t))
	run("", "version").expect(t, 0, "thought dev (api 1)\n", "")
}
