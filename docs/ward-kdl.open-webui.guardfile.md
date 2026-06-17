# ward-kdl ops open-webui

Spec-driven CLI. Every verb issues an HTTP request against the API base (resolved from SSM /coilysiren/open-webui/url).

Authenticates with the "Authorization" header (scheme bearer), reading the token from SSM at /coilysiren/open-webui/api-key. The token value is never shown.

## ward-kdl ops open-webui session get

`GET /api/v1/auths/`

Authorized by grant: can get session. Not destructive.

Takes no arguments.

## ward-kdl ops open-webui models list

`GET /api/v1/models/list`

Authorized by grant: can list models. Not destructive.

Takes no arguments.

## ward-kdl ops open-webui completion create

`POST /api/chat/completions`

Authorized by grant: can create completion. Not destructive.

Takes no arguments.

## ward-kdl ops open-webui ollama-status get

`GET /ollama/`

Authorized by grant: can get ollama-status. Not destructive.

Takes no arguments.

## ward-kdl ops open-webui ollama-models list

`GET /ollama/api/tags`

Authorized by grant: can list ollama-models. Not destructive.

Takes no arguments.

## ward-kdl ops open-webui ollama-loaded list

`GET /ollama/api/ps`

Authorized by grant: can list ollama-loaded. Not destructive.

Takes no arguments.

## ward-kdl ops open-webui ollama-model show

`POST /ollama/api/show`

Authorized by grant: can show ollama-model. Not destructive.

Takes no arguments.

## ward-kdl ops open-webui ollama generate

`POST /ollama/api/generate`

Authorized by grant: can generate ollama. Not destructive.

Options (3):

- `--context` ([]string, optional)
- `--images` ([]string, optional)
- `--model` (string, required)

## ward-kdl ops open-webui ollama chat

`POST /ollama/api/chat`

Authorized by grant: can chat ollama. Not destructive.

Takes no arguments.

## ward-kdl ops open-webui ollama-model pull

`POST /ollama/api/pull`

Authorized by grant: can pull ollama-model. Not destructive.

Options (1):

- `--url_idx` (integer, optional)

## ward-kdl ops open-webui chats list

`GET /api/v1/chats/list`

Authorized by grant: can list chats. Not destructive.

Takes no arguments.

## ward-kdl ops open-webui chats search

`GET /api/v1/chats/search`

Authorized by grant: can search chats. Not destructive.

Options (1):

- `--text` (string, required)

## ward-kdl ops open-webui chat get

`GET /api/v1/chats/{id}`

Authorized by grant: can get chat. Not destructive.

Positional arguments (1):

- `<id>` (string)

## ward-kdl ops open-webui chat create

`POST /api/v1/chats/new`

Authorized by grant: can create chat. Not destructive.

Takes no arguments.

## ward-kdl ops open-webui chat delete

`DELETE /api/v1/chats/{id}`

Authorized by grant: can delete chat. Destructive - mutates irreversibly.

Positional arguments (1):

- `<id>` (string)

## ward-kdl ops open-webui config export

`GET /api/v1/configs/export`

Authorized by grant: can export config. Not destructive.

Takes no arguments.
