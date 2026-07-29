---
doc_goal: Define Ward's bounded live-verification fixture mode so deployments can prove the director, engineer, and QA chain without granting general live mutation authority.
---
# Verification fixtures

`--verification-fixture` admits one explicitly configured disposable repository
and issue into a bounded director, engineer, and QA proof.

The mode is for verifying Ward's real hosted control path. It does not grant an
engineer or QA general live-operations authority.

## Admission

The invoking repository config owns the allowlist:

```yaml
agent:
  verification:
    fixtures:
      - repository: example/ward-qa-fixture
        issue-label: qa-fixture
```

Ward admits a run only when both conditions match:

* The exact target repository equals a configured `repository`.
* The exact target issue carries the configured `issue-label`.

No fixture entries means no live-verification target is admitted.

## Director and engineer proof

The director accepts one exact issue and carries the flag to the engineer:

```bash
ward agent director example/ward-qa-fixture#7 --burndown --verification-fixture --max-cycles 1
```

Ward disables startup triage, fixes the director pool at one engineer, and
refuses repository-wide or organization-wide fixture scope. The engineer gets
the deterministic `issue-7` branch and a forced `remote-branch-only` workflow.
Ward refuses extra writable repositories, custom branches, capacity overrides,
and pull request inputs in this mode.

The engineer may publish the disposable issue branch. The engineer cannot
merge, close the issue, or turn the proof into a general backlog run.

## QA proof

After the engineer publishes the issue branch, QA verifies that exact remote
branch and records its commit:

```bash
ward agent qa example/ward-qa-fixture#7 --verification-fixture
```

Ward resolves `issue-7`, checks out that branch in QA's fresh read-only
container, and stamps the reviewed commit in the verdict comment. A missing
branch or commit fails closed.

## Evidence and cleanup

Every admitted container receives:

* `WARD_VERIFICATION_FIXTURE=1`
* `ward.verification-fixture=true`

The workflow label remains `remote-branch-only`. The deployment that owns the
fixture also owns issue preparation, branch cleanup, and any operator-only
observation required after the run.

## See also

* [ward-yaml.md](ward-yaml.md) - repository configuration schema.
* [agent-director.md](agent-director.md) - the director surface.
* [agent-qa.md](agent-qa.md) - QA verdict behavior.
* [agent-workflow.md](agent-workflow.md) - landing modes.
