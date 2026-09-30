package cli

import (
	"fmt"
	"strings"
)

// help prints the verb list (v nil) or one verb's help to stdout.
func (a *app) help(v *verb) int {
	var b strings.Builder
	if v == nil {
		b.WriteString("thought: the one home for thoughts.\n\nusage: thought <verb> [args]   (bare `thought` is `thought review`)\n\nverbs:\n")
		for _, v := range verbs {
			if !v.hidden {
				fmt.Fprintf(&b, "  %-11s%s\n", v.name, v.summary)
			}
		}
		b.WriteString("\nRun `thought help <verb>` for its flags and an example.\n")
	} else {
		fmt.Fprintf(&b, "usage: thought %s\n\n%s\n", v.usage, v.about)
		if len(v.flags) > 0 {
			b.WriteString("\nflags:\n")
			for _, f := range v.flags {
				fmt.Fprintf(&b, "  %-20s%s\n", strings.TrimSpace(f.name+" "+f.arg), f.help)
			}
		}
		fmt.Fprintf(&b, "\nexample:\n  %s\n", v.example)
	}
	fmt.Fprint(a.stdout, b.String())
	return exitOK
}

func runHelp(a *app, in *input) int {
	switch len(in.args) {
	case 0:
		return a.help(nil)
	case 1:
		if v := lookup(in.args[0]); v != nil {
			return a.help(v)
		}
		return a.usage("unknown verb '%s'; see `thought help`", in.args[0])
	}
	return a.usagef(in.verb, "too many arguments")
}
