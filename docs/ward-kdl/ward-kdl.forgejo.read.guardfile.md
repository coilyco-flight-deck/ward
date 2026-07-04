# ward-kdl-read ops forgejo

Spec-driven CLI. Every verb issues an HTTP request against the API base https://forgejo.coilysiren.me/api/v1.

Authenticates with the "Authorization" header (scheme header-token), reading the token from ssm /forgejo/coilyco-ops/api-token. The token value is never shown.

## ward-kdl-read ops forgejo repo get

`GET /repos/{owner}/{repo}`

Authorized by grant: can get repo. Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-read ops forgejo repo search - cross-repo repo finder (GET /repos/search)

`GET /repos/search`

Authorized by grant: can search repo. Not destructive.

Options (17):

- `--q` (string, optional): keyword
- `--topic` (boolean, optional): Limit search to repositories with keyword as topic
- `--includeDesc` (boolean, optional): include search of keyword within repository description
- `--uid` (integer, optional): search only for repos that the user with the given id owns or contributes to
- `--priority_owner_id` (integer, optional): repo owner to prioritize in the results
- `--team_id` (integer, optional): search only for repos that belong to the given team id
- `--starredBy` (integer, optional): search only for repos that the user with the given id has starred
- `--private` (boolean, optional): include private repositories this user has access to (defaults to true)
- `--is_private` (boolean, optional): show only public, private or all repositories (defaults to all)
- `--template` (boolean, optional): include template repositories this user has access to (defaults to true)
- `--archived` (boolean, optional): show only archived, non-archived or all repositories (defaults to all)
- `--mode` (string, optional): type of repository to search for. Supported values are "fork", "source", "mirror" and "collaborative"
- `--exclusive` (boolean, optional): if `uid` is given, search only for repos that the user owns
- `--sort` (string, optional): sort repos by attribute. Supported values are "alpha", "created", "updated", "size", "git_size", "lfs_size", "stars", "forks" and "id". Default is "alpha"
- `--order` (string, optional): sort order, either "asc" (ascending) or "desc" (descending). Default is "asc", ignored if "sort" is not specified.
- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo org get

`GET /orgs/{org}`

Authorized by grant: can get org. Not destructive.

Positional arguments (1):

- `<org>` (string)

## ward-kdl-read ops forgejo org list

`GET /orgs`

Authorized by grant: can list org. Not destructive.

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo org-repo list - list the repos an org owns (GET /orgs/{org}/repos). The route survey (ward#92) catalogs each primary org's repos through this leaf; `user-repo list` covers the one primary owner that is a user, not an org.

`GET /orgs/{org}/repos`

Authorized by grant: can list org-repo. Not destructive.

Positional arguments (1):

- `<org>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo user-repo list - list the repos a user owns (GET /users/{username}/repos). coilysiren is a user, not an org, so the route survey reaches its repos here rather than through `org-repo list`. Read-only, the survey's other half.

`GET /users/{username}/repos`

Authorized by grant: can list user-repo. Not destructive.

Positional arguments (1):

- `<username>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo user search - user finder (GET /users/search)

`GET /users/search`

Authorized by grant: can search user. Not destructive.

Options (5):

- `--q` (string, optional): keyword
- `--uid` (integer, optional): ID of the user to search for
- `--sort` (string, optional): sort order of results
- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo topic search - topic finder (GET /topics/search)

`GET /topics/search`

Authorized by grant: can search topic. Not destructive.

Options (3):

- `--q` (string, required): keyword to search for
- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo org-label get

`GET /orgs/{org}/labels/{id}`

Authorized by grant: can get org-label. Not destructive.

Positional arguments (2):

- `<org>` (string)
- `<id>` (string)

## ward-kdl-read ops forgejo org-label list

`GET /orgs/{org}/labels`

Authorized by grant: can list org-label. Not destructive.

Positional arguments (1):

- `<org>` (string)

Options (3):

- `--sort` (string, optional): Specifies the sorting method: mostissues, leastissues, or reversealphabetically.
- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo org-member list - org member list (GET /orgs/{org}/members)

`GET /orgs/{org}/members`

Authorized by grant: can list org-member. Not destructive.

Positional arguments (1):

- `<org>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo milestone get

`GET /repos/{owner}/{repo}/milestones/{id}`

Authorized by grant: can get milestone. Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<id>` (string)

## ward-kdl-read ops forgejo milestone list

`GET /repos/{owner}/{repo}/milestones`

Authorized by grant: can list milestone. Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (4):

- `--state` (string, optional): Milestone state, Recognized values are open, closed and all. Defaults to "open"
- `--name` (string, optional): filter by milestone name
- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo issue get

`GET /repos/{owner}/{repo}/issues/{index}`

Authorized by grant: can get issue. Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

## ward-kdl-read ops forgejo issue list

`GET /repos/{owner}/{repo}/issues`

Authorized by grant: can list issue. Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (13):

- `--state` (string, optional): whether issue is open or closed
- `--labels` (string, optional): comma separated list of labels. Fetch only issues that have any of this labels. Non existent labels are discarded
- `--q` (string, optional): search string
- `--type` (string, optional): filter by type (issues / pulls) if set
- `--milestones` (string, optional): comma separated list of milestone names or ids. It uses names and fall back to ids. Fetch only issues that have any of this milestones. Non existent milestones are discarded
- `--since` (string, optional): Only show items updated after the given time. This is a timestamp in RFC 3339 format
- `--before` (string, optional): Only show items updated before the given time. This is a timestamp in RFC 3339 format
- `--created_by` (string, optional): Only show items which were created by the given user
- `--assigned_by` (string, optional): Only show items for which the given user is assigned
- `--mentioned_by` (string, optional): Only show items in which the given user was mentioned
- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results
- `--sort` (string, optional): Type of sort

## ward-kdl-read ops forgejo issue search

`GET /repos/issues/search`

Authorized by grant: can search issue. Not destructive.

Options (18):

- `--state` (string, optional): State of the issue
- `--labels` (string, optional): Comma-separated list of label names. Fetch only issues that have any of these labels. Non existent labels are discarded.
- `--milestones` (string, optional): Comma-separated list of milestone names. Fetch only issues that have any of these milestones. Non existent milestones are discarded.
- `--q` (string, optional): Search string
- `--priority_repo_id` (integer, optional): Repository ID to prioritize in the results
- `--type` (string, optional): Filter by issue type
- `--since` (string, optional): Only show issues updated after the given time (RFC 3339 format)
- `--before` (string, optional): Only show issues updated before the given time (RFC 3339 format)
- `--assigned` (boolean, optional): Filter issues or pulls assigned to the authenticated user
- `--created` (boolean, optional): Filter issues or pulls created by the authenticated user
- `--mentioned` (boolean, optional): Filter issues or pulls mentioning the authenticated user
- `--review_requested` (boolean, optional): Filter pull requests where the authenticated user's review was requested
- `--reviewed` (boolean, optional): Filter pull requests reviewed by the authenticated user
- `--owner` (string, optional): Filter by repository owner
- `--team` (string, optional): Filter by team (requires organization owner parameter)
- `--page` (integer, optional): Page number of results to return (1-based)
- `--limit` (integer, optional): Number of items per page
- `--sort` (string, optional): Type of sort

## ward-kdl-read ops forgejo issue-comment list - the comments sub-collection GET, pinned by op (the convention resolves `list issue-comment` against issueGetComments poorly). The `view issue` shadow action below chains this so a view returns the thread.

`GET /repos/{owner}/{repo}/issues/{index}/comments`

Authorized by grant: can list issue-comment. Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

Options (2):

- `--since` (string, optional): if provided, only comments updated since the specified time are returned.
- `--before` (string, optional): if provided, only comments updated before the provided time are returned.

## ward-kdl-read ops forgejo commit list - commit list (GET /repos/{owner}/{repo}/commits). op pinned because the operationId is repoGetAllCommits - a `get`-shaped id the `list commit` convention does not reach.

`GET /repos/{owner}/{repo}/commits`

Authorized by grant: can list commit. Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (8):

- `--sha` (string, optional): SHA or branch to start listing commits from (usually 'master')
- `--path` (string, optional): filepath of a file/dir
- `--stat` (boolean, optional): include diff stats for every commit (disable for speedup, default 'true')
- `--verification` (boolean, optional): include verification for every commit (disable for speedup, default 'true')
- `--files` (boolean, optional): include a list of affected files for every commit (disable for speedup, default 'true')
- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results (ignored if used with 'path')
- `--not` (string, optional): commits that match the given specifier will not be listed.

## ward-kdl-read ops forgejo branch list - branch list (GET /repos/{owner}/{repo}/branches). op pinned because a bare `list branch` otherwise resolves to repoListBranchProtection (GET .../branch_protections), the shallower branch-prefixed match.

`GET /repos/{owner}/{repo}/branches`

Authorized by grant: can list branch. Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo tag list - tag list (GET /repos/{owner}/{repo}/tags)

`GET /repos/{owner}/{repo}/tags`

Authorized by grant: can list tag. Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results, default maximum page size is 50

## ward-kdl-read ops forgejo release get

`GET /repos/{owner}/{repo}/releases/{id}`

Authorized by grant: can get release. Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<id>` (string)

## ward-kdl-read ops forgejo release list

`GET /repos/{owner}/{repo}/releases`

Authorized by grant: can list release. Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (5):

- `--draft` (boolean, optional): filter (exclude / include) drafts, if you dont have repo write access none will show
- `--pre-release` (boolean, optional): filter (exclude / include) pre-releases
- `--q` (string, optional): Search string
- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo issue-label list

`GET /repos/{owner}/{repo}/issues/{index}/labels`

Authorized by grant: can list issue-label. Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

## ward-kdl-read ops forgejo tasks list

`GET /repos/{owner}/{repo}/actions/tasks`

Authorized by grant: can list tasks. Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results, default maximum page size is 50

## ward-kdl-read ops forgejo issue list-all - List all issues by auto-paginating issue list.

Shadows the generated `issue list-all` leaf: invoking it runs this composite in the leaf's place.

Complex action. Collects every page from `GET /repos/{owner}/{repo}/issues`, incrementing `page` and appending array responses until a page returns fewer than `50` item(s).

Authorized by grant: can list issue.

## ward-kdl-read ops forgejo issue view - View an issue with its full comment thread (issue + comments). Shadows the generated `issue view` (same 3-arg signature) so a view never misses the comments - the ward#170 failure mode where an agent reads the body and skips the thread. ward#225: ward's CLI renders this through a lean projection that collapses every commenter to its login literal, so a multi-comment issue no longer repeats each commenter's full profile once per comment; the guardfile shape is unchanged.

Shadows the generated `issue view` leaf: invoking it runs this composite in the leaf's place.

Complex action. Runs 2 granted calls in order, threading $step.field data between them:

1. `GET /repos/{owner}/{repo}/issues/{index}` - binds the response as `issue`
2. `GET /repos/{owner}/{repo}/issues/{index}/comments` - binds the response as `comments`

## Condition language

The `until` and `fail-when` expressions above are [JMESPath, Community Edition](https://jmespath.site), evaluated against the polled response as the root. A `$name` is a bound input or `as` capture, supplied through the Community Edition's variable scope - baseline JMESPath (https://jmespath.org) has no `$variable` syntax, so these expressions are not portable to an original-spec evaluator.

## Scope restrictions

Every verb whose path carries one of these parameters must supply a value matching a glob below, or it fails closed.

- `owner` must match: coily*

## Denied operations

### ward-kdl-read ops forgejo repo fork (denied)

forking is a human-only operation; fork in the web UI

### ward-kdl-read ops forgejo repo archive (denied)

archive/unarchive flips a repo's lifecycle; do it in the web UI

### ward-kdl-read ops forgejo repo unarchive (denied)

archive/unarchive flips a repo's lifecycle; do it in the web UI

### ward-kdl-read ops forgejo org create (denied)

org creation is a human-only operation

### ward-kdl-read ops forgejo org delete (denied)

org deletion is irreversible and human-only

### ward-kdl-read ops forgejo label create (denied)

repo-level label create is policy-disabled (ward#107): it mints labels that duplicate and shadow the org P0-P4 taxonomy. Create org labels with `create org-label`.

### ward-kdl-read ops forgejo label edit (denied)

repo-level label edit is policy-disabled (ward#107): edit the org taxonomy with `edit org-label`.

### ward-kdl-read ops forgejo issue delete (denied)

issue deletion is irreversible; close it instead (move-issue does this)

### ward-kdl-read ops forgejo pr view (denied)

pull requests are not exposed through ward; read them in the web UI

### ward-kdl-read ops forgejo pr list (denied)

pull requests are not exposed through ward; read them in the web UI

## See also

- [ward-kdl.md](../ward-kdl.md) - the build-time authoring layer behind this surface
- [ward-kdl-surface.md](../ward-kdl-surface.md) - the full generated verb surface, area by area
