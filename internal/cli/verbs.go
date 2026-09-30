package cli

// verb is one `thought <verb>`: its help and what it runs.
type verb struct {
	name    string
	usage   string // after `thought `
	summary string // one line for `thought help`
	about   string // the verb's help text
	example string // one agent-oriented example
	flags   []flagSpec
	hidden  bool // left out of help and completion
	// filter: the positionals are Filter words, so an unknown single-dash
	// argument such as `-has:effort` is a negated term, not a flag.
	filter bool
	run    func(*app, *input) int
}

func (v *verb) flag(name string) *flagSpec {
	for i := range v.flags {
		if v.flags[i].name == name {
			return &v.flags[i]
		}
	}
	return nil
}

var (
	jsonFlag    = flagSpec{"--json", "", "print the API's JSON"}
	versionFlag = flagSpec{"--version", "<n>", "write only if the thought is still at Version n"}
	forceFlag   = flagSpec{"--force", "", "write whatever the Version"}
)

// verbs in help order. Filled in init to break the cycle through help.
var verbs []*verb

func init() {
	verbs = []*verb{
		{
			name:    "add",
			usage:   "add <title> [-t <tag>]... [-s <k>=<v>]... [--body-file <path>|-] [--edit] [--json]",
			summary: "capture a new thought",
			about: "Creates a thought and prints its id. The body is empty unless --body-file names a\n" +
				"file, or `-` for stdin; stdin is never read otherwise. --edit opens $EDITOR\n" +
				"first (seeded with --body-file) and creates the thought when you save a change.",
			example: "thought add 'Borrow checker for config files' -t rust -s effort=small --body-file - <<'EOF'\n" +
				"  Config files could be checked like Rust borrows...\n  EOF",
			flags: []flagSpec{
				{"-t", "<tag>", "add a tag (repeatable)"},
				{"-s", "<k>=<v>", "set an attribute (repeatable), status included"},
				{"--body-file", "<path>", "read the body from a file, or - for stdin"},
				{"--edit", "", "write the body in $EDITOR first"},
				jsonFlag,
			},
			run: runAdd,
		},
		{
			name:    "show",
			usage:   "show <id>... [--json]",
			summary: "print thoughts with their metadata and body",
			about: "Prints each thought: a header (title, id, status, tags, attributes, version,\n" +
				"dates), a blank line, then the body. Thoughts are separated by `---`.\n" +
				"--json prints one envelope per line, with the version to guard a write.",
			example: "thought show 42 --json",
			flags:   []flagSpec{jsonFlag},
			run:     runShow,
		},
		{
			name:    "ls",
			filter:  true,
			usage:   "ls [<filter words>...] [--sort updated|created|title|rank] [--asc|--desc] [--limit <n>] [--offset <n>] [-q|--json]",
			summary: "list thoughts matching a Filter",
			about: "Lists the thoughts matching the Filter, the words joined by spaces. A word\n" +
				"like -has:effort is a negated term; put `--` before one that is also a\n" +
				"flag (-q, -h). Filters: text words, tag:rust,\n" +
				"status:raw,active, key:value, has:key, created:2026-09, updated:..90d,\n" +
				"`or`, -negation and (grouping). The default order is rank for text,\n" +
				"otherwise updated, newest first.",
			example: "thought ls borrow checker tag:rust --json",
			flags: []flagSpec{
				{"--sort", "<field>", "updated, created, title or rank"},
				{"--asc", "", "ascending order"},
				{"--desc", "", "descending order"},
				{"--limit", "<n>", "show at most n thoughts"},
				{"--offset", "<n>", "skip the first n thoughts"},
				{"-q", "", "print ids only"},
				jsonFlag,
			},
			run: runLs,
		},
		{
			name:    "body",
			usage:   "body <id>",
			summary: "print a thought's body, byte-exact",
			about:   "Prints the raw markdown body exactly as stored.",
			example: "thought body 42 > /tmp/thought-42.md",
			run:     runBody,
		},
		{
			name:    "write",
			usage:   "write <id> --version <n>|--force [--body-file <path>] [--json]",
			summary: "replace a thought's body",
			about: "Replaces the body with stdin, or with --body-file. Needs the Version you read\n" +
				"(thought show --json) or --force. Empty input writes an empty body.",
			example: "thought write 42 --version 7 --json < body.md",
			flags:   []flagSpec{versionFlag, forceFlag, {"--body-file", "<path>", "read the body from a file, or - for stdin"}, jsonFlag},
			run:     runWrite,
		},
		{
			name:    "replace",
			usage:   "replace <id> <old> <new> [--old-file <path>] [--new-file <path>] [--all] [--version <n>|--force] [--json]",
			summary: "replace text in a thought's body",
			about: "Replaces <old> with <new>, byte-exact. <old> must occur exactly once unless\n" +
				"--all (exit 5 otherwise). An empty <new> deletes. --old-file and --new-file\n" +
				"take the text from files instead of arguments. If the thought changes\n" +
				"meanwhile, it re-reads and retries up to 3 times.",
			example: "thought replace 42 'small effort' 'medium effort' --json",
			flags: []flagSpec{
				{"--old-file", "<path>", "read <old> from a file"},
				{"--new-file", "<path>", "read <new> from a file"},
				{"--all", "", "replace every occurrence"},
				versionFlag, forceFlag, jsonFlag,
			},
			run: runReplace,
		},
		{
			name:    "edit",
			usage:   "edit <id> [--version <n>|--force] [--json]",
			summary: "edit a thought's body in $EDITOR",
			about: "Opens the body in $VISUAL, $EDITOR or vi, and writes it back when you save a\n" +
				"change. If the thought changed meanwhile, asks: [o]verwrite, [r]e-edit or\n" +
				"[a]bort; abort prints your text to stdout.",
			example: "EDITOR=nvim thought edit 42",
			flags:   []flagSpec{versionFlag, forceFlag, jsonFlag},
			run:     runEdit,
		},
		{
			name:    "title",
			usage:   "title <id> <words...> [--version <n>|--force] [--json]",
			summary: "rename a thought",
			about:   "Sets the title to the words joined by single spaces.",
			example: "thought title 42 Borrow checker for config files --version 7",
			flags:   []flagSpec{versionFlag, forceFlag, jsonFlag},
			run:     runTitle,
		},
		{
			name:    "tag",
			usage:   "tag <id> <tag>... [--version <n>] [--json]",
			summary: "add tags",
			about:   "Adds each tag in order, stopping at the first failure.",
			example: "thought tag 42 rust config",
			flags:   []flagSpec{versionFlag, jsonFlag},
			run:     runTag,
		},
		{
			name:    "untag",
			usage:   "untag <id> <tag>... [--version <n>] [--json]",
			summary: "remove tags",
			about:   "Removes each tag in order, stopping at the first failure.",
			example: "thought untag 42 config",
			flags:   []flagSpec{versionFlag, jsonFlag},
			run:     runUntag,
		},
		{
			name:    "set",
			usage:   "set <id> <k>=<v>... [--version <n>] [--json]",
			summary: "set attributes",
			about:   "Sets each attribute in order, splitting on the first `=`, stopping at the\nfirst failure.",
			example: "thought set 42 effort=small area=cli",
			flags:   []flagSpec{versionFlag, jsonFlag},
			run:     runSet,
		},
		{
			name:    "unset",
			usage:   "unset <id> <key>... [--version <n>] [--json]",
			summary: "remove attributes",
			about:   "Removes each attribute in order, stopping at the first failure. Status\ncannot be removed.",
			example: "thought unset 42 effort",
			flags:   []flagSpec{versionFlag, jsonFlag},
			run:     runUnset,
		},
		{
			name:    "status",
			usage:   "status <id> <raw|active|done|dropped> [--version <n>] [--json]",
			summary: "set a thought's Status",
			about:   "Sets the Status: raw and active are open, done and dropped are closed.",
			example: "thought status 42 active",
			flags:   []flagSpec{versionFlag, jsonFlag},
			run:     runStatus,
		},
		{
			name:    "rm",
			usage:   "rm <id>... [-y] [--version <n>|--force]",
			summary: "delete thoughts for good",
			about: "Deletes each thought; there is no undo. On a terminal it asks first; without\n" +
				"one it refuses unless -y.",
			example: "! thought rm 42   # agents: hand this line to the user, never run it",
			flags:   []flagSpec{{"-y", "", "do not ask"}, versionFlag, forceFlag},
			run:     runRm,
		},
		{
			name:    "review",
			filter:  true,
			usage:   "review [<filter words>...]",
			summary: "walk thoughts in the Review TUI (bare `thought` does this)",
			about: "Opens the Review TUI on the Filter, or on the open thoughts\n" +
				"(`status:raw,active`) when none is given. Press ? inside.",
			example: "thought review tag:rust",
			run:     runReview,
		},
		{
			name:    "tags",
			usage:   "tags [--json]",
			summary: "list tags with counts",
			about:   "Lists every tag in use with the number of thoughts carrying it.",
			example: "thought tags --json",
			flags:   []flagSpec{jsonFlag},
			run:     runTags,
		},
		{
			name:    "attrs",
			usage:   "attrs [<key>] [--json]",
			summary: "list attribute keys, or one key's values, with counts",
			about:   "Lists every attribute key in use, or the values of <key>, with counts.",
			example: "thought attrs effort --json",
			flags:   []flagSpec{jsonFlag},
			run:     runAttrs,
		},
		{
			name:    "daemon",
			usage:   "daemon",
			summary: "run the server in the foreground (launchd runs this)",
			about:   "Serves the API on the socket until SIGTERM or SIGINT. `thought install` has\nlaunchd run it; run it by hand only with THOUGHTS_HOME set.",
			example: "THOUGHTS_HOME=/tmp/thoughts thought daemon",
			run:     runDaemon,
		},
		{
			name:    "install",
			usage:   "install",
			summary: "install or upgrade the daemon and the agent skills",
			about: "Writes the LaunchAgent, (re)starts the daemon under launchd, waits for it,\n" +
				"and installs the agent skills in ~/.claude/skills: thought, and new-thought for\n" +
				"the /new-thought command. Run it again after upgrading the binary.",
			example: "thought install",
			run:     runInstall,
		},
		{
			name:    "uninstall",
			usage:   "uninstall",
			summary: "remove the daemon and the agent skills, keeping the thoughts",
			about:   "Stops the daemon and removes the LaunchAgent and the skills. The thoughts and\nthe logs stay where they are.",
			example: "thought uninstall",
			run:     runUninstall,
		},
		{
			name:    "start",
			usage:   "start",
			summary: "start the installed daemon",
			about:   "Loads the installed LaunchAgent, which starts the daemon.",
			example: "thought start",
			run:     runStart,
		},
		{
			name:    "stop",
			usage:   "stop",
			summary: "stop the daemon",
			about:   "Unloads the LaunchAgent, which stops the daemon until `thought start`. A safe\nwindow to copy the database.",
			example: "thought stop",
			run:     runStop,
		},
		{
			name:    "ping",
			usage:   "ping",
			summary: "check that the daemon answers",
			about:   "Prints the service and api number, or explains why the daemon is down\n(exit 7).",
			example: "thought ping || echo 'thoughts are unavailable'",
			run:     runPing,
		},
		{
			name:    "help",
			usage:   "help [<verb>]",
			summary: "show help",
			about:   "Lists the verbs, or explains one.",
			example: "thought help replace",
			run:     runHelp,
		},
		{
			name:    "version",
			usage:   "version",
			summary: "print the version",
			about:   "Prints the binary's version and api, and the daemon's api when it answers.",
			example: "thought version",
			run:     runVersion,
		},
		{
			name:    "completion",
			usage:   "completion fish|zsh|bash",
			summary: "print a shell completion script",
			about:   "Prints the completion script for the shell.",
			example: "thought completion fish > ~/.config/fish/completions/thought.fish",
			run:     runCompletion,
		},
		{
			name:    "complete",
			usage:   "complete <kind> [<prefix>]",
			summary: "completion candidates",
			about:   "Prints completion candidates of a kind: verbs, tags, keys, values <key>, ids.",
			example: "thought complete tags ru",
			hidden:  true,
			run:     runComplete,
		},
	}
}

func lookup(name string) *verb {
	for _, v := range verbs {
		if v.name == name {
			return v
		}
	}
	return nil
}
