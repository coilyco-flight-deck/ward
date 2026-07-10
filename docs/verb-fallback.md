---
doc_goal: Describe the unknown-verb rewrite in one place so the command surface stays predictable when a caller skips the explicit `ward exec` prefix.
---
# ward verb fallback

If `ward` sees an unknown verb, it rewrites the call toward the gated dev-verb
surface.

- That keeps bare repo verbs on the audited path.
- It avoids silently falling through to the shell.
- It makes `ward` act like a repo-aware command wrapper.

## See also

- [exec-verb.md](exec-verb.md) - the explicit gated path.
- [git-verbs.md](git-verbs.md) - the audited git surface.
