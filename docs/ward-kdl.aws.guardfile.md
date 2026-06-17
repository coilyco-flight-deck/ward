# ward-kdl ops aws

Exec-dialect CLI. Every verb runs `aws` with the granted subcommand (or its `argv` override) appended; the binary and its prefix are fixed and the caller can never substitute them.

## ward-kdl ops aws sts get-caller-identity

`aws sts get-caller-identity`

Flags: unrestricted passthrough.

## ward-kdl ops aws sts get-session-token

`aws sts get-session-token`

Flags: unrestricted passthrough.

## ward-kdl ops aws ssm get-parameter

`aws ssm get-parameter`

Flags: unrestricted passthrough.

Preflight:

- denies when name matches */prod/* or *secret*

## ward-kdl ops aws ssm get-parameters-by-path

`aws ssm get-parameters-by-path`

Flags: unrestricted passthrough.

## ward-kdl ops aws ssm put-parameter

`aws ssm put-parameter`

Flags: unrestricted passthrough.

## ward-kdl ops aws secretsmanager get-secret-value

`aws secretsmanager get-secret-value`

Flags: unrestricted passthrough.

Preflight:

- denies when secret-id matches *prod*

## ward-kdl ops aws secretsmanager list-secrets

`aws secretsmanager list-secrets`

Flags: unrestricted passthrough.

## ward-kdl ops aws s3 ls

`aws s3 ls`

Flags: unrestricted passthrough.

Preflight:

- denies when arg0 matches *tfstate* or *-backup*

## ward-kdl ops aws s3 cp

`aws s3 cp`

Flags: unrestricted passthrough.

## ward-kdl ops aws s3 sync

`aws s3 sync`

Flags: unrestricted passthrough.

## ward-kdl ops aws ec2 describe-instances

`aws ec2 describe-instances`

Flags: unrestricted passthrough.

## ward-kdl ops aws ec2 describe-security-groups

`aws ec2 describe-security-groups`

Flags: unrestricted passthrough.
