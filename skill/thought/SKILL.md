---
name: thought
description: Capture, find and edit the user's thoughts with the `thought` CLI. Use when the user asks to capture something ("capture this", "save this as a thought"), to fold material into an existing thought ("add this to my thought about…"), or asks about their thoughts ("what thoughts do I have on…").
---

# thought

The user's thoughts live on one local server, reached only through the `thought` CLI. For any verb's syntax, flags and exit codes, run `thought help <verb>`. There is no history: every write overwrites, so anything you drop is gone for good.

Touch thoughts only when the user refers to them. Reading is then unrestricted; otherwise leave the thoughts alone and never search them unprompted.

## Capture

Capture only when the user asks for it; the request is the trigger. Never capture, or offer to, on your own initiative.

1. **Search first**, before every `thought add`: a text Filter on the thought's key terms, e.g. `thought ls borrow checker tag:rust`.
   - A clear match: ask whether to fold the discussion into that thought (then it is an edit, see below) or capture a new one. This is the only question in the capture path.
   - No match: capture without asking.
2. **Write the body in the user's words**, as a self-contained markdown document:
   - Correct spelling and grammar. Rephrase only where the English is plainly bad, and only enough to make it read well.
   - Keep everything else as the user wrote it: the thoughts, the reasoning, the order and the tone.
   - Never question the thought. Don't challenge it, critique it, add to it, or guess at why the user thought of it, and don't add open questions they didn't raise.
   - It stands alone: no transcript, no link to the conversation, no "as discussed".
   - The title lives apart from the body, so the body carries no H1 repeating it.
3. **Name it.** You pick the title. Run `thought tags` and reuse the existing tags; mint a new tag only when none fits, and say so in the report.
4. **Leave the rest at its defaults.** Status stays `raw` unless the user names one. Set an attribute only when the user names it or a key already listed by `thought attrs` plainly applies; a new key comes only from the user. Record no provenance (source, session, "captured by").
5. **Add it directly.** Review is the approval step, so there is no draft to approve.
6. **Report** one line per thought: id, title, tags, e.g. `42 Borrow checker for config files [rust, dsl]`.

**One thought per distinct thought.** A discussion with two separable thoughts becomes two captures, each through the steps above, and the report names both ids. Cross-reference in plain prose: "see thought 42".

## Editing

1. **Find the thought:** `thought ls <filter> --json`. With one clear match, proceed and name the thought you touch. With several, list them and ask which one. Never guess.
2. **Change it:**
   - A local change: `thought replace`.
   - A restructure: `thought show --json <id>`, rewrite the body, then `thought write <id> --version <n>` with the `version` from that show.
3. **Integrate** new material where it belongs, in the user's words as in capture: fix spelling and grammar, and never question it. The document stays a current statement of the thought, with no dated log or "Update:" sections.
4. **No silent loss.** Keep existing content unless the user asks you to remove or contradict it. When you remove something, quote what was removed in your report.
5. **Title and status** change only when the user asks.
6. **Exit 4** means the thought changed under you: re-read it, reapply your change to the fresh body, and retry.

## Boundaries

- **Every write names a Version.** `--force` and `thought rm` are off-limits. Asked to delete, hand the user the command to run themselves, `! thought rm <id>`, and if they only want the thought out of the open set, offer `thought status <id> dropped`.
- **Several thoughts at once:** list the affected thoughts, wait for the go-ahead, then work one thought at a time.
- **Exit 7**, the daemon is unreachable: report the CLI's message and print the finished document into the conversation, so nothing is lost. The daemon is the user's to run: never start it, and never write the document to a fallback file.
