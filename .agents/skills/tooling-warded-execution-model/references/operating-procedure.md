# Ward execution-model operating procedure

Use this procedure before filing or changing config, hooks, doctrine,
permissions, lifecycle, recovery, or rollout work.

1. Name the actor and surface. Distinguish the operator's host harness, Ward's
   host launcher or broker, and the agent inside an ephemeral container.
2. Read the owning product contract. Start with architecture, then use the
   lifecycle, broker, container, config, observability, or recovery page linked
   from the parent skill. Confirm the current source file named by that contract.
3. Choose the owning layer:
   * Ward Go code owns what a Ward container reads, mounts, may invoke, records,
     drains, recovers, or receives through the broker.
   * Host automation owns persistent operator-harness files, host hooks, and
     host permissions outside Ward.
   * Repository `.ward/ward.yaml` owns declared commands, repository security,
     catalog dependencies, and repository launch preferences.
   * Operator `~/.ward/config.yaml` owns per-host Ward preferences.
4. Choose the artifact:
   * Human-supported configuration, observation, maintenance, or recovery goes
     in one canonical human contract.
   * Agent-only decision routing and procedure stays in this skill tree.
   * Mandatory contributor doctrine stays in `AGENTS.md`.
   * Immediate command syntax and refusal guidance stays in CLI help/errors.
   * Silent mechanics stay in code, comments, and tests.
5. Implement at the lowest layer that fully determines the behavior. Never
   fetch runtime config downward from a higher reference or deployment repo.
6. Update existing canonical links instead of adding an anchor or forwarding
   page. Validate docs, skill links, generated references, and repository tests.

When the question is only how to operate a run, return the narrow human
contract and supported Ward command. Do not expose raw credential paths,
transient harness state, or internal caches as an alternative procedure.
