# forgejo token audit

Ward keeps the raw `FORGEJO_TOKEN` read surface small and documented.

- the token is read in the resolver path.
- the surrounding docs explain where the credential is seeded and why.
- the audit keeps the write path from quietly growing new secret reads.

## See also

- [ops-forgejo.md](ops-forgejo.md) - the operator surface that uses the token.
