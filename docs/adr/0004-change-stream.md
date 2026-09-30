# The daemon pushes changes; the Review list merges them

v1 left change notifications out of scope, and the Review list was a snapshot that only a filter edit, `o` or `ctrl-r` refreshed, so a thought captured by an agent or the CLI while Review was open stayed invisible until the user thought to requery. The daemon now streams every successful write on `GET /events` as server-sent events carrying only the id and the new Version (or `deleted`), and the TUI folds them into its snapshot: new matching thoughts are inserted, changed rows are refetched, deleted rows are marked, and nothing reorders or disappears, which keeps the snapshot's promise that a thought does not jump or vanish under the cursor mid-Review.

The events are notices, not state: the client fetches what it shows through the ordinary endpoints, so there is one source of truth and no second serialization of a thought to keep in step. There is no replay: a client relists on every (re)connection, which is cheap over a local socket and is also how a client cut off for falling behind recovers.

## Considered options

- **Poll `GET /thoughts` on a timer**: no server change, but it trades latency against a steady stream of queries that almost always find nothing, and it cannot tell a deleted thought from one that stopped matching.
- **Requery on every change**: simplest on the client, but it reorders the list and drops thoughts the user just marked done under the default filter, breaking Review mid-walk.
- **Events carrying the full metadata**: saves the client a `GET` per changed row, but writes that answer with only a Version would need an extra read to publish, and concurrent writes would publish snapshots whose order has to be reasoned about; the Version on a notice already says which is newer.
- **Long-polling or WebSockets**: server-sent events are plain HTTP/1.1 over the existing socket, one-way as the need is, and parsed in a few lines by the client.
