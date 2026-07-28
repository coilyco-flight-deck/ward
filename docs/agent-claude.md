# warded claude

The `claude` role is the cloud-subscription harness.

- It reads host credentials from `~/.claude`.
- It pre-trusts the target clone unless `TrustDirs` supplies a narrower set.
- It launches one-shot work with `claude -p`.

## See also

- [agent-harnesses.md](agent-harnesses.md) - the harness comparison.
- [agent-lifecycle.md](agent-lifecycle.md) - launch and preflight.
- [agent-roster.md](agent-roster.md) - the generated roster entry.
