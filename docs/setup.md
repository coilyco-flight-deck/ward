# ward setup

`ward setup` (public face `warded setup`) is the guided onboarding verb: it
scaffolds a repo's `.ward/ward.yaml` so a new consumer can adopt the gate in one
command instead of hand-authoring the allowlist. It is the front door to
[`ward exec`](exec-verb.md) and [`ward doctor`](doctor.md), not a guardfile
author.

## What it does

1. Walks up from the target dir (default cwd, `--dir` overrides) to the **repo
   root** - the first ancestor holding a `Makefile`.
2. Reads every Makefile target carrying a `## <description>` help comment and
   turns it into a verb: `run: make <name>`, `description:` copied verbatim from
   the help comment. This is exactly the shape [`ward doctor`](doctor.md)'s
   allowlist drift guard checks, so the generated file passes on the first run.
   Targets whose names are not legal verbs (uppercase, dots, underscores) are
   skipped with a note.
3. Writes `.ward/ward.yaml` with a **commented-out `security:` scaffold** - an
   inert template of the protected-binary / sudo / hooks policy the
   [doctor](doctor.md) probes read. It stays commented so a fresh scaffold parses
   to "no security declared". As of [ward#450](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/450) a standalone `ward doctor` **fails**
   on that (a `security:` block is required for a green run), but `ward setup`'s
   own doctor step reports it as a remediation `NOTE` rather than failing, so
   onboarding is not walled - uncomment and tailor the block, then doctor goes
   green.
4. Runs `ward doctor` against the file it just wrote (skip with `--skip-doctor`).

It **authors no guardfiles**. `.ward/ward.yaml` is the adoption contract
([ward#426](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/426), per [#455](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/455)/[#437](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/437)); the ward-kdl permission surfaces are a separate,
build-time layer ([ward-kdl.md](ward-kdl.md)).

## Usage

```bash
ward setup                 # scaffold ./.ward/ward.yaml from ./Makefile, run doctor
warded setup               # the cute spelling - same thing (carved out of the agent shim)
ward setup --dir path/to/repo
ward setup --force         # overwrite an existing .ward/ward.yaml
ward setup --skip-doctor   # write the config but do not run doctor
```

By default `ward setup` **refuses to clobber** an existing `.ward/ward.yaml`;
pass `--force` to regenerate. After it writes, prune the verbs you do not want to
expose and uncomment the `security:` block to fit your fleet.

## The `warded setup` carve-out

`warded` is normally a thin `ward` symlink that rewrites `warded <args>` into
`ward agent <args>` ([agent.md](agent.md)). `setup` is the one subcommand carved
out of that rewrite: `warded setup` routes to `ward setup`, because onboarding a
repo is a contributor action, not an agent dispatch. Every other `warded` word
still fronts `ward agent`.

## See also

- [doctor.md](doctor.md) - the drift guard `ward setup` runs, and the security probes.
- [exec-verb.md](exec-verb.md) - `ward exec <verb>`, what the scaffolded verbs feed.
- [config-discovery.md](config-discovery.md) - how ward finds the `.ward/ward.yaml` setup writes.
- [agent.md](agent.md) - the `warded` -> `ward agent` shim setup is carved out of.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
