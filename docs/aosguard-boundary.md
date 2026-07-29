---
doc_goal: Explain the Ward to AOSguard ownership boundary after runtime policy bundles were removed.
---
# AOSguard boundary

Specgen and `aosguard` belong to AOS. Specgen compiles AOS operator policy into
the standalone `aosguard ops ...` command family and its generated agent skill.
The AOS image and package releases ship that binary.

Ward does not compile, embed, mount, or dispatch generated operator commands.
Ward therefore needs no `wardguard` variant. Its root command intentionally
omits generated operator leaves.

Ward retains typed harness mechanics and fixed workflow behavior in product
code. [`.ward/ward.yaml`](../.ward/ward.yaml) declares repository dev verbs and
supported repository launch preferences. It cannot grant container authority.

Ward's hand-written `agent`, `container`, `exec`, `git`, reservation, PR
workflow, and tracker adapters stay native. These paths do not call AOSguard or
load its specifications.

## See also

- [AOSguard](https://github.com/coilyco-flight-deck/agentic-os/blob/main/docs/aosguard.md) - the owning operator documentation.
- [repository configuration](../.ward/README.md) - Ward's repository inputs.
- [architecture.md](architecture.md) - the full Ward ownership split.
