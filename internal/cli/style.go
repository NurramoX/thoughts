package cli

import "github.com/NurramoX/thoughts/internal/api"

// style colours human output, or does nothing when colour is off.
type style struct{ on bool }

func (s style) wrap(code, x string) string {
	if !s.on || x == "" {
		return x
	}
	return "\x1b[" + code + "m" + x + "\x1b[0m"
}

func (s style) bold(x string) string { return s.wrap("1", x) }
func (s style) dim(x string) string  { return s.wrap("2", x) }

// status colours a Status value: open ones bright, closed ones dim.
func (s style) status(x string) string {
	switch x {
	case api.StatusRaw:
		return s.wrap("33", x)
	case api.StatusActive:
		return s.wrap("32", x)
	case api.StatusDone:
		return s.wrap("34", x)
	}
	return s.dim(x)
}
