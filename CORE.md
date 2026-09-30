# Core decisions

The ADRs in `docs/adr/`, each cut to a tenth.

**[Unix socket only](docs/adr/0001-unix-socket-only.md).** The daemon never listens on TCP; the socket's `0600` permissions are the whole access control, since the API has no auth.

**[Compact filter syntax](docs/adr/0002-compact-filter-syntax.md).** Filters use GitHub-style `key:value` terms (`tag:rust status:raw,active -has:effort`), so nearly every filter types bare on the command line. Dates use `..` ranges, not shell-unsafe `<`/`>`. The cost: a missed quote or typo'd key silently changes or empties the result.

**[No review record](docs/adr/0003-no-review-record.md).** Viewing a thought leaves no trace; only a change moves `updated_at`. Migration 2 irreversibly dropped `reviewed_at`; `-updated:90d..` finds stale thoughts, and a Status retires the rest.

**[Change stream](docs/adr/0004-change-stream.md).** `GET /events` pushes each write's id and Version (or `deleted`) as server-sent events. They are notices, not state: clients refetch through the normal endpoints and relist on every reconnect. The TUI merges them without reordering rows.
