# forgejo token audit

Ward keeps the raw `FORGEJO_TOKEN` read surface small and documented.

- the token is read in the resolver path.
- current Compose directors route the native Forgejo client through the
  sibling broker and never resolve the raw token in the director process.
- the surrounding docs explain where the credential is seeded and why.
- the audit keeps the write path from quietly growing new secret reads.
- engineer and QA launch resolution reads only `WARD_FORGEJO_GIT_TOKEN`,
  rejects a missing value, and rejects equality with broad `FORGEJO_TOKEN`.
- the Git-only value crosses only the secret launch env-file. It does not enter
  Ward's printable environment map, generated Compose YAML, argv, or logs.
- agent-role Forgejo reads use the broker. Agent-role mutations use typed broker
  operations with an authenticated role and record kind.

## See also

- [broker.md](broker.md) - the native broker path that uses the token.
