# GitHub token path

When `warded` is pointed at a GitHub issue URL, GitHub-side comments and pull
request creation use a user-supplied `GITHUB_TOKEN` from the private env-file.
That token stays out of argv/audit, and it needs enough repo scope to read the
issue thread, post comments, and open a PR in the target repo.

Use this path when GitHub is the public front door and Forgejo is the canonical
mirror behind it.
