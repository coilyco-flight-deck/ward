# ward-kdl pkg glama

Spec-driven CLI. Every verb issues an HTTP request against the API base https://glama.ai/api/mcp.

Authenticates with the "Authorization" header (scheme bearer), reading the token from ssm /glama/api-key. The token value is never shown.

## ward-kdl pkg glama server list

`GET /v1/servers`

Authorized by grant: can list server. Not destructive.

Options (2):

- `--after` (string, optional): Pagination cursor.
- `--first` (string, optional): Page size.

## ward-kdl pkg glama server get

`GET /v1/servers/{namespace}/{slug}`

Authorized by grant: can get server. Not destructive.

Positional arguments (2):

- `<namespace>` (string)
- `<slug>` (string)

## ward-kdl pkg glama attribute list

`GET /v1/attributes`

Authorized by grant: can list attribute. Not destructive.

Takes no arguments.

## ward-kdl pkg glama instance list

`GET /v1/instances`

Authorized by grant: can list instance. Not destructive.

Takes no arguments.

## ward-kdl pkg glama usage create

`POST /v1/telemetry/usage`

Authorized by grant: can create usage. Not destructive.

Takes no arguments.
