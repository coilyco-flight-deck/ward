# ward agent: surfaces

Detail backing [`../SKILL.md`](../SKILL.md) step 4. Ref normalization is in [`normalization.md`](normalization.md).

`ward agent <surface> <ref>` takes a surface and `--driver` picks the harness (claude|codex|qwen|goose, default claude). The two issue-carrying surfaces are `work` (interactive) and `headless` (detached); both run the agent in a fresh ephemeral container that clones the repo inside itself. (`task` files an issue from a freeform pointer first then runs the headless flow; `reply` and `ask` answer on an existing thread - all out of scope here, this skill takes an already-open ref.)

**headless (the bare-ref default).** Pre-decided work: "go execute the thing we already figured out." Does not need Kai in the loop; the PR is the review gate. A bare ref with no surface word runs this. Detaches, runs print mode (`claude -p`), and runs an autonomous GO/NO-GO pre-flight before detaching when launched from a terminal (`--no-preflight` skips it).

```bash
ward agent headless coilyco-flight-deck/ward#98   # detached container, fire-and-forget, no human eyes
ward agent coilyco-flight-deck/ward#98            # bare ref -> the same headless carry
ward agent headless coilyco-flight-deck/ward#98 --driver codex   # pick another harness
```

Headless detaches by design: the container backgrounds and the call returns - returning is not the work finishing. It streams live progress to the container log (`docker logs <name>` / `ward container exec`). The reaper backstops residual work if the agent crashes.

**work (interactive).** Pick when Kai signals supervision ("open one for me", "let me iterate on this", "spin this up", "HITL this", "give me a session on it") or live decisions remain in the issue (thin spec, open design questions). A locked design doc with bounded open questions is headless; a one-line "figure out X" is work. `work` attaches the container to the terminal; `--detach` backgrounds it.

```bash
ward agent work coilyco-flight-deck/ward#98             # container attached to this terminal
ward agent work coilyco-flight-deck/ward#98 --new-tab   # spawn it into its own Warp tab (the sidequest path)
```

**`--new-tab`** spawns the work into its own Warp tab instead of attaching here - the sidequest spawn. See `tooling-sidequest`.

**Explicit surface words always win.** "headless"/"AFK" or "interactive"/"supervised"/"watch" override the heuristic.

**Isolation is the container (ward#174, docs/container.md).** Every surface runs in a fresh ephemeral container that clones the repo inside itself (no host worktree, no host-tree pollution), under `bypassPermissions`, reaper-backed, and reserves the issue (2h TTL) so two runs never double-work it; `--force` reclaims a stale or foreign hold. A run can be granted extra writable repos with `--repo owner/name` (repeatable; `--with-repo` is the legacy alias). Clone, prompt seeding, reservation, audit, and the reaper are owned by `ward agent` / `ward container`, not this skill.

## Examples

* "coily dispatch coily-siren coily-issue 125" -> `ward agent headless coilyco-bridge/coily#125`
* "dispatch coily co ai 313" -> `ward agent headless coilyco-bridge/agentic-os-kai#313`
* "fire an agent on ward 56" -> `ward agent headless coilyco-flight-deck/ward#56`
* "open one for me on backend 12" -> `ward agent work coilyco-flight-deck/backend#12`
* "let me iterate on the site issue 5" -> `ward agent work coilysiren/website#5`
* "spawn eco mods 17 in its own tab" -> `ward agent work coilyco-bridge/eco-mods#17 --new-tab`
