# ward-kdl ops forgejo

Spec-driven CLI. Every verb issues an HTTP request against the API base https://forgejo.coilysiren.me/api/v1.

Authenticates with the "Authorization" header (scheme header-token), reading the token from SSM at /forgejo/api-token. The token value is never shown.

## ward-kdl ops forgejo org get

`GET /orgs/{org}`

Authorized by grant: can read orgs. Not destructive.

Positional arguments (1):

- `<org>` (string)

## ward-kdl ops forgejo org list

`GET /orgs`

Authorized by grant: can list orgs. Not destructive.

Options (2):

- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl ops forgejo org create

`POST /orgs`

Authorized by grant: can create orgs. Not destructive.

Options (8):

- `--description` (string, optional)
- `--email` (string, optional)
- `--full_name` (string, optional)
- `--location` (string, optional)
- `--repo_admin_change_team_access` (boolean, optional)
- `--username` (string, required)
- `--visibility` (string, optional): possible values are `public` (default), `limited` or `private`
- `--website` (string, optional)

## ward-kdl ops forgejo org delete

`DELETE /orgs/{org}`

Authorized by grant: can delete orgs. Destructive - mutates irreversibly.

Positional arguments (1):

- `<org>` (string)

## ward-kdl ops forgejo label get

`GET /repos/{owner}/{repo}/labels/{id}`

Authorized by grant: can read labels. Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<id>` (string)

## ward-kdl ops forgejo label list

`GET /repos/{owner}/{repo}/labels`

Authorized by grant: can list labels. Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (3):

- `--sort` (string, optional): Specifies the sorting method: mostissues, leastissues, or reversealphabetically.
- `--page` (integer, optional): page number of results to return (1-based)
- `--limit` (integer, optional): page size of results

## ward-kdl ops forgejo label create

`POST /repos/{owner}/{repo}/labels`

Authorized by grant: can create labels. Not destructive.

Positional arguments (2):

- `<owner>` (string)
- `<repo>` (string)

Options (5):

- `--color` (string, required)
- `--description` (string, optional)
- `--exclusive` (boolean, optional)
- `--is_archived` (boolean, optional)
- `--name` (string, required)

## ward-kdl ops forgejo label edit

`PATCH /repos/{owner}/{repo}/labels/{id}`

Authorized by grant: can edit labels. Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<id>` (string)

Options (5):

- `--color` (string, optional)
- `--description` (string, optional)
- `--exclusive` (boolean, optional)
- `--is_archived` (boolean, optional)
- `--name` (string, optional)

## ward-kdl ops forgejo label delete

`DELETE /repos/{owner}/{repo}/labels/{id}`

Authorized by grant: can delete labels. Destructive - mutates irreversibly.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<id>` (string)

## ward-kdl ops forgejo milestone get

`GET /repos/{owner}/{repo}/milestones/{id}`

Authorized by grant: can read milestones. Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<id>` (string)

## ward-kdl ops forgejo milestone list

`GET /repos/{owner}/{repo}/milestones`

Authorized by grant: can list milestones. Not destructive.

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

Authorized by grant: can create milestones. Not destructive.

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

Authorized by grant: can edit milestones. Not destructive.

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

Authorized by grant: can delete milestones. Destructive - mutates irreversibly.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<id>` (string)

## ward-kdl ops forgejo issue create

`POST /repos/{owner}/{repo}/issues`

Authorized by grant: can create issues. Not destructive.

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

## ward-kdl ops forgejo issue list

`GET /repos/{owner}/{repo}/issues`

Authorized by grant: can list issues. Not destructive.

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

## ward-kdl ops forgejo issue view

`GET /repos/{owner}/{repo}/issues/{index}`

Authorized by grant: can view issues. Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

## ward-kdl ops forgejo issue edit

`PATCH /repos/{owner}/{repo}/issues/{index}`

Authorized by grant: can edit issues. Not destructive.

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

Authorized by grant: can comment issues. Not destructive.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

Options (2):

- `--body` (string, required): The body of the comment
- `--updated_at` (string, optional): The time of the comment's update, needs admin or repository owner permission

## ward-kdl ops forgejo issue close

`PATCH /repos/{owner}/{repo}/issues/{index}`

Authorized by grant: can close issues. Not destructive.

Always sends the fixed body {"state": "closed"}; takes no body flags.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

## ward-kdl ops forgejo issue reopen

`PATCH /repos/{owner}/{repo}/issues/{index}`

Authorized by grant: can reopen issues. Not destructive.

Always sends the fixed body {"state": "open"}; takes no body flags.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)

## ward-kdl ops forgejo issue delete

`DELETE /repos/{owner}/{repo}/issues/{index}`

Authorized by grant: can delete issues. Destructive - mutates irreversibly.

Positional arguments (3):

- `<owner>` (string)
- `<repo>` (string)
- `<index>` (string)
