# Thoughts

A single-user service that is the one home for thoughts, so they stop rotting in scattered folders.

## Language

**Thought**:
A single markdown document with a title, identified by a server-assigned id. Thoughts are flat: they have no hierarchy and no location other than the server.
_Avoid_: Note, file, entry, project

**Tag**:
A bare label on a thought, with no key and no value, used for grouping and filtering. A thought carries a set of tags. A tag is not an attribute.
_Avoid_: Category, folder, label

**Attribute**:
A free-form key–value pair on a thought, with exactly one value per key. Status is the only built-in attribute: the only one the service itself gives meaning to.
_Avoid_: Field, property, metadata

**Status**:
The built-in attribute saying where a thought is in its life. Every thought has exactly one: raw (captured, not yet through Review, however much it was discussed beforehand), active (alive, however slowly it moves), done (realised or concluded) or dropped (decided against). Raw and active are open; done and dropped are closed. A thought may move from any status to any other.
_Avoid_: State, stage

**Capture**:
Creating a new thought, either by an agent writing it down in the user's words (spelling and grammar corrected, never questioned) or by the user writing it down. A captured thought starts raw unless a status is named. Folding a later discussion into an existing thought is an edit, not a capture.
_Avoid_: Upload, import, save

**Version**:
A counter on a thought that advances with every change. A writer must name the version it last saw, so a change it never saw is not overwritten. It is not history: earlier versions are gone.
_Avoid_: Revision, etag

**Filter**:
An expression over a thought's tags, attributes, text and dates that selects a set of thoughts.
_Avoid_: Search, query

**Review**:
Cycling through thoughts one at a time to revisit and act on them. Nothing records that a thought was reviewed: only a change to the thought leaves a trace.
_Avoid_: Browse, triage
