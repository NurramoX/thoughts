package cli

import (
	"fmt"

	"github.com/NurramoX/thoughts/internal/api"
	"github.com/NurramoX/thoughts/internal/client"
	"github.com/NurramoX/thoughts/internal/home"
)

func runPing(a *app, in *input) int {
	if len(in.args) > 0 {
		return a.usagef(in.verb, "too many arguments")
	}
	if code := a.connect(); code != exitOK {
		return code
	}
	fmt.Fprintf(a.stdout, "thoughts api %d\n", api.APIVersion)
	return exitOK
}

// runVersion prints the binary's version, and the daemon's api when it
// answers, warning on a mismatch. It never diagnoses or retries.
func runVersion(a *app, in *input) int {
	if len(in.args) > 0 {
		return a.usagef(in.verb, "too many arguments")
	}
	fmt.Fprintf(a.stdout, "thought %s (api %d)\n", Version, api.APIVersion)
	h, err := home.Resolve()
	if err != nil {
		return exitOK
	}
	s, err := client.New(h.Socket()).Ping(a.ctx)
	if err != nil {
		return exitOK
	}
	fmt.Fprintf(a.stdout, "daemon api %d\n", s.API)
	if s.API != api.APIVersion {
		a.errorf("warning: the daemon speaks api %d, this thought speaks api %d: run `thought install`", s.API, api.APIVersion)
	}
	return exitOK
}
