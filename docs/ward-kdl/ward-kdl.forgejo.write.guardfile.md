# ward-kdl-write ops forgejo

Spec-driven CLI. Every verb issues an HTTP request against the API base https://forgejo.coilysiren.me/api/v1.

Authenticates with the "Authorization" header (scheme header-token), reading the token from ssm /forgejo/coilyco-ops/api-token. The token value is never shown.

## ward-kdl-write ops forgejo activitie get

`GET /activitypub/user-id/{user-id}/activities/{activity-id}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (2):

- `<user-id>` (string)
- `<activity-id>` (string)

## ward-kdl-write ops forgejo archive get

`GET /repos/{owner}/{repo}/archive/{archive}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<archive>` (string)

## ward-kdl-write ops forgejo blob get

`GET /repos/{owner}/{repo}/git/blobs/{sha}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<sha>` (string)

## ward-kdl-write ops forgejo branch_protection get

`GET /repos/{owner}/{repo}/branch_protections/{name}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<name>` (string)

## ward-kdl-write ops forgejo branche get

`GET /repos/{owner}/{repo}/branches/{branch}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<branch>` (string)

## ward-kdl-write ops forgejo collaborator get

`GET /repos/{owner}/{repo}/collaborators/{collaborator}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<collaborator>` (string)

## ward-kdl-write ops forgejo comment get

`GET /repos/{owner}/{repo}/issues/comments/{id}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<id>` (string)

## ward-kdl-write ops forgejo compare get

`GET /repos/{owner}/{repo}/compare/{basehead}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<basehead>` (string)

## ward-kdl-write ops forgejo content get

`GET /repos/{owner}/{repo}/contents/{filepath}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<filepath>` (string)

Options (1):

- `--ref` (string, optional): The name of the commit/branch/tag. Default the repository’s default branch (usually master)

## ward-kdl-write ops forgejo editorconfig get

`GET /repos/{owner}/{repo}/editorconfig/{filepath}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<filepath>` (string)

Options (1):

- `--ref` (string, optional): The name of the commit/branch/tag. Default the repository’s default branch (usually master)

## ward-kdl-write ops forgejo flag get

`GET /repos/{owner}/{repo}/flags/{flag}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<flag>` (string)

## ward-kdl-write ops forgejo following get

`GET /user/following/{username}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (1):

- `<username>` (string)

## ward-kdl-write ops forgejo git get

`GET /repos/{owner}/{repo}/hooks/git/{id}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<id>` (string)

## ward-kdl-write ops forgejo gpg_key get

`GET /user/gpg_keys/{id}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (1):

- `<id>` (string)

## ward-kdl-write ops forgejo group get

`GET /admin/quota/groups/{quotagroup}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (1):

- `<quotagroup>` (string)

## ward-kdl-write ops forgejo issue get

`GET /repos/{owner}/{repo}/issues/{index}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

## ward-kdl-write ops forgejo key get

`GET /user/keys/{id}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (1):

- `<id>` (string)

## ward-kdl-write ops forgejo label get

`GET /orgs/{org}/labels/{id}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (2):

- `<org>` (string)
- `<id>` (string)

## ward-kdl-write ops forgejo license get

`GET /licenses/{name}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (1):

- `<name>` (string)

## ward-kdl-write ops forgejo media get

`GET /repos/{owner}/{repo}/media/{filepath}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<filepath>` (string)

Options (1):

- `--ref` (string, optional): The name of the commit/branch/tag. Default the repository’s default branch (usually master)

## ward-kdl-write ops forgejo milestone get

`GET /repos/{owner}/{repo}/milestones/{id}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<id>` (string)

## ward-kdl-write ops forgejo note get

`GET /repos/{owner}/{repo}/git/notes/{sha}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<sha>` (string)

Options (2):

- `--verification` (boolean, optional): include verification for every commit (disable for speedup, default 'true')
- `--files` (boolean, optional): include a list of affected files for every commit (disable for speedup, default 'true')

## ward-kdl-write ops forgejo oauth2 get

`GET /user/applications/oauth2/{id}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (1):

- `<id>` (string)

## ward-kdl-write ops forgejo org get

`GET /orgs/{org}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (1):

- `<org>` (string)

## ward-kdl-write ops forgejo package get

`GET /packages/{owner}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (1):

- `<owner>` (string)

Options (4):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results
- `--type` (string, optional): package type filter
- `--q` (string, optional): name filter

## ward-kdl-write ops forgejo page get

`GET /repos/{owner}/{repo}/wiki/page/{pageName}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<pageName>` (string)

## ward-kdl-write ops forgejo public_member get

`GET /orgs/{org}/public_members/{username}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (2):

- `<org>` (string)
- `<username>` (string)

## ward-kdl-write ops forgejo pull get

`GET /repos/{owner}/{repo}/pulls/{index}/reviews/{id}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (4):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)
- `<id>` (string)

## ward-kdl-write ops forgejo push_mirror get

`GET /repos/{owner}/{repo}/push_mirrors/{name}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<name>` (string)

## ward-kdl-write ops forgejo raw get

`GET /repos/{owner}/{repo}/raw/{filepath}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<filepath>` (string)

Options (1):

- `--ref` (string, optional): The name of the commit/branch/tag. Default the repository’s default branch (usually master)

## ward-kdl-write ops forgejo ref get

`GET /repos/{owner}/{repo}/git/refs/{ref}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<ref>` (string)

## ward-kdl-write ops forgejo release get

`GET /repos/{owner}/{repo}/releases/{id}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<id>` (string)

## ward-kdl-write ops forgejo repo get

`GET /repos/{owner}/{repo}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-write ops forgejo repositorie get

`GET /repositories/{id}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (1):

- `<id>` (string)

## ward-kdl-write ops forgejo review get

`GET /repos/{owner}/{repo}/pulls/{index}/reviews/{id}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (4):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)
- `<id>` (string)

## ward-kdl-write ops forgejo revision get

`GET /repos/{owner}/{repo}/wiki/revisions/{pageName}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<pageName>` (string)

Options (1):

- `--page` (integer, optional): page number of results to return (1-based)

## ward-kdl-write ops forgejo rule get

`GET /admin/quota/rules/{quotarule}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (1):

- `<quotarule>` (string)

## ward-kdl-write ops forgejo run get

`GET /repos/{owner}/{repo}/actions/runs/{run_id}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<run_id>` (string)

## ward-kdl-write ops forgejo starred get

`GET /user/starred/{owner}/{repo}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-write ops forgejo statuse get

`GET /repos/{owner}/{repo}/statuses/{sha}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<sha>` (string)

Options (4):

- `--sort` (string, optional): type of sort
- `--state` (string, optional): type of state
- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo sync_fork get

`GET /repos/{owner}/{repo}/sync_fork/{branch}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<branch>` (string)

## ward-kdl-write ops forgejo tag get

`GET /repos/{owner}/{repo}/tags/{tag}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<tag>` (string)

## ward-kdl-write ops forgejo tag_protection get

`GET /repos/{owner}/{repo}/tag_protections/{id}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<id>` (string)

## ward-kdl-write ops forgejo team get

`GET /teams/{id}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (1):

- `<id>` (string)

## ward-kdl-write ops forgejo thread get

`GET /notifications/threads/{id}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (1):

- `<id>` (string)

## ward-kdl-write ops forgejo time get

`GET /repos/{owner}/{repo}/times/{user}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<user>` (string)

## ward-kdl-write ops forgejo tree get

`GET /repos/{owner}/{repo}/git/trees/{sha}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<sha>` (string)

Options (3):

- `--recursive` (boolean, optional): show all directories and files
- `--page` (integer, optional): page number; the 'truncated' field in the response will be true if there are still more items after this page, false if the last page
- `--per_page` (integer, optional): number of items per page

## ward-kdl-write ops forgejo user get

`GET /users/{username}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (1):

- `<username>` (string)

## ward-kdl-write ops forgejo variable get

`GET /user/actions/variables/{variablename}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (1):

- `<variablename>` (string)

## ward-kdl-write ops forgejo activity list

`GET /activitypub/user-id/{user-id}/activities/{activity-id}/activity`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<user-id>` (string)
- `<activity-id>` (string)

## ward-kdl-write ops forgejo actor list

`GET /activitypub/actor`

Authorized by grant: can list "*". Not destructive.

Takes no arguments.

## ward-kdl-write ops forgejo api list

`GET /settings/api`

Authorized by grant: can list "*". Not destructive.

Takes no arguments.

## ward-kdl-write ops forgejo artifact list

`GET /user/quota/artifacts`

Authorized by grant: can list "*". Not destructive.

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo assignee list

`GET /repos/{owner}/{repo}/assignees`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-write ops forgejo attachment list

`GET /user/quota/attachments`

Authorized by grant: can list "*". Not destructive.

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo blob list

`GET /repos/{owner}/{repo}/git/blobs`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (1):

- `--shas` (string, required): a comma separated list of blob-sha (mind the overall URL-length limit of ~2,083 chars)

## ward-kdl-write ops forgejo block list

`GET /repos/{owner}/{repo}/issues/{index}/blocks`

Authorized by grant: can list "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo branch_protection list

`GET /repos/{owner}/{repo}/branch_protections`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-write ops forgejo branche list

`GET /repos/{owner}/{repo}/branches`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo check list

`GET /user/quota/check`

Authorized by grant: can list "*". Not destructive.

Options (1):

- `--subject` (string, required): subject of the quota

## ward-kdl-write ops forgejo collaborator list

`GET /repos/{owner}/{repo}/collaborators`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo comment list

`GET /repos/{owner}/{repo}/issues/comments`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (4):

- `--since` (string, optional): if provided, only comments updated since the provided time are returned.
- `--before` (string, optional): if provided, only comments updated before the provided time are returned.
- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo commit list

`GET /repos/{owner}/{repo}/commits`

Authorized by grant: can list "*". Not destructive.

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

## ward-kdl-write ops forgejo content list

`GET /repos/{owner}/{repo}/contents`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (1):

- `--ref` (string, optional): The name of the commit/branch/tag. Default the repository’s default branch (usually master)

## ward-kdl-write ops forgejo cron list

`GET /admin/cron`

Authorized by grant: can list "*". Not destructive.

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo dependencie list

`GET /repos/{owner}/{repo}/issues/{index}/dependencies`

Authorized by grant: can list "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo flag list

`GET /repos/{owner}/{repo}/flags`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-write ops forgejo follower list

`GET /user/followers`

Authorized by grant: can list "*". Not destructive.

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo following list

`GET /user/following`

Authorized by grant: can list "*". Not destructive.

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo fork list

`GET /repos/{owner}/{repo}/forks`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo git list

`GET /repos/{owner}/{repo}/hooks/git`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-write ops forgejo gpg_key list

`GET /user/gpg_keys`

Authorized by grant: can list "*". Not destructive.

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo gpg_key_token list

`GET /user/gpg_key_token`

Authorized by grant: can list "*". Not destructive.

Takes no arguments.

## ward-kdl-write ops forgejo group list

`GET /admin/quota/groups`

Authorized by grant: can list "*". Not destructive.

Takes no arguments.

## ward-kdl-write ops forgejo heatmap list

`GET /users/{username}/heatmap`

Authorized by grant: can list "*". Not destructive.

Positional arguments (1):

- `<username>` (string)

## ward-kdl-write ops forgejo issue list

`GET /repos/{owner}/{repo}/issues`

Authorized by grant: can list "*". Not destructive.

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

## ward-kdl-write ops forgejo issue_config list

`GET /repos/{owner}/{repo}/issue_config`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-write ops forgejo issue_template list

`GET /repos/{owner}/{repo}/issue_templates`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-write ops forgejo job list

`GET /admin/runners/jobs`

Authorized by grant: can list "*". Not destructive.

Options (1):

- `--labels` (string, optional): a comma separated list of run job labels to search for

## ward-kdl-write ops forgejo key list

`GET /user/keys`

Authorized by grant: can list "*". Not destructive.

Options (3):

- `--fingerprint` (string, optional): fingerprint of the key
- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo label list

`GET /orgs/{org}/labels`

Authorized by grant: can list "*". Not destructive.

Positional arguments (1):

- `<org>` (string)

Options (3):

- `--sort` (string, optional): Specifies the sorting method: mostissues, leastissues, or reversealphabetically.
- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo language list

`GET /repos/{owner}/{repo}/languages`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-write ops forgejo latest list

`GET /repos/{owner}/{repo}/releases/latest`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-write ops forgejo license list

`GET /licenses`

Authorized by grant: can list "*". Not destructive.

Takes no arguments.

## ward-kdl-write ops forgejo list_blocked list

`GET /user/list_blocked`

Authorized by grant: can list "*". Not destructive.

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo merge list

`GET /repos/{owner}/{repo}/pulls/{index}/merge`

Authorized by grant: can list "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

## ward-kdl-write ops forgejo milestone list

`GET /repos/{owner}/{repo}/milestones`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (4):

- `--state` (string, optional): Milestone state, Recognized values are open, closed and all. Defaults to "open"
- `--name` (string, optional): filter by milestone name
- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo new list

`GET /notifications/new`

Authorized by grant: can list "*". Not destructive.

Takes no arguments.

## ward-kdl-write ops forgejo new_pin_allowed list

`GET /repos/{owner}/{repo}/new_pin_allowed`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-write ops forgejo nodeinfo list

`GET /nodeinfo`

Authorized by grant: can list "*". Not destructive.

Takes no arguments.

## ward-kdl-write ops forgejo notification list

`GET /notifications`

Authorized by grant: can list "*". Not destructive.

Options (5):

- `--all` (boolean, optional): If true, show notifications marked as read. Default value is false
- `--since` (string, optional): Only show notifications updated after the given time. This is a timestamp in RFC 3339 format
- `--before` (string, optional): Only show notifications updated before the given time. This is a timestamp in RFC 3339 format
- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo oauth2 list

`GET /user/applications/oauth2`

Authorized by grant: can list "*". Not destructive.

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo org list

`GET /orgs`

Authorized by grant: can list "*". Not destructive.

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo outbox list

`GET /activitypub/user-id/{user-id}/outbox`

Authorized by grant: can list "*". Not destructive.

Positional arguments (1):

- `<user-id>` (string)

## ward-kdl-write ops forgejo package list

`GET /user/quota/packages`

Authorized by grant: can list "*". Not destructive.

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo page list

`GET /repos/{owner}/{repo}/wiki/pages`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo permission list

`GET /users/{username}/orgs/{org}/permissions`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<username>` (string)
- `<org>` (string)

## ward-kdl-write ops forgejo public_member list

`GET /orgs/{org}/public_members`

Authorized by grant: can list "*". Not destructive.

Positional arguments (1):

- `<org>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo pull list

`GET /repos/{owner}/{repo}/pulls`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (6):

- `--state` (string, optional): State of pull request
- `--sort` (string, optional): Type of sort
- `--milestone` (integer, optional): ID of the milestone
- `--poster` (string, optional): Filter by pull request author
- `--page` (integer, optional): Page number of results to return (1-based)
- `--limit` (integer, optional): Page size of results

## ward-kdl-write ops forgejo push_mirror list

`GET /repos/{owner}/{repo}/push_mirrors`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo quota list

`GET /user/quota`

Authorized by grant: can list "*". Not destructive.

Takes no arguments.

## ward-kdl-write ops forgejo reaction list

`GET /repos/{owner}/{repo}/issues/{index}/reactions`

Authorized by grant: can list "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo ref list

`GET /repos/{owner}/{repo}/git/refs`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-write ops forgejo release list

`GET /repos/{owner}/{repo}/releases`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (5):

- `--draft` (boolean, optional): filter (exclude / include) drafts, if you dont have repo write access none will show
- `--pre-release` (boolean, optional): filter (exclude / include) pre-releases
- `--q` (string, optional): Search string
- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo repo list

`GET /user/repos`

Authorized by grant: can list "*". Not destructive.

Options (3):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results
- `--order_by` (string, optional): order the repositories

## ward-kdl-write ops forgejo repository list

`GET /settings/repository`

Authorized by grant: can list "*". Not destructive.

Takes no arguments.

## ward-kdl-write ops forgejo review list

`GET /repos/{owner}/{repo}/pulls/{index}/reviews`

Authorized by grant: can list "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo reviewer list

`GET /repos/{owner}/{repo}/reviewers`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-write ops forgejo rule list

`GET /admin/quota/rules`

Authorized by grant: can list "*". Not destructive.

Takes no arguments.

## ward-kdl-write ops forgejo run list

`GET /repos/{owner}/{repo}/actions/runs`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (6):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results, default maximum page size is 50
- `--run_number` (integer, optional): Returns the workflow run associated with the run number.

- `--head_sha` (string, optional): Only returns workflow runs that are associated with the specified head_sha.
- `--ref` (string, optional): Only return workflow runs that involve the given Git reference, for example, `refs/heads/main`.
- `--workflow_id` (string, optional): Only return workflow runs that involve the given workflow ID.

## ward-kdl-write ops forgejo secret list

`GET /orgs/{org}/actions/secrets`

Authorized by grant: can list "*". Not destructive.

Positional arguments (1):

- `<org>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo setting list

`GET /user/settings`

Authorized by grant: can list "*". Not destructive.

Takes no arguments.

## ward-kdl-write ops forgejo stargazer list

`GET /repos/{owner}/{repo}/stargazers`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo starred list

`GET /user/starred`

Authorized by grant: can list "*". Not destructive.

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo statu list

`GET /repos/{owner}/{repo}/commits/{ref}/status`

Authorized by grant: can list "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<ref>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo statuse list

`GET /repos/{owner}/{repo}/commits/{ref}/statuses`

Authorized by grant: can list "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<ref>` (string)

Options (4):

- `--sort` (string, optional): type of sort
- `--state` (string, optional): type of state
- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo stopwatche list

`GET /user/stopwatches`

Authorized by grant: can list "*". Not destructive.

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo subscriber list

`GET /repos/{owner}/{repo}/subscribers`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo subscription list

`GET /user/subscriptions`

Authorized by grant: can list "*". Not destructive.

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo sync_fork list

`GET /repos/{owner}/{repo}/sync_fork`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-write ops forgejo tag list

`GET /repos/{owner}/{repo}/tags`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results, default maximum page size is 50

## ward-kdl-write ops forgejo tag_protection list

`GET /repos/{owner}/{repo}/tag_protections`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-write ops forgejo task list

`GET /repos/{owner}/{repo}/actions/tasks`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results, default maximum page size is 50

## ward-kdl-write ops forgejo team list

`GET /user/teams`

Authorized by grant: can list "*". Not destructive.

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo time list

`GET /user/times`

Authorized by grant: can list "*". Not destructive.

Options (4):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results
- `--since` (string, optional): Only show times updated after the given time. This is a timestamp in RFC 3339 format
- `--before` (string, optional): Only show times updated before the given time. This is a timestamp in RFC 3339 format

## ward-kdl-write ops forgejo timeline list

`GET /repos/{owner}/{repo}/issues/{index}/timeline`

Authorized by grant: can list "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

Options (4):

- `--since` (string, optional): if provided, only comments updated since the specified time are returned.
- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results
- `--before` (string, optional): if provided, only comments updated before the provided time are returned.

## ward-kdl-write ops forgejo token list

`GET /users/{username}/tokens`

Authorized by grant: can list "*". Not destructive.

Positional arguments (1):

- `<username>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo topic list

`GET /repos/{owner}/{repo}/topics`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo ui list

`GET /settings/ui`

Authorized by grant: can list "*". Not destructive.

Takes no arguments.

## ward-kdl-write ops forgejo unadopted list

`GET /admin/unadopted`

Authorized by grant: can list "*". Not destructive.

Options (3):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results
- `--pattern` (string, optional): pattern of repositories to search for

## ward-kdl-write ops forgejo user list

`GET /admin/users`

Authorized by grant: can list "*". Not destructive.

Options (5):

- `--source_id` (integer, optional): ID of the user's login source to search for
- `--login_name` (string, optional): user's login name to search for
- `--sort` (string, optional): sort order of results
- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo validate list

`GET /repos/{owner}/{repo}/issue_config/validate`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-write ops forgejo variable list

`GET /user/actions/variables`

Authorized by grant: can list "*". Not destructive.

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-write ops forgejo version list

`GET /version`

Authorized by grant: can list "*". Not destructive.

Takes no arguments.

## ward-kdl-write ops forgejo accept create

`POST /repos/{owner}/{repo}/transfer/accept`

Authorized by grant: can create "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-write ops forgejo avatar create

`POST /user/avatar`

Authorized by grant: can create "*". Not destructive.

Options (1):

- `--image` (string, optional): image must be base64 encoded

## ward-kdl-write ops forgejo block create

`POST /repos/{owner}/{repo}/issues/{index}/blocks`

Authorized by grant: can create "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

Options (3):

- `--index` (integer, optional)
- `--owner` (string, optional)
- `--repo` (string, optional)

## ward-kdl-write ops forgejo branch_protection create

`POST /repos/{owner}/{repo}/branch_protections`

Authorized by grant: can create "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (25):

- `--apply_to_admins` (boolean, optional)
- `--approvals_whitelist_teams` ([]string, optional)
- `--approvals_whitelist_username` ([]string, optional)
- `--block_on_official_review_requests` (boolean, optional)
- `--block_on_outdated_branch` (boolean, optional)
- `--block_on_rejected_reviews` (boolean, optional)
- `--branch_name` (string, optional): Deprecated: true
- `--dismiss_stale_approvals` (boolean, optional)
- `--enable_approvals_whitelist` (boolean, optional)
- `--enable_merge_whitelist` (boolean, optional)
- `--enable_push` (boolean, optional)
- `--enable_push_whitelist` (boolean, optional)
- `--enable_status_check` (boolean, optional)
- `--ignore_stale_approvals` (boolean, optional)
- `--merge_whitelist_teams` ([]string, optional)
- `--merge_whitelist_usernames` ([]string, optional)
- `--protected_file_patterns` (string, optional)
- `--push_whitelist_deploy_keys` (boolean, optional)
- `--push_whitelist_teams` ([]string, optional)
- `--push_whitelist_usernames` ([]string, optional)
- `--require_signed_commits` (boolean, optional)
- `--required_approvals` (integer, optional)
- `--rule_name` (string, optional)
- `--status_check_contexts` ([]string, optional)
- `--unprotected_file_patterns` (string, optional)

## ward-kdl-write ops forgejo branche create

`POST /repos/{owner}/{repo}/branches`

Authorized by grant: can create "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (3):

- `--new_branch_name` (string, required): Name of the branch to create
- `--old_branch_name` (string, optional): Deprecated: true
Name of the old branch to create from
- `--old_ref_name` (string, optional): Name of the old branch/tag/commit to create from

## ward-kdl-write ops forgejo comment create

`POST /repos/{owner}/{repo}/issues/{index}/comments`

Authorized by grant: can create "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

Options (2):

- `--body` (string, required): The body of the comment
- `--updated_at` (string, optional): The time of the comment's update, needs admin or repository owner permission

## ward-kdl-write ops forgejo content create

`POST /repos/{owner}/{repo}/contents`

Authorized by grant: can create "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (4):

- `--branch` (string, optional): branch (optional) to base this file from. if not given, the default branch is used
- `--message` (string, optional): message (optional) for the commit of this file. if not supplied, a default message will be used
- `--new_branch` (string, optional): new_branch (optional) will make a new branch from `branch` before creating the file
- `--signoff` (boolean, optional): Add a Signed-off-by trailer by the committer at the end of the commit log message.

## ward-kdl-write ops forgejo convert create

`POST /repos/{owner}/{repo}/convert`

Authorized by grant: can create "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-write ops forgejo deadline create

`POST /repos/{owner}/{repo}/issues/{index}/deadline`

Authorized by grant: can create "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

Options (1):

- `--due_date` (string, required)

## ward-kdl-write ops forgejo dependencie create

`POST /repos/{owner}/{repo}/issues/{index}/dependencies`

Authorized by grant: can create "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

Options (3):

- `--index` (integer, optional)
- `--owner` (string, optional)
- `--repo` (string, optional)

## ward-kdl-write ops forgejo diffpatch create

`POST /repos/{owner}/{repo}/diffpatch`

Authorized by grant: can create "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (7):

- `--branch` (string, optional): branch (optional) to base this file from. if not given, the default branch is used
- `--content` (string, required): content must be base64 encoded
- `--from_path` (string, optional): from_path (optional) is the path of the original file which will be moved/renamed to the path in the URL
- `--message` (string, optional): message (optional) for the commit of this file. if not supplied, a default message will be used
- `--new_branch` (string, optional): new_branch (optional) will make a new branch from `branch` before creating the file
- `--sha` (string, required): sha is the SHA for the file that already exists
- `--signoff` (boolean, optional): Add a Signed-off-by trailer by the committer at the end of the commit log message.

## ward-kdl-write ops forgejo dismissal create

`POST /repos/{owner}/{repo}/pulls/{index}/reviews/{id}/dismissals`

Authorized by grant: can create "*". Not destructive.

Positional arguments (4):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)
- `<id>` (string)

Options (2):

- `--message` (string, optional)
- `--priors` (boolean, optional)

## ward-kdl-write ops forgejo dispatche create

`POST /repos/{owner}/{repo}/actions/workflows/{workflowfilename}/dispatches`

Authorized by grant: can create "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<workflowfilename>` (string)

Options (2):

- `--ref` (string, required): Git reference for the workflow
- `--return_run_info` (boolean, optional): Flag to return the run info

## ward-kdl-write ops forgejo email create

`POST /user/emails`

Authorized by grant: can create "*". Not destructive.

Options (1):

- `--emails` ([]string, optional): email addresses to add

## ward-kdl-write ops forgejo fork create

`POST /repos/{owner}/{repo}/forks`

Authorized by grant: can create "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (2):

- `--name` (string, optional): name of the forked repository
- `--organization` (string, optional): organization name, if forking into an organization

## ward-kdl-write ops forgejo generate create

`POST /repos/{template_owner}/{template_repo}/generate`

Authorized by grant: can create "*". Not destructive.

Positional arguments (2):

- `<template_owner>` (string)
- `<template_repo>` (string)

Options (12):

- `--avatar` (boolean, optional): include avatar of the template repo
- `--default_branch` (string, optional): Default branch of the new repository
- `--description` (string, optional): Description of the repository to create
- `--git_content` (boolean, optional): include git content of default branch in template repo
- `--git_hooks` (boolean, optional): include git hooks in template repo
- `--labels` (boolean, optional): include labels in template repo
- `--name` (string, required): Name of the repository to create
- `--owner` (string, required): The organization or person who will own the new repository
- `--private` (boolean, optional): Whether the repository is private
- `--protected_branch` (boolean, optional): include protected branches in template repo
- `--topics` (boolean, optional): include topics in template repo
- `--webhooks` (boolean, optional): include webhooks in template repo

## ward-kdl-write ops forgejo gpg_key create

`POST /user/gpg_keys`

Authorized by grant: can create "*". Not destructive.

Options (2):

- `--armored_public_key` (string, required): An armored GPG key to add
- `--armored_signature` (string, optional)

## ward-kdl-write ops forgejo gpg_key_verify create

`POST /user/gpg_key_verify`

Authorized by grant: can create "*". Not destructive.

Options (2):

- `--armored_signature` (string, optional)
- `--key_id` (string, required): An Signature for a GPG key token

## ward-kdl-write ops forgejo group create

`POST /admin/quota/groups`

Authorized by grant: can create "*". Not destructive.

Options (1):

- `--name` (string, optional): Name of the quota group to create

## ward-kdl-write ops forgejo inbox create

`POST /activitypub/actor/inbox`

Authorized by grant: can create "*". Not destructive.

Takes no arguments.

## ward-kdl-write ops forgejo issue create

`POST /repos/{owner}/{repo}/issues`

Authorized by grant: can create "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (9):

- `--assignee` (string, optional): deprecated
- `--assignees` ([]string, optional)
- `--body` (string, optional)
- `--closed` (boolean, optional)
- `--due_date` (string, optional)
- `--labels` ([]integer, optional): list of label ids
- `--milestone` (integer, optional): milestone id
- `--ref` (string, optional)
- `--title` (string, required)

## ward-kdl-write ops forgejo key create

`POST /user/keys`

Authorized by grant: can create "*". Not destructive.

Options (3):

- `--key` (string, required): An armored SSH key to add
- `--read_only` (boolean, optional): Describe if the key has only read access or read/write
- `--title` (string, required): Title of the key to add

## ward-kdl-write ops forgejo label create

`POST /orgs/{org}/labels`

Authorized by grant: can create "*". Not destructive.

Positional arguments (1):

- `<org>` (string)

Options (5):

- `--color` (string, required)
- `--description` (string, optional)
- `--exclusive` (boolean, optional)
- `--is_archived` (boolean, optional)
- `--name` (string, required)

## ward-kdl-write ops forgejo markdown create

`POST /markdown`

Authorized by grant: can create "*". Not destructive.

Options (4):

- `--Context` (string, optional): Context to render

in: body
- `--Mode` (string, optional): Mode to render (comment, gfm, markdown)

in: body
- `--Text` (string, optional): Text markdown to render

in: body
- `--Wiki` (boolean, optional): Is it a wiki page ?

in: body

## ward-kdl-write ops forgejo markup create

`POST /markup`

Authorized by grant: can create "*". Not destructive.

Options (6):

- `--BranchPath` (string, optional): The current branch path where the form gets posted

in: body
- `--Context` (string, optional): Context to render

in: body
- `--FilePath` (string, optional): File path for detecting extension in file mode

in: body
- `--Mode` (string, optional): Mode to render (comment, gfm, markdown, file)

in: body
- `--Text` (string, optional): Text markup to render

in: body
- `--Wiki` (boolean, optional): Is it a wiki page ?

in: body

## ward-kdl-write ops forgejo merge create

`POST /repos/{owner}/{repo}/pulls/{index}/merge`

Authorized by grant: can create "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

Options (8):

- `--Do` (string, required)
- `--MergeCommitID` (string, optional)
- `--MergeMessageField` (string, optional)
- `--MergeTitleField` (string, optional)
- `--delete_branch_after_merge` (boolean, optional)
- `--force_merge` (boolean, optional)
- `--head_commit_id` (string, optional)
- `--merge_when_checks_succeed` (boolean, optional)

## ward-kdl-write ops forgejo migrate create

`POST /repos/migrate`

Authorized by grant: can create "*". Not destructive.

Options (20):

- `--auth_password` (string, optional)
- `--auth_token` (string, optional)
- `--auth_username` (string, optional)
- `--clone_addr` (string, required)
- `--description` (string, optional)
- `--issues` (boolean, optional)
- `--labels` (boolean, optional)
- `--lfs` (boolean, optional)
- `--lfs_endpoint` (string, optional)
- `--milestones` (boolean, optional)
- `--mirror` (boolean, optional)
- `--mirror_interval` (string, optional)
- `--private` (boolean, optional)
- `--pull_requests` (boolean, optional)
- `--releases` (boolean, optional)
- `--repo_name` (string, required)
- `--repo_owner` (string, optional): Name of User or Organisation who will own Repo after migration
- `--service` (string, optional)
- `--uid` (integer, optional): deprecated (only for backwards compatibility)
- `--wiki` (boolean, optional)

## ward-kdl-write ops forgejo milestone create

`POST /repos/{owner}/{repo}/milestones`

Authorized by grant: can create "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (4):

- `--description` (string, optional)
- `--due_on` (string, optional)
- `--state` (string, optional)
- `--title` (string, optional)

## ward-kdl-write ops forgejo new create

`POST /repos/{owner}/{repo}/wiki/new`

Authorized by grant: can create "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (3):

- `--content_base64` (string, optional): content must be base64 encoded
- `--message` (string, optional): optional commit message summarizing the change
- `--title` (string, optional): page title. leave empty to keep unchanged

## ward-kdl-write ops forgejo oauth2 create

`POST /user/applications/oauth2`

Authorized by grant: can create "*". Not destructive.

Options (3):

- `--confidential_client` (boolean, optional)
- `--name` (string, optional)
- `--redirect_uris` ([]string, optional)

## ward-kdl-write ops forgejo org create

`POST /orgs`

Authorized by grant: can create "*". Not destructive.

Options (8):

- `--description` (string, optional)
- `--email` (string, optional)
- `--full_name` (string, optional)
- `--location` (string, optional)
- `--repo_admin_change_team_access` (boolean, optional)
- `--username` (string, required)
- `--visibility` (string, optional): possible values are `public` (default), `limited` or `private`
- `--website` (string, optional)

## ward-kdl-write ops forgejo outbox create

`POST /activitypub/actor/outbox`

Authorized by grant: can create "*". Not destructive.

Takes no arguments.

## ward-kdl-write ops forgejo page create

`POST /repos/{owner}/{repo}/wiki/new`

Authorized by grant: can create "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (3):

- `--content_base64` (string, optional): content must be base64 encoded
- `--message` (string, optional): optional commit message summarizing the change
- `--title` (string, optional): page title. leave empty to keep unchanged

## ward-kdl-write ops forgejo pin create

`POST /repos/{owner}/{repo}/issues/{index}/pin`

Authorized by grant: can create "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

## ward-kdl-write ops forgejo pull create

`POST /repos/{owner}/{repo}/pulls`

Authorized by grant: can create "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (9):

- `--assignee` (string, optional)
- `--assignees` ([]string, optional)
- `--base` (string, optional)
- `--body` (string, optional)
- `--due_date` (string, optional)
- `--head` (string, optional)
- `--labels` ([]integer, optional)
- `--milestone` (integer, optional)
- `--title` (string, optional)

## ward-kdl-write ops forgejo push_mirror create

`POST /repos/{owner}/{repo}/push_mirrors`

Authorized by grant: can create "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (7):

- `--branch_filter` (string, optional)
- `--interval` (string, optional)
- `--remote_address` (string, optional)
- `--remote_password` (string, optional)
- `--remote_username` (string, optional)
- `--sync_on_commit` (boolean, optional)
- `--use_ssh` (boolean, optional)

## ward-kdl-write ops forgejo raw create

`POST /markdown/raw`

Authorized by grant: can create "*". Not destructive.

Takes no arguments.

## ward-kdl-write ops forgejo reaction create

`POST /repos/{owner}/{repo}/issues/{index}/reactions`

Authorized by grant: can create "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

Options (1):

- `--content` (string, optional)

## ward-kdl-write ops forgejo reject create

`POST /repos/{owner}/{repo}/transfer/reject`

Authorized by grant: can create "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-write ops forgejo release create

`POST /repos/{owner}/{repo}/releases`

Authorized by grant: can create "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (7):

- `--body` (string, optional)
- `--draft` (boolean, optional)
- `--hide_archive_links` (boolean, optional)
- `--name` (string, optional)
- `--prerelease` (boolean, optional)
- `--tag_name` (string, required)
- `--target_commitish` (string, optional)

## ward-kdl-write ops forgejo rename create

`POST /orgs/{org}/rename`

Authorized by grant: can create "*". Not destructive.

Positional arguments (1):

- `<org>` (string)

Options (1):

- `--new_name` (string, required): New username for this org. This name cannot be in use yet by any other user.

## ward-kdl-write ops forgejo repo create

`POST /user/repos`

Authorized by grant: can create "*". Not destructive.

Options (12):

- `--auto_init` (boolean, optional): Whether the repository should be auto-initialized?
- `--default_branch` (string, optional): DefaultBranch of the repository (used when initializes and in template)
- `--description` (string, optional): Description of the repository to create
- `--gitignores` (string, optional): Gitignores to use
- `--issue_labels` (string, optional): Label-Set to use
- `--license` (string, optional): License to use
- `--name` (string, required): Name of the repository to create
- `--object_format_name` (string, optional): ObjectFormatName of the underlying git repository
- `--private` (boolean, optional): Whether the repository is private
- `--readme` (string, optional): Readme of the repository to create
- `--template` (boolean, optional): Whether the repository is template
- `--trust_model` (string, optional): TrustModel of the repository

## ward-kdl-write ops forgejo requested_reviewer create

`POST /repos/{owner}/{repo}/pulls/{index}/requested_reviewers`

Authorized by grant: can create "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

Options (2):

- `--reviewers` ([]string, optional)
- `--team_reviewers` ([]string, optional)

## ward-kdl-write ops forgejo review create

`POST /repos/{owner}/{repo}/pulls/{index}/reviews`

Authorized by grant: can create "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

Options (3):

- `--body` (string, optional)
- `--commit_id` (string, optional)
- `--event` (string, optional): ReviewStateType review state type

## ward-kdl-write ops forgejo rule create

`POST /admin/quota/rules`

Authorized by grant: can create "*". Not destructive.

Options (3):

- `--limit` (integer, optional): The limit set by the rule
- `--name` (string, optional): Name of the rule to create
- `--subjects` ([]string, optional): The subjects affected by the rule

## ward-kdl-write ops forgejo start create

`POST /repos/{owner}/{repo}/issues/{index}/stopwatch/start`

Authorized by grant: can create "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

## ward-kdl-write ops forgejo statu create

`POST /repos/{owner}/{repo}/statuses/{sha}`

Authorized by grant: can create "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<sha>` (string)

Options (4):

- `--context` (string, optional)
- `--description` (string, optional)
- `--state` (string, optional): CommitStatusState holds the state of a CommitStatus
It can be "pending", "success", "error", "failure" and "warning"
- `--target_url` (string, optional)

## ward-kdl-write ops forgejo stop create

`POST /repos/{owner}/{repo}/issues/{index}/stopwatch/stop`

Authorized by grant: can create "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

## ward-kdl-write ops forgejo sync_fork create

`POST /repos/{owner}/{repo}/sync_fork`

Authorized by grant: can create "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-write ops forgejo tag create

`POST /repos/{owner}/{repo}/tags`

Authorized by grant: can create "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (3):

- `--message` (string, optional)
- `--tag_name` (string, required)
- `--target` (string, optional)

## ward-kdl-write ops forgejo tag_protection create

`POST /repos/{owner}/{repo}/tag_protections`

Authorized by grant: can create "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (3):

- `--name_pattern` (string, optional)
- `--whitelist_teams` ([]string, optional)
- `--whitelist_usernames` ([]string, optional)

## ward-kdl-write ops forgejo team create

`POST /orgs/{org}/teams`

Authorized by grant: can create "*". Not destructive.

Positional arguments (1):

- `<org>` (string)

Options (6):

- `--can_create_org_repo` (boolean, optional)
- `--description` (string, optional)
- `--includes_all_repositories` (boolean, optional)
- `--name` (string, required)
- `--permission` (string, optional)
- `--units` ([]string, optional)

## ward-kdl-write ops forgejo test create

`POST /repos/{owner}/{repo}/hooks/{id}/tests`

Authorized by grant: can create "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<id>` (string)

Options (1):

- `--ref` (string, optional): The name of the commit/branch/tag, indicates which commit will be loaded to the webhook payload.

## ward-kdl-write ops forgejo time create

`POST /repos/{owner}/{repo}/issues/{index}/times`

Authorized by grant: can create "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

Options (3):

- `--created` (string, optional)
- `--time` (integer, required): time in seconds
- `--user_name` (string, optional): User who spent the time (optional)

## ward-kdl-write ops forgejo token create

`POST /users/{username}/tokens`

Authorized by grant: can create "*". Not destructive.

Positional arguments (1):

- `<username>` (string)

Options (2):

- `--name` (string, required)
- `--scopes` ([]string, optional)

## ward-kdl-write ops forgejo transfer create

`POST /repos/{owner}/{repo}/transfer`

Authorized by grant: can create "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (2):

- `--new_owner` (string, required)
- `--team_ids` ([]integer, optional): ID of the team or teams to add to the repository. Teams can only be added to organization-owned repositories.

## ward-kdl-write ops forgejo undismissal create

`POST /repos/{owner}/{repo}/pulls/{index}/reviews/{id}/undismissals`

Authorized by grant: can create "*". Not destructive.

Positional arguments (4):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)
- `<id>` (string)

## ward-kdl-write ops forgejo unlink create

`POST /packages/{owner}/{type}/{name}/-/unlink`

Authorized by grant: can create "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<type>` (string)
- `<name>` (string)

## ward-kdl-write ops forgejo update create

`POST /repos/{owner}/{repo}/pulls/{index}/update`

Authorized by grant: can create "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

Options (1):

- `--style` (string, optional): how to update pull request

## ward-kdl-write ops forgejo user create

`POST /admin/users`

Authorized by grant: can create "*". Not destructive.

Options (11):

- `--created_at` (string, optional): For explicitly setting the user creation timestamp. Useful when users are
migrated from other systems. When omitted, the user's creation timestamp
will be set to "now".
- `--email` (string, required)
- `--full_name` (string, optional)
- `--login_name` (string, optional)
- `--must_change_password` (boolean, optional)
- `--password` (string, optional)
- `--restricted` (boolean, optional)
- `--send_notify` (boolean, optional)
- `--source_id` (integer, optional)
- `--username` (string, required)
- `--visibility` (string, optional)

## ward-kdl-write ops forgejo variable create

`POST /user/actions/variables/{variablename}`

Authorized by grant: can create "*". Not destructive.

Positional arguments (1):

- `<variablename>` (string)

Options (1):

- `--value` (string, required): Value of the variable to create. Special characters will be retained. Line endings will be normalized to LF to
match the behaviour of browsers. Encode the data with Base64 if line endings should be retained.

## ward-kdl-write ops forgejo block edit

`PUT /user/block/{username}`

Authorized by grant: can edit "*". Not destructive.

Positional arguments (1):

- `<username>` (string)

## ward-kdl-write ops forgejo branch_protection edit

`PATCH /repos/{owner}/{repo}/branch_protections/{name}`

Authorized by grant: can edit "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<name>` (string)

Options (23):

- `--apply_to_admins` (boolean, optional)
- `--approvals_whitelist_teams` ([]string, optional)
- `--approvals_whitelist_username` ([]string, optional)
- `--block_on_official_review_requests` (boolean, optional)
- `--block_on_outdated_branch` (boolean, optional)
- `--block_on_rejected_reviews` (boolean, optional)
- `--dismiss_stale_approvals` (boolean, optional)
- `--enable_approvals_whitelist` (boolean, optional)
- `--enable_merge_whitelist` (boolean, optional)
- `--enable_push` (boolean, optional)
- `--enable_push_whitelist` (boolean, optional)
- `--enable_status_check` (boolean, optional)
- `--ignore_stale_approvals` (boolean, optional)
- `--merge_whitelist_teams` ([]string, optional)
- `--merge_whitelist_usernames` ([]string, optional)
- `--protected_file_patterns` (string, optional)
- `--push_whitelist_deploy_keys` (boolean, optional)
- `--push_whitelist_teams` ([]string, optional)
- `--push_whitelist_usernames` ([]string, optional)
- `--require_signed_commits` (boolean, optional)
- `--required_approvals` (integer, optional)
- `--status_check_contexts` ([]string, optional)
- `--unprotected_file_patterns` (string, optional)

## ward-kdl-write ops forgejo branche edit

`PATCH /repos/{owner}/{repo}/branches/{branch}`

Authorized by grant: can edit "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<branch>` (string)

Options (1):

- `--name` (string, required): New branch name

## ward-kdl-write ops forgejo collaborator edit

`PUT /repos/{owner}/{repo}/collaborators/{collaborator}`

Authorized by grant: can edit "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<collaborator>` (string)

Options (1):

- `--permission` (string, optional)

## ward-kdl-write ops forgejo comment edit

`PATCH /repos/{owner}/{repo}/issues/comments/{id}`

Authorized by grant: can edit "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<id>` (string)

Options (2):

- `--body` (string, required): The body of the comment
- `--updated_at` (string, optional): The time of the comment's update, needs admin or repository owner permission

## ward-kdl-write ops forgejo content edit

`PUT /repos/{owner}/{repo}/contents/{filepath}`

Authorized by grant: can edit "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<filepath>` (string)

Options (7):

- `--branch` (string, optional): branch (optional) to base this file from. if not given, the default branch is used
- `--content` (string, required): content must be base64 encoded
- `--from_path` (string, optional): from_path (optional) is the path of the original file which will be moved/renamed to the path in the URL
- `--message` (string, optional): message (optional) for the commit of this file. if not supplied, a default message will be used
- `--new_branch` (string, optional): new_branch (optional) will make a new branch from `branch` before creating the file
- `--sha` (string, required): sha is the SHA for the file that already exists
- `--signoff` (boolean, optional): Add a Signed-off-by trailer by the committer at the end of the commit log message.

## ward-kdl-write ops forgejo flag edit

`PUT /repos/{owner}/{repo}/flags/{flag}`

Authorized by grant: can edit "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<flag>` (string)

## ward-kdl-write ops forgejo following edit

`PUT /user/following/{username}`

Authorized by grant: can edit "*". Not destructive.

Positional arguments (1):

- `<username>` (string)

## ward-kdl-write ops forgejo git edit

`PATCH /repos/{owner}/{repo}/hooks/git/{id}`

Authorized by grant: can edit "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<id>` (string)

Options (1):

- `--content` (string, optional)

## ward-kdl-write ops forgejo issue edit

`PATCH /repos/{owner}/{repo}/issues/{index}`

Authorized by grant: can edit "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

Options (10):

- `--assignee` (string, optional): deprecated
- `--assignees` ([]string, optional)
- `--body` (string, optional)
- `--due_date` (string, optional)
- `--milestone` (integer, optional)
- `--ref` (string, optional)
- `--state` (string, optional)
- `--title` (string, optional)
- `--unset_due_date` (boolean, optional)
- `--updated_at` (string, optional)

## ward-kdl-write ops forgejo label edit

`PATCH /orgs/{org}/labels/{id}`

Authorized by grant: can edit "*". Not destructive.

Positional arguments (2):

- `<org>` (string)
- `<id>` (string)

Options (5):

- `--color` (string, optional)
- `--description` (string, optional)
- `--exclusive` (boolean, optional)
- `--is_archived` (boolean, optional)
- `--name` (string, optional)

## ward-kdl-write ops forgejo member edit

`PUT /teams/{id}/members/{username}`

Authorized by grant: can edit "*". Not destructive.

Positional arguments (2):

- `<id>` (string)
- `<username>` (string)

## ward-kdl-write ops forgejo milestone edit

`PATCH /repos/{owner}/{repo}/milestones/{id}`

Authorized by grant: can edit "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<id>` (string)

Options (4):

- `--description` (string, optional)
- `--due_on` (string, optional)
- `--state` (string, optional)
- `--title` (string, optional)

## ward-kdl-write ops forgejo oauth2 edit

`PATCH /user/applications/oauth2/{id}`

Authorized by grant: can edit "*". Not destructive.

Positional arguments (1):

- `<id>` (string)

Options (3):

- `--confidential_client` (boolean, optional)
- `--name` (string, optional)
- `--redirect_uris` ([]string, optional)

## ward-kdl-write ops forgejo org edit

`PATCH /orgs/{org}`

Authorized by grant: can edit "*". Not destructive.

Positional arguments (1):

- `<org>` (string)

Options (7):

- `--description` (string, optional)
- `--email` (string, optional)
- `--full_name` (string, optional)
- `--location` (string, optional)
- `--repo_admin_change_team_access` (boolean, optional)
- `--visibility` (string, optional): possible values are `public`, `limited` or `private`
- `--website` (string, optional)

## ward-kdl-write ops forgejo page edit

`PATCH /repos/{owner}/{repo}/wiki/page/{pageName}`

Authorized by grant: can edit "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<pageName>` (string)

Options (3):

- `--content_base64` (string, optional): content must be base64 encoded
- `--message` (string, optional): optional commit message summarizing the change
- `--title` (string, optional): page title. leave empty to keep unchanged

## ward-kdl-write ops forgejo pin edit

`PATCH /repos/{owner}/{repo}/issues/{index}/pin/{position}`

Authorized by grant: can edit "*". Not destructive.

Positional arguments (4):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)
- `<position>` (string)

## ward-kdl-write ops forgejo public_member edit

`PUT /orgs/{org}/public_members/{username}`

Authorized by grant: can edit "*". Not destructive.

Positional arguments (2):

- `<org>` (string)
- `<username>` (string)

## ward-kdl-write ops forgejo pull edit

`PATCH /repos/{owner}/{repo}/pulls/{index}`

Authorized by grant: can edit "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

Options (11):

- `--allow_maintainer_edit` (boolean, optional)
- `--assignee` (string, optional)
- `--assignees` ([]string, optional)
- `--base` (string, optional)
- `--body` (string, optional)
- `--due_date` (string, optional)
- `--labels` ([]integer, optional)
- `--milestone` (integer, optional)
- `--state` (string, optional)
- `--title` (string, optional)
- `--unset_due_date` (boolean, optional)

## ward-kdl-write ops forgejo release edit

`PATCH /repos/{owner}/{repo}/releases/{id}`

Authorized by grant: can edit "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<id>` (string)

Options (7):

- `--body` (string, optional)
- `--draft` (boolean, optional)
- `--hide_archive_links` (boolean, optional)
- `--name` (string, optional)
- `--prerelease` (boolean, optional)
- `--tag_name` (string, optional)
- `--target_commitish` (string, optional)

## ward-kdl-write ops forgejo repo edit

`PATCH /repos/{owner}/{repo}`

Authorized by grant: can edit "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (31):

- `--allow_fast_forward_only_merge` (boolean, optional): either `true` to allow fast-forward-only merging pull requests, or `false` to prevent fast-forward-only merging.
- `--allow_manual_merge` (boolean, optional): either `true` to allow mark pr as merged manually, or `false` to prevent it.
- `--allow_merge_commits` (boolean, optional): either `true` to allow merging pull requests with a merge commit, or `false` to prevent merging pull requests with merge commits.
- `--allow_rebase` (boolean, optional): either `true` to allow rebase-merging pull requests, or `false` to prevent rebase-merging.
- `--allow_rebase_explicit` (boolean, optional): either `true` to allow rebase with explicit merge commits (--no-ff), or `false` to prevent rebase with explicit merge commits.
- `--allow_rebase_update` (boolean, optional): either `true` to allow updating pull request branch by rebase, or `false` to prevent it.
- `--allow_squash_merge` (boolean, optional): either `true` to allow squash-merging pull requests, or `false` to prevent squash-merging.
- `--archived` (boolean, optional): set to `true` to archive this repository.
- `--autodetect_manual_merge` (boolean, optional): either `true` to enable AutodetectManualMerge, or `false` to prevent it. Note: In some special cases, misjudgments can occur.
- `--default_allow_maintainer_edit` (boolean, optional): set to `true` to allow edits from maintainers by default
- `--default_branch` (string, optional): sets the default branch for this repository.
- `--default_delete_branch_after_merge` (boolean, optional): set to `true` to delete pr branch after merge by default
- `--default_merge_style` (string, optional): set to a merge style to be used by this repository: "merge", "rebase", "rebase-merge", "squash", "fast-forward-only", "manually-merged", or "rebase-update-only".
- `--default_update_style` (string, optional): set to a update style to be used by this repository: "rebase" or "merge"
- `--description` (string, optional): a short description of the repository.
- `--enable_prune` (boolean, optional): enable prune - remove obsolete remote-tracking references when mirroring
- `--globally_editable_wiki` (boolean, optional): set the globally editable state of the wiki
- `--has_actions` (boolean, optional): either `true` to enable actions unit, or `false` to disable them.
- `--has_issues` (boolean, optional): either `true` to enable issues for this repository or `false` to disable them.
- `--has_packages` (boolean, optional): either `true` to enable packages unit, or `false` to disable them.
- `--has_projects` (boolean, optional): either `true` to enable project unit, or `false` to disable them.
- `--has_pull_requests` (boolean, optional): either `true` to allow pull requests, or `false` to prevent pull request.
- `--has_releases` (boolean, optional): either `true` to enable releases unit, or `false` to disable them.
- `--has_wiki` (boolean, optional): either `true` to enable the wiki for this repository or `false` to disable it.
- `--ignore_whitespace_conflicts` (boolean, optional): either `true` to ignore whitespace for conflicts, or `false` to not ignore whitespace.
- `--mirror_interval` (string, optional): set to a string like `8h30m0s` to set the mirror interval time
- `--name` (string, optional): name of the repository
- `--private` (boolean, optional): either `true` to make the repository private or `false` to make it public.
Note: you will get a 422 error if the organization restricts changing repository visibility to organization
owners and a non-owner tries to change the value of private.
- `--template` (boolean, optional): either `true` to make this repository a template or `false` to make it a normal repository
- `--website` (string, optional): a URL with more information about the repository.
- `--wiki_branch` (string, optional): sets the branch used for this repository's wiki.

## ward-kdl-write ops forgejo rule edit

`PATCH /admin/quota/rules/{quotarule}`

Authorized by grant: can edit "*". Not destructive.

Positional arguments (1):

- `<quotarule>` (string)

Options (2):

- `--limit` (integer, optional): The limit set by the rule
- `--subjects` ([]string, optional): The subjects affected by the rule

## ward-kdl-write ops forgejo secret edit

`PUT /user/actions/secrets/{secretname}`

Authorized by grant: can edit "*". Not destructive.

Positional arguments (1):

- `<secretname>` (string)

Options (1):

- `--data` (string, required): Data of the secret. Special characters will be retained. Line endings will be normalized to LF to match the
behaviour of browsers. Encode the data with Base64 if line endings should be retained.

## ward-kdl-write ops forgejo starred edit

`PUT /user/starred/{owner}/{repo}`

Authorized by grant: can edit "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-write ops forgejo subscription edit

`PUT /repos/{owner}/{repo}/issues/{index}/subscriptions/{user}`

Authorized by grant: can edit "*". Not destructive.

Positional arguments (4):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)
- `<user>` (string)

## ward-kdl-write ops forgejo tag edit

`PATCH /repos/{owner}/{repo}/tag_protections/{id}`

Authorized by grant: can edit "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<id>` (string)

Options (3):

- `--name_pattern` (string, optional)
- `--whitelist_teams` ([]string, optional)
- `--whitelist_usernames` ([]string, optional)

## ward-kdl-write ops forgejo tag_protection edit

`PATCH /repos/{owner}/{repo}/tag_protections/{id}`

Authorized by grant: can edit "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<id>` (string)

Options (3):

- `--name_pattern` (string, optional)
- `--whitelist_teams` ([]string, optional)
- `--whitelist_usernames` ([]string, optional)

## ward-kdl-write ops forgejo team edit

`PATCH /teams/{id}`

Authorized by grant: can edit "*". Not destructive.

Positional arguments (1):

- `<id>` (string)

Options (6):

- `--can_create_org_repo` (boolean, optional)
- `--description` (string, optional)
- `--includes_all_repositories` (boolean, optional)
- `--name` (string, required)
- `--permission` (string, optional)
- `--units` ([]string, optional)

## ward-kdl-write ops forgejo thread edit

`PATCH /notifications/threads/{id}`

Authorized by grant: can edit "*". Not destructive.

Positional arguments (1):

- `<id>` (string)

Options (1):

- `--to-status` (string, optional): Status to mark notifications as

## ward-kdl-write ops forgejo topic edit

`PUT /repos/{owner}/{repo}/topics/{topic}`

Authorized by grant: can edit "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<topic>` (string)

## ward-kdl-write ops forgejo unblock edit

`PUT /user/unblock/{username}`

Authorized by grant: can edit "*". Not destructive.

Positional arguments (1):

- `<username>` (string)

## ward-kdl-write ops forgejo user edit

`PATCH /admin/users/{username}`

Authorized by grant: can edit "*". Not destructive.

Positional arguments (1):

- `<username>` (string)

Options (20):

- `--active` (boolean, optional)
- `--admin` (boolean, optional)
- `--allow_create_organization` (boolean, optional)
- `--allow_git_hook` (boolean, optional)
- `--allow_import_local` (boolean, optional)
- `--description` (string, optional)
- `--email` (string, optional)
- `--full_name` (string, optional)
- `--hide_email` (boolean, optional)
- `--location` (string, optional)
- `--login_name` (string, optional)
- `--max_repo_creation` (integer, optional)
- `--must_change_password` (boolean, optional)
- `--password` (string, optional)
- `--prohibit_login` (boolean, optional)
- `--pronouns` (string, optional)
- `--restricted` (boolean, optional)
- `--source_id` (integer, optional)
- `--visibility` (string, optional)
- `--website` (string, optional)

## ward-kdl-write ops forgejo variable edit

`PUT /user/actions/variables/{variablename}`

Authorized by grant: can edit "*". Not destructive.

Positional arguments (1):

- `<variablename>` (string)

Options (2):

- `--name` (string, optional): New name for the variable. If the field is empty, the variable name won't be updated. Forgejo will convert it to
uppercase.
- `--value` (string, required): Value of the variable to update. Special characters will be retained. Line endings will be normalized to LF to
match the behaviour of browsers. Encode the data with Base64 if line endings should be retained.

## Scope restrictions

Every verb whose path carries one of these parameters must supply a value matching a glob below, or it fails closed.

- `owner` must match: coily*
