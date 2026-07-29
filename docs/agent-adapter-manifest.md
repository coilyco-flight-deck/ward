---
doc_goal: Describe Ward's typed harness adapter boundary.
---
# Agent adapters

Ward's harness adapters are typed product code.

- Each adapter records binary, context level, auth, stream, and argv shape.
- The typed roster is the source of truth for how a harness is invoked.
- Model, endpoint, reasoning, and identity are explicit harness-owner inputs.
- A workflow role never changes an adapter.

## See also

- [agent-harnesses.md](agent-harnesses.md) - the harness comparison.
- [agent-roles.md](agent-roles.md) - the role semantics.
