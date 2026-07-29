---
doc_goal: State that Ward roles do not grant runtime capability.
---
# Agent authority boundary

Ward role labels describe workflow. They do not grant runtime authority.

Credentials, mounts, network access, broker operations, merge behavior, and
container topology are fixed by their owning product paths. Context bundles and
role labels can only add descriptive context.

See [agent-roles.md](agent-roles.md) and
[container-contract.md](container-contract.md).
