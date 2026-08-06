# ward agent: ref normalization

Detail backing [`../SKILL.md`](../SKILL.md) steps 1-2. Surfaces are in [`surfaces.md`](surfaces.md).

## Parse rules

Recognize the shape `[ward ]agent <role>? <owner-tokens> <repo-tokens> [issue|number|ticket|hash|pound|#] <N>`, or the legacy `[coily ]dispatch <...>` shape.

**Filler to drop:** "the", "issue", "number", "ticket", "hash", "pound", "on", "for", "please". Also drop a repeated "coily" used as a dictation discourse marker rather than the actual repo name - e.g. "coily-siren coily-issue 125", the second "coily" is filler.

**Repo resolution:** lowercase the repo tokens, strip hyphens/spaces, and use
an exact known collision below. Otherwise require an explicit `owner/repo`, an
issue URL, or the current checkout's origin. Do not query an external inventory
or guess.

**Owner resolution:** the owner is **whatever the explicit ref, checkout origin,
or collision entry names**. There is no fixed default owner. A bare `#N` with
no owner/repo tokens, run from inside a repo checkout, lets `ward agent` infer
`owner/repo` from the cwd's git origin (ward#282). Refuse owners outside Ward's
configured trust boundary.

## Known dictation collisions

Bake these in. Voice dictation produces them constantly:

* "ward" -> `coilyco-flight-deck/ward`
* "galaxy gen" -> `coilyco-flight-deck/galaxy-gen`
* "infra" / "infrastructure" -> `coilyco-flight-deck/infrastructure`
* "website" / "the site" -> `coilysiren/website`
* "tap" / "homebrew tap" -> `coilyco-flight-deck/homebrew-tap`
