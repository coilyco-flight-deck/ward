# ward-kdl ops forgejo

Spec-driven CLI. Every verb issues an HTTP request against the API base https://forgejo.coilysiren.me/api/v1.

Authenticates with the "Authorization" header (scheme header-token), reading the token from SSM at /forgejo/api-token. The token value is never shown.

## ward-kdl ops forgejo repo get

`GET /repos/{owner}/{repo}`

Authorized by grant: can get repo. Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl ops forgejo repo search

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

## ward-kdl ops forgejo repo create

`POST /user/repos`

Authorized by grant: can create repo. Not destructive.

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

## ward-kdl ops forgejo repo edit

`PATCH /repos/{owner}/{repo}`

Authorized by grant: can edit repo. Not destructive.

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

## ward-kdl ops forgejo repo delete - irreversible: deletes the repo and all its data

`DELETE /repos/{owner}/{repo}`

Authorized by grant: can delete repo. Destructive - mutates irreversibly.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

## ward-kdl ops forgejo org get

`GET /orgs/{org}`

Authorized by grant: can get org. Not destructive.

Positional arguments (1):

- `<org>` (string)

## ward-kdl ops forgejo org list

`GET /orgs`

Authorized by grant: can list org. Not destructive.

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl ops forgejo org-label get

`GET /orgs/{org}/labels/{id}`

Authorized by grant: can get org-label. Not destructive.

Positional arguments (2):

- `<org>` (string)
- `<id>` (string)

## ward-kdl ops forgejo org-label list

`GET /orgs/{org}/labels`

Authorized by grant: can list org-label. Not destructive.

Positional arguments (1):

- `<org>` (string)

Options (3):

- `--sort` (string, optional): Specifies the sorting method: mostissues, leastissues, or reversealphabetically.
- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl ops forgejo org-label create

`POST /orgs/{org}/labels`

Authorized by grant: can create org-label. Not destructive.

Positional arguments (1):

- `<org>` (string)

Options (5):

- `--color` (string, required)
- `--description` (string, optional)
- `--exclusive` (boolean, optional)
- `--is_archived` (boolean, optional)
- `--name` (string, required)

## ward-kdl ops forgejo org-label edit

`PATCH /orgs/{org}/labels/{id}`

Authorized by grant: can edit org-label. Not destructive.

Positional arguments (2):

- `<org>` (string)
- `<id>` (string)

Options (5):

- `--color` (string, optional)
- `--description` (string, optional)
- `--exclusive` (boolean, optional)
- `--is_archived` (boolean, optional)
- `--name` (string, optional)

## ward-kdl ops forgejo org-label delete

`DELETE /orgs/{org}/labels/{id}`

Authorized by grant: can delete org-label. Destructive - mutates irreversibly.

Positional arguments (2):

- `<org>` (string)
- `<id>` (string)

## ward-kdl ops forgejo milestone get

`GET /repos/{owner}/{repo}/milestones/{id}`

Authorized by grant: can get milestone. Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<id>` (string)

## ward-kdl ops forgejo milestone list

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

## ward-kdl ops forgejo milestone create

`POST /repos/{owner}/{repo}/milestones`

Authorized by grant: can create milestone. Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (4):

- `--description` (string, optional)
- `--due_on` (string, optional)
- `--state` (string, optional)
- `--title` (string, optional)

## ward-kdl ops forgejo milestone edit

`PATCH /repos/{owner}/{repo}/milestones/{id}`

Authorized by grant: can edit milestone. Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<id>` (string)

Options (4):

- `--description` (string, optional)
- `--due_on` (string, optional)
- `--state` (string, optional)
- `--title` (string, optional)

## ward-kdl ops forgejo milestone delete

`DELETE /repos/{owner}/{repo}/milestones/{id}`

Authorized by grant: can delete milestone. Destructive - mutates irreversibly.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<id>` (string)

## ward-kdl ops forgejo milestone close

`PATCH /repos/{owner}/{repo}/milestones/{id}`

Authorized by grant: can close milestone. Not destructive.

Always sends the fixed body {"state": "closed"}; takes no body flags.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<id>` (string)

## ward-kdl ops forgejo milestone reopen

`PATCH /repos/{owner}/{repo}/milestones/{id}`

Authorized by grant: can reopen milestone. Not destructive.

Always sends the fixed body {"state": "open"}; takes no body flags.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<id>` (string)

## ward-kdl ops forgejo issue get

`GET /repos/{owner}/{repo}/issues/{index}`

Authorized by grant: can get issue. Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

## ward-kdl ops forgejo issue view

`GET /repos/{owner}/{repo}/issues/{index}`

Authorized by grant: can view issue. Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

## ward-kdl ops forgejo issue list

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

## ward-kdl ops forgejo issue search

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

## ward-kdl ops forgejo issue create

`POST /repos/{owner}/{repo}/issues`

Authorized by grant: can create issue. Not destructive.

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

## ward-kdl ops forgejo issue edit

`PATCH /repos/{owner}/{repo}/issues/{index}`

Authorized by grant: can edit issue. Not destructive.

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

## ward-kdl ops forgejo issue comment

`POST /repos/{owner}/{repo}/issues/{index}/comments`

Authorized by grant: can comment issue. Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

Options (2):

- `--body` (string, required): The body of the comment
- `--updated_at` (string, optional): The time of the comment's update, needs admin or repository owner permission

## ward-kdl ops forgejo issue close

`PATCH /repos/{owner}/{repo}/issues/{index}`

Authorized by grant: can close issue. Not destructive.

Always sends the fixed body {"state": "closed"}; takes no body flags.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

## ward-kdl ops forgejo issue reopen

`PATCH /repos/{owner}/{repo}/issues/{index}`

Authorized by grant: can reopen issue. Not destructive.

Always sends the fixed body {"state": "open"}; takes no body flags.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

## ward-kdl ops forgejo release get

`GET /repos/{owner}/{repo}/releases/{id}`

Authorized by grant: can get release. Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<id>` (string)

## ward-kdl ops forgejo release list

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

## ward-kdl ops forgejo release create

`POST /repos/{owner}/{repo}/releases`

Authorized by grant: can create release. Not destructive.

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

## ward-kdl ops forgejo release edit

`PATCH /repos/{owner}/{repo}/releases/{id}`

Authorized by grant: can edit release. Not destructive.

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

## ward-kdl ops forgejo release delete

`DELETE /repos/{owner}/{repo}/releases/{id}`

Authorized by grant: can delete release. Destructive - mutates irreversibly.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<id>` (string)

## ward-kdl ops forgejo release upload-asset

`POST /repos/{owner}/{repo}/releases/{id}/assets`

Authorized by grant: can upload-asset release. Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<id>` (string)

Options (3):

- `--name` (string, optional): name of the attachment
- `--attachment` (file, optional): attachment to upload (this parameter is incompatible with `external_url`)
- `--external_url` (string, optional): url to external asset (this parameter is incompatible with `attachment`)

## ward-kdl ops forgejo issue-label list

`GET /repos/{owner}/{repo}/issues/{index}/labels`

Authorized by grant: can list issue-label. Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

## ward-kdl ops forgejo issue-label add

`POST /repos/{owner}/{repo}/issues/{index}/labels`

Authorized by grant: can add issue-label. Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

Options (2):

- `--labels` ([]string, optional): Labels can be a list of integers representing label IDs
or a list of strings representing label names
- `--updated_at` (string, optional)

## ward-kdl ops forgejo issue-label set

`PUT /repos/{owner}/{repo}/issues/{index}/labels`

Authorized by grant: can set issue-label. Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

Options (2):

- `--labels` ([]string, optional): Labels can be a list of integers representing label IDs
or a list of strings representing label names
- `--updated_at` (string, optional)

## ward-kdl ops forgejo issue-label remove

`DELETE /repos/{owner}/{repo}/issues/{index}/labels/{identifier}`

Authorized by grant: can remove issue-label. Not destructive.

Positional arguments (4):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)
- `<identifier>` (string)

Options (1):

- `--updated_at` (string, optional)

## ward-kdl ops forgejo tasks list

`GET /repos/{owner}/{repo}/actions/tasks`

Authorized by grant: can list tasks. Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results, default maximum page size is 50

## ward-kdl ops forgejo action ci-watch - Watch a CI run to completion, then surface failing-job status.

Complex action. Polls `GET /repos/{owner}/{repo}/actions/tasks` every 10s, up to 30m0s, until:

    length(workflow_runs[?run_number==$run && status!='success'
        && status!='failure' && status!='cancelled'
        && status!='skipped']) == `0`

Authorized by grant: can list tasks.

Exits non-zero when:

    length($run_tasks.workflow_runs[?run_number==$run && status=='failure']) > `0`

## ward-kdl ops forgejo action move-issue - Move an issue to another repo (copy title/body, back-link, close source). Never deletes.

Complex action. Runs 4 granted calls in order, threading $step.field data between them:

1. `GET /repos/{owner}/{repo}/issues/{index}` - binds the response as `src`
2. `POST /repos/{owner}/{repo}/issues` - binds the response as `dst`
3. `POST /repos/{owner}/{repo}/issues/{index}/comments`
4. `PATCH /repos/{owner}/{repo}/issues/{index}`

## Condition language

The `until` and `fail-when` expressions above are [JMESPath, Community Edition](https://jmespath.site), evaluated against the polled response as the root. A `$name` is a bound input or `as` capture, supplied through the Community Edition's variable scope - baseline JMESPath (https://jmespath.org) has no `$variable` syntax, so these expressions are not portable to an original-spec evaluator.

## Scope restrictions

Every verb whose path carries one of these parameters must supply a value matching a glob below, or it fails closed.

- `owner` must match: coily*

## Denied operations

### ward-kdl ops forgejo repo fork (denied)

forking is a human-only operation; fork in the web UI

### ward-kdl ops forgejo repo archive (denied)

archive/unarchive flips a repo's lifecycle; do it in the web UI

### ward-kdl ops forgejo repo unarchive (denied)

archive/unarchive flips a repo's lifecycle; do it in the web UI

### ward-kdl ops forgejo org create (denied)

org creation is a human-only operation

### ward-kdl ops forgejo org delete (denied)

org deletion is irreversible and human-only

### ward-kdl ops forgejo issue delete (denied)

issue deletion is irreversible; close it instead (move-issue does this)

### ward-kdl ops forgejo pr view (denied)

pull requests are not exposed through ward; read them in the web UI

### ward-kdl ops forgejo pr list (denied)

pull requests are not exposed through ward; read them in the web UI
