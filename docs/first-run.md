---
doc_goal: Take a new operator from installation through one non-mutating, verifiable agent launch preview.
---
# First run

The first run should prove configuration, target resolution, harness
selection, workflow selection, and container planning without launching work.

## Prerequisites

* Install `ward` and confirm `ward --version` works.
* Start Docker and confirm its server is reachable.
* Authenticate the selected harness on the host.
* Choose an open issue in a trusted repository.

## Configure and validate

1. Run `ward setup`. It creates `~/.ward/config.yaml` only when missing and
   reports Docker readiness.
2. Replace any `example-owner/example-repo` director scope in that file.
3. Run `ward doctor`. Resolve every `FAIL` before continuing.
4. From the target checkout, run:

   ```bash
   warded engineer owner/repo#123 --print
   ```

   Replace the example ref with the chosen open issue. A shell-safe
   `owner/repo#N` ref avoids treating `#N` as a shell comment.

## Verify the preview

The first line confirms exact resolution:

```text
ward agent: resolved issue ref owner/repo#123 -> owner/repo#123
```

The rendered plan then includes the title, repository, branch, workflow,
container name, workspace mounts, correlation fields, image, and would-be
Docker command. Its heading names the `engineer` role and selected harness.

`--print` may read local configuration, the issue and its comments, Docker
state, and launch inputs. It may refresh Ward's disposable local launch-asset
staging. It does not edit the issue or comments, reserve work, change the
checkout, create or start a container, start a harness, push a branch, or open
or merge a pull request. A refusal names the failing trust, target, config,
auth, or Docker check.

After the preview is correct, run the same command without `--print` to start
the detached engineer.

## See also

* [doctor.md](doctor.md) - checks and remedies.
* [agent-lifecycle.md](agent-lifecycle.md) - full launch sequence.
* [agent-harnesses.md](agent-harnesses.md) - host authentication sources.
