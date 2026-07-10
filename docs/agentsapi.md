# agentsapi

`internal/agentsapi` is the seam that keeps the agent registry portable.

- each harness lives behind the registry.
- the registry keeps the launch code from hard-coding per-agent branches.
- the `Agent` contract includes `Install`, so bootstrap can prove the harness
  is ready before launch or fail loudly.
- the docs page exists so the seam stays named.

## See also

- [agent.md](agent.md) - the entrypoint umbrella.
