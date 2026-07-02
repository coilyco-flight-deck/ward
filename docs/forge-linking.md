# Which forge a doc link points at (ward#443)

ward is **Forgejo-canonical** with a **read-only GitHub mirror** as the public
front door ([github-mirror.md](github-mirror.md)). The same repo is therefore
Forgejo for the maintainer and GitHub for everyone else, so a forge link is easy
to get backwards. This is the rule to check any doc link against, in author
priority order.

## The three cases

1. **Same-repo file -> relative link.** A link to another tracked file
   (`[CONTRIBUTING.md](../CONTRIBUTING.md)`, `[agent.md](agent.md)`) resolves on
   **whichever** forge renders the page - Forgejo for the canonical repo, GitHub
   for the mirror - so one link is correct for both audiences at once. Prefer it
   over any absolute forge URL whenever the target lives in this repo. Most
   cross-links fall here, and this case is what dissolves most of the
   inconsistency: a relative link never has to pick a forge.

2. **External-contributor navigation -> GitHub.** Where a public reader files an
   issue, opens a PR, or lands on the project front door, link the **GitHub**
   mirror (`github.com/coilyco-flight-deck/ward`, `/issues/new/choose`). Forgejo
   registration is closed, so a GitHub link is the only one an outside
   contributor can act on. This covers the README's support/contribute copy,
   the issue templates, and the PR template.

3. **Canonical or infrastructural reference -> Forgejo.** Things that only exist
   on Forgejo - the brew tap URL, the container registry, the canonical releases
   page, a specific `ward#N` issue cross-reference, a `closes #N` link - point at
   Forgejo, because that is literally where they live. A GitHub link here would
   404 or misdirect.

## By surface

- **Public-facing** (`README.md`, `SECURITY.md`, issue templates, PR template)
  keeps Forgejo to the canonical-fact case 3 and sends every **navigational**
  link to GitHub under case 2.
- **Contributor-facing** (`CONTRIBUTING.md`, `AGENTS.md`, `docs/`, source) names
  Forgejo freely under case 3, since its readers work on the canonical repo.

Either way, reach for a relative link first (case 1) and only fall to an
absolute forge URL when the target is not a tracked file in this repo.

## See also

- [github-mirror.md](github-mirror.md) - what the GitHub mirror syncs, and why.
- [../CONTRIBUTING.md](../CONTRIBUTING.md) - the two-forge contributor flow.
- [../AGENTS.md](../AGENTS.md) - carries this rule as an agent authoring rule.
