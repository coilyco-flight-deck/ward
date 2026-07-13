---
doc_goal: Describe the public demo image build that exercises a workspace plus substrate mount pair without baking personal repos into the image.
---
# demo image

`ward exec demo-image` builds the demo container image used for simple mount
demos.

- The image carries the `ward` binary plus a tiny shell walkthrough.
- It defaults to a public OSS workspace and substrate pair.
- It does not bake any personal repo names into the image.
- The run accepts `WARD_DEMO_REPO`, `WARD_DEMO_ORG`,
  `WARD_DEMO_WORKSPACE`, and `WARD_DEMO_SUBSTRATE`.

## Defaults

- Workspace repo - `cli/cli`
- Substrate org - `cli`
- Workspace mount - `/workspace/cli`
- Substrate mount - `/substrate/cli`

## Build

```sh
ward exec demo-image
```

That runs the `make demo-image` target, which builds
`ward-demo:dev` from [docker/demo/Dockerfile](../docker/demo/Dockerfile).

## Run

```sh
docker run --rm -it \
  -v "$HOME/src/cli:/workspace/cli:ro" \
  -v "$HOME/src/cli-substrate:/substrate/cli:ro" \
  ward-demo:dev
```

The image prints a small read-only tour:

- `ward version`
- `git status --short --branch` for the mounted workspace
- the first lines of the workspace `README.md`
- a directory listing of the mounted substrate

## See also

- [container-substrate.md](container-substrate.md) - the `/substrate` contract.
- [exec-verb.md](exec-verb.md) - `ward exec` and repo verbs.
