# Nothing records that a thought was reviewed

A thought has no `reviewed_at`. Review is a walk through a Filter, ordered by `updated`, `created` or at random; looking at a thought leaves no trace, and only a change to it moves `updated_at`. v1 had recorded every review in a separate timestamp, with a mark-reviewed endpoint and verb, a `reviewed` date term and sort, a `✓` marker, and a default Review queue of open thoughts unseen for 90 days. Migration 2 drops the column and its data, which is why this is an ADR: the record cannot be rebuilt.

It was removed because it was a second, invisible notion of "touched" next to `updated_at`: it changed nothing about the thought, it was set by keys that otherwise only moved the selection, and it hid open thoughts from the default queue by a rule the user had to remember. What the queue was for is covered by concepts the user already has: a thought that should stop coming up gets a Status, and `-updated:90d..` finds the ones nothing has happened to.

## Considered options

- **Keep the timestamp, drop it from the default queue**: the smallest change, but the concept stays in the data model, the API, the Filter and the TUI for a queue that no longer uses it.
- **Keep it as an ordinary attribute**: the user could set `reviewed=2026-09-23` by hand, but attribute values have no date semantics, so it could not order or filter by age, which was the timestamp's whole point.
