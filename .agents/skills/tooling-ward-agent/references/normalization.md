# ward agent: ref normalization

Detail backing [`../SKILL.md`](../SKILL.md) steps 1-2. Surfaces are in [`surfaces.md`](surfaces.md).

## Parse rules

Recognize the shape `[ward ]agent <role>? <owner-tokens> <repo-tokens> [issue|number|ticket|hash|pound|#] <N>`, or the legacy `[coily ]dispatch <...>` shape.

**Filler to drop:** "the", "issue", "number", "ticket", "hash", "pound", "on", "for", "please". Also drop a repeated "coily" used as a dictation discourse marker rather than the actual repo name - e.g. "coily-siren coily-issue 125", the second "coily" is filler.

**Repo resolution:** lowercase the repo tokens, strip hyphens/spaces, fuzzy-match against the registry's repo column.

**Owner resolution:** the owner is **whatever org the matched registry row names** - repos span four orgs (`coilysiren`, `coilyco-bridge`, `coilyco-flight-deck`, `coilyco-gaming`), so there is no fixed default owner. A bare `#N` with no owner/repo tokens, run from inside a repo checkout, lets `ward agent` infer `owner/repo` from the cwd's git origin (ward#282). Refuse if a dictated owner names an org outside the four primary orgs - that is the trust boundary `ward agent` enforces.

## Known dictation collisions

Bake these in. Voice dictation produces them constantly:

* "coily" alone -> `coilyco-bridge/coily` (NOT `agentic-os-kai`). The bare word is the retiring ops CLI; its verbs fold into `ward ops`.
* "coily co ai" / "coily-co-ai" / "coilco ai" / "coily-coai" -> `coilyco-bridge/agentic-os-kai`
* "ward" -> `coilyco-flight-deck/ward`
* "eco mods" -> `coilyco-bridge/eco-mods` (private superset; public subset is `coilyco-flight-deck/eco-mods-public`)
* "galaxy gen" -> `coilyco-flight-deck/galaxy-gen`
* "sirens discord" / "discord ops" -> `coilyco-bridge/sirens-discord-ops`
* "infra" / "infrastructure" -> `coilyco-flight-deck/infrastructure`
* "website" / "the site" -> `coilysiren/website`
* "tap" / "homebrew tap" -> `coilyco-flight-deck/homebrew-tap`
