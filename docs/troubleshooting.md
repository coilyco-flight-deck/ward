---
doc_goal: Give an operator a symptom-indexed single entry point that turns any stalled, refused, or nothing-landed `warded` run into the one diagnostic surface and fix, keyed to the guarded-execution reality (in-container auth, NO-GO pre-flight, clean-tree gate, land-or-salvage reaper) rather than to which subsystem broke.
---
# Troubleshooting a warded run

Your `warded` run failed or seemed to do nothing. Find your **symptom** below - not
the subsystem that failed - and it points at the one diagnostic surface and the fix.
Entries quote the error text so a search for the string you saw lands here.

First stop for any failed headless run: **its logs.** Every exited run drains to
`~/.ward/agent-logs/<container>/` before cleanup - `console.log` (agent + reaper
output), `transcript.jsonl` (the session), `meta.json` (dims + `outcome`). Read
`meta.json` first: the `outcome` field already classifies the run. See
[agent-observability.md](agent-observability.md).
For lifecycle markers, see [container lifecycle logs](container-lifecycle-logs.md).

## By symptom

- **Run launched, then nothing happened** - the container idled or exited with no
  work. Almost always the seeded credential could not authenticate in-container.
  The pre-launch smoke test aborts loudly with `auth smoke test: claude -p rejected
  the credentials` or `... did not respond within 90s`. **Fix:** refresh the host
  login - re-run `claude` on the host - and relaunch. Confirm in
  `~/.ward/agent-logs/<container>/console.log`. See [agent-credentials.md](agent-credentials.md).

- **Run never launched, no container appeared** - the interactive pre-flight
  returned **NO-GO** and blocked dispatch. It does not fail silently: ward **posts a
  comment on the issue** with the reason and how to re-dispatch. **Fix:** read that
  comment, address it, then re-dispatch (a comment answering the concern clears the
  gate; `--no-preflight` fires blind). See [agent-preflight.md](agent-preflight.md).

- **`refusing untrusted owner "<x>"`** - the trust gate declined the ref. This build
  dispatches only for its compiled-in primary orgs. **Fix:** dispatch against a
  trusted owner. See [agent-trust-gate.md](agent-trust-gate.md).

- **`resolve issue <ref>: ... get issue <ref>: exit status 3`** - the forge rejected
  or could not answer the pre-container issue read. ward now **retries a transient
  blip** (a 5xx, an unreachable API, a timeout) up to three times with a backoff,
  logging a `retrying in ...` note between tries, so a passing forge hiccup no longer
  fails a whole dispatch. A **permanent 4xx** skips the retry and surfaces its cause
  at once: `-> 403 Forbidden` means the host forge token cannot see that repo (a
  visibility/trust gap, not a bug), `-> 404 Not Found` means the issue is gone. **Fix:**
  for a 403, grant the host forge token access to the repo (or dispatch a repo it can
  read); for a 404, check the ref. The folded envelope after `exit status 3:` names
  which ([ward#497](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/497),
  [ward#596](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/596)).

- **`already reserved remotely` / `already reserved locally`** - another container
  holds this issue (1h TTL from the smart-defaults bundle). **Fix:** wait for it to
  finish, or pass `--force` to override/reclaim. See [agent-reservation.md](agent-reservation.md).

- **`ward exec` refused - `repo verb gated on a clean, synced tree`** - the audit row
  must be reconstructable from committed history, so the gate declines when the
  declaring `ward.yaml` is dirty or HEAD has `no synced upstream`. **Fix:** commit and
  push (or set upstream), then retry. Genuine emergency:
  `ward --audit-override-dirty exec <verb>` (stamps `audit_override=true`). See
  [exec-verb.md](exec-verb.md).

- **`... contains a shell metacharacter`** - the argv policy declined a token. **Fix:**
  drop the metacharacter (`;`, `|`, `&`, backticks, ...) from the argument. See
  [exec-verb.md](exec-verb.md).

- **Container ran for a long time and appears stuck** - a headless claude whose
  credential silently blocks can look busy forever. The smoke test exists to abort
  that case up front; if you deliberately bypassed it with `WARD_SMOKE_TEST_SKIP=1`,
  re-enable it. Whatever the exit, the [reaper](container-reap.md) backstops the work.

- **The run finished but nothing landed on `main`** - the reaper could not push
  cleanly (conflict, scan finding, or dead PAT), so it **preserved your work on a
  `ward-salvage/<id>` branch** and posted a salvage notice with recovery commands:
  on the carried issue if the run had one (reopening it), else a standalone
  `[ward-salvage]` issue. **Fix:** follow it. See [reap](container-reap.md).

A symptom-aware `ward agent doctor` verb is tracked in [ward#195](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/195).

## See also

- [../README.md](../README.md) - the intro that links here.
- [agent-observability.md](agent-observability.md) - the `~/.ward/agent-logs/` drain.
- [agent-preflight.md](agent-preflight.md) - the GO/NO-GO pre-flight.
- [container-reap.md](container-reap.md) - land-or-salvage on teardown.
- [doctor.md](doctor.md) - `ward doctor`, the allowlist + host-probe diagnostic.
