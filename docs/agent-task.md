# ward agent task

`ward agent <name> task [owner/repo]` is the from-scratch sibling of
[`headless`](agent.md). `work` and `headless` both need an issue that already
exists; `task` *files* one from your instructions, then runs the exact `headless`
flow against it. The agent reads the freshly-filed issue, carries it to merge,
and **closes it (`closes #N`) to end the loop**.

## Usage

```bash
ward agent claude task coilyco-flight-deck/ward --instructions "add a --foo flag to bar"
ward agent claude task --instructions "fix the flaky reaper test"   # repo inferred from cwd's git origin
ward agent claude task coilyco-flight-deck/ward --instructions-file ./task.md   # long/multi-line bodies
ward agent claude task coilyco-flight-deck/ward -i "do the thing" --print   # show issue + plan, file nothing
```

## How the issue is built

- **Repo.** The positional `owner/repo` is optional; omit it and `task` infers it
  from the cwd's git origin, same as `ward container up`.
- **Instructions.** Pass exactly one of `--instructions`/`-i` (a string) or
  `--instructions-file` (a path - the escape hatch for long bodies).
- **Title.** The first non-empty line of the instructions, truncated at 72 runes.
- **Body.** The full instructions plus a `Filed by ward agent <mode> task`
  provenance footer, so the issue reads as agent-filed rather than hand-written.

## Trust gate and dry-run

The same trust gate as `work`/`headless` runs *before* anything is filed: an
off-org owner is refused, because the container runs under `bypassPermissions`.

`--print` resolves the repo, renders the issue that *would* be filed and the
docker plan, then exits having filed nothing and run nothing - the seed prompt's
`#N` shows as `#0` until the issue is actually filed.

## Pre-flight (ward#149)

Because `task` runs the exact `headless` flow, it also runs the same
[pre-flight feasibility check](agent.md#headless-pre-flight-ward137-ward147)
before detaching: it files the issue, then asks the agent whether it can carry it
to merge unattended. A **GO** launches the container; a **NO-GO** launches
nothing and comments the reason on the just-filed issue, leaving a real issue a
human can review and re-dispatch with `ward agent <name> headless <ref>
--no-preflight`. The check is skipped under `--print`, with `--no-preflight`, and
when there is no terminal (scripted dispatch).

## See also

- [docs/agent.md](agent.md) - the `work`/`headless` surfaces and the shared
  container launch, credentials, and reaper model.
- [docs/container.md](container.md) - the ephemeral least-access container model.
