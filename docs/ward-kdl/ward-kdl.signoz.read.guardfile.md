# ward-kdl-read ops signoz

Spec-driven CLI. Every verb issues an HTTP request against the API base (resolved from ssm /coilysiren/signoz-ser8/base-url).

Authenticates with the "SIGNOZ-API-KEY" header (scheme header-token), reading the token from ssm /coilysiren/signoz-ser8/api-token. The token value is never shown.

## ward-kdl-read ops signoz pipeline get - the active log-pipeline set (GET /api/v1/logs/pipelines/latest). op pinned: the path ends in a static /latest, not an item {id}, so convention `get` would not resolve it.

`GET /api/v1/logs/pipelines/latest`

Authorized by grant: can get pipeline. Not destructive.

Takes no arguments.

## ward-kdl-read ops signoz dashboard get - a dashboard's full configuration (GET /api/v1/dashboards/{uuid})

`GET /api/v1/dashboards/{uuid}`

Authorized by grant: can get dashboard. Not destructive.

Positional arguments (1):

- `<uuid>` (string)

## ward-kdl-read ops signoz dashboard list - all dashboards with their summaries (GET /api/v1/dashboards)

`GET /api/v1/dashboards`

Authorized by grant: can list dashboard. Not destructive.

Takes no arguments.

## ward-kdl-read ops signoz rule get - an alert rule (GET /api/v1/rules/{id})

`GET /api/v1/rules/{id}`

Authorized by grant: can get rule. Not destructive.

Positional arguments (1):

- `<id>` (string)

## ward-kdl-read ops signoz rule list - all alert rules (GET /api/v1/rules)

`GET /api/v1/rules`

Authorized by grant: can list rule. Not destructive.

Takes no arguments.
