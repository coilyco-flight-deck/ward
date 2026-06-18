# ward-kdl pkg skillsmp

Spec-driven CLI. Every verb issues an HTTP request against the API base https://skillsmp.com/api/v1/skills.

Authenticates with the "Authorization" header (scheme bearer), reading the token from ssm /skillsmp/api-key. The token value is never shown.

## ward-kdl pkg skillsmp skills search

`GET /search`

Authorized by grant: can search skills. Not destructive.

Options (4):

- `--q` (string, required): Search query.
- `--sortBy` (string, optional): Sort field (e.g. stars).
- `--limit` (integer, optional): Results per page.
- `--page` (integer, optional): Page number (1-indexed).

## ward-kdl pkg skillsmp skills ai-search

`GET /ai-search`

Authorized by grant: can ai-search skills. Not destructive.

Options (1):

- `--q` (string, required): Natural-language query.
