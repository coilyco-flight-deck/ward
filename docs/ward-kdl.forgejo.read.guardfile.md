# ward-kdl-read ops forgejo

Spec-driven CLI. Every verb issues an HTTP request against the API base https://forgejo.coilysiren.me/api/v1.

Authenticates with the "Authorization" header (scheme header-token), reading the token from ssm /forgejo/coilyco-ops/api-token. The token value is never shown.

## ward-kdl-read ops forgejo activitie get

`GET /activitypub/user-id/{user-id}/activities/{activity-id}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (2):

- `<user-id>` (string)
- `<activity-id>` (string)

## ward-kdl-read ops forgejo archive get

`GET /repos/{owner}/{repo}/archive/{archive}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<archive>` (string)

## ward-kdl-read ops forgejo blob get

`GET /repos/{owner}/{repo}/git/blobs/{sha}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<sha>` (string)

## ward-kdl-read ops forgejo branch_protection get

`GET /repos/{owner}/{repo}/branch_protections/{name}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<name>` (string)

## ward-kdl-read ops forgejo branche get

`GET /repos/{owner}/{repo}/branches/{branch}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<branch>` (string)

## ward-kdl-read ops forgejo collaborator get

`GET /repos/{owner}/{repo}/collaborators/{collaborator}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<collaborator>` (string)

## ward-kdl-read ops forgejo comment get

`GET /repos/{owner}/{repo}/issues/comments/{id}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<id>` (string)

## ward-kdl-read ops forgejo compare get

`GET /repos/{owner}/{repo}/compare/{basehead}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<basehead>` (string)

## ward-kdl-read ops forgejo content get

`GET /repos/{owner}/{repo}/contents/{filepath}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<filepath>` (string)

Options (1):

- `--ref` (string, optional): The name of the commit/branch/tag. Default the repository’s default branch (usually master)

## ward-kdl-read ops forgejo editorconfig get

`GET /repos/{owner}/{repo}/editorconfig/{filepath}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<filepath>` (string)

Options (1):

- `--ref` (string, optional): The name of the commit/branch/tag. Default the repository’s default branch (usually master)

## ward-kdl-read ops forgejo flag get

`GET /repos/{owner}/{repo}/flags/{flag}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<flag>` (string)

## ward-kdl-read ops forgejo following get

`GET /user/following/{username}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (1):

- `<username>` (string)

## ward-kdl-read ops forgejo git get

`GET /repos/{owner}/{repo}/hooks/git/{id}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<id>` (string)

## ward-kdl-read ops forgejo gpg_key get

`GET /user/gpg_keys/{id}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (1):

- `<id>` (string)

## ward-kdl-read ops forgejo group get

`GET /admin/quota/groups/{quotagroup}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (1):

- `<quotagroup>` (string)

## ward-kdl-read ops forgejo issue get

`GET /repos/{owner}/{repo}/issues/{index}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

## ward-kdl-read ops forgejo key get

`GET /user/keys/{id}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (1):

- `<id>` (string)

## ward-kdl-read ops forgejo label get

`GET /orgs/{org}/labels/{id}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (2):

- `<org>` (string)
- `<id>` (string)

## ward-kdl-read ops forgejo license get

`GET /licenses/{name}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (1):

- `<name>` (string)

## ward-kdl-read ops forgejo media get

`GET /repos/{owner}/{repo}/media/{filepath}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<filepath>` (string)

Options (1):

- `--ref` (string, optional): The name of the commit/branch/tag. Default the repository’s default branch (usually master)

## ward-kdl-read ops forgejo milestone get

`GET /repos/{owner}/{repo}/milestones/{id}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<id>` (string)

## ward-kdl-read ops forgejo note get

`GET /repos/{owner}/{repo}/git/notes/{sha}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<sha>` (string)

Options (2):

- `--verification` (boolean, optional): include verification for every commit (disable for speedup, default 'true')
- `--files` (boolean, optional): include a list of affected files for every commit (disable for speedup, default 'true')

## ward-kdl-read ops forgejo oauth2 get

`GET /user/applications/oauth2/{id}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (1):

- `<id>` (string)

## ward-kdl-read ops forgejo org get

`GET /orgs/{org}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (1):

- `<org>` (string)

## ward-kdl-read ops forgejo package get

`GET /packages/{owner}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (1):

- `<owner>` (string)

Options (4):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results
- `--type` (string, optional): package type filter
- `--q` (string, optional): name filter

## ward-kdl-read ops forgejo page get

`GET /repos/{owner}/{repo}/wiki/page/{pageName}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<pageName>` (string)

## ward-kdl-read ops forgejo public_member get

`GET /orgs/{org}/public_members/{username}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (2):

- `<org>` (string)
- `<username>` (string)

## ward-kdl-read ops forgejo push_mirror get

`GET /repos/{owner}/{repo}/push_mirrors/{name}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<name>` (string)

## ward-kdl-read ops forgejo raw get

`GET /repos/{owner}/{repo}/raw/{filepath}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<filepath>` (string)

Options (1):

- `--ref` (string, optional): The name of the commit/branch/tag. Default the repository’s default branch (usually master)

## ward-kdl-read ops forgejo ref get

`GET /repos/{owner}/{repo}/git/refs/{ref}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<ref>` (string)

## ward-kdl-read ops forgejo release get

`GET /repos/{owner}/{repo}/releases/{id}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<id>` (string)

## ward-kdl-read ops forgejo repo get

`GET /repos/{owner}/{repo}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-read ops forgejo repositorie get

`GET /repositories/{id}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (1):

- `<id>` (string)

## ward-kdl-read ops forgejo review get

`GET /repos/{owner}/{repo}/pulls/{index}/reviews/{id}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (4):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)
- `<id>` (string)

## ward-kdl-read ops forgejo revision get

`GET /repos/{owner}/{repo}/wiki/revisions/{pageName}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<pageName>` (string)

Options (1):

- `--page` (integer, optional): page number of results to return (1-based)

## ward-kdl-read ops forgejo rule get

`GET /admin/quota/rules/{quotarule}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (1):

- `<quotarule>` (string)

## ward-kdl-read ops forgejo run get

`GET /repos/{owner}/{repo}/actions/runs/{run_id}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<run_id>` (string)

## ward-kdl-read ops forgejo starred get

`GET /user/starred/{owner}/{repo}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-read ops forgejo statuse get

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

## ward-kdl-read ops forgejo sync_fork get

`GET /repos/{owner}/{repo}/sync_fork/{branch}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<branch>` (string)

## ward-kdl-read ops forgejo tag get

`GET /repos/{owner}/{repo}/tags/{tag}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<tag>` (string)

## ward-kdl-read ops forgejo tag_protection get

`GET /repos/{owner}/{repo}/tag_protections/{id}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<id>` (string)

## ward-kdl-read ops forgejo team get

`GET /teams/{id}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (1):

- `<id>` (string)

## ward-kdl-read ops forgejo thread get

`GET /notifications/threads/{id}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (1):

- `<id>` (string)

## ward-kdl-read ops forgejo time get

`GET /repos/{owner}/{repo}/times/{user}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<user>` (string)

## ward-kdl-read ops forgejo tree get

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

## ward-kdl-read ops forgejo user get

`GET /users/{username}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (1):

- `<username>` (string)

## ward-kdl-read ops forgejo variable get

`GET /user/actions/variables/{variablename}`

Authorized by grant: can get "*". Not destructive.

Positional arguments (1):

- `<variablename>` (string)

## ward-kdl-read ops forgejo activity list

`GET /activitypub/user-id/{user-id}/activities/{activity-id}/activity`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<user-id>` (string)
- `<activity-id>` (string)

## ward-kdl-read ops forgejo actor list

`GET /activitypub/actor`

Authorized by grant: can list "*". Not destructive.

Takes no arguments.

## ward-kdl-read ops forgejo api list

`GET /settings/api`

Authorized by grant: can list "*". Not destructive.

Takes no arguments.

## ward-kdl-read ops forgejo artifact list

`GET /user/quota/artifacts`

Authorized by grant: can list "*". Not destructive.

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo assignee list

`GET /repos/{owner}/{repo}/assignees`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-read ops forgejo attachment list

`GET /user/quota/attachments`

Authorized by grant: can list "*". Not destructive.

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo blob list

`GET /repos/{owner}/{repo}/git/blobs`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (1):

- `--shas` (string, required): a comma separated list of blob-sha (mind the overall URL-length limit of ~2,083 chars)

## ward-kdl-read ops forgejo block list

`GET /repos/{owner}/{repo}/issues/{index}/blocks`

Authorized by grant: can list "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo branch_protection list

`GET /repos/{owner}/{repo}/branch_protections`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-read ops forgejo branche list

`GET /repos/{owner}/{repo}/branches`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo check list

`GET /user/quota/check`

Authorized by grant: can list "*". Not destructive.

Options (1):

- `--subject` (string, required): subject of the quota

## ward-kdl-read ops forgejo collaborator list

`GET /repos/{owner}/{repo}/collaborators`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo comment list

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

## ward-kdl-read ops forgejo commit list

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

## ward-kdl-read ops forgejo content list

`GET /repos/{owner}/{repo}/contents`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (1):

- `--ref` (string, optional): The name of the commit/branch/tag. Default the repository’s default branch (usually master)

## ward-kdl-read ops forgejo cron list

`GET /admin/cron`

Authorized by grant: can list "*". Not destructive.

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo dependencie list

`GET /repos/{owner}/{repo}/issues/{index}/dependencies`

Authorized by grant: can list "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo flag list

`GET /repos/{owner}/{repo}/flags`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-read ops forgejo follower list

`GET /user/followers`

Authorized by grant: can list "*". Not destructive.

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo following list

`GET /user/following`

Authorized by grant: can list "*". Not destructive.

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo fork list

`GET /repos/{owner}/{repo}/forks`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo git list

`GET /repos/{owner}/{repo}/hooks/git`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-read ops forgejo gpg_key list

`GET /user/gpg_keys`

Authorized by grant: can list "*". Not destructive.

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo gpg_key_token list

`GET /user/gpg_key_token`

Authorized by grant: can list "*". Not destructive.

Takes no arguments.

## ward-kdl-read ops forgejo group list

`GET /admin/quota/groups`

Authorized by grant: can list "*". Not destructive.

Takes no arguments.

## ward-kdl-read ops forgejo heatmap list

`GET /users/{username}/heatmap`

Authorized by grant: can list "*". Not destructive.

Positional arguments (1):

- `<username>` (string)

## ward-kdl-read ops forgejo issue list

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

## ward-kdl-read ops forgejo issue_config list

`GET /repos/{owner}/{repo}/issue_config`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-read ops forgejo issue_template list

`GET /repos/{owner}/{repo}/issue_templates`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-read ops forgejo job list

`GET /admin/runners/jobs`

Authorized by grant: can list "*". Not destructive.

Options (1):

- `--labels` (string, optional): a comma separated list of run job labels to search for

## ward-kdl-read ops forgejo key list

`GET /user/keys`

Authorized by grant: can list "*". Not destructive.

Options (3):

- `--fingerprint` (string, optional): fingerprint of the key
- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo label list

`GET /orgs/{org}/labels`

Authorized by grant: can list "*". Not destructive.

Positional arguments (1):

- `<org>` (string)

Options (3):

- `--sort` (string, optional): Specifies the sorting method: mostissues, leastissues, or reversealphabetically.
- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo language list

`GET /repos/{owner}/{repo}/languages`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-read ops forgejo latest list

`GET /repos/{owner}/{repo}/releases/latest`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-read ops forgejo license list

`GET /licenses`

Authorized by grant: can list "*". Not destructive.

Takes no arguments.

## ward-kdl-read ops forgejo list_blocked list

`GET /user/list_blocked`

Authorized by grant: can list "*". Not destructive.

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo merge list

`GET /repos/{owner}/{repo}/pulls/{index}/merge`

Authorized by grant: can list "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

## ward-kdl-read ops forgejo milestone list

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

## ward-kdl-read ops forgejo new list

`GET /notifications/new`

Authorized by grant: can list "*". Not destructive.

Takes no arguments.

## ward-kdl-read ops forgejo new_pin_allowed list

`GET /repos/{owner}/{repo}/new_pin_allowed`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-read ops forgejo nodeinfo list

`GET /nodeinfo`

Authorized by grant: can list "*". Not destructive.

Takes no arguments.

## ward-kdl-read ops forgejo notification list

`GET /notifications`

Authorized by grant: can list "*". Not destructive.

Options (5):

- `--all` (boolean, optional): If true, show notifications marked as read. Default value is false
- `--since` (string, optional): Only show notifications updated after the given time. This is a timestamp in RFC 3339 format
- `--before` (string, optional): Only show notifications updated before the given time. This is a timestamp in RFC 3339 format
- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo oauth2 list

`GET /user/applications/oauth2`

Authorized by grant: can list "*". Not destructive.

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo org list

`GET /orgs`

Authorized by grant: can list "*". Not destructive.

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo outbox list

`GET /activitypub/user-id/{user-id}/outbox`

Authorized by grant: can list "*". Not destructive.

Positional arguments (1):

- `<user-id>` (string)

## ward-kdl-read ops forgejo package list

`GET /user/quota/packages`

Authorized by grant: can list "*". Not destructive.

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo page list

`GET /repos/{owner}/{repo}/wiki/pages`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo permission list

`GET /users/{username}/orgs/{org}/permissions`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<username>` (string)
- `<org>` (string)

## ward-kdl-read ops forgejo public_member list

`GET /orgs/{org}/public_members`

Authorized by grant: can list "*". Not destructive.

Positional arguments (1):

- `<org>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo pull list

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

## ward-kdl-read ops forgejo push_mirror list

`GET /repos/{owner}/{repo}/push_mirrors`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo quota list

`GET /user/quota`

Authorized by grant: can list "*". Not destructive.

Takes no arguments.

## ward-kdl-read ops forgejo reaction list

`GET /repos/{owner}/{repo}/issues/{index}/reactions`

Authorized by grant: can list "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo ref list

`GET /repos/{owner}/{repo}/git/refs`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-read ops forgejo release list

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

## ward-kdl-read ops forgejo repo list

`GET /user/repos`

Authorized by grant: can list "*". Not destructive.

Options (3):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results
- `--order_by` (string, optional): order the repositories

## ward-kdl-read ops forgejo repository list

`GET /settings/repository`

Authorized by grant: can list "*". Not destructive.

Takes no arguments.

## ward-kdl-read ops forgejo review list

`GET /repos/{owner}/{repo}/pulls/{index}/reviews`

Authorized by grant: can list "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo reviewer list

`GET /repos/{owner}/{repo}/reviewers`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-read ops forgejo rule list

`GET /admin/quota/rules`

Authorized by grant: can list "*". Not destructive.

Takes no arguments.

## ward-kdl-read ops forgejo run list

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

## ward-kdl-read ops forgejo secret list

`GET /orgs/{org}/actions/secrets`

Authorized by grant: can list "*". Not destructive.

Positional arguments (1):

- `<org>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo setting list

`GET /user/settings`

Authorized by grant: can list "*". Not destructive.

Takes no arguments.

## ward-kdl-read ops forgejo stargazer list

`GET /repos/{owner}/{repo}/stargazers`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo starred list

`GET /user/starred`

Authorized by grant: can list "*". Not destructive.

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo statu list

`GET /repos/{owner}/{repo}/commits/{ref}/status`

Authorized by grant: can list "*". Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<ref>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo statuse list

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

## ward-kdl-read ops forgejo stopwatche list

`GET /user/stopwatches`

Authorized by grant: can list "*". Not destructive.

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo subscriber list

`GET /repos/{owner}/{repo}/subscribers`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo subscription list

`GET /user/subscriptions`

Authorized by grant: can list "*". Not destructive.

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo sync_fork list

`GET /repos/{owner}/{repo}/sync_fork`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-read ops forgejo tag list

`GET /repos/{owner}/{repo}/tags`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results, default maximum page size is 50

## ward-kdl-read ops forgejo tag_protection list

`GET /repos/{owner}/{repo}/tag_protections`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-read ops forgejo task list

`GET /repos/{owner}/{repo}/actions/tasks`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results, default maximum page size is 50

## ward-kdl-read ops forgejo team list

`GET /user/teams`

Authorized by grant: can list "*". Not destructive.

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo time list

`GET /user/times`

Authorized by grant: can list "*". Not destructive.

Options (4):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results
- `--since` (string, optional): Only show times updated after the given time. This is a timestamp in RFC 3339 format
- `--before` (string, optional): Only show times updated before the given time. This is a timestamp in RFC 3339 format

## ward-kdl-read ops forgejo timeline list

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

## ward-kdl-read ops forgejo token list

`GET /users/{username}/tokens`

Authorized by grant: can list "*". Not destructive.

Positional arguments (1):

- `<username>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo topic list

`GET /repos/{owner}/{repo}/topics`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo ui list

`GET /settings/ui`

Authorized by grant: can list "*". Not destructive.

Takes no arguments.

## ward-kdl-read ops forgejo unadopted list

`GET /admin/unadopted`

Authorized by grant: can list "*". Not destructive.

Options (3):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results
- `--pattern` (string, optional): pattern of repositories to search for

## ward-kdl-read ops forgejo user list

`GET /admin/users`

Authorized by grant: can list "*". Not destructive.

Options (5):

- `--source_id` (integer, optional): ID of the user's login source to search for
- `--login_name` (string, optional): user's login name to search for
- `--sort` (string, optional): sort order of results
- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo validate list

`GET /repos/{owner}/{repo}/issue_config/validate`

Authorized by grant: can list "*". Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl-read ops forgejo variable list

`GET /user/actions/variables`

Authorized by grant: can list "*". Not destructive.

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl-read ops forgejo version list

`GET /version`

Authorized by grant: can list "*". Not destructive.

Takes no arguments.

## Scope restrictions

Every verb whose path carries one of these parameters must supply a value matching a glob below, or it fails closed.

- `owner` must match: coily*
