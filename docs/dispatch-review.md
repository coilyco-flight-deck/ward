---
doc_goal: Keep the review gate on its own page so the workflow doc can point at a stable release-era reference.
---
# dispatch review

The review gate is the pre-PR or pre-merge check on a candidate diff.

- It runs only when enabled.
- It fails closed.
- It records a verdict, not just a yes or no.

## What it looks at

- the candidate diff.
- the current filesystem state.
- the selected review class.
- the harness family that is doing the review.

## Why it exists

The landing path needs one more guard than CI alone. Review keeps the worker
from claiming merge-ready when the human or policy signal is not actually
settled.

## Typical outputs

- approved.
- rejected.
- needs more information.
- reviewer error.

## See also

- [agent-workflow.md](agent-workflow.md) - the landing policy.
