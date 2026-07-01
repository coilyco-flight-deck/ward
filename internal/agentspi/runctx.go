package agentspi

import "context"

// Logger is the blog-style stderr logger core threads to per-agent code so its
// notes join the same container log stream; the entrypoint's blog() satisfies it.
type Logger func(format string, args ...any)

// Exec is the subprocess seam per-agent code runs commands through (host
// keychain/SSM reads, container installers); *shell.Runner satisfies it.
type Exec interface {
	Exec(ctx context.Context, bin string, argv ...string) error
	Capture(ctx context.Context, bin string, argv ...string) ([]byte, error)
}

// HostCtx is the narrow launching-host view a CredentialProvider resolves
// against, never the whole Runner (which would reintroduce the import cycle).
type HostCtx struct {
	// Ctx carries the resolve deadline so Exec calls need no separate argument.
	Ctx context.Context
	// GOOS is the host runtime.GOOS, selecting host-specific cred paths (macOS
	// keychain vs a dotfile).
	GOOS string
	// Home is the operator's home dir, the root of the ~/.claude, ~/.codex reads.
	Home string
	// Exec runs host reads (Capture for the keychain and SSM lookups).
	Exec Exec
	// Log emits host-side warnings (unreadable creds, a failed SSM lookup).
	Log Logger
}

// RunCtx is the narrow in-container view the capabilities and LaunchArgv act
// against, carrying only the bootstrapEnv fields per-agent code reads (ward#410).
type RunCtx struct {
	// Ctx carries the launch deadline so Exec calls need no separate argument.
	Ctx context.Context
	// AgentHome is the agent user's HOME, the root of every config/cred write.
	AgentHome string
	// TargetName is the target repo's short name; the clone lives at
	// /workspace/<TargetName> (onboarding seeds its project entry).
	TargetName string
	// AgentUID and AgentGID are the non-root agent user's ids, for the setpriv drop
	// a launch gate (the claude smoke test) probes under.
	AgentUID string
	AgentGID string
	// Headless and Ask are the one-shot launch posture flags shaping LaunchArgv;
	// the one-shot posture is Headless || Ask.
	Headless bool
	Ask      bool
	// CodexModel, CodexEffort, CodexVerbosity are the codex config knobs, carried
	// from the entrypoint env with its defaults applied.
	CodexModel     string
	CodexEffort    string
	CodexVerbosity string
	// OpencodeModel is the ollama-backed model the opencode config points at
	// (bootstrapEnv.QwenModel today; the qwen->opencode untangle is Phase 2).
	OpencodeModel string
	// OllamaURL is the local ollama endpoint the opencode config binds.
	OllamaURL string
	// Seed is the agent's seed argv (the entrypoint's "$@"): the one-shot prompt,
	// empty for a bare interactive session. LaunchArgv appends it.
	Seed []string
	// Exec runs in-container subprocesses (e.g. the opencode installer).
	Exec Exec
	// Log is the blog stderr logger.
	Log Logger
}
