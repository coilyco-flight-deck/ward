---
doc_goal: Teach a spec author the dialect-1 KDL guardfile grammar that feeds ward-kdl's build-time generator - the deny-by-default spec and exec sub-dialects, a minimal working surface, and where auth lives as a lazily-resolved reference not a baked secret - so an audited ward ops verb can be authored from source.
---
# Guardfile grammar (dialect 1)

This is the dialect-1 KDL grammar, a minimal working guardfile, and where auth
config lives ([ward#437](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/437)). For **whether you need a guardfile at all**, read
[ward-kdl.md](ward-kdl.md) first - most adopters do not.

A permission surface is one [KDL](https://kdl.dev) document (a node is a name,
arguments, then an optional `{ ... }` child block, with `//` comments). A guardfile
is one top-level `wrap` node whose arguments are the **mount path**: the leading
`ward-kdl` token maps to the `ward` root and drops, so `wrap ward-kdl ops forgejo`
mounts at `ward ops forgejo` ([ward-kdl-in-ward.md](ward-kdl-in-ward.md)). The
block is one of two sub-dialects, picked by its first child.

## Spec surface - `spec <file>` first

cli-guard `specverb` binds each verb to an OpenAPI/Swagger operation:

- `spec <file>` - the spec lock in the bundle dir this surface compiles against.
- `base-url "<host>/api/..."` - the endpoint. Deployment config ([ward#441](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/441)).
- `auth <scheme> { ... }` - authentication, see "Where auth config lives" below.
- `restrict <field> matches <glob>` - scope gate: a leaf carrying `{field}` in its path is denied unless the value matches (`restrict owner matches example*`).
- `can <verb> <resource>` - grant one operation (`can list issue`). Ungranted verbs are absent at compile time, not denied at runtime.

## Exec surface - `exec <bin>` first

cli-guard `execverb` wraps a local CLI, the binary fixed at parse so the caller
can never substitute it:

- `exec <bin>` - the wrapped binary (`docker`, `kubectl`).
- `env <NAME> { value <provider> "<addr>" }` - a var resolved lazily at exec time.
- `can run <verb...> { describe "..." }` - grant one subcommand (quote multi-word: `can run "volume ls"`). At least one token must follow `run`.
- `deny-when <field> matches <glob...>` - per-operation guard on the kwarg or positional naming the resource.
- `never run <verb>` - spell out a withheld verb (unlisted verbs are denied anyway).
- `argv <token...>` - inside a `can run` block, fix the real invocation this friendly verb maps onto (`can run headless { argv -p }` runs the binary with `-p`). `argv` is the C/POSIX **argument vector**, the array a program receives - a bare `argv` runs the binary with no extra tokens. This is [ward#287](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/287)'s named offender; [kdl-legibility.md](kdl-legibility.md) proposes a plain-English alias.

Both sub-dialects are deny-by-default. Exec flags pass through unrestricted, so
only verbs safe under any flag (reads) belong on an exec surface.

## A minimal working guardfile

The smallest complete exec surface names one binary and one subcommand:

```kdl
// ward-kdl.kube.guardfile.kdl
wrap ward-kdl ops kube {
    exec kubectl
    can run get {
        describe "read/list resources"
    }
}
```

Drop it in `.ward/ward-kdl/`, run `make build-ward-kdl`, and `ward ops kube get
pods` works while every other `kubectl` form stays absent. This is the shape
[ward#226](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/226) opened the `git` guardfile with. For a real start, copy the complete
spec surface in [examples/ward-specs/](../examples/ward-specs).

## Where auth config lives

Auth is a node **inside** the `wrap` block, never a separate file or a
compiled-in secret. The value is a **reference** resolved lazily, so the source
carries the token's location, not the token:

```kdl
auth header-token {
    header Authorization
    prefix "token "
    value ssm "/example/forgejo/api-token"
}
```

`value ssm "<path>"` names an AWS SSM path the deploying fleet owns, so the
`base-url`, `ssm` path, and `restrict owner` gate are all deployment config you
swap, not engine code ([ward#441](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/441)). An exec surface pulls a credential with `env
<NAME> { value ssm "<path>" }` the same way, resolved at exec time so mounting
never reads a token.

## Composite verbs - the `action` (stepflow) dialect

A `can` grants one operation. An `action` chains several into one bounded
composite verb (the forgejo `move-issue`, the `view issue` comment-thread
shadow). Its child nodes are the least-obvious tokens a reader hits, so decode
them here:

- `action <verb> [<resource>]` - declare the composite. Same call shape as the leaf it may shadow.
- `input <name> { positional | flag ; required ; help "..." }` - one declared argument. `positional` is order-bound, `flag` is `--name`.
- `call <verb> <resource> { args { ... } as <bind> }` - run one mount verb, then remember its result under `<bind>`.
- `collect <verb> <resource> { ... page-param P limit-param L as <bind> }` - like `call`, but auto-paginates the listing and binds the merged array.
- `$<name>` - **the value of** the input or bound result named `<name>`. `$owner` is the `owner` input, `$src.title` reads the `title` field of the result bound `as src`. This dotted `$ref` is [ward#287](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/287)'s other named offender (the `$input.repo` shape); [kdl-legibility.md](kdl-legibility.md) proposes a spelled-out alias.
- `fail-when "<expr>"` - exit non-zero when `<expr>` holds (`"$issue.state == 'closed'"`), so the step surfaces a warning instead of passing silently.

Read a whole action top to bottom: each `input` names an argument, each
`call`/`collect` runs a step and binds its output, and later steps read those
bindings back with `$name` / `$name.field`.

## See also

- [ward-kdl-authoring.md](ward-kdl-authoring.md) - how you get the compiler and swap the bundle.
- [ward-kdl.md](ward-kdl.md) - the build-time layer, and whether you need a guardfile.
- [kdl-legibility.md](kdl-legibility.md) - the [ward#287](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/287) proposal to rename these quirky tokens.
