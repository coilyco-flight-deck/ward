---
doc_goal: Answer the graft-removal consult - for each Go graft ward stacks on the specverb-generated `ops forgejo` tree, say whether the guardfile can already express it or an upstream cli-guard capability is missing, so removal lands each behavior in the guardfile without regressing the verb.
---
# Removing the ward-over-ward-kdl graft layer ([ward#407](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/407))

`buildForgejoOps` (`cmd/ward/ops.go`) calls `specverb.Build`, then mutates the
built `ops forgejo` tree with four Go patches. Each works around something the
guardfile + `specverb` do **not** generate the way ward wants. This issue
tracks removing the layer by pushing every behavior **down** into the authoring
layer so the generated verb is already correct. This is the per-graft design that
removal needs first - not the removal. Pulling a graft before its behavior is
re-homed regresses the verb (exactly how [ward#404](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/404) happened: the [ward#380](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/380) shadow
dropped `specverb`'s synthesized `--body-file`).

## Verdict: not yet, for all four

The `action` grammar (`http/guardfile`, cli-guard `v0.75.0` in `go.mod`, driver
`v0.72.0` via Makefile `REF`) accepts only `describe`, `input`, `call`,
`collect`, `poll`, `canary`, `compensate`, `fail-when`; an `input` is only
`positional`/`flag`/`required`/`help`/`default`. There is **no** node to shape an
action's output (project/select/template) and **no** node to source an input from
a file. Each graft is one of those two missing shapes, or a cross-dialect merge
the generator does not emit. cli-guard is a pinned upstream ward **consumes**,
not a repo a ward container is granted to release, so every re-home is gated on a
cli-guard release, a `go.mod` + `REF` bump (the [ward#326](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/326) dance), the guardfile
rewrite, then the graft deletion - deletion **last**.

## The four grafts

1. **`overrideForgejoViewIssue` - lean `issue view`.** Renders `{issue,
   comments}` with each user collapsed to its login literal. The guardfile's
   `action view issue` already fans out the two calls but cannot say "render
   through this projection." Blocked on a `specverb` **per-call output
   projection** hook (JMESPath-shaped, declared on the action). See
   [ops-forgejo-view.md](ops-forgejo-view.md).
2. **`overrideForgejoCreateIssue` - `--quiet`.** Forces `--output text --query
   number`, then prints `{owner}/{repo}#N`. Needs a declared *output mode* plus a
   *template* joining request inputs with a response field - the same projection
   hook as graft 1, with input interpolation. See
   [ops-forgejo-quiet.md](ops-forgejo-quiet.md).
3. **`overrideForgejoCommentIssue` - `--body-file`.** `specverb` synthesizes
   `--body-file` on generated write leaves but not on the `action comment issue`
   shadow (confirmed: `issue create --help` lists it, the comment shadow's does
   not). Blocked on teaching `specverb` to carry that synthesis onto a custom
   action whose `call` targets a write leaf. Narrowest of the four; the code
   exists, it just does not reach the action path.
4. **`graftForgejoAdminExec` - admin/doctor remote-exec.** Not an override but a
   *merge of two dialects* (spec + exec) under one command. The [ward#284](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/284)
   auto-mount ([ops-forgejo-in-ward.md](ops-forgejo-in-ward.md)) deliberately
   excludes it because it targets the specverb group. Blocked on a build-time
   ward-kdl capability to co-emit one spec+exec area. See
   [ops-forgejo-admin.md](ops-forgejo-admin.md).

## Sequencing

Land the cli-guard capabilities upstream (order of rising surface: 3, then 1+2,
then 4), bump `go.mod` + `REF`, rewrite the guardfiles + `make build-ward-kdl`,
then delete each graft behind a green guardrail. Composes with the four-layer
cleanup in **[agentic-os#306](https://github.com/coilysiren/agentic-os/issues/306) / [agentic-os#308](https://github.com/coilysiren/agentic-os/issues/308)**; sequence after those so this does not churn
code about to move.

## The guardrail

[ward#404](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/404) added `TestForgejoClientInvocationsUseAcceptedFlags` (a dropped flag
fails the build). [ward#407](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/407) adds `TestForgejoGraftInventory`
(`cmd/ward/forgejo_ops_test.go`): every behavior this removal must re-home - the
lean `view`, `create --quiet`, `comment --body-file`, the `admin`/`doctor` exec
leaves - asserted present on the built tree. A removal that pulls a graft without
re-homing turns it red. As a behavior folds down, move its assertion from "graft
present" to "generated leaf carries it."

## See also

- [ops-forgejo-view.md](ops-forgejo-view.md) - graft 1, the lean `issue view`.
- [ops-forgejo-quiet.md](ops-forgejo-quiet.md) - graft 2, `create --quiet`.
- [ops-forgejo-admin.md](ops-forgejo-admin.md) - graft 4, the admin/doctor slice.
- [ops-forgejo-in-ward.md](ops-forgejo-in-ward.md) - the auto-mount, and why graft 4 is excluded.
- [ward-kdl.md](ward-kdl.md) - the build-time layer the removal pushes into.
</content>
