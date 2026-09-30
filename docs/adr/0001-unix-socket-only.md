# The daemon listens on a Unix socket only, never TCP

The daemon serves its HTTP API over a Unix domain socket (mode `0600`, inside a `0700` directory) and never opens a TCP listener, even on `127.0.0.1`. The API carries no auth, so on a TCP port every local process, every other user on the machine, and any web page (a plain cross-origin `POST` to localhost, or DNS rebinding) could create and delete thoughts; a Unix socket makes the file's permissions the whole access-control story and removes port collisions. The cost is that browsers cannot reach the daemon and clients need `curl --unix-socket` or a custom dialer; a web UI is out of scope, and if one ever arrives it gets its own explicitly opened listener rather than a change to this one.

## Considered options

- **TCP on localhost with a token**: reachable from browsers, but adds a secret to provision and distribute to every client (CLI, TUI, nvim plugin, agents) for a single-user local tool.
- **launchd socket activation** (launchd owns the socket): removes the sub-second connection-refused window during restarts, but needs a cgo-free shim (`purego`) over Go runtime internals and a listener path that only runs under launchd, which tests cannot reach. Rejected in favour of `RunAtLoad` + `KeepAlive` with the daemon binding its own socket.
