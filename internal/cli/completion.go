package cli

import (
	"embed"
	"fmt"
	"strconv"
	"strings"

	"github.com/NurramoX/thoughts/internal/api"
	"github.com/NurramoX/thoughts/internal/client"
	"github.com/NurramoX/thoughts/internal/home"
)

//go:embed completion/thought.fish completion/thought.zsh completion/thought.bash
var scripts embed.FS

func runCompletion(a *app, in *input) int {
	v := in.verb
	if len(in.args) != 1 {
		return a.usagef(v, "one shell is required")
	}
	b, err := scripts.ReadFile("completion/thought." + in.args[0])
	if err != nil {
		return a.usagef(v, "no completion for '%s'", in.args[0])
	}
	a.stdout.Write(b)
	return exitOK
}

// runComplete prints the candidates of a kind that start with the prefix,
// one per line. When the daemon is down it prints nothing and exits 0.
func runComplete(a *app, in *input) int {
	v := in.verb
	args := in.args
	if len(args) == 0 {
		return a.usagef(v, "a kind is required")
	}
	kind, args := args[0], args[1:]
	key := ""
	if kind == "values" {
		if len(args) == 0 {
			return a.usagef(v, "values needs a key")
		}
		key, args = args[0], args[1:]
	}
	if len(args) > 1 {
		return a.usagef(v, "too many arguments")
	}
	prefix := ""
	if len(args) == 1 {
		prefix = args[0]
	}
	var cands []string
	switch {
	case kind == "verbs":
		for _, v := range verbs {
			if !v.hidden {
				cands = append(cands, v.name)
			}
		}
	case kind == "values" && key == api.StatusKey:
		cands = api.Statuses
	case kind == "tags" || kind == "keys" || kind == "values" || kind == "ids":
		c := quietClient(a)
		if c == nil {
			return exitOK
		}
		cands = remote(a, c, kind, key)
	default:
		return a.usagef(v, "unknown kind '%s'", kind)
	}
	for _, c := range cands {
		if strings.HasPrefix(c, prefix) {
			fmt.Fprintln(a.stdout, c)
		}
	}
	return exitOK
}

// quietClient is a client for a daemon that answers GET / with our api, or
// nil: no diagnosis and no retry.
func quietClient(a *app) client.Client {
	h, err := home.Resolve()
	if err != nil {
		return nil
	}
	c := client.New(h.Socket())
	if s, err := c.Ping(a.ctx); err != nil || s.API != api.APIVersion {
		return nil
	}
	return c
}

// remote fetches candidates from the daemon; errors yield none.
func remote(a *app, c client.Client, kind, key string) []string {
	var out []string
	switch kind {
	case "tags":
		tags, _ := c.Tags(a.ctx)
		for _, t := range tags {
			out = append(out, t.Tag)
		}
	case "keys":
		keys, _ := c.Attributes(a.ctx)
		for _, k := range keys {
			out = append(out, k.Key)
		}
	case "values":
		vals, _ := c.AttributeValues(a.ctx, key)
		for _, v := range vals {
			out = append(out, v.Value)
		}
	case "ids":
		l, _ := c.List(a.ctx, client.ListParams{Sort: "updated", Order: "desc", Limit: 50})
		for _, m := range l.Thoughts {
			out = append(out, strconv.FormatInt(m.ID, 10)+"\t"+m.Title)
		}
	}
	return out
}
