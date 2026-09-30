package cli

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/NurramoX/thoughts/internal/api"
	"github.com/NurramoX/thoughts/internal/client"
	"github.com/NurramoX/thoughts/internal/hint"
)

var sorts = []string{"updated", "created", "title", "rank"}

func runLs(a *app, in *input) int {
	v := in.verb
	p := client.ListParams{Filter: strings.Join(in.args, " ")}
	var err error
	if s, ok := in.value("--sort"); ok {
		if !slices.Contains(sorts, s) {
			return a.usagef(v, "--sort is one of %s, not '%s'", strings.Join(sorts, ", "), s)
		}
		p.Sort = s
	}
	switch asc, desc := in.bool("--asc"), in.bool("--desc"); {
	case asc && desc:
		return a.usagef(v, "--asc and --desc exclude each other")
	case asc:
		p.Order = "asc"
	case desc:
		p.Order = "desc"
	}
	if p.Limit, err = in.count("--limit", 1); err != nil {
		return a.usagef(v, "%v", err)
	}
	if p.Offset, err = in.count("--offset", 0); err != nil {
		return a.usagef(v, "%v", err)
	}
	quiet := in.bool("-q")
	if quiet && a.json {
		return a.usagef(v, "-q and --json exclude each other")
	}
	if code := a.connect(); code != exitOK {
		return code
	}
	l, err := a.c.List(a.ctx, p)
	if err != nil {
		return a.failFilter(p.Filter, err)
	}
	switch {
	case a.json:
		a.printJSON(l)
	case quiet:
		for _, m := range l.Thoughts {
			fmt.Fprintln(a.stdout, m.ID)
		}
	default:
		a.listTable(l.Thoughts)
		if l.Total > len(l.Thoughts) {
			fmt.Fprintf(a.stderr, "%d thoughts\n", l.Total)
		}
	}
	if len(l.Thoughts) == 0 {
		lines, _ := hint.Vocabulary(a.ctx, a.c, p.Filter)
		for _, line := range lines {
			fmt.Fprintln(a.stderr, line)
		}
	}
	return exitOK
}

// listTable prints id, status, title, tags and snippet.
func (a *app) listTable(thoughts []api.Meta) {
	rows := make([][]string, len(thoughts))
	for i, m := range thoughts {
		rows[i] = []string{
			strconv.FormatInt(m.ID, 10),
			m.Attributes[api.StatusKey],
			m.Title,
			strings.Join(m.Tags, ","),
			strings.Join(strings.Fields(m.Snippet), " "),
		}
	}
	st := style{a.color()}
	fmt.Fprint(a.stdout, table(rows, func(col int, cell string) string {
		switch col {
		case 1:
			return st.status(cell)
		case 3, 4:
			return st.dim(cell)
		}
		return cell
	}))
}

func runTags(a *app, in *input) int {
	if len(in.args) > 0 {
		return a.usagef(in.verb, "too many arguments")
	}
	if code := a.connect(); code != exitOK {
		return code
	}
	tags, err := a.c.Tags(a.ctx)
	if err != nil {
		return a.fail(err)
	}
	if a.json {
		a.printJSON(tags)
		return exitOK
	}
	rows := make([][]string, len(tags))
	for i, t := range tags {
		rows[i] = []string{t.Tag, strconv.Itoa(t.Count)}
	}
	a.counts(rows)
	return exitOK
}

func runAttrs(a *app, in *input) int {
	if len(in.args) > 1 {
		return a.usagef(in.verb, "at most one key")
	}
	if code := a.connect(); code != exitOK {
		return code
	}
	var rows [][]string
	if len(in.args) == 0 {
		keys, err := a.c.Attributes(a.ctx)
		if err != nil {
			return a.fail(err)
		}
		if a.json {
			a.printJSON(keys)
			return exitOK
		}
		for _, k := range keys {
			rows = append(rows, []string{k.Key, strconv.Itoa(k.Count)})
		}
	} else {
		vals, err := a.c.AttributeValues(a.ctx, in.args[0])
		if err != nil {
			return a.fail(err)
		}
		if a.json {
			a.printJSON(vals)
			return exitOK
		}
		for _, v := range vals {
			rows = append(rows, []string{v.Value, strconv.Itoa(v.Count)})
		}
	}
	a.counts(rows)
	return exitOK
}

// counts prints name/count rows with dimmed counts.
func (a *app) counts(rows [][]string) {
	st := style{a.color()}
	fmt.Fprint(a.stdout, table(rows, func(col int, cell string) string {
		if col == 1 {
			return st.dim(cell)
		}
		return cell
	}))
}
