# ward agent task

`ward agent <name> task` is the from-scratch sibling of [`headless`](agent.md):
`work`/`headless` need an issue that already exists; `task` *files* one, carries
it to merge, and **closes it (`closes #N`)**. It runs in one of two modes, chosen
by **repo omission**:

- **DIRECT** — an explicit `owner/repo` (or a bare `--instructions` with cwd
  inference); the issue is filed there and carried, same as it always has.
- **ROUTE** (ward#164) — a freeform task as the positional argument and *no*
  repo; ward finds the repo for you.

A positional that parses as `owner/repo` is DIRECT; a freeform positional with no
instruction flag is ROUTE. Passing *both* a freeform positional and
`--instructions`/`--instructions-file` is a contradiction and errors.

## Usage

```bash
ward agent claude task "the reaper test is flaky, make it deterministic"   # ROUTE
ward agent claude task coilyco-flight-deck/ward -i "add a --foo flag"        # DIRECT
ward agent claude task -i "fix the flaky reaper test"          # DIRECT, repo from cwd
ward agent claude task coilyco-flight-deck/ward --instructions-file ./task.md
ward agent claude task coilyco-flight-deck/ward -i "x" --print   # show plan, file nothing
```

## DIRECT mode

Pass exactly one of `--instructions`/`-i` or `--instructions-file` (the escape
hatch for long bodies). The **title** is the first non-empty instruction line
(≤72 runes); the **body** is the instructions plus a provenance footer. DIRECT
runs the exact `headless` flow, including the same
[pre-flight feasibility check](agent.md#headless-pre-flight-ward137-ward147)
before detaching (a NO-GO comments and launches nothing).

## ROUTE mode (ward#164)

Hand ward a task and it does the placement:

1. **Intake record.** The literal task is filed verbatim in `coilysiren/inbox`,
   capturing the raw ask before routing.
2. **Live survey.** ward lists the primary-org repos via the Forgejo API (no
   snapshots) and asks the agent, in a one-shot host call, to route the task,
   ending on `REPO: owner/repo - <note>` or `UNCLEAR: <reason>`.
3. **Consult exit.** An UNCLEAR verdict — or a routed target that isn't a trusted
   owner, or is the inbox itself — files no child and launches nothing; it
   comments the reason on the intake record and leaves it open for a human.
4. **Scoped child.** On a confident REPO, ward files a scoped child issue in that
   repo (the note + original task verbatim + a cross-link to the intake record).
5. **Cross-link + close.** The intake record gets a comment linking the child,
   then closes.
6. **Carry to merge.** The child is carried by the standard headless container.

The survey *is* ROUTE's feasibility gate, so ROUTE skips the headless pre-flight.

### Parity caveat (ward#148)

The survey is a host one-shot agent call, so ROUTE only works for a mode with a
host self-assessment slot — `claude`/`goose` have one; `codex`/`qwen` don't, and
are steered to DIRECT (`ward agent codex task <owner/repo> -i "..."`).

## Trust gate and dry-run

The same trust gate as `work`/`headless` runs *before* anything is filed: an
off-org owner is refused. In ROUTE it also applies to the *routed* target — a
survey that picks an off-org repo is bounced, not filed.

`--print` files nothing and runs nothing: DIRECT renders the issue + docker plan
(`#N` shows `#0` until filed); ROUTE renders the intake record and describes the
live survey + child + carry that would follow.

## See also

- [docs/agent.md](agent.md) — the `work`/`headless` surfaces and shared launch.
- [docs/container.md](container.md) — the ephemeral least-access container model.
