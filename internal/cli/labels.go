package cli

import (
	"context"

	"github.com/NurramoX/thoughts/internal/api"
)

// labelCall is one tag or attribute operation on a thought.
type labelCall func(ctx context.Context, id int64, pre api.Precondition, item string) (version int64, err error)

func runTag(a *app, in *input) int {
	return a.labels(in.verb, in, nil, func(ctx context.Context, id int64, pre api.Precondition, tag string) (int64, error) {
		return a.c.PutTag(ctx, id, pre, tag)
	})
}

func runUntag(a *app, in *input) int {
	return a.labels(in.verb, in, nil, func(ctx context.Context, id int64, pre api.Precondition, tag string) (int64, error) {
		return a.c.DeleteTag(ctx, id, pre, tag)
	})
}

func runSet(a *app, in *input) int {
	check := func(s string) error {
		_, _, err := splitKV(s)
		return err
	}
	return a.labels(in.verb, in, check, func(ctx context.Context, id int64, pre api.Precondition, kv string) (int64, error) {
		k, v, _ := splitKV(kv)
		return a.c.PutAttribute(ctx, id, pre, k, v)
	})
}

func runUnset(a *app, in *input) int {
	return a.labels(in.verb, in, nil, func(ctx context.Context, id int64, pre api.Precondition, key string) (int64, error) {
		return a.c.DeleteAttribute(ctx, id, pre, key)
	})
}

func runStatus(a *app, in *input) int {
	if len(in.args) != 2 {
		return a.usagef(in.verb, "an id and one status are required")
	}
	return a.labels(in.verb, in, nil, func(ctx context.Context, id int64, pre api.Precondition, value string) (int64, error) {
		return a.c.PutAttribute(ctx, id, pre, api.StatusKey, value)
	})
}

// labels runs call for each item after the id, in order, stopping at the
// first failure. Only --version sends If-Match: it guards the first call,
// and each later call is guarded by the Version the previous one returned.
func (a *app) labels(v *verb, in *input, check func(string) error, call labelCall) int {
	if len(in.args) < 2 {
		return a.usagef(v, "an id and at least one argument after it are required")
	}
	id, err := parseID(in.args[0])
	if err != nil {
		return a.usagef(v, "%v", err)
	}
	items := in.args[1:]
	for _, item := range items {
		if check != nil {
			if err := check(item); err != nil {
				return a.usagef(v, "%v", err)
			}
		}
	}
	pre, err := in.precondition()
	if err != nil {
		return a.usagef(v, "%v", err)
	}
	if code := a.connect(); code != exitOK {
		return code
	}
	var version int64
	for _, item := range items {
		version, err = call(a.ctx, id, pre, item)
		if err != nil {
			return a.failWrite(id, pre, err)
		}
		if !pre.IsZero() {
			pre.Version = version
		}
	}
	if a.json {
		a.written(id, version)
	}
	return exitOK
}
