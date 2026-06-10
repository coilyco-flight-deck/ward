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

Takes no arguments.

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
