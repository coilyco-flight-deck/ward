# ward-kdl ops glitchtip

Spec-driven CLI. Every verb issues an HTTP request against the API base (resolved from ssm /glitchtip/base-url).

Authenticates with the "Authorization" header (scheme bearer), reading the token from ssm /glitchtip/api-token. The token value is never shown.

## ward-kdl ops glitchtip organization get

`GET /api/0/organizations/{organization_slug}/`

Authorized by grant: can get organization. Not destructive.

Positional arguments (1):

- `<organization_slug>` (string)

## ward-kdl ops glitchtip organization list

`GET /api/0/organizations/`

Authorized by grant: can list organization. Not destructive.

Takes no arguments.

## ward-kdl ops glitchtip team get

`GET /api/0/teams/{organization_slug}/{team_slug}/`

Authorized by grant: can get team. Not destructive.

Positional arguments (2):

- `<organization_slug>` (string)
- `<team_slug>` (string)

## ward-kdl ops glitchtip team list

`GET /api/0/organizations/{organization_slug}/teams/`

Authorized by grant: can list team. Not destructive.

Positional arguments (1):

- `<organization_slug>` (string)

## ward-kdl ops glitchtip team create

`POST /api/0/organizations/{organization_slug}/teams/`

Authorized by grant: can create team. Not destructive.

Positional arguments (1):

- `<organization_slug>` (string)

Options (1):

- `--slug` (string, required)

## ward-kdl ops glitchtip project get

`GET /api/0/projects/{organization_slug}/{project_slug}/`

Authorized by grant: can get project. Not destructive.

Positional arguments (2):

- `<organization_slug>` (string)
- `<project_slug>` (string)

## ward-kdl ops glitchtip project list

`GET /api/0/projects/`

Authorized by grant: can list project. Not destructive.

Takes no arguments.

## ward-kdl ops glitchtip project create

`POST /api/0/teams/{organization_slug}/{team_slug}/projects/`

Authorized by grant: can create project. Not destructive.

Positional arguments (2):

- `<organization_slug>` (string)
- `<team_slug>` (string)

Options (1):

- `--name` (string, required)

## ward-kdl ops glitchtip project edit

`PUT /api/0/projects/{organization_slug}/{project_slug}/`

Authorized by grant: can edit project. Not destructive.

Positional arguments (2):

- `<organization_slug>` (string)
- `<project_slug>` (string)

Options (1):

- `--name` (string, required)

## ward-kdl ops glitchtip project delete - irreversible: deletes the project and all of its events

`DELETE /api/0/projects/{organization_slug}/{project_slug}/`

Authorized by grant: can delete project. Destructive - mutates irreversibly.

Positional arguments (2):

- `<organization_slug>` (string)
- `<project_slug>` (string)

## ward-kdl ops glitchtip project-key list

`GET /api/0/projects/{organization_slug}/{project_slug}/keys/`

Authorized by grant: can list project-key. Not destructive.

Positional arguments (2):

- `<organization_slug>` (string)
- `<project_slug>` (string)

## ward-kdl ops glitchtip project-key get

`GET /api/0/projects/{organization_slug}/{project_slug}/keys/{key_id}/`

Authorized by grant: can get project-key. Not destructive.

Positional arguments (3):

- `<organization_slug>` (string)
- `<project_slug>` (string)
- `<key_id>` (string)

## ward-kdl ops glitchtip project-key create

`POST /api/0/projects/{organization_slug}/{project_slug}/keys/`

Authorized by grant: can create project-key. Not destructive.

Positional arguments (2):

- `<organization_slug>` (string)
- `<project_slug>` (string)

## ward-kdl ops glitchtip project-key delete

`DELETE /api/0/projects/{organization_slug}/{project_slug}/keys/{key_id}/`

Authorized by grant: can delete project-key. Destructive - mutates irreversibly.

Positional arguments (3):

- `<organization_slug>` (string)
- `<project_slug>` (string)
- `<key_id>` (string)

## ward-kdl ops glitchtip issue list

`GET /api/0/organizations/{organization_slug}/issues/`

Authorized by grant: can list issue. Not destructive.

Positional arguments (1):

- `<organization_slug>` (string)

Options (1):

- `--sort` (string, optional)

## ward-kdl ops glitchtip issue get

`GET /api/0/issues/{issue_id}/`

Authorized by grant: can get issue. Not destructive.

Positional arguments (1):

- `<issue_id>` (string)

## ward-kdl ops glitchtip event list

`GET /api/0/issues/{issue_id}/events/`

Authorized by grant: can list event. Not destructive.

Positional arguments (1):

- `<issue_id>` (string)

## ward-kdl ops glitchtip event get

`GET /api/0/issues/{issue_id}/events/{event_id}/`

Authorized by grant: can get event. Not destructive.

Positional arguments (2):

- `<issue_id>` (string)
- `<event_id>` (string)

## ward-kdl ops glitchtip action provision-project - Create a project under a team and mint its DSN in one shot.

Complex action. Runs 2 granted calls in order, threading $step.field data between them:

1. `POST /api/0/teams/{organization_slug}/{team_slug}/projects/` - binds the response as `project`
2. `POST /api/0/projects/{organization_slug}/{project_slug}/keys/` - binds the response as `key`

## Condition language

The `until` and `fail-when` expressions above are [JMESPath, Community Edition](https://jmespath.site), evaluated against the polled response as the root. A `$name` is a bound input or `as` capture, supplied through the Community Edition's variable scope - baseline JMESPath (https://jmespath.org) has no `$variable` syntax, so these expressions are not portable to an original-spec evaluator.

## Denied operations

### ward-kdl ops glitchtip organization create (denied)

org creation is human-only; do it in the UI on first login

### ward-kdl ops glitchtip organization delete (denied)

org deletion is irreversible and human-only

### ward-kdl ops glitchtip event store (denied)

event ingest is the SDK's job, not ward
