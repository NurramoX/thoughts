package cli

import (
	"errors"
	"os"
	"time"

	"github.com/NurramoX/thoughts/internal/api"
	"github.com/NurramoX/thoughts/internal/client"
	"github.com/NurramoX/thoughts/internal/home"
	"github.com/NurramoX/thoughts/internal/launchd"
)

// respondWithin is how long a loaded but silent daemon gets.
const respondWithin = 2 * time.Second

// connect makes the client and checks the daemon with GET / before anything
// else is sent. It returns exitOK, or the exit code after diagnosing.
func (a *app) connect() int {
	h, err := home.Resolve()
	if err != nil {
		return a.fail(err)
	}
	a.home = h
	a.c = client.New(h.Socket())
	s, err := a.c.Ping(a.ctx)
	var ue *client.UnreachableError
	if errors.As(err, &ue) {
		return a.diagnose()
	}
	return a.compatible(s, err)
}

// compatible turns the answer to GET / into exitOK or exitMismatch.
func (a *app) compatible(s api.Service, err error) int {
	switch {
	case err != nil || s.Service != "thoughts":
		a.errorf("the socket does not answer as thoughts api %d: run `thought install`", api.APIVersion)
	case s.API != api.APIVersion:
		a.errorf("the daemon speaks api %d, this thought speaks api %d: run `thought install`", s.API, api.APIVersion)
	default:
		return exitOK
	}
	return exitMismatch
}

// diagnose explains an unreachable daemon, waiting briefly for one that
// launchd has loaded but that is not answering yet.
func (a *app) diagnose() int {
	if a.home.Custom {
		a.errorf("no daemon at %s: run `thought daemon`", a.home.Dir)
		return exitUnreachable
	}
	ag, err := agent()
	if err != nil {
		return a.fail(err)
	}
	if !ag.Loaded(a.ctx) {
		a.errorf("not installed: run `thought install`")
		return exitUnreachable
	}
	if code, ok := a.await(respondWithin); ok {
		return code
	}
	a.errorf("installed but not responding, see %s", ag.LogPath())
	return exitUnreachable
}

// await polls GET / with backoff for up to d. ok is false when the daemon
// never answered; otherwise code is the compatibility verdict.
func (a *app) await(d time.Duration) (code int, ok bool) {
	deadline := time.Now().Add(d)
	wait := 50 * time.Millisecond
	for {
		sleep := min(wait, time.Until(deadline))
		if sleep <= 0 {
			return 0, false
		}
		time.Sleep(sleep)
		wait *= 2
		s, err := a.c.Ping(a.ctx)
		var ue *client.UnreachableError
		if !errors.As(err, &ue) {
			return a.compatible(s, err), true
		}
	}
}

// agent is the user's LaunchAgent.
func agent() (launchd.Agent, error) {
	h, err := os.UserHomeDir()
	return launchd.Agent{UserHome: h, UID: os.Getuid(), Ctl: launchctl}, err
}
