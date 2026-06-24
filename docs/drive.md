# ward drive / warded - the flag boundary

`ward drive <harness> [args...]` runs a headless harness (gptme, goose, ...)
behind ward's policy and audit boundary. Its public face is **`warded`** - a
thin argv shim so the one-liner reads like `sudo`, `timeout`, `nice`,
`firejail`:

```
warded --policy=strict gptme "deploy the thing"
```

An SRE parses that as "firejail for agents" in one token, and it instantiates
the "warded agent" noun. `warded` is a multicall name for the same binary, not
a second code path: invoked as `warded`, ward rewrites its own argv to
`ward drive ...` before anything else runs (ward#247).

## The boundary

The hard part is one line of grammar:

```
warded [ward flags] <harness> [harness flags...]
```

- **The first bare (non-flag) token after `drive` is the harness.** Everything
  before it is a ward flag; everything after it is the harness's own argv,
  passed through verbatim.
- **`--` forces passthrough.** `warded gptme -- --non-interactive "..."` hands
  `--non-interactive` to gptme even though it sits right after the harness. A
  single `--` immediately after the harness is the marker and is stripped; any
  later `--` is the harness's own and survives.

So in `warded --policy=strict gptme --non-interactive "deploy"`:

- `--policy=strict` is **ward's** (the policy profile the harness runs under),
- `gptme` is the harness,
- `--non-interactive "deploy"` is **gptme's**, untouched.

## Why ward owns the split (ward#248)

urfave/cli cannot express "parse my flags, then stop at the first positional and
hand the rest through raw." By default it keeps parsing flags past the harness
token and rejects the harness's own flags as undefined - `--non-interactive`
comes back as `flag provided but not defined`. So the `drive` command sets
`SkipFlagParsing: true`: cli hands ward every token after `drive` untouched, and
ward's own `splitDriveArgs` draws the boundary. That is what the issue means by
"skip-flag-parsing past the harness arg."

A flag that looks like ward's but is not - `warded --bogus gptme` - is a
boundary error, not a silent passthrough. The message names the boundary,
because the boundary is the product: harness flags belong after the harness name
(or after `--`), ward flags before it.

## Today's behavior

This milestone ships the surface and the boundary parsing. `ward drive` resolves
the invocation and prints the parsed plan:

```
$ warded --policy=strict gptme --non-interactive "deploy the thing"
ward drive (parsed plan):
  policy:  strict
  harness: gptme
  argv:    gptme --non-interactive "deploy the thing"
  (guarded execution is not yet wired; this prints the parsed invocation)
```

Guarded container execution of the resolved harness - the part that makes a
denial show up on screen - lands in ward#249.

## See also

- [docs/architecture.md](architecture.md) - ward as the run-time product
  (`warded`) over cli-guard and ward-kdl.
- [docs/FEATURES.md](FEATURES.md) - the command inventory.
