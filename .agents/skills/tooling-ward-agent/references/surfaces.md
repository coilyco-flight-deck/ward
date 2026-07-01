# ward agent: the engineer (detached / autonomous only)

Detail backing [`../SKILL.md`](../SKILL.md) step 4. Ref normalization is in [`normalization.md`](normalization.md).

`ward agent <role> <ref>` takes a role and `--driver` picks the harness (claude|codex|qwen|goose, default claude). The roster is `engineer`|`director`|`advisor` (ward#347, ward#353). This skill dispatches the issue-carrying role, **`engineer`** - it runs the agent in a fresh ephemeral container that clones the repo inside itself. The argument type selects the mode: a ref carries an existing issue (this skill's case); freeform text would file an issue first. (`director` is the autonomous backlog loop that also surfaces a read-only scope + dispatch session on drain - the old `architect` role, folded in by ward#353; `advisor` answers without code - both out of scope here, this skill takes an already-open ref.)

**Engineer is detached / autonomous only (ward#356).** Every engineer run is the same fire-and-forget shape: "go execute the thing we already figured out." It does not need Kai in the loop; the PR is the review gate. A bare ref with no role word runs it. It detaches, runs print mode (`claude -p`), and runs an autonomous GO/NO-GO pre-flight before detaching when launched from a terminal (`--no-preflight` skips it).

```bash
ward agent engineer coilyco-flight-deck/ward#98   # detached container, fire-and-forget, no human eyes
ward agent coilyco-flight-deck/ward#98            # bare ref -> the same engineer
ward agent engineer coilyco-flight-deck/ward#98 --driver codex   # pick another harness
```

The detached run backgrounds by design: the container backgrounds and the call returns - returning is not the work finishing. It streams live progress to the container log (`docker logs <name>` / `ward container exec`). The reaper backstops residual work if the agent crashes.

**No attach surface anymore (ward#356).** The old `--watch` (`-w`, the `work` mode) and its `--new-tab` Warp sidequest spawn are **retired** - they error as unknown. When Kai signals supervision ("open one for me", "let me iterate on this", "HITL this", "give me a session on it"), that is no longer an attached engineer run: hands-on work funnels to the **`director`** (the managed interactive shell, `ward agent director --repo owner/name`), which itself surfaces a read-only scope + dispatch session on drain (or before the first drain, if you decline the opening drain) - the old `architect` path, folded into the director by ward#353. You drive the managed shell, you do not pair with one worker.

**Isolation is the container (ward#174, docs/container.md).** Every role runs in a fresh ephemeral container that clones the repo inside itself (no host worktree, no host-tree pollution), under `bypassPermissions`, reaper-backed, and reserves the issue (2h TTL) so two runs never double-work it; `--force` reclaims a stale or foreign hold. A run can be granted extra writable repos with `--repo owner/name` (repeatable). Clone, prompt seeding, reservation, audit, and the reaper are owned by `ward agent` / `ward container`, not this skill.

## Examples

* "coily dispatch coily-siren coily-issue 125" -> `ward agent engineer coilyco-bridge/coily#125`
* "dispatch coily co ai 313" -> `ward agent engineer coilyco-bridge/agentic-os-kai#313`
* "fire an agent on ward 56" -> `ward agent engineer coilyco-flight-deck/ward#56`
* "open one for me on backend 12" -> engineer has no attach surface; for hands-on work drive the director: `ward agent director --repo coilyco-flight-deck/backend`
