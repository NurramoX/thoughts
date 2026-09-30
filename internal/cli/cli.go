// Package cli is the `thought` command line (spec §7): hand-rolled verb
// dispatch, output, exit codes, launchd management and completion.
package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"

	"golang.org/x/term"

	"github.com/NurramoX/thoughts/internal/client"
	"github.com/NurramoX/thoughts/internal/home"
	"github.com/NurramoX/thoughts/internal/launchd"
)

// Version is the binary's version, set at build time with
// -ldflags "-X github.com/NurramoX/thoughts/internal/cli.Version=...".
var Version = "dev"

// Exit codes (spec §7).
const (
	exitOK          = 0
	exitInternal    = 1
	exitUsage       = 2
	exitNotFound    = 3
	exitStale       = 4
	exitRejected    = 5
	exitMalformed   = 6
	exitUnreachable = 7
	exitMismatch    = 8
)

// Seams for tests: launchctl and terminal detection.
var (
	launchctl  launchd.Ctl = launchd.Launchctl
	isTerminal             = func(x any) bool {
		f, ok := x.(*os.File)
		return ok && term.IsTerminal(int(f.Fd()))
	}
)

// app is one invocation.
type app struct {
	ctx    context.Context
	stdin  io.Reader
	in     *bufio.Reader // stdin for prompts, made on first use
	stdout io.Writer
	stderr io.Writer
	// json: the verb was given --json, so problems go to stderr verbatim.
	json bool
	home home.Home
	c    client.Client
}

// Main runs `thought` with args (without the program name) and returns the exit
// code.
func Main(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	a := &app{ctx: context.Background(), stdin: stdin, stdout: stdout, stderr: stderr}
	if len(args) == 0 {
		args = []string{"review"}
	}
	name := args[0]
	if name == "-h" || name == "--help" {
		return a.help(nil)
	}
	v := lookup(name)
	if v == nil {
		return a.usage("unknown verb '%s'; see `thought help`", name)
	}
	in, err := parse(v, args[1:])
	if err != nil {
		return a.usagef(v, "%v", err)
	}
	if in.bool("-h") || in.bool("--help") {
		return a.help(v)
	}
	a.json = in.bool("--json")
	return v.run(a, in)
}

// usage reports a usage error (exit 2).
func (a *app) usage(format string, args ...any) int {
	fmt.Fprintf(a.stderr, "thought: "+format+"\n", args...)
	return exitUsage
}

// usagef reports a usage error of verb v with its usage line.
func (a *app) usagef(v *verb, format string, args ...any) int {
	fmt.Fprintf(a.stderr, "thought %s: %s\nusage: thought %s\n", v.name, fmt.Sprintf(format, args...), v.usage)
	return exitUsage
}

// errorf prints a diagnostic.
func (a *app) errorf(format string, args ...any) {
	fmt.Fprintf(a.stderr, "thought: "+format+"\n", args...)
}

// prompt asks a question on stderr and reads one line of answer from stdin.
// ok is false at end of input.
func (a *app) prompt(q string) (answer string, ok bool) {
	fmt.Fprint(a.stderr, q)
	if a.in == nil {
		a.in = bufio.NewReader(a.stdin)
	}
	line, err := a.in.ReadString('\n')
	if err != nil && line == "" {
		fmt.Fprintln(a.stderr)
		return "", false
	}
	return trimLine(line), true
}

// color reports whether stdout gets colour.
func (a *app) color() bool {
	return os.Getenv("NO_COLOR") == "" && isTerminal(a.stdout)
}
