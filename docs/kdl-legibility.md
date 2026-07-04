---
doc_goal: The KDL-legibility proposal - name every "quirky programmer shortcut" token a reader hits in a ward KDL file, give each its plain-English meaning, and propose a human-readable rename or alias to land as a cli-guard PR (the engine owns the grammar), so ward's guardfiles read as prose without breaking any existing file.
---
# KDL legibility - renaming the quirky tokens

[ward#287](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/287)
asks that ward's KDL files read as prose, not "quirky programmer shortcuts."
Two tokens were named: `argv0` and `$input.repo`. This doc is the **proposal**:
what each offender means today, and the human-readable spelling to adopt.

## Where the change lands

The tokens are grammar, parsed by cli-guard's `specverb-gen` (the engine), not
by anything in this repo. So the rename is a **cli-guard PR that adds aliases**,
and ward's guardfiles switch to the new spelling only once that alias ships.
Aliases, never hard renames: every current file keeps parsing, `kdlfmt` and
`ward doctor` stay green, and authors migrate on their own schedule. Related:
[ward#205](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/205),
[ward#297](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/297).
The legend for reading them **today** lives in
[guardfile-grammar.md](guardfile-grammar.md).

## The offenders and their proposed spelling

- `argv` (exec per-grant override, `can run headless { argv -p }`) - the C/POSIX **argument vector**. Proposed alias **`invoke`**: `can run headless { invoke -p }` reads as "invoke the binary with `-p`".
- `argv { ... }` (fleet launch block, dialect 2) - the per-mode launch command-line. Proposed alias **`launch`**: `launch { headless ...; interactive ... }`.
- `$name` / `$name.field` (stepflow refs, the `$input.repo` family) - "the value of the input or step-result named `name`". Keep the `$` short form, and add a spelled-out **`value name`** / **`value name.field`** for authors who want prose. Drop the implicit `input.` namespace the issue quoted - ward already writes the barer `$repo`, not `$input.repo`.
- `op <id>` (operationId pin) - proposed alias **`operation-id`**, so `op issueCreateLabel` becomes `operation-id issueCreateLabel`.

`restrict <field> matches`, `deny-when ... matches`, `never run`, and `collect`
already read in plain English and need no rename - listed here only so the
audit is complete.

## Status

Proposal only. No ward KDL file changes until the cli-guard alias lands, so this
run ships the legend (above) plus this ratifiable naming call. The naming is
Kai's to ratify; the spellings above are the recommendation, not a fait
accompli. The engine change is tracked on cli-guard.

## See also

- [guardfile-grammar.md](guardfile-grammar.md) - reads every token listed here in its current spelling.
- [ward-kdl.md](ward-kdl.md) - the build-time authoring layer the grammar feeds.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
