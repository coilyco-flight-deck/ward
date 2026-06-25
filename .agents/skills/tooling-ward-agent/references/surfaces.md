# ward agent: the engineer carry (detached vs --watch)

Detail backing [`../SKILL.md`](../SKILL.md) step 4. Ref normalization is in [`normalization.md`](normalization.md).

`ward agent <role> <ref>` takes a role and `--driver` picks the harness (claude|codex|qwen|goose, default claude). The roster is `engineer`|`architect`|`director`|`advisor` (ward#347). This skill dispatches the issue-carrying role, **`engineer`** - it runs the agent in a fresh ephemeral container that clones the repo inside itself. The argument type selects the mode: a ref carries an existing issue (this skill's case); freeform text would file an issue first. (`architect` is a read-only scoping session, `director` the autonomous backlog loop, `advisor` answers without code - all out of scope here, this skill takes an already-open ref.)

**The detached carry (the bare-ref default).** Pre-decided work: "go execute the thing we already figured out." Does not need Kai in the loop; the PR is the review gate. A bare ref with no role word runs this. Detaches, runs print mode (`claude -p`), and runs an autonomous GO/NO-GO pre-flight before detaching when launched from a terminal (`--no-preflight` skips it).

```bash
ward agent engineer coilyco-flight-deck/ward#98   # detached container, fire-and-forget, no human eyes
ward agent coilyco-flight-deck/ward#98            # bare ref -> the same engineer carry
ward agent engineer coilyco-flight-deck/ward#98 --driver codex   # pick another harness
```

The detached carry backgrounds by design: the container backgrounds and the call returns - returning is not the work finishing. It streams live progress to the container log (`docker logs <name>` / `ward container exec`). The reaper backstops residual work if the agent crashes.

**`--watch` (`-w`): the attached carry.** Pick when Kai signals supervision ("open one for me", "let me iterate on this", "spin this up", "HITL this", "give me a session on it") or live decisions remain in the issue (thin spec, open design questions). A locked design doc with bounded open questions is the detached carry; a one-line "figure out X" wants `--watch`. `--watch` attaches the container to the terminal.

```bash
ward agent engineer coilyco-flight-deck/ward#98 --watch             # container attached to this terminal
ward agent engineer coilyco-flight-deck/ward#98 --watch --new-tab   # spawn it into its own Warp tab (the sidequest path)
```

**`--new-tab`** spawns the attached carry into its own Warp tab instead of attaching here - the sidequest spawn. See `tooling-sidequest`.

**Explicit words always win.** "headless"/"AFK"/"detached" or "interactive"/"supervised"/"watch" override the heuristic (detached is the default; `--watch` attaches).

**Isolation is the container (ward#174, docs/container.md).** Every role runs in a fresh ephemeral container that clones the repo inside itself (no host worktree, no host-tree pollution), under `bypassPermissions`, reaper-backed, and reserves the issue (2h TTL) so two runs never double-work it; `--force` reclaims a stale or foreign hold. A run can be granted extra writable repos with `--repo owner/name` (repeatable; `--with-repo` is the legacy alias). Clone, prompt seeding, reservation, audit, and the reaper are owned by `ward agent` / `ward container`, not this skill.

## Examples

* "coily dispatch coily-siren coily-issue 125" -> `ward agent engineer coilyco-bridge/coily#125`
* "dispatch coily co ai 313" -> `ward agent engineer coilyco-bridge/agentic-os-kai#313`
* "fire an agent on ward 56" -> `ward agent engineer coilyco-flight-deck/ward#56`
* "open one for me on backend 12" -> `ward agent engineer coilyco-flight-deck/backend#12 --watch`
* "let me iterate on the site issue 5" -> `ward agent engineer coilysiren/website#5 --watch`
* "spawn eco mods 17 in its own tab" -> `ward agent engineer coilyco-bridge/eco-mods#17 --watch --new-tab`
