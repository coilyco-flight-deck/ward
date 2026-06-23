# ward agent: headless pre-flight (ward#137, ward#147)

`headless` detaches into a fire-and-forget run nobody is watching, so when it is
**dispatched interactively** (a human at the terminal) ward inserts a quick
pre-flight *before* detaching. The gate is **fire-and-forget from your POV**
(ward#147): you launch and walk away, and ward acts on the agent's verdict with
no prompt to answer:

1. The agent gets a short prompt with the issue title + body **and its comment
   thread** and answers, in a sentence or two, whether it thinks it can carry the
   issue to merge unattended, ending on a `GO` / `NO-GO: <reason>` line. The
   thread is fed so a decision the author made in the comments overrides the
   original framing (ward#154) - the prompt tells the agent to weigh the latest
   word, not just the body, so re-dispatching after answering an open question in
   a comment actually clears the gate. ward's own automated comments (reservation
   pings and prior NO-GO verdicts, both carrying a hidden marker) are stripped
   from that thread, so only human words sway the read and a stale NO-GO posted
   after the author decided can't anchor the next one. ward runs this as a
   one-shot on the host (`claude -p`, or `goose run -t` for the goose mode),
   echoes the read to your terminal, and parses that final verdict line
   (markdown bold, bullets, and quote markers are tolerated; the last verdict line
   wins). The read is **issue-text-only**: the real run happens in a fresh clone
   of the issue's repo inside the container, so the prompt tells the agent the host
   cwd is unrelated scratch and to judge feasibility from the issue alone, never
   from whatever files are in the local tree. ward also runs the read in a neutral
   empty temp dir, **not the dispatch cwd** (ward#169), so a coding agent that
   ignores that instruction and walks the working tree finds nothing there to
   mistake for the clone - this is what stops a read dispatched from one repo's
   checkout from false-flagging `WRONG-REPO` because the issue's files look
   "missing" locally. Both levers are belt-and-suspenders; either alone kills the
   false gate.
2. On **GO** - or any read ward can't pin to an explicit NO-GO - the detached run
   launches. The bias is to proceed: only the agent itself saying "don't" blocks.
3. On **NO-GO** ward launches nothing and instead **posts a comment on the issue**
   with the reason, the full read (folded away), and how to re-dispatch. The work
   lands back in front of a human rather than failing silently.
4. On **WRONG-REPO** (ward#159) - the agent judged, from the issue text alone,
   that the work plainly belongs in a *different* repo - ward **blind-fires** a
   fresh issue into that repo and launches nothing here. See
   [docs/agent-wrong-repo.md](agent-wrong-repo.md).

## When the check is skipped

The check is skipped when there is no terminal (scripted/piped), on
`--print` (a dry run), and with `--no-preflight` (the escape hatch for a run
launched from a TTY that you still want to fire blind - it also re-dispatches a
NO-GO issue you've decided is good to go). The gate runs for both full
carry-to-merge harnesses, **claude and goose**, which are kept at parity here so
a rapidly-dispatched goose chore is feasibility-checked the same way (ward#148);
goose answers via `goose run -t`, claude via `claude -p`. Modes with no host
one-shot wired yet (`codex`/`qwen`), a host without the agent binary, or a read
that doesn't complete all **proceed** rather than block, since none of those is
the agent declining the work (and the reaper still backstops residual work).
`task` runs this **same pre-flight** (ward#149); see
[docs/agent-subcommands.md](agent-subcommands.md).

## See also

- [docs/agent.md](agent.md) - the `ward agent` verb family and usage.
- [docs/agent-wrong-repo.md](agent-wrong-repo.md) - the WRONG-REPO blind-fire path.
- [docs/agent-reservation.md](agent-reservation.md) - the reservation precheck that runs first.
