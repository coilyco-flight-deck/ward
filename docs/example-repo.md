---
doc_goal: Show examples/toy/ as the smallest thing that is still a real ward-managed repo and the concrete anchor for demo and spec-bundle - the four ward-facing pieces (Makefile, .ward/ward.yaml with a required security block, guardfile, dev-base) and what each teaches - so a reader can copy it as a starting point and tell it apart from the sibling spec bundle.
---
# the toy example repo

`examples/toy/` is ward's minimal, self-contained example project ([ward#463](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/463),
formerly sketched as "seed"). It is the concrete anchor the demo ([ward#250](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/250) /
[#251](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/251)) and the minimal example spec bundle ([ward#453](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/453)) point at: the smallest
thing that is still a real ward-managed repo.

## What it is

A dependency-free POSIX-`sh` CLI (`greet`) with a real `build` / `test` / `vet`
/ `install` surface, so `ward exec <verb>` runs anywhere with no toolchain. The
point is not the program - it is the four ward-facing pieces around it:

- **Makefile** - self-documenting targets, each with a `## <help>` comment.
- **`.ward/ward.yaml`** - the allowlist: the build/test/install triple plus a
  `security:` block (required per [ward#450](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/450)).
- **`toy.guardfile.kdl`** - a minimal ward-kdl permission surface, deny-by-default.
- **dev-base** - the container image a `ward agent` run pulls (referenced, not baked).

## What each piece teaches

- **The verb gate** - `ward exec build` runs `make build` through cli-guard:
  argv validated, one audit row, clean+synced gate. See [exec-verb.md](exec-verb.md).
- **`ward doctor`** - cross-checks each `commands.<name>` against the Makefile
  (`run:` is `make <name>`, the `## help` text equals `description:`) and
  summarizes the `security:` block. See [doctor.md](doctor.md).
- **The security block** - `protected_binaries` (docker, deny-direct) and `sudo`.
  Field-by-field: [ward-yaml.md](ward-yaml.md).
- **The guardfile** - the exec-dialect shape a consumer authors before
  generating a gated CLI. See [ward-kdl-authoring.md](ward-kdl-authoring.md).
- **The dev-base image** - what `ward agent` clones the repo into. See
  [container-image.md](container-image.md).

## Using it

```sh
cd examples/toy
ward exec build
ward exec test
ward doctor
```

The guardfile and `.ward/ward.yaml` are complete enough to copy into a new repo
as a starting point, then edit down to that repo's real verbs and policy.

`examples/toy/` is the runnable *managed repo* an adopter's project looks like;
its sibling `examples/ward-specs/` ([ward#453](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/453)) is the *spec bundle* you build a
`ward` binary from. Different inputs, both deployment-agnostic anchors.

## See also

- [../examples/toy/README.md](../examples/toy/README.md) - the repo's own README.
- [../examples/ward-specs/README.md](../examples/ward-specs/README.md) - the sibling spec bundle (build input for `ward` itself).
- [ward-yaml.md](ward-yaml.md) - the `.ward/ward.yaml` schema the config demonstrates.
- [FEATURES.md](FEATURES.md) - inventory.
