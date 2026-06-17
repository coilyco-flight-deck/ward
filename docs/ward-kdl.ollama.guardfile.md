# ward-kdl agents ollama

Exec-dialect CLI. Every verb runs `ollama` with the granted subcommand (or its `argv` override) appended; the binary and its prefix are fixed and the caller can never substitute them.

## ward-kdl agents ollama list - list installed models

`ollama list`

Flags: unrestricted passthrough.

## ward-kdl agents ollama ps - list running models

`ollama ps`

Flags: unrestricted passthrough.

## ward-kdl agents ollama show - show a model's info; pass the model name as args

`ollama show`

Flags: unrestricted passthrough.

## ward-kdl agents ollama run - run a model: interactive, or one-shot with a prompt arg

`ollama run`

Flags: unrestricted passthrough.

## ward-kdl agents ollama pull - pull a model from the registry; pass the model name

`ollama pull`

Flags: unrestricted passthrough.

## ward-kdl agents ollama stop - stop a running model; pass the model name

`ollama stop`

Flags: unrestricted passthrough.

## ward-kdl agents ollama version - print the Ollama version

`ollama version`

Flags: unrestricted passthrough.
