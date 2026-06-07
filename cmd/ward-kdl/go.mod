// ward-kdl is an isolated, throwaway proving module: its main.go is generated
// from forgejo.guardfile.kdl by cli-guard's specverb-gen. Kept out of the
// parent ward module so the dev-loop `replace` + AWS SDK never touch production
// ward's build or CI. Dropped wholesale when the spec-driven path folds in.
module ward-kdl

go 1.25.5

require (
	forgejo.coilysiren.me/coilyco-flight-deck/cli-guard v0.3.0
	github.com/aws/aws-sdk-go-v2/config v1.32.23
	github.com/aws/aws-sdk-go-v2/service/ssm v1.69.2
	github.com/urfave/cli/v3 v3.9.0
)

require (
	github.com/aws/aws-sdk-go-v2 v1.41.12 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.19.22 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.28 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.28 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.28 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.29 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.12 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.28 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.1.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.31.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.36.5 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.43.2 // indirect
	github.com/aws/smithy-go v1.27.1 // indirect
	github.com/calico32/kdl-go v0.14.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/clipperhouse/displaywidth v0.10.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.6.0 // indirect
	github.com/fatih/color v1.18.0 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-runewidth v0.0.19 // indirect
	github.com/olekukonko/cat v0.0.0-20250911104152-50322a0618f6 // indirect
	github.com/olekukonko/errors v1.2.0 // indirect
	github.com/olekukonko/ll v0.1.6 // indirect
	github.com/olekukonko/tablewriter v1.1.4 // indirect
	golang.org/x/sys v0.44.0 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

// specverb is unreleased; build against the local cli-guard checkout.
replace forgejo.coilysiren.me/coilyco-flight-deck/cli-guard => ../../../cli-guard
