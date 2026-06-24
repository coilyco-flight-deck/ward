# ward vs NVIDIA OpenShell

Written before someone else frames it. NVIDIA OpenShell is the closest analog to
ward in the wild, and the comparison is flattering to both: its existence
validates the category - policy-first runtimes for agents that hold real
credentials - and the way it is built leaves ward's niche wide open.

Both are **policy-first drivers**: the agent does not get a raw shell, it gets a
bounded surface, and the boundary is declared up front. They diverge on **what
the boundary is made of** and **what it is for**.

## The two shapes

- **ward** - a lightweight single Go binary. The boundary is **verb-level**:
  cli-guard compiles an OpenAPI spec plus a KDL guardfile into a scoped,
  audited CLI, so the agent can call `get` but never `delete`, and every call
  writes one append-only audit row. Cloud-agnostic ops tasks, FOSS, terminal-first.
- **OpenShell** (NVIDIA, GTC 2026, ~7.2k stars, Rust, Apache-2.0, alpha) - a
  K3s-in-a-container runtime. The boundary is the **kernel**: Landlock for
  filesystem reach, Seccomp for syscalls, OPA/Rego for policy, layered
  defense-in-depth. Declarative YAML policy, aimed at long-running coding agents,
  NVIDIA-hardware-flavored.

## Where the line falls

- **Enforcement layer** - ward gates at the **verb** (what API call is even
  expressible); OpenShell gates at the **kernel** (what a process can touch once
  running). Verb-level denial is legible in an audit row. Kernel-level denial is
  stronger against a process that has already escaped its intended path.
- **Weight** - ward is one binary you `brew install` and run in a terminal.
  OpenShell boots a K3s cluster inside a Docker container. That buys orchestration
  (pod security, network policy) at the cost of a heavyweight host.
- **Target** - ward bounds **cloud-agnostic ops tasks**: an agent driving real
  infrastructure verbs (forgejo, aws, kubectl) through a typed least-privilege
  surface. OpenShell bounds **long-running coding agents** inside a sandboxed dev
  environment.
- **Reach** - ward is cloud-agnostic and runs anywhere Go runs. OpenShell leans
  on a Linux host (kernel >=5.13 for Landlock) and an NVIDIA-flavored stack.

## Why both can be right

The category is real and OpenShell proves it: agents that hold credentials need a
boundary, not a hope. Heavyweight kernel sandboxing is the right tool for a
long-lived coding agent you hand a whole machine. It is the wrong tool for an
operator who wants one audited `aws` or `forgejo` call from a terminal without
standing up a cluster first. That lightweight, cloud-agnostic, verb-level ops
niche is the one ward fills, and OpenShell's heft leaves it open.

## The boundary is the product

ward's claim is not "it can do X" - it is "it will refuse Y, and prove it." Every
demo and one-liner shows a **denial**, not just a capability: the guardfile says
`never delete "*"`, and the binary cannot express the delete at all. OpenShell
makes the same bet from the kernel side. The shared thesis is that for an agent
with real credentials, the part worth shipping is the part that says no.

## See also

- [README.md](../README.md) - what ward is.
- [docs/architecture.md](architecture.md) - the cli-guard / ward-kdl / ward layers.
- [docs/FEATURES.md](FEATURES.md) - the verb inventory.
