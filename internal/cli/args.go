package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/NurramoX/thoughts/internal/api"
)

// flagSpec is one flag a verb accepts. arg names its value; a flag without
// arg is a switch.
type flagSpec struct {
	name, arg, help string
}

// input is a verb's parsed command line.
type input struct {
	verb *verb
	args []string
	vals map[string][]string
}

// parse splits args into positionals and v's flags. Flags may appear
// anywhere; `--` ends them. A value flag takes the next argument, or
// `--name=value`. -h and --help are accepted by every verb. For a verb that
// takes a Filter, an unknown `-word` is a Filter word.
func parse(v *verb, args []string) (*input, error) {
	in := &input{verb: v, vals: map[string][]string{}}
	for i := 0; i < len(args); i++ {
		s := args[i]
		if s == "--" {
			in.args = append(in.args, args[i+1:]...)
			break
		}
		if len(s) < 2 || s[0] != '-' {
			in.args = append(in.args, s)
			continue
		}
		name, val, hasVal := s, "", false
		if strings.HasPrefix(s, "--") {
			name, val, hasVal = strings.Cut(s, "=")
		}
		if name == "-h" || name == "--help" {
			in.vals[name] = append(in.vals[name], "")
			continue
		}
		f := v.flag(name)
		switch {
		case f == nil && v.filter && !strings.HasPrefix(s, "--"):
			in.args = append(in.args, s)
			continue
		case f == nil:
			return nil, fmt.Errorf("unknown flag %s", name)
		case f.arg == "" && hasVal:
			return nil, fmt.Errorf("flag %s takes no value", name)
		case f.arg != "" && !hasVal:
			if i+1 == len(args) {
				return nil, fmt.Errorf("flag %s needs %s", name, f.arg)
			}
			i++
			val = args[i]
		}
		in.vals[name] = append(in.vals[name], val)
	}
	return in, nil
}

func (in *input) bool(name string) bool { return len(in.vals[name]) > 0 }

// value is the last value given to a flag.
func (in *input) value(name string) (string, bool) {
	vs := in.vals[name]
	if len(vs) == 0 {
		return "", false
	}
	return vs[len(vs)-1], true
}

func (in *input) values(name string) []string { return in.vals[name] }

// parseID reads a plain decimal id.
func parseID(s string) (int64, error) {
	id, ok := api.ParseID(s)
	if !ok {
		return 0, fmt.Errorf("'%s' is not an id", s)
	}
	return id, nil
}

func parseIDs(ss []string) ([]int64, error) {
	ids := make([]int64, len(ss))
	for i, s := range ss {
		id, err := parseID(s)
		if err != nil {
			return nil, err
		}
		ids[i] = id
	}
	return ids, nil
}

// precondition reads --version and --force.
func (in *input) precondition() (api.Precondition, error) {
	s, hasVersion := in.value("--version")
	force := in.bool("--force")
	switch {
	case hasVersion && force:
		return api.Precondition{}, fmt.Errorf("--version and --force exclude each other")
	case force:
		return api.Precondition{Force: true}, nil
	case hasVersion:
		v, ok := api.ParseETag(s)
		if !ok || s[0] == '"' {
			return api.Precondition{}, fmt.Errorf("--version needs a positive integer, not '%s'", s)
		}
		return api.Precondition{Version: v}, nil
	}
	return api.Precondition{}, nil
}

// count reads a non-negative integer flag; min is its smallest value.
func (in *input) count(name string, min int) (int, error) {
	s, ok := in.value(name)
	if !ok {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < min {
		return 0, fmt.Errorf("%s needs an integer of at least %d, not '%s'", name, min, s)
	}
	return n, nil
}

// splitKV splits k=v on the first '='.
func splitKV(s string) (k, v string, err error) {
	k, v, ok := strings.Cut(s, "=")
	if !ok || k == "" {
		return "", "", fmt.Errorf("'%s' is not <key>=<value>", s)
	}
	if v == "" {
		return "", "", fmt.Errorf("empty value for '%s': to remove a key, use `thought unset`", k)
	}
	return k, v, nil
}

func trimLine(s string) string { return strings.TrimRight(s, "\r\n") }
