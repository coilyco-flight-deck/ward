---
doc_goal: Explain how the aws capability delivers creds - export the launching host's whole credential chain and inject it as short-lived AWS_* env, with a ~/.aws mount as the fallback - so an operator knows why a file-credential-less host still gets working creds and when the creds-less warning fires.
---
# ward agent: how the aws capability delivers creds ([ward#586](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/586))

The aws capability (per-role guardfile membership, [agent-capability.md](agent-capability.md))
delivers AWS credentials to a run. This doc covers the **mechanism**.

## Export-and-inject (the primary path)

The old mechanism **only** bind-mounted the host `~/.aws` into the run. That works only
when creds live in `~/.aws` **files**, and it forces the container to re-walk AWS's whole
credential chain (SSO login, profile/role resolution), which the operator will not
replicate in-container. A host whose creds come from env, SSO, an assumed role, or IMDS
got `NoCredentials` even though the host itself was authenticated.

So at launch, when the aws capability is on, ward now:

1. Runs `aws configure export-credentials --format process` on the **launching host**
   (AWS CLI v2). This resolves the **whole** chain - SSO, env, profile, assumed role,
   IMDS - to flat JSON `{AccessKeyId, SecretAccessKey, SessionToken, Expiration}`.
2. Injects those as `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` / `AWS_SESSION_TOKEN`
   (plus the host's resolved `AWS_DEFAULT_REGION` / `AWS_REGION`, since dropping the
   mount removes `~/.aws/config` as a region source) into `docker run` via the private
   `--env-file`, never argv - so the creds never land in `docker inspect`'s command.
3. **Drops the `~/.aws` mount** - the injected env supersedes it. The container gets
   working creds with **zero chain replication**, regardless of host auth source.

The creds are **short-lived** (session/SSO/assumed-role expiry), which is *more* aligned
with "no long-lived secret in agent hands" than mounting raw `~/.aws`. ward logs the
returned `Expiration` at launch. A very long-running container could outlive it; provision
runs are minutes, so re-export is not solved this pass.

This runs where the chain resolves - the **launching host** - so a **broker-forwarded**
engineer dispatch (the director-surface path) re-runs this launch step host-side, not in
the read-only surface container. The `aws` invocation and JSON parse are cross-platform,
so a **Windows** broker host resolves and injects the same way.

## The `~/.aws` mount fallback + the creds-less warning ([ward#579](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/579))

If `export-credentials` **errors** (AWS CLI v2 absent) or returns nothing, ward falls back
to the original mechanism: bind `~/.aws` read-only and lean on any files there. That mount
**forwards** the host identity, it mints none, so it only delivers creds when `~/.aws`
holds a `config` or `credentials` file. A credential-less host is the silent trap: docker
mounts a **missing** source as an **empty** dir, so `aws` / `ssm` calls fail
`NoCredentials` deep in a script.

ward makes that gap **loud**: only on the fallback path (export returned nothing) **and**
when `~/.aws` holds no creds either, ward warns to stderr (mirroring `--host-net`). It does
**not** hard-fail. This closes the [ward#579](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/579)
false-warning gap - a host with working env/SSO/role creds exports cleanly and is never
warned, because the real "no creds" signal is now an export failure, not an empty `~/.aws`.

## See also

- [agent-capability.md](agent-capability.md) - where the aws capability is configured (per-role guardfile set).
- [container.md](container.md) - the least-access model this keys into.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
