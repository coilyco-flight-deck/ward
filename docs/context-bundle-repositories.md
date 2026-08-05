# Context-bundle repository references

Back to the [context-bundle contract](context-bundle.md).

For an ordinary repository-backed launch, Ward resolves the selected identities
under the host projects root. `$PROJECTS_ROOT` may select that root. Otherwise,
Ward derives it from the current checkout.

Every selected checkout must exist as a real directory at the exact
owner-qualified path. Missing paths, symlinks, and paths resolving outside the
projects root fail before Docker starts.

Validated repositories mount read-only at `/refs/<owner>/<repository>`. The
manifest supplies no host path and cannot request a writable mount. Ward's
baked, product-neutral `/substrate` remains a separate reference surface.

Repository-free collaboration peers validate and retain bundle metadata but do
not derive repository mounts. Their no-target authority boundary stays intact.
