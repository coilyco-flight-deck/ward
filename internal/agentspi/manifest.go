package agentspi

import "forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/attribution"

// Manifest is the inert per-agent data record an Agent serves from Record();
// Phase 2 feeds it from the agent-adapter manifest (ward#401). Fields only.
type Manifest struct {
	// Name is the roster key (the --mode value), e.g. "claude".
	Name string
	// Binary is the in-container command the agent launches, e.g. "claude".
	Binary string
	// ContextLevel is the least-access operating-context tier (2=full..0=minimal).
	ContextLevel int
	// Stream identifies the headless output format (e.g. "stream-json"), telling
	// core whether to wrap the launch in the progress parser.
	Stream string
	// Auth is the credential-kind enum core feature-tests to route resolution
	// (e.g. "claude-keychain", "codex-file", "ollama-ssm", "none").
	Auth string
	// Argv holds the three argv prefixes ward invokes the agent with.
	Argv Argv
	// Identity is the name + optional pronouns the agent signs with; Signer() reads
	// it into an attribution.Signer.
	Identity attribution.Identity
	// Model is the default backend model (e.g. "gpt-5.4-mini"); empty when none.
	Model string
	// Endpoint is the default provider endpoint (e.g. the ollama URL); empty when
	// none applies.
	Endpoint string
}

// Argv holds the argv prefixes for the three ways ward invokes an agent; the
// caller appends the prompt (preflight) or seed (headless/interactive).
type Argv struct {
	Preflight   []string
	Headless    []string
	Interactive []string
}

// EnvLine is one resolved credential entry a CredentialProvider returns host-side:
// a bare key/value pair core renders as "KEY=VALUE" into the private --env-file.
type EnvLine struct {
	Key   string
	Value string
}
