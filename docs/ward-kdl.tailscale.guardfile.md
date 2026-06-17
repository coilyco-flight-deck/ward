# ward-kdl ops tailscale

Spec-driven CLI. Every verb issues an HTTP request against the API base https://api.tailscale.com/api/v2.

Authenticates with the "Authorization" header (scheme bearer), reading the token from ssm /tailscale/api-key. The token value is never shown.

## ward-kdl ops tailscale devices list

`GET /tailnet/{tailnet}/devices`

Authorized by grant: can list devices. Not destructive.

Positional arguments (1):

- `<tailnet>` (string)

Options (2):

- `--fields` (string, optional): Optionally controls whether the response returns **all** fields or only a predefined subset of fields.
Currently, there are two supported options:

- **`all`:** return all fields in the response
- **`default`:** return the following fields
  - `addresses`
  - `id`
  - `nodeId`
  - `user`
  - `name`
  - `hostname`
  - `clientVersion`
  - `updateAvailable`
  - `os`
  - `created`
  - `connectedToControl`
  - `lastSeen`
  - `keyExpiryDisabled`
  - `expires`
  - `authorized`
  - `isExternal`
  - `machineKey`
  - `nodeKey`
  - `blocksIncomingConnections`
  - `tailnetLockKey`
  - `tailnetLockError`
  - `tags`
  - `isEphemeral`

If the `fields` parameter is not supplied, then the default (limited fields) option is used.

- `--<field>=<value> filters` (string, optional): This endpoint supports server-side filtering of devices by specifying one
or more filters in the form `<field>=<value>`. Fields must be a top-level
device property - e.g. `isEphemeral`, `tags`, `hostname`, etc. Values are
matched as _exact_ matches. Properties with simple types (strings, numbers,
dates, etc) and lists are supported. Properties that are complex objects
(e.g. `clientConnectivity`) are not supported. When multiple parameters are
provided, the server performs a logical `AND` across all filter parameters
before returning results. For example,
`isEphemeral=true&tags=tag:prod&tags=tag:subnetrouter` would return devices
where `isEphemeral` is `true` and `tags` contains both `tag:prod` and
`tag:subnetrouter`.


## ward-kdl ops tailscale device get

`GET /device/{deviceId}`

Authorized by grant: can get device. Not destructive.

Positional arguments (1):

- `<deviceId>` (string)

Options (1):

- `--fields` (string, optional): Optionally controls whether the response returns **all** fields or only a predefined subset of fields.
Currently, there are two supported options:

- **`all`:** return all fields in the response
- **`default`:** return the following fields
  - `addresses`
  - `id`
  - `nodeId`
  - `user`
  - `name`
  - `hostname`
  - `clientVersion`
  - `updateAvailable`
  - `os`
  - `created`
  - `connectedToControl`
  - `lastSeen`
  - `keyExpiryDisabled`
  - `expires`
  - `authorized`
  - `isExternal`
  - `machineKey`
  - `nodeKey`
  - `blocksIncomingConnections`
  - `tailnetLockKey`
  - `tailnetLockError`
  - `tags`
  - `isEphemeral`

If the `fields` parameter is not supplied, then the default (limited fields) option is used.


## ward-kdl ops tailscale policy get

`GET /tailnet/{tailnet}/acl`

Authorized by grant: can get policy. Not destructive.

Positional arguments (1):

- `<tailnet>` (string)

Options (1):

- `--details` (boolean, optional): Request a detailed description of the tailnet policy file by providing `details=true` in the URL query string.
Supplying any other value for `details`, or not sending the param, is treated as sending `details=false`.
If using this, do not supply an `Accept` parameter in the header.

The response will contain a JSON object with the fields:
- `acl`: a base64-encoded string representation of the huJSON format.
- `warnings`: array of strings for syntactically valid but nonsensical entries.
- `errors`: an array of strings for parsing failures.


## ward-kdl ops tailscale policy set

`POST /tailnet/{tailnet}/acl`

Authorized by grant: can set policy. Not destructive.

Positional arguments (1):

- `<tailnet>` (string)

## ward-kdl ops tailscale keys list

`GET /tailnet/{tailnet}/keys`

Authorized by grant: can list keys. Not destructive.

Positional arguments (1):

- `<tailnet>` (string)

Options (1):

- `--all` (boolean, required): If set to true, this will return all auth keys, API access tokens and OAuth clients for the tailnet.

## ward-kdl ops tailscale keys get

`GET /tailnet/{tailnet}/keys/{keyId}`

Authorized by grant: can get keys. Not destructive.

Positional arguments (2):

- `<tailnet>` (string)
- `<keyId>` (string)

## ward-kdl ops tailscale keys create

`POST /tailnet/{tailnet}/keys`

Authorized by grant: can create keys. Not destructive.

Positional arguments (1):

- `<tailnet>` (string)

Options (8):

- `--audience` (string, optional): The value used when matching against the `aud` claim from an OIDC identity token.

Specifying the audience is optional as Tailscale will generate a secure audience at creation time by default.
It is recommended to let Tailscale generate the audience unless the identity provider you are integrating with
requires a specific audience format.

Only applies to federated identities.

- `--description` (string, optional): A short string specifying the purpose of the key. Can be a maximum of 50 alphanumeric characters. Hyphens and spaces are also allowed.

- `--expirySeconds` (integer, optional): Specifies the duration in seconds until the key expires. Defaults to 90 days if not supplied.

Only applies to auth keys.

- `--issuer` (string, optional): The issuer of the OIDC identity token used in the token exchange. Must be a valid and publicly reachable https:// URL.

Only applies to federated identities.

- `--keyType` (string, optional): The type of key to create. Defaults to "auth" if omitted.

- `--scopes` ([]string, optional): A list of scopes to grant to the key. At least one scope is required for OAuth clients and federated identities.
See [trust credentials scopes](https://tailscale.com/kb/1623/trust-credentials#scopes) for a list of available scopes.

Only applies to OAuth clients and federated identities.

- `--subject` (string, optional): The pattern used when matching against the `sub` claim from an OIDC identity token.
Patterns can include `*` characters to match against any character.

Only applies to federated identities.

- `--tags` ([]string, optional): A list of tags associated to the trust credential. Auth keys created with this credential must have these exact tags, or tags owned by the credential's tags.
Mandatory if the scopes include "devices:core" or "auth_keys".

Only applies to OAuth clients and federated identities.


## ward-kdl ops tailscale keys delete

`DELETE /tailnet/{tailnet}/keys/{keyId}`

Authorized by grant: can delete keys. Destructive - mutates irreversibly.

Positional arguments (2):

- `<tailnet>` (string)
- `<keyId>` (string)

## Scope restrictions

Every verb whose path carries one of these parameters must supply a value matching a glob below, or it fails closed.

- `tailnet` must match: coily*

## Denied operations

### ward-kdl ops tailscale device delete (denied)

removing a device from the tailnet is disruptive; do it in the admin console
