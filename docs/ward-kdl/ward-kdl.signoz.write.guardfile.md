# ward-kdl-write ops signoz

Spec-driven CLI. Every verb issues an HTTP request against the API base (resolved from ssm /coilysiren/signoz-ser8/base-url).

Authenticates with the "SIGNOZ-API-KEY" header (scheme header-token), reading the token from ssm /coilysiren/signoz-ser8/api-token. The token value is never shown.

## ward-kdl-write ops signoz pipeline get - the active log-pipeline set (GET /api/v1/logs/pipelines/latest). op pinned: the path ends in a static /latest, not an item {id}, so convention `get` would not resolve it.

`GET /api/v1/logs/pipelines/latest`

Authorized by grant: can get pipeline. Not destructive.

Takes no arguments.

## ward-kdl-write ops signoz dashboard get - a dashboard's full configuration (GET /api/v1/dashboards/{uuid})

`GET /api/v1/dashboards/{uuid}`

Authorized by grant: can get dashboard. Not destructive.

Positional arguments (1):

- `<uuid>` (string)

## ward-kdl-write ops signoz dashboard list - all dashboards with their summaries (GET /api/v1/dashboards)

`GET /api/v1/dashboards`

Authorized by grant: can list dashboard. Not destructive.

Takes no arguments.

## ward-kdl-write ops signoz rule get - an alert rule (GET /api/v1/rules/{id})

`GET /api/v1/rules/{id}`

Authorized by grant: can get rule. Not destructive.

Positional arguments (1):

- `<id>` (string)

## ward-kdl-write ops signoz rule list - all alert rules (GET /api/v1/rules)

`GET /api/v1/rules`

Authorized by grant: can list rule. Not destructive.

Takes no arguments.

## ward-kdl-write ops signoz query-range create - execute a v3 range query (POST /api/v3/query_range). A POST that READS telemetry - nothing is created. Supply the builder/clickhouse/promql tree via --body-file; --start/--end (epoch ms) and --step are convenience flags.

`POST /api/v3/query_range`

Authorized by grant: can create query-range. Not destructive.

Options (3):

- `--end` (integer, required): range end, epoch milliseconds
- `--start` (integer, required): range start, epoch milliseconds
- `--step` (integer, optional): step interval in seconds

## ward-kdl-write ops signoz pipeline create - apply a new log-pipeline version (POST /api/v1/logs/pipelines). Versioned server-side, so this is append-only: rollback is re-applying a prior set, never a delete. Supply the full set via --body-file.

`POST /api/v1/logs/pipelines`

Authorized by grant: can create pipeline. Not destructive.

Options (1):

- `--pipelines` ([]string, required): the ordered pipeline objects; supply the full set via --body-file

## ward-kdl-write ops signoz dashboard create - create a dashboard (POST /api/v1/dashboards). Supply the JSON document via --body-file; --title/--description/--tags lower to flags.

`POST /api/v1/dashboards`

Authorized by grant: can create dashboard. Not destructive.

Options (5):

- `--description` (string, optional): dashboard description
- `--layout` ([]string, optional): panel layout; via --body-file
- `--tags` ([]string, optional): dashboard tags
- `--title` (string, optional): dashboard title
- `--widgets` ([]string, optional): panel widgets; via --body-file

## ward-kdl-write ops signoz dashboard edit - replace a dashboard by uuid (PUT /api/v1/dashboards/{uuid}). Supply the JSON document via --body-file.

`PUT /api/v1/dashboards/{uuid}`

Authorized by grant: can edit dashboard. Not destructive.

Positional arguments (1):

- `<uuid>` (string)

Options (5):

- `--description` (string, optional): dashboard description
- `--layout` ([]string, optional): panel layout; via --body-file
- `--tags` ([]string, optional): dashboard tags
- `--title` (string, optional): dashboard title
- `--widgets` ([]string, optional): panel widgets; via --body-file

## ward-kdl-write ops signoz rule create - create an alert rule (POST /api/v1/rules). Supply the JSON document via --body-file; --alert/--ruleType/--alertType/--evalWindow/--frequency lower to flags.

`POST /api/v1/rules`

Authorized by grant: can create rule. Not destructive.

Options (5):

- `--alert` (string, optional): alert (rule) name
- `--alertType` (string, optional): alert type, e.g. METRIC_BASED_ALERT
- `--evalWindow` (string, optional): evaluation window, e.g. 5m0s
- `--frequency` (string, optional): evaluation frequency, e.g. 1m0s
- `--ruleType` (string, optional): rule type, e.g. threshold_rule or prom_rule

## ward-kdl-write ops signoz rule edit - replace an alert rule by id (PUT /api/v1/rules/{id}). Supply the JSON document via --body-file.

`PUT /api/v1/rules/{id}`

Authorized by grant: can edit rule. Not destructive.

Positional arguments (1):

- `<id>` (string)

Options (5):

- `--alert` (string, optional): alert (rule) name
- `--alertType` (string, optional): alert type, e.g. METRIC_BASED_ALERT
- `--evalWindow` (string, optional): evaluation window, e.g. 5m0s
- `--frequency` (string, optional): evaluation frequency, e.g. 1m0s
- `--ruleType` (string, optional): rule type, e.g. threshold_rule or prom_rule

## Denied operations

### ward-kdl-write ops signoz dashboard delete (denied)

dashboard deletion is irreversible (drops the dashboard and all its panels); do it in the SigNoz UI. ward exposes create/read/update only (ward#241).

### ward-kdl-write ops signoz rule delete (denied)

alert-rule deletion is irreversible; do it in the SigNoz UI. ward exposes create/read/update only, and the enable/disable toggle is deliberately not surfaced (ward#241).
