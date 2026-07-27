---
doc_goal: Explain the Ward to AOSguard ownership boundary and Ward's retained native policy.
---
# AOSguard boundary

Specgen and `aosguard` belong to AOS. Specgen compiles AOS operator policy into
the standalone `aosguard ops ...` command family and its generated agent skill.
The AOS image and package releases ship that binary.

Ward does not compile, embed, mount, or dispatch generated operator commands.
Ward therefore needs no `wardguard` variant. Its root command intentionally
omits `ops`, `aws`, `kubectl`, `docker`, and `pkg`.

Ward retains typed native role and launch policy directly under
[`.ward/`](../.ward/). Those files select agent behavior and
container capabilities such as AWS credential delivery and tailnet attachment.
They do not generate a command tree.

Ward's hand-written `agent`, `container`, `exec`, `git`, reservation, PR
workflow, and tracker adapters stay native. These paths do not call AOSguard or
load its specifications.

## See also

- [AOSguard](https://github.com/coilyco-flight-deck/agentic-os/blob/main/docs/aosguard.md) - the owning operator documentation.
- [native policy assets](../.ward/README.md) - Ward's retained inputs.
- [architecture.md](architecture.md) - the full Ward ownership split.
