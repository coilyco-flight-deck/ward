---
doc_goal: Make a reader understand the GO/NO-GO pre-flight as a bias-to-proceed guard that only lets the agent itself veto an unattended run - covering prompt shape, thread filtering, cwd isolation, and every skip path - so they can predict exactly when a run launches, comments, or blind-fires.
---
# ward agent: headless pre-flight

`headless` detaches into a fire-and-forget run nobody is watching, so when it is
**dispatched interactively** (a human at the terminal) ward inserts a quick
pre-flight *before* detaching. The gate is **fire-and-forget from your POV**:
you launch and walk away, and ward acts on the agent's verdict with
no prompt to answer:

1. The agent gets a short prompt and answers whether it can carry the issue to
   merge unattended, ending on a `GO` / `NO-GO: <reason>` line. Four moving parts:
   - **Prompt shape** - carries the issue title + body **and its comment thread**.
   - **Thread filtering** - the thread is fed so a decision the author made in the
     comments overrides the original framing: the prompt weighs the
     latest word, not just the body, so re-dispatching after a comment answers a
     question clears the gate. ward's own automated
     comments (reservation pings and prior NO-GO verdicts, both carrying a hidden
     marker) are stripped from the thread, so only human words sway the read.
   - **Execution mechanics** - run as a one-shot on the host (`claude -p`); ward
     echoes the read to your terminal and parses the final verdict line (markdown
     bold, bullets, and quote markers tolerated; the last verdict line wins).
   - **cwd isolation** - the read is **issue-text-only**: the real run happens in
     a fresh clone in the container, so the prompt tells the agent the host cwd
     is unrelated scratch and to judge from the issue alone. ward also runs the
     read in a neutral empty temp dir, **not the dispatch cwd**, so an
     agent walking the working tree finds nothing to mistake for the clone -
     stopping a read from one repo's checkout false-flagging `WRONG-REPO` when its
     files look "missing" locally. Both levers are belt-and-suspenders; either
     alone kills the false gate (the original [ward#153](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/153) cwd-NO-GO).
2. On **GO** - or any read ward can't pin to an explicit NO-GO - the detached run
   launches. The bias is to proceed: only the agent itself saying "don't" blocks.
   An explicit **GO** also folds the read into the
   [reservation comment](agent-reservation.md) ward posts to claim the issue,
   so the thread records *why* it was judged carriable. A read with no
   clear verdict line proceeds but folds in nothing.
3. On **NO-GO** ward launches nothing and instead **posts a comment on the issue**
   with the reason, the full read (folded away), and how to re-dispatch - the work
   lands back in front of a human rather than failing silently.
4. On **WRONG-REPO** - the agent judged, from the issue text alone,
   that the work plainly belongs in a *different* repo - ward **blind-fires** a
   fresh issue into that repo and launches nothing here. See
   [docs/agent-wrong-repo.md](agent-wrong-repo.md).

## When the check is skipped

The check is skipped when there is no terminal (scripted/piped), on
`--print` (a dry run), and with `--no-preflight` (the escape hatch for a run
launched from a TTY that you still want to fire blind - it also re-dispatches a
NO-GO issue you've decided is good to go). Only a **trusted cloud harness**
(claude) runs the host read; a **local-model harness** (goose/opencode) is barred
([agent-preflight-trust.md](agent-preflight-trust.md)). A barred mode, a
mode with no one-shot wired (`codex`), no agent binary, or an incomplete read all
**proceed** rather than block, since none of those is the agent declining the work
(and the reaper still backstops residual work).
`task` runs this **same pre-flight**; see
[docs/agent-subcommands.md](agent-subcommands.md).

## See also

- [docs/agent.md](agent.md) - the `ward agent` verb family and usage.
- [docs/agent-wrong-repo.md](agent-wrong-repo.md) - the WRONG-REPO blind-fire path.
- [docs/agent-reservation.md](agent-reservation.md) - the reservation precheck that runs first.
