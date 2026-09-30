# Thoughts v1 spec

This spec is build-ready. v1 can be built from it without making any further decisions. The vocabulary is in [`CONTEXT.md`](../CONTEXT.md) (Thought, Tag, Attribute, Status, Capture, Version, Filter, Review), and decisions that are hard to reverse are in [`docs/adr/`](adr/). Each section links the ticket that decided it; the ticket's resolution holds the reasoning. When this spec and a ticket disagree, this spec wins.

## Contents

1. [Overview](#1-overview)
2. [Data model](#2-data-model)
3. [Storage](#3-storage)
4. [Filter language](#4-filter-language)
5. [HTTP API](#5-http-api)
6. [Daemon lifecycle](#6-daemon-lifecycle)
7. [CLI](#7-cli)
8. [Review TUI](#8-review-tui)
9. [Agent skill](#9-agent-skill)
10. [nvim plugin](#10-nvim-plugin)
11. [Out of scope](#11-out-of-scope)

## 1. Overview

Thoughts is a single-user, local service that is the one home for thoughts. It runs on macOS only.

**Deliverables.**

| Deliverable | Where | What |
|---|---|---|
| `thought` binary | Go module `github.com/NurramoX/thoughts` | One binary that is the daemon (`thought daemon`), the CLI (every other verb) and the Review TUI (`thought review`, bare `thought`). |
| Agent skills | `skill/<name>/SKILL.md` | The `thought` skill and the `/new-thought` command for Claude Code. They are embedded in the binary and installed by `thought install`. |
| nvim plugin | `nvim/` | `:e thought://<id>` read/write and `:NewThought`, built on the CLI. |

**Stack.** Go, `modernc.org/sqlite` (cgo-free, FTS5 built in), bubbletea, bubbles, lipgloss and glamour **v2** (`charm.land/...`). The shape mirrors `~/Projects/cdf`: one binary, launchd owns the lifecycle, hand-rolled verb dispatch. Unlike cdf, the daemon speaks HTTP.

**Principles.**
- The server is the only home of a thought. There are no local copies and nothing to sync.
- Writes overwrite. There is no revision history.
- Agents make most edits.
- Text a user or agent wrote leaves through the server or stdout, never as a leftover file.

## 2. Data model

Decided in [Thought data model](https://github.com/NurramoX/ideation/issues/2).

An **thought** carries: `id`, `title`, `body`, a set of `tags`, a set of `attributes`, a `version`, and the timestamps `created_at` and `updated_at`, all set by the server. Every violation of the rules below is a `422`.

| Field | Rules |
|---|---|
| `id` | A server-assigned integer, never reused (`INTEGER PRIMARY KEY AUTOINCREMENT`). Plain decimal on the wire. |
| `title` | Separate from the body; the server never parses the body for it. 1–400 characters after trimming, single line, no control characters, not unique. |
| `body` | Markdown, stored byte-exact, valid UTF-8, at most 10 MB (`413` over). May be empty, stored as `""`, never null. No frontmatter. |
| tag | A bare label, its own concept rather than an attribute. `[a-z0-9][a-z0-9_-]*` with input lowercased, at most 128 bytes, at most 128 per thought. Returned sorted. |
| attribute | `key → value`, exactly one value per key. The key follows the tag rules. The value is single-line UTF-8 (no LF, CR, VT, FF, NEL, U+2028 or U+2029; tabs are fine), 1–2000 bytes, trimmed, case preserved; an empty value is rejected (remove the key instead). At most 128 per thought. Returned sorted by key. |
| reserved keys | `id`, `title`, `body`, `tag`, `has`, `created`, `updated` can never be attribute keys. |
| `version` | An integer. It advances, together with `updated_at`, only on a real change; a no-op write moves neither. |
| `created_at`, `updated_at` | RFC 3339 UTC with millisecond precision (`2026-09-22T14:03:07.412Z`). |

**Status** is the only built-in attribute: an ordinary attribute in the API that the server validates and defaults, and that cannot be removed (`422`). Input is lowercased, and an unknown value is a `422`. Any value may move to any other.

| Value | Meaning | |
|---|---|---|
| `raw` | captured, not yet through Review (the default on create) | open |
| `active` | alive, however slowly it moves | open |
| `done` | realised or concluded | closed |
| `dropped` | decided against | closed |


**Vocabulary** is derived: a tag or key exists exactly while at least one thought carries it.

**Delete** is a hard delete. There are no tombstones; the id then answers `404`.

## 3. Storage

Decided in [Full-text search with modernc.org/sqlite](https://github.com/NurramoX/ideation/issues/5) and [Daemon lifecycle and local HTTP exposure](https://github.com/NurramoX/ideation/issues/4). Only the daemon opens the database.

**Connection.**
- Pragmas: `journal_mode=WAL`, `foreign_keys=ON`, `busy_timeout=5000`, `synchronous=FULL`.
- A single connection (`SetMaxOpenConns(1)`), so everything serialises.

**Migrations.**
- Ordered, forward-only and embedded in the binary.
- Tracked in `PRAGMA user_version`, and each runs in a transaction at daemon start.
- If the database is newer than the binary knows, the daemon refuses to start.

**Schema.** The table names are free; these constraints are not:
- `thought(id INTEGER PRIMARY KEY AUTOINCREMENT, title, body, version, created_at, updated_at)`, plus tables for tags (`thought_id, tag`) and attributes (`thought_id, key, value, value_folded`), both `ON DELETE CASCADE`. Status is an attribute row.
- Rows of `thought` change only by `UPDATE` or upsert, **never** `INSERT OR REPLACE` / `REPLACE INTO`. External-content FTS5 treats REPLACE as ABORT, and REPLACE skips the delete trigger.
- `value_folded` is the value under Unicode simple case folding, computed in Go on write. Filters compare against it. Don't use SQLite `NOCASE`, which is ASCII-only.

**Full-text index.** An external-content FTS5 table kept in sync by triggers, so the index and the content commit atomically:

```sql
CREATE VIRTUAL TABLE thought_fts USING fts5(
  title, body,
  content='thought', content_rowid='id',
  tokenize='porter unicode61 remove_diacritics 2'
);
CREATE TRIGGER thought_ai AFTER INSERT ON thought BEGIN
  INSERT INTO thought_fts(rowid, title, body) VALUES (new.id, new.title, new.body);
END;
CREATE TRIGGER thought_ad AFTER DELETE ON thought BEGIN
  INSERT INTO thought_fts(thought_fts, rowid, title, body) VALUES ('delete', old.id, old.title, old.body);
END;
CREATE TRIGGER thought_au AFTER UPDATE ON thought BEGIN
  INSERT INTO thought_fts(thought_fts, rowid, title, body) VALUES ('delete', old.id, old.title, old.body);
  INSERT INTO thought_fts(rowid, title, body) VALUES (new.id, new.title, new.body);
END;
INSERT INTO thought_fts(thought_fts, rank) VALUES('rank', 'bm25(10.0, 1.0)');  -- title outweighs body
```

The FTS5 facts the filter compiler depends on:
- **`MATCH` only works as a top-level AND-ed term** in a query that joins `thought_fts.rowid = thought.id`. Only that join form can use `rank`, `bm25`, `snippet` and `highlight`.
- **Text under `or` or `-`** compiles to `id [NOT] IN (SELECT rowid FROM thought_fts WHERE thought_fts MATCH ?)`. It filters, but can't be ranked or snippeted.
- **Quoting:** every text term is sent as one quoted phrase (`"..."`, with inner `"` doubled), bound as a parameter, never interpolated. Raw input such as `key=value`, `foo-bar` or an empty string is a MATCH syntax error.
- **`rank`** is negative, and more negative is better: `ORDER BY rank` ascending. The scores are unstable and are never exposed.
- **Snippets** are plain text. Use `snippet(thought_fts, -1, '', '', '…', 16)`, with no highlight markers, because markers would land inside the markdown.
- **Maintenance:** `'rebuild'` and `'integrity-check'` repair and verify the index. Run `'rebuild'` after any migration that touches `thought`.

## 4. Filter language

Decided in [Filter language](https://github.com/NurramoX/ideation/issues/6); see [ADR 0002](adr/0002-compact-filter-syntax.md). A Filter is one string. Only the server parses it; clients pass it through opaquely.

```
filter := or                          empty filter matches every thought
or     := and ( OR and )*             OR is the word `or` in any case; loosest
and    := term+                       juxtaposition is AND
term   := '-' term                    negation; binds tightest
        | '(' or ')'
        | WORD | STRING               text term over title and body
        | KEY ':' value (',' value)*  any-of: `status:raw,active`
value  := WORD | STRING
WORD   := run of characters other than whitespace ( ) : , "
STRING := "..." with "" for a literal quote
```

**Syntax rules.**
- Keys, and `tag:` and `has:` values, match lowercased: ASCII letters only, as labels are lowercased.
- A word right before `:` is a key, even `or`: `or:x` tests the attribute `or`.
- A value or text term needs quotes when it contains whitespace, `(`, `)`, `:`, `,` or `"`, starts with `-`, or is the word `or` in any case; an unquoted value that starts with `-` or is `or` is a parse error.
- There are no `<`/`>` operators, no wildcards, no prefix match and no ordering on attribute values.

| Key | Example | Meaning |
|---|---|---|
| any attribute, including `status` | `effort:small` | attribute equals value, case-insensitively (Unicode simple folding) |
| `tag` | `tag:rust` | the thought carries the tag |
| `id` | `id:12` | the thought's id |
| `has` | `has:effort`, `has:tag` | attribute present; any tag present |
| `title`, `body` | `title:borrow` | text term restricted to that FTS column |
| `created`, `updated` | `created:2026-09`, `updated:..90d` | date within a period or range |
| none | `borrow`, `"borrow checker"` | text term over title and body |

**Text.**
- Each text term becomes one quoted FTS5 phrase (see [Storage](#3-storage)).
- Top-level AND text terms are **ranked**: they enable `snippet` and `sort=rank`.
- Text terms under `or` or `-` are allowed but unranked.

**Dates.**
- A **period** is `2026`, `2026-09`, `2026-09-01`, `today` or a full RFC 3339 instant; `created:2026-09` means during September.
- A **range** is `a..b`, `a..` or `..b`, both ends inclusive. An end is a period or a **relative instant**, `90d`, `2w`, `6m` or `1y`, meaning that long before now.
- `today` and the relative units are lowercase only; `90D` or `Today` is a parse error.
- A lone relative instant (`created:7d`) is a parse error that points at the range spelling.
- Periods are read in the machine's local time zone.

**Errors.**
- A parse error is a `400` carrying `position`, the 1-based rune position of the offending character.
- An unknown tag or key is **valid and matches nothing**: a filter's validity never depends on data. Clients catch typos with a vocabulary hint (see [CLI](#7-cli)).

## 5. HTTP API

Decided in [HTTP API surface](https://github.com/NurramoX/ideation/issues/7), with amendments from the data model. Plain HTTP/1.1 over the Unix socket ([ADR 0001](adr/0001-unix-socket-only.md)): no auth and no version prefix.

| Method and path | Purpose |
|---|---|
| `GET /` | `{"service":"thoughts","api":1}`: compatibility check and liveness probe. |
| `POST /thoughts` | JSON `{title, body?, tags?, attributes?}`, where `title` is required → `201` with `Location`, `ETag` and the envelope. May set `status`. |
| `GET /thoughts?filter=&sort=&order=&limit=&offset=` | List: `{thoughts: [metadata], total}`. Never includes bodies. |
| `GET`/`HEAD /thoughts/{id}` | Envelope `{id, title, tags, attributes, body, version, created_at, updated_at}`. |
| `PATCH /thoughts/{id}` | Merge update (RFC 7396): absent means untouched, `tags` replaces the set, `attributes: {k: null}` removes a key, and `body` is replaced if present → `200` with the envelope (body omitted unless it was sent). There is no JSON `PUT`. |
| `GET`/`HEAD`/`PUT /thoughts/{id}/body` | Raw `text/markdown; charset=utf-8`. `PUT` → `204`. |
| `PUT`/`DELETE /thoughts/{id}/tags/{tag}` | Idempotent → `204`, including when already present or absent. |
| `PUT`/`DELETE /thoughts/{id}/attributes/{key}` | The request body is the value. Idempotent → `204`. |
| `DELETE /thoughts/{id}` | Hard delete → `204`. |
| `GET /tags`, `GET /attributes`, `GET /attributes/{key}` | Vocabulary: `[{tag, count}]`, `[{key, count}]`, `[{value, count}]`. |
| `GET /events` | The change stream: `text/event-stream`, open until the client leaves. See **Events**. |

**Lists.**
- Metadata is the envelope without `body`.
- A plain-text `snippet` appears only when the filter has a ranked text term.
- `sort` is one of `updated|created|title|rank` and `order` is `asc|desc`. The default is `rank` when a ranked text term is present, otherwise `updated desc`. `sort=rank` without a ranked term is a `400`.
- `limit` and `offset` exist, with no default limit and no cursors.
- An unknown or repeated query parameter is a `400`, so a typo such as `filtr=` is caught rather than ignored.
- An absent `filter` means all thoughts.

**Version.**
- Every response about one thought exposes its Version as `ETag` and as `version`, and every successful write returns the new `ETag`. Lists, the vocabulary and `GET /` carry no single Version and send no `ETag`.
- `If-Match` is **required** on `PATCH`, `PUT /body` and `DELETE /thoughts/{id}`:
  - missing → `428`
  - stale → `412` with `current_version`
  - `If-Match: *` forces the write
- `If-Match` is optional but honoured on the tag and attribute operations.
- Reads support `If-None-Match` → `304`.

**Events.** `GET /events` sends one server-sent event per successful write, from any client, as a single `data:` line: `{"id":12,"version":4}` after a create or a write (the Version after it), `{"id":12,"deleted":true}` after a delete. See [ADR 0004](adr/0004-change-stream.md).
- A failed write sends nothing. A no-op write may still send its unchanged Version.
- There is no replay and no event id: a client that connects, or reconnects, relists what it shows, since it has missed whatever happened before.
- Delivery never slows a write. A stream that falls 64 events behind is closed, and its client reconnects and relists.
- Events from concurrent writes to one thought may arrive out of order; the Version says which is newer.
- On shutdown the daemon ends every stream before draining, and a stream opened after that is a `503`.

**Bodies.** Returned byte-exact. Invalid UTF-8 → `422`, over 10 MB → `413`, a `Content-Type` other than `text/markdown` → `415`.

**Ids.** A non-numeric id → `400`. An unknown or deleted id → `404`.

**Errors.** `application/problem+json` (RFC 9457) on every endpoint, raw ones included:

| Status | When | Extra member |
|---|---|---|
| `400` | malformed request, id or filter | `position` for filter errors |
| `404` | unknown or deleted id | |
| `412` | stale `If-Match` | `current_version` |
| `413` | body over 10 MB | |
| `415` | wrong `Content-Type` | |
| `422` | invalid content | |
| `428` | missing `If-Match` | |

## 6. Daemon lifecycle

Decided in [Daemon lifecycle and local HTTP exposure](https://github.com/NurramoX/ideation/issues/4).

**Home.** `~/Library/Application Support/thoughts/` holds exactly `thoughts.db` (plus WAL files), `thoughts.sock` and `daemon.lock`.
- The daemon creates it and `chmod`s it to `0700` on every start.
- `THOUGHTS_HOME`, read the same way by daemon and CLI, replaces the root for tests and dev builds.
- The launchd job never sets `THOUGHTS_HOME`.

**Socket.** `<home>/thoughts.sock`, `chmod`ed to `0600` after bind. There is never a TCP listener, and there is no token or peer check. The daemon refuses to start if the socket path exceeds 103 bytes: macOS's `sun_path` holds 104 including the terminating NUL.

**Single instance.** The first act of `thought daemon` is `flock(LOCK_EX|LOCK_NB)` on `daemon.lock`, held for the life of the process.
- If another process holds the lock, the daemon exits non-zero with "already running" and touches nothing.
- Only the lock holder may unlink a stale socket and bind.

**Shutdown.** On `SIGTERM`/`SIGINT`, in order:
1. Stop accepting new connections.
2. End every `GET /events` stream, then `http.Server.Shutdown`, draining for up to 5 s, then force-close the stragglers.
3. `PRAGMA wal_checkpoint(TRUNCATE)`.
4. Close the DB.
5. Remove the socket.
6. Release the lock.
7. Exit 0.

**Logs.** Stderr only: lifecycle events and errors. No access log, never titles or bodies, no rotation.

**launchd.** The daemon runs as a LaunchAgent with label `io.github.nurramox.thoughts` and plist `~/Library/LaunchAgents/io.github.nurramox.thoughts.plist`.

| Key | Value |
|---|---|
| `Label` | `io.github.nurramox.thoughts` |
| `ProgramArguments` | `[os.Executable() (symlinks not resolved), "daemon"]` |
| `RunAtLoad` | true |
| `KeepAlive` | true |
| `StandardOutPath`, `StandardErrorPath` | `~/Library/Logs/thoughts/daemon.log` |
| `ExitTimeOut` | 10 |

- Deliberately absent: `EnvironmentVariables`, `ProcessType`, `ThrottleInterval` and `Sockets` (no socket activation).
- `thought daemon` itself knows nothing about launchd.
- A `gui/<uid>` agent runs only while the user is logged in graphically.

## 7. CLI

Decided in [CLI command surface](https://github.com/NurramoX/ideation/issues/9), with amendments from the daemon, filter and Review TUI tickets.

**Dispatch.**
- Verbs are dispatched by hand, verb first: `thought <verb> [args]`.
- Bare `thought` is `thought review`.
- An unknown verb is exit 2, never an implicit Filter.
- Flags may appear anywhere after the verb, and `--` ends them. There are no global flags.
- Ids are plain decimal; anything else is exit 2.
- The CLI validates nothing about tags, keys or values: it relays the server's `422`.

**Verbs.**

| Verb | Behaviour |
|---|---|
| `add <title> [-t <tag>]... [-s <k>=<v>]... [--body-file <path>\|-] [--edit]` | One `POST /thoughts`. Reads the body from stdin only with an explicit `-`, never by sniffing whether stdin is a tty; otherwise the body is empty. `--edit` opens the editor first and creates on save. Prints exactly the new id. |
| `show <id>...` | Human output: a header block (title, id, status, tags, attributes, version, dates), a blank line, then the body; several thoughts are separated by `---`. `--json`: one envelope per line (JSON Lines). |
| `ls [<filter words>...] [--sort updated\|created\|title\|rank] [--asc\|--desc] [--limit n] [--offset n] [-q\|--json]` | The Filter is the positionals joined by single spaces. An unknown single-dash word (`-has:effort`) is a negated term, here and in `review`; `--` guards one that is also a flag (`-q`, `-h`). Human output: an aligned table of id, status, title, tags and snippet, plus `N thoughts` on stderr when `total` exceeds what is shown. `-q`: ids only. `--json`: `{thoughts, total}` verbatim. `-q` with `--json` → exit 2. An empty result is exit 0 plus the vocabulary hint. |
| `body <id>` | The raw body, byte-exact. |
| `write <id> --version <n>\|--force [--body-file <path>]` | Reads the body from stdin by default. Refuses (exit 2) without `--version` or `--force`. Empty input writes an empty body. |
| `replace <id> <old> <new> [--old-file p] [--new-file p] [--all]` | Read, literal byte-exact replace, guarded write. `<old>` must occur exactly once unless `--all`; zero or several matches → exit 5 with the count. An empty `<old>` → exit 2, and an empty `<new>` deletes. On a `412` it re-reads and retries the whole operation up to 3 times, then exits 4. It is the only verb that retries. |
| `edit <id>` | `$EDITOR` round-trip, see below. |
| `title <id> <words...>` | Words joined by one space. Guarded. |
| `tag <id> <tag>...`, `untag`, `set <id> <k>=<v>...`, `unset <id> <key>...`, `status <id> <value>` | One unguarded idempotent call per tag or key, in order, stopping at the first failure. `set` splits on the first `=`, and an empty value → exit 2 pointing at `unset`. `status` is `set status=<value>`. |
| `rm <id>... [-y] [--force]` | Hard delete. On a terminal it prompts `delete thought 42 "<title>"? [y/N]`. Without a terminal and without `-y` it refuses (exit 2). |
| `review [<filter words>...]` | The Review TUI ([section 8](#8-review-tui)). |
| `tags`, `attrs [<key>]` | Vocabulary with counts. `--json` prints the API arrays. |
| `daemon` | Runs the server in the foreground. This is what launchd executes. |
| `install` | Idempotent, and also the upgrade step. It creates the home and log directories, writes the plist, runs `launchctl enable`, `bootout` if loaded (waiting until launchd has dropped the job, since a `bootstrap` before then fails with EIO), then `bootstrap gui/<uid>`. It waits for `GET /`, **writes each embedded skill to `~/.claude/skills/<name>/SKILL.md`**, and prints the socket, DB, log and skill paths. |
| `uninstall` | `bootout`, then removes the plist and each skill's `~/.claude/skills/<name>/`. It never touches the home or the logs, and prints where they remain. |
| `stop` / `start` | `launchctl bootout` / `bootstrap` of the installed plist, never HTTP. `start` without a plist → error pointing at `install`. |
| `ping` | `GET /`: prints the service and api number. Exit 0, or exit 7 when the daemon is down. |
| `help [<verb>]`, `-h`/`--help`, `version` | Help goes to stdout; each verb's help carries one agent-oriented example. `version` prints the binary version, plus the daemon api when reachable, warning on a mismatch. |
| `completion fish\|zsh\|bash`, hidden `complete <kind> [<prefix>]` | See Completion below. |

**Batch and `THOUGHTS_HOME` rules.**
- Only `show` and `rm` take several ids. They process each in order, continue past failures (one stderr line each), and exit with the code of the first failure. Bulk work is a shell loop over `thought ls -q`.
- `install`, `uninstall`, `start` and `stop` refuse (exit 2) while `THOUGHTS_HOME` is set.

**Version flags.**
- Every guarded verb accepts `--version <n>` (sent as `If-Match`) and `--force` (`If-Match: *`).
- `title`, `rm`, `replace` and `edit` GET first when given neither; `--force` skips that GET.
- `write` demands one of the two.
- `tag`, `untag`, `set`, `unset` and `status` send `If-Match` only when given `--version`.
- A `412` prints `thought 42 changed: you had version 7, it is now 9` and exits 4 (only `replace` retries).

**Output.**
- Human output by default. `--json` is explicit, never sniffed, and prints the API's own shapes.
- Mutating verbs print nothing on success, with two exceptions:
  - `add` prints the id.
  - With `--json`, `add`, `write`, `replace`, `edit`, `title`, `tag`, `untag`, `set`, `unset` and `status` print `{"id":<n>,"version":<n>}`.
- Diagnostics go to stderr.
- No colour when stdout is not a terminal or when `NO_COLOR` is set.
- The CLI never adds or strips a trailing newline in `body`, `write`, `replace` or `edit`.

**Exit codes.**

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | unexpected or internal error, including `428` |
| 2 | usage error, never sent to the server |
| 3 | not found (`404`) |
| 4 | stale Version (`412`) |
| 5 | rejected content (`422`, `413`, `415`), or a `replace` match-count failure |
| 6 | malformed request or Filter (`400`); filter errors are shown with a caret under `position` |
| 7 | daemon unreachable, with a diagnosis |
| 8 | daemon api mismatch: "run `thought install`" |

Humans see `thought: <title>: <detail>` on stderr. With `--json`, the problem+json document goes to stderr verbatim.

**Reaching the daemon.** Every invocation first calls `GET /`.
- **api mismatch:** exit 8, before anything else is sent.
- **Connect failure:** the CLI runs `launchctl print gui/<uid>/<label>`.
  - Job not loaded → exit 7 "not installed: run `thought install`".
  - Job loaded → retry with backoff for up to 2 s, then exit 7 "installed but not responding, see <log path>".
- **With `THOUGHTS_HOME` set:** no launchctl and no retry; exit 7 "no daemon at <home>: run `thought daemon`".
- The CLI never starts the daemon.

**Vocabulary hint.** When `ls` or `review` gets an empty result, the CLI checks the filter's tags and keys against `/tags` and `/attributes`. It prints e.g. `no thoughts have the key 'tga'` on stderr.

**Edit round-trip.** Shared by `edit`, `add --edit` and the TUI's `e`.
- **Temp file:** it holds the body only, at `$TMPDIR/thought-<id>-*.md`, mode `0600`, inside a `0700` directory. It is always removed, including on SIGINT and SIGTERM.
- **Editor:** `$VISUAL`, then `$EDITOR`, then `vi`.
- **No write:** when the content is unchanged or the editor exits non-zero.
- **On a `412`,** the CLI prompts:
  - `[o]verwrite`
  - `[r]e-edit`: prints the other party's current body to stderr, reopens the user's text, and guards against the new Version. There are no conflict markers.
  - `[a]bort`: prints the user's text to stdout.
- **Non-interactive `412`:** abort, text to stdout, exit 4.

**Completion.** The hidden `thought complete <kind> [<prefix>]` prints one candidate per line, and exits 0 silently when the daemon is down, with no retry.

| Kind | Used for | Candidates |
|---|---|---|
| `verbs` | the verb | every verb |
| `tags` | `tag`, `untag`, `add -t` | existing tags |
| `keys` | `set`, `unset` | existing attribute keys |
| `values <key>` | after `set <id> key=` | existing values of that key; the four Status values for `status` |
| `ids` | id arguments | `<id>\t<title>` for the 50 most recently updated thoughts |

The Filter gets no completion. `thought completion fish|zsh|bash` prints the script; fish comes first, and zsh and bash behave the same.

## 8. Review TUI

Decided in [Review TUI](https://github.com/NurramoX/ideation/issues/8). It is telescope-style: a filter bar and list on the left, a preview on the right.

**Layout.**
- **Left:** a one-line filter bar showing the Filter verbatim, with the list under it. Each row shows a status letter, the title, dimmed tags and, right-aligned, the age of the current sort field. `▸` marks the selected row.
- **Right:** the preview. A header (title, id, status, tags, attributes, created and updated ages, version), then the body rendered with glamour at pane width. `m` toggles rendered ↔ raw.
- **Bottom:** a status line with `n/total`, the sort order, errors and a `?` hint.
- **Proportions:** the list takes about 40% of the width, at least 30 columns. Under 80 columns only one pane shows, and `tab` switches between them.

**Opening.** `thought review [<filter words>...]`, or bare `thought`. The bar is prefilled with the given Filter, or with the open thoughts, `status:raw,active`, when none is given. Focus starts on the list.

**Order.** The default is `sort=updated` desc. `o` cycles:
1. updated (newest first)
2. created (newest first)
3. random (a client-side shuffle)

**Filter bar.** `/` or `f` focuses it with the cursor at the end.
- **Live requery:** typing requeries after about 150 ms of debounce.
- **Moving while typing:** `ctrl-n`/`ctrl-p` and the arrow keys move the selection while in the bar.
- **Leaving the bar:** `enter` or `esc` returns to the list and keeps the filter.
- **Errors:** a `400` keeps the last good list and shows the error with a caret under `position`.
- **Empty result:** shows the vocabulary hint.
- **Empty filter:** lists everything.

**Snapshot.** The list is the result of the last query, kept live by `GET /events`.
- **Actions don't reorder:** an action updates its row in place, and nothing reorders or disappears until the next requery.
- **What requeries:** a filter edit, `o`, or `ctrl-r`.
- **Selection:** a requery keeps the selected thought when it is still present, and otherwise selects the top row.
- **Live:** changes made anywhere, by the CLI, an agent or nvim, are caught up with about 100 ms after they stop arriving, without reordering or removing a row:
  - a new thought that matches the snapshot's Filter is inserted after the row the server lists before it, or at the top, keeping the selection and the rows in view in place;
  - a changed row is fetched again, updating its metadata and, when selected, its preview. It stays even when it no longer matches;
  - a deleted row is marked deleted.
- **Catching up:** new thoughts are found by relisting the snapshot's Filter and order, after any list request that is still out. The TUI relists on every (re)connection of the stream and retries a lost stream every second.

**Keys (list focus).**

| Key | Does | Then |
|---|---|---|
| `j`/`k`, arrows, `g`/`G` | move the selection | |
| `J`/`K`, `ctrl-d`/`ctrl-u` | scroll the preview | |
| `space` `n` | next | advance |
| `a` `d` `x` `r` | status → active / done / dropped / raw | advance |
| `t` | retag inline (edit the space-separated tag list; `enter` applies per-tag `PUT`/`DELETE`) | stay |
| `enter` `e` | body round-trip in the editor (see [CLI](#7-cli)) | stay |
| `D` | delete after `y`/N, with `If-Match` set to the Version shown | advance |
| `/` `f` | focus the filter bar | |
| `o` | cycle order | |
| `ctrl-r` | requery | |
| `m` | rendered ↔ raw | |
| `?` | help | |
| `q` `ctrl-c` | quit | |

"Advance" moves the selection to the next row. Attributes are read-only in Review.

**Data.** The list uses `GET /thoughts`, and follows `GET /events` while the TUI runs. The preview `GET`s the selected thought with a short debounce and revalidates with `If-None-Match`; that response's Version guards `e` and `D`.

**Conflicts.**
- A `412` on delete shows "changed, not deleted" and refreshes the preview.
- A `412` after the editor offers `[o]verwrite / [r]e-edit / [a]bort`. **Re-edit** shows the other party's current body in the preview rather than on stderr, which the TUI hides, and reopens the user's text on `enter`. **Abort** prints the user's text to stdout after the TUI exits and the terminal is restored.

## 9. Agent skill

Decided in [Agent capture and editing flow](https://github.com/NurramoX/ideation/issues/3).

Agents use the `thought` CLI through one skill, `thought`. There is no MCP server and no raw HTTP. The skill's source is `skill/thought/SKILL.md`; it is embedded in the binary and installed by `thought install` (see [CLI](#7-cli)). It carries **judgment only**: for syntax it points at `thought help <verb>` and never restates the verb table. Its description triggers on phrases like "capture this", "save this as a thought", "add this to my thought about…" and "what thoughts do I have on…". The skill's prose is the builder's to write. The rules below are normative and must be in it.

**`/new-thought`.** A second skill, `new-thought` (`skill/new-thought/SKILL.md`), is a user-only command (`disable-model-invocation`), installed alongside. `/new-thought <thought>` is shorthand for asking to capture `<thought>`: it hands the text to the `thought` skill's capture steps and adds no rules of its own. With no text, it captures the thought being discussed in the conversation.

**Capture.**
- **When:** only when the user asks. The agent never captures, or offers to, on its own.
- **Search first:** before every `thought add`, run a text Filter on the thought's key terms (`thought ls borrow checker tag:rust`).
  - On a clear match, ask whether to fold the discussion into that thought or create a new one. This is the only confirmation in the capture path.
  - With no match, capture without asking.
- **Body:** a self-contained markdown document in the user's words.
  - The agent corrects spelling and grammar, and rephrases only where the English is plainly bad. The thoughts, reasoning, order and tone stay the user's.
  - The agent never questions the thought: no critique, no additions, no guessing at the user's motives, and no open questions the user didn't raise.
  - No transcript, no link to the conversation, no "as discussed".
  - No H1 repeating the title.
- **Title and tags:** the agent picks the title. It runs `thought tags` and reuses existing tags, minting a new one only when nothing fits, and says so when it does.
- **Status** stays `raw` unless the user names one.
- **Attributes:** no provenance attribute. The agent sets an attribute only when the user names it, or when a key already in `thought attrs` plainly applies. It never creates a new key unprompted.
- **One thought per distinct thought.** A discussion with two separable thoughts becomes two captures, and both ids are reported. Cross-references are plain prose ("see thought 42").
- **Report:** after uploading, one line: id, title, tags. There is no draft approval; Review is the approval step.

**Editing.**
- **Finding the thought:** `thought ls <filter> --json`. With one clear match, proceed and name the thought touched. With several, list them and ask. Never guess.
- **Tools:** `thought replace` for local changes. `thought show --json` then `thought write --version <n>` to restructure.
- **Integrate new material** in the user's words, as in capture. The document stays a current statement of the thought, with no dated log sections.
- **No history, so no silent loss:** never remove or contradict existing content unless asked. When removing, quote what was removed in the report.
- **Title and status** change only when the user asks.
- **On exit 4:** re-read the thought, reapply the change to the fresh body, and retry.

**Boundaries.**
- **Forbidden:** `--force` and `rm`.
  - Asked to delete, the agent hands the user `! thought rm <id>`, and offers `thought status <id> dropped` if the user only wants the thought out of the open set.
- **Reading** is unrestricted, but only when the user refers to thoughts. The agent never searches them unprompted.
- **Several thoughts:** list the affected thoughts, wait for the go-ahead, then work one thought at a time.
- **On exit 7:** report the failure and print the finished document into the conversation. Never start the daemon and never write a fallback file.

## 10. nvim plugin

Decided in [nvim plugin](https://github.com/NurramoX/ideation/issues/10).

**Layout.** `nvim/plugin/thought.lua` and `nvim/lua/thought/`, loaded with lazy.nvim `dir = "~/Projects/thoughts/nvim"`. It shells out to `thought` through `vim.system` and never speaks HTTP, so the CLI's api check, daemon diagnosis and messages all apply.

**`BufReadCmd thought://*`.**
- The part after `thought://` must be a decimal id.
- It runs `thought show --json <id>` and loads `body` into the buffer. A trailing `\n` sets `eol`; its absence sets `noeol` + `nofixeol`, so the round-trip is byte-exact.
- It sets `fileformat=unix`, `buftype=acwrite`, `filetype=markdown`, `noswapfile`, `b:thought_version` and `b:thought_title`.
- `:e!` reloads through the same path.

**`BufWriteCmd thought://*`.**
- It pipes the buffer to `thought write <id> --version <b:thought_version> --json`. The buffer is the lines joined by `\n`, plus a trailing `\n` only when `eol` is set.
- On success it stores the new `version` and clears `modified`.
- On failure it shows the CLI's stderr through `vim.notify` at error level and leaves the buffer modified. A stale Version (exit 4) is just that error, and `:e!` is the way out.
- It never forces a write.

**`:NewThought <title>`.**
- It runs `thought add <title>` and opens `thought://<id>`.
- An empty title is an error.

## 11. Out of scope

Out of scope for v1:
- Multi-user use, remote access and cross-machine sync.
- A mountable filesystem.
- A web UI. Any future web UI gets its own listener ([ADR 0001](adr/0001-unix-socket-only.md)).
- Revision history.
- A designed migration of existing thoughts; that is done ad hoc with an agent and the CLI.
- Bulk API operations, tag rename/merge, `sort=attr:<key>`, server-side partial body edits and auth.
- Resurfacing: reminders and nudges.
- Backup and export. `thought stop` gives a safe window to copy the DB file.
- An nvim picker and nvim conflict handling.
- Platforms other than macOS.
