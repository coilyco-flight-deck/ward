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
