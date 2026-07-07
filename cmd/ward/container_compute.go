package main

// container_compute.go holds the pure, testable computation behind `ward
// container`; cmd/ward/container.go owns side effects. See docs/container.md.

import (
	"fmt"
	"net"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	// containerImageDefault is the aos-published dev-base image, run unmodified;
	// ward bind-mounts its embedded entrypoint+doctrine and downloads ward.
	containerImageDefault = "forgejo.coilysiren.me/coilyco-flight-deck/agentic-os"

	// containerImageTagDefault tracks the image's :latest moving tag.
	containerImageTagDefault = "latest"

	// envAgentImage / envAgentTag pin the dev-base image + tag once for every
	// `ward agent` dispatch; an explicit --image/--tag still overrides (ward#312).
	envAgentImage = "WARD_AGENT_IMAGE"
	envAgentTag   = "WARD_AGENT_TAG"

	// envAgentVersion pins the ward release the container downloads, independent of
	// the dev-base image tag; --ward-version overrides it per run (ward#312).
	envAgentVersion = "WARD_AGENT_VERSION"

	// containerWardAssets is where ward's embedded entrypoint + doctrine are
	// bind-mounted, read-only. The image bakes none of this in.
	containerWardAssets    = "/opt/ward"
	containerEntrypointRel = "entrypoint.sh"

	// containerWardSrcMount is where --ward-source mounts a ward checkout, so
	// the entrypoint builds ward from it instead of downloading the release.
	containerWardSrcMount = "/opt/ward-src"

	// containerContextMount holds the read-only host cwd (operating context):
	// the only default host bind, a sibling of containerWardAssets (not nested).
	containerContextMount = "/opt/ward-context"

	// containerGitcacheVol is a shared named volume of bare mirrors (never a
	// host dir) so fresh clones are cheap and never land in the host repo tree.
	containerGitcacheVol = "ward-gitcache"
	containerGitcacheMnt = "/gitcache"

	// containerAWSMount is where ~/.aws lands as the aws capability's fallback bind, used
	// only when host credential export fails (ward#586). Off by default.
	containerAWSMount = "/root/.aws"

	// containerAgentLogsMount is where the host agent-log drain binds read-only in a
	// director surface session; surface-only opt-in (ward#525, redacted source ward#526).
	containerAgentLogsMount = "/opt/ward-agent-logs"

	// containerDockerSock is the host docker socket bound into a read-only surface session
	// so it can dispatch sibling runs; same path both sides (ward#315). See container.md.
	containerDockerSock = "/var/run/docker.sock"

	// containerHostGateway is the DNS name the container dials to reach the host
	// broker's TCP port; Linux wires it with --add-host=...:host-gateway (ward#391).
	containerHostGateway = "host.docker.internal"

	// containerLabel marks ward-managed containers for filtering; identity rides
	// labels, not the name, now (ward#364, docs/container.md).
	containerLabel = "ward=true"

	// The ward.* label keys carrying a run's identity for poll/reaper/sweep: role
	// and driver always, repo always, issue on an engineer run, machine the id.
	labelRole     = "ward.role"
	labelDriver   = "ward.driver"
	labelRepo     = "ward.repo"
	labelIssue    = "ward.issue"
	labelMachine  = "ward.machine"
	labelWorkflow = "ward.workflow"

	// containerSubstrateSeed is where the dev-base image bakes image-tier bare
	// mirrors; the entrypoint hydrates the gitcache from here on a cold volume.
	containerSubstrateSeed = "/opt/substrate-seed"

	// containerSubstrateDest is where the entrypoint materialises substrate
	// working copies (reference repos every agent gets regardless of target).
	containerSubstrateDest = "/substrate"

	// containerSubstrateManifest is the bind-mounted preclone manifest the
	// entrypoint reads; it rides the same read-only assets mount as the entrypoint.
	containerSubstrateManifest = containerWardAssets + "/" + containerSubstrateRel
	containerSubstrateRel      = "preclone-repos.txt"

	// containerSubstrateTTL is the gitcache refresh TTL (seconds): a burst of
	// containers does one fetch per repo per window, the rest skip the gate.
	containerSubstrateTTL = "600"

	// containerReadOnlyExtraRepoTTL is the longer refresh TTL for advisor-only
	// upstream/context repos. Their mirrors are stable enough to reuse for a day.
	containerReadOnlyExtraRepoTTL = 24 * time.Hour
)

// Tailnet + tower topology (ward#395): infra DATA, not baked identity. Each value takes
// a WARD_* env override, the old literal kept as the fail-safe default. See the doc.
const (
	// envTailnetNetwork overrides the shared docker network runs attach to (ward#349).
	envTailnetNetwork     = "WARD_TAILNET_NETWORK"
	defaultTailnetNetwork = "ward-tailnet"

	// envTailnetProxy overrides the standing SOCKS5 box host:port; host half is also
	// its container name for the attach preflight (ward#349).
	envTailnetProxy     = "WARD_TAILNET_PROXY"
	defaultTailnetProxy = "mac-proxy:1055"

	// proxySocks5Scheme is socks5h (not socks5): the proxy resolves the tower's
	// MagicDNS name tailnet-side, so the run dials by name (ward#337; the doc).
	proxySocks5Scheme = "socks5h://"

	// envTowerHost overrides the tower's MagicDNS node name, dialed by name through
	// the proxy, no SSM IP lookup (ward#337).
	envTowerHost     = "WARD_TOWER_HOST"
	defaultTowerHost = "kai-tower-3026"

	// envTowerOllamaPort overrides the port the tower serves ollama on.
	envTowerOllamaPort     = "WARD_TOWER_OLLAMA_PORT"
	defaultTowerOllamaPort = "11434"
)

// tailnetNetwork resolves the shared docker network name (env > baked default; ward#395).
func tailnetNetwork() string { return envOr(envTailnetNetwork, defaultTailnetNetwork) }

// proxyBoxAddr resolves the SOCKS5 box's host:port endpoint (env > default; ward#395).
func proxyBoxAddr() string { return envOr(envTailnetProxy, defaultTailnetProxy) }

// proxyBoxName is the proxy box's container name - the host half of proxyBoxAddr, used
// by the attach preflight. A port-less override falls back to the whole value.
func proxyBoxName() string {
	if host, _, err := net.SplitHostPort(proxyBoxAddr()); err == nil {
		return host
	}
	return proxyBoxAddr()
}

// towerMagicDNS resolves the tower's MagicDNS node name (env > baked default; ward#395).
func towerMagicDNS() string { return envOr(envTowerHost, defaultTowerHost) }

// towerOllamaPort resolves the port the tower serves ollama on (env > default; ward#395).
func towerOllamaPort() string { return envOr(envTowerOllamaPort, defaultTowerOllamaPort) }

// towerOllamaURL is the by-name tower endpoint a --ts-sidecar run dials through the
// proxy; no per-launch SSM IP lookup (ward#337; the doc).
func towerOllamaURL() string { return "http://" + towerMagicDNS() + ":" + towerOllamaPort() }

// substrateRepo is one entry in the container substrate manifest: a
// Forgejo-canonical owner/name plus its seed tier (image|cache).
type substrateRepo struct {
	Owner string
	Name  string
	Tier  string
}

func (s substrateRepo) slug() string { return s.Owner + "/" + s.Name }

// substrateTiers is the closed set of valid tier values. image-tier repos are
// also baked into the image as a seed; cache-tier repos are warm-cache only.
var substrateTiers = map[string]bool{"image": true, "cache": true}

// parseSubstrateManifest parses `owner/name  tier` lines ('#' comments and
// blanks ignored); a malformed line or unknown tier is a hard error.
func parseSubstrateManifest(data string) ([]substrateRepo, error) {
	var out []substrateRepo
	for i, raw := range strings.Split(data, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("preclone-repos.txt line %d: want `owner/name tier`, got %q", i+1, line)
		}
		m := ownerNameRe.FindStringSubmatch(fields[0])
		if m == nil {
			return nil, fmt.Errorf("preclone-repos.txt line %d: %q is not owner/name", i+1, fields[0])
		}
		if !substrateTiers[fields[1]] {
			return nil, fmt.Errorf("preclone-repos.txt line %d: tier %q must be image|cache", i+1, fields[1])
		}
		out = append(out, substrateRepo{Owner: m[1], Name: m[2], Tier: fields[1]})
	}
	return out, nil
}

// containerMode selects the agent harness and how much operating context the
// container composes (progressively less, mirroring agent-compose slices).
type containerMode string

const (
	modeClaude   containerMode = "claude"
	modeCodex    containerMode = "codex"
	modeOpencode containerMode = "opencode"
	modeGoose    containerMode = "goose"
)

// modeQwenAlias is the retired roster key ward#401 renamed to modeOpencode;
// parseMode still accepts it (deprecation warning) so --mode qwen keeps working.
const modeQwenAlias = "qwen"

// gooseHintsRel is goose's in-HOME hints path composeContext mirrors the doctrine
// into; it lives with the mode table so core's context composer stays agent-neutral.
const gooseHintsRel = ".config/goose/.goosehints"

// container roles lead the name + the ward.role label (ward#364). director is a host
// loop, not a container, but its surface session runs as roleSession (ward#353).
const (
	roleEngineer = "engineer"
	roleAdvisor  = "advisor"
	roleSession  = "session"
	// roleDirector keys the director's per-role capability lookup (ward#578); empty
	// set by default - it forwards capability to children, holds none itself.
	roleDirector = "director"
)

// agentBinary is the in-container command each mode launches.
func (m containerMode) agentBinary() string {
	switch m {
	case modeCodex:
		return "codex"
	case modeOpencode:
		return "opencode"
	case modeGoose:
		return "goose"
	case modeClaude:
		return "claude"
	default:
		return "claude"
	}
}

// hostPreflightArgv is the host one-shot argv asking this mode's agent prompt,
// plus whether one exists (claude+goose yes, codex/qwen not yet). See docs/agent.md.
func (m containerMode) hostPreflightArgv(prompt string) ([]string, bool) { //nolint:unparam // ward#418 flipped the varying-prompt callers to the registry; only the contract test's constant prompt remains until Phase 4 deletes this switch
	switch m {
	case modeClaude:
		return []string{m.agentBinary(), "-p", prompt}, true
	case modeGoose:
		return []string{m.agentBinary(), "run", "-t", prompt}, true
	case modeCodex, modeOpencode:
		return nil, false
	default:
		return nil, false
	}
}

// contextLevel maps a mode onto the least-access ladder (2=full, 1=scoped, 0=minimal);
// see docs/container.md. goose is full (level 2) like claude, mirrored to its hints file.
func (m containerMode) contextLevel() int {
	switch m {
	case modeOpencode:
		return 0
	case modeCodex:
		return 1
	case modeGoose:
		return 2
	case modeClaude:
		return 2
	default:
		return 2
	}
}

// parseMode validates a --mode value against the embedded fleet roster.
// qwen still aliases to opencode with a deprecation warning (ward#401).
func parseMode(s string) (containerMode, error) {
	if s == modeQwenAlias {
		fmt.Fprintln(os.Stderr, "ward: --mode qwen is deprecated; use --mode opencode (qwen is the backing model, opencode the harness). Aliasing to opencode for now.")
		return modeOpencode, nil
	}
	fleet, err := loadFleetConfig()
	if err != nil {
		return "", err
	}
	for _, a := range fleet.Agents {
		if a.Name == s {
			return containerMode(a.Name), nil
		}
	}
	return "", fmt.Errorf("unknown --mode %q: want a fleet agent name (qwen is a deprecated alias for opencode)", s)
}

// targetRepo is a forgejo owner/name pair the container clones and works.
type targetRepo struct {
	Owner string
	Name  string
}

func (t targetRepo) slug() string { return t.Owner + "/" + t.Name }

// cloneURL is the git-over-HTTPS URL the container clones and pushes to.
func (t targetRepo) cloneURL(base string) string {
	return strings.TrimRight(base, "/") + "/" + t.Owner + "/" + t.Name + ".git"
}

// mirrorName is the bare-mirror directory name inside the gitcache volume.
func (t targetRepo) mirrorName() string { return t.Owner + "__" + t.Name + ".git" }

// ownerNameRe matches a bare owner/name ref, the canonical short form.
var ownerNameRe = regexp.MustCompile(`^([A-Za-z0-9._-]+)/([A-Za-z0-9._-]+?)(?:\.git)?$`)

// repoPathRe pulls the trailing owner/name(.git) out of a URL or scp-style
// remote (https://host/owner/name.git, git@host:owner/name.git).
var repoPathRe = regexp.MustCompile(`[:/]([A-Za-z0-9._-]+)/([A-Za-z0-9._-]+?)(?:\.git)?/?$`)

// parseRepoRef resolves a target ref (bare owner/name, https clone URL, or
// scp-style remote) into a targetRepo. Empty means infer from cwd.
func parseRepoRef(ref string) (targetRepo, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return targetRepo{}, fmt.Errorf("empty repo ref")
	}
	if !strings.Contains(ref, "://") && !strings.Contains(ref, "@") && ownerNameRe.MatchString(ref) {
		m := ownerNameRe.FindStringSubmatch(ref)
		return targetRepo{Owner: m[1], Name: m[2]}, nil
	}
	if m := repoPathRe.FindStringSubmatch(ref); m != nil {
		return targetRepo{Owner: m[1], Name: m[2]}, nil
	}
	return targetRepo{}, fmt.Errorf("cannot parse repo ref %q: want owner/name or a forgejo clone URL", ref)
}

// targetFromRemoteURL derives the target from a git remote URL when `up` is run
// with no explicit ref; the container still clones fresh from forgejo.
func targetFromRemoteURL(remoteURL string) (targetRepo, error) {
	if m := repoPathRe.FindStringSubmatch(strings.TrimSpace(remoteURL)); m != nil {
		return targetRepo{Owner: m[1], Name: m[2]}, nil
	}
	return targetRepo{}, fmt.Errorf("cannot derive owner/name from remote %q", remoteURL)
}

// nameSanitizeRe strips characters docker forbids in a container name.
var nameSanitizeRe = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

// safeRepoName sanitizes a repo name into a docker-safe container-name segment,
// stripping forbidden characters and falling back to "repo" when nothing's left.
func safeRepoName(repo targetRepo) string {
	safe := nameSanitizeRe.ReplaceAllString(repo.Name, "-")
	safe = strings.Trim(safe, "-._")
	if safe == "" {
		safe = "repo"
	}
	return safe
}

// containerRoleName builds the role-led, prefixless container name (ward#364):
// engineer-<driver>-<repo>-<N> for an engineer, else <role>-<driver>-<machine>.
func containerRoleName(role string, mode containerMode, repo targetRepo, issue int, machine string) string {
	if role == roleEngineer {
		return fmt.Sprintf("%s-%s-%s-%d", role, mode, safeRepoName(repo), issue)
	}
	return fmt.Sprintf("%s-%s-%s", role, mode, machine)
}

// mountSpec is one docker -v binding: a host path or named volume, the
// in-container target, and whether it is read-only.
type mountSpec struct {
	Source   string
	Target   string
	ReadOnly bool
	Volume   bool // true => named volume, false => host bind
}

func (m mountSpec) arg() string {
	out := m.Source + ":" + m.Target
	if m.ReadOnly {
		out += ":ro"
	}
	return out
}

// mountOpts collects the optional, host-derived inputs to the mount set so the
// default stays least-access and every addition is an explicit opt-in.
type mountOpts struct {
	// AssetsDir holds ward's embedded entrypoint + doctrine, written to a
	// per-run tmp dir and mounted read-only. Always set in practice.
	AssetsDir string
	// AWSHome, when non-empty, mounts ~/.aws read-only as the aws capability's fallback
	// bind; the launch path drops it when host credential export succeeds (ward#586).
	AWSHome string
	// WardSource, when non-empty, mounts a local ward checkout (--ward-source)
	// so the container builds ward from source instead of downloading.
	WardSource string
	// AgentLogsDir, when non-empty, mounts a host agent-log drain read-only at
	// containerAgentLogsMount (ward#525); the surface passes the REDACTED tree (ward#526).
	AgentLogsDir string
}

// leastAccessMounts is the default set: cwd + assets read-only and the gitcache
// volume. The target repo is never mounted; --aws/--ward-source are opt-ins.
func leastAccessMounts(hostCwd string, opts mountOpts) []mountSpec {
	mounts := []mountSpec{
		{Source: hostCwd, Target: containerContextMount, ReadOnly: true, Volume: false},
		{Source: containerGitcacheVol, Target: containerGitcacheMnt, ReadOnly: false, Volume: true},
	}
	if opts.AssetsDir != "" {
		mounts = append(mounts, mountSpec{Source: opts.AssetsDir, Target: containerWardAssets, ReadOnly: true, Volume: false})
	}
	if opts.AWSHome != "" {
		mounts = append(mounts, mountSpec{Source: opts.AWSHome, Target: containerAWSMount, ReadOnly: true, Volume: false})
	}
	if opts.WardSource != "" {
		mounts = append(mounts, mountSpec{Source: opts.WardSource, Target: containerWardSrcMount, ReadOnly: true, Volume: false})
	}
	if opts.AgentLogsDir != "" {
		mounts = append(mounts, mountSpec{Source: opts.AgentLogsDir, Target: containerAgentLogsMount, ReadOnly: true, Volume: false})
	}
	return mounts
}

// dockerSockMount binds the host docker socket read-write for explore's dispatch
// path; not in the least-access default, only explore opts in (ward#315).
func dockerSockMount() mountSpec {
	return mountSpec{Source: containerDockerSock, Target: containerDockerSock, ReadOnly: false, Volume: false}
}

// upPlan is the fully-resolved description of one container bring-up, minus
// the forgejo token (held out so it never reaches a print path or audit row).
type upPlan struct {
	Image string
	Name  string
	// Role leads the name + the ward.role label (engineer/advisor/session).
	Role string
	// ConfigRole resolves the per-role model/effort overlay in-container (WARD_ROLE,
	// ward#620): the capability role, not the `session` label the director surface wears.
	ConfigRole string
	// Machine is the per-container disambiguator on the ward.machine label (ward#364).
	Machine     string
	Repo        targetRepo
	Mode        containerMode
	Branch      string
	ForgejoBase string
	// Forge is the TARGET repo's host (ward#489): forgeGitHub clones/comments on GitHub,
	// else Forgejo. ForgejoBase stays Forgejo (ward's own release/broker), regardless.
	Forge   forge
	HostCwd string
	Mounts  []mountSpec
	// Interactive attaches the run (stdin kept open); false means --detach (-d).
	Interactive bool
	// TTY allocates a pseudo-terminal (-t), auto-detected: true only with a real
	// terminal, since docker rejects -t against non-terminal stdin. See docs.
	TTY bool
	// Capture marks an attached foreground run ward reads stdout back from (the
	// one-shot advisor research): streams stdout, attaches no stdin (ward#411, #606).
	Capture bool
	// WardVersion pins the ward release the entrypoint downloads (matches the
	// launcher); "dev" or "" tells the entrypoint to resolve the latest release.
	WardVersion string
	// WardFromSource is set when --ward-source mounted a checkout: the
	// entrypoint builds ward from it instead of downloading.
	WardFromSource bool
	// AgentArgs ride after the image as the in-container agent's argv (the
	// entrypoint's `"$WARD_AGENT" "$@"`); empty for a bare interactive bring-up.
	AgentArgs []string
	// Headless runs the in-container agent in print mode (claude -p), exporting
	// WARD_HEADLESS=1; set by the detached `ward agent engineer` run.
	Headless bool
	// Ask runs the in-container agent one-shot, attached (claude -p plain, no
	// stream-json); exports WARD_ASK=1, set by `ward agent advisor`'s freeform mode.
	Ask bool
	// GoBootstrap (EXPERIMENTAL, ward#181) exports WARD_USE_GO_BOOTSTRAP=1 so the
	// entrypoint delegates to `ward container bootstrap` instead of its bash logic.
	GoBootstrap bool
	// ExtraRepos are additional writable repos this run was granted to clone +
	// operate against (--repo, ward#230); see docs/container-multi-repo.md.
	ExtraRepos []targetRepo
	// Issue is the carried issue number (0 for a bare `container up`), exported as
	// WARD_TARGET_ISSUE so the reaper can release a pre-launch hold (ward#264).
	Issue int
	// ReadOnly marks a read-only surface session (the director's drain surface, ward#293,
	// ward#353): exports WARD_READONLY=1. See docs/agent-surface.md.
	ReadOnly bool
	// DispatchBrokerAddr, when set, exports WARD_DISPATCH_BROKER_ADDR (host.docker
	// .internal:<port>) and flips on the --add-host wiring (ward#391).
	DispatchBrokerAddr string
	// DispatchBrokerToken exports WARD_DISPATCH_BROKER_TOKEN, the per-launch secret
	// the surface echoes back so the host broker authenticates the dial.
	DispatchBrokerToken string
	// HostNet joins the container to the host network (--network=host) so a run
	// inherits the host's tailnet route (--host-net, ward#330). docs/agent-host-net.md.
	HostNet bool
	// TSSidecar attaches the run to the shared ward-tailnet network so it reaches
	// the standing mac-proxy box (--ts-sidecar, ward#349). docs/agent-ts-sidecar.md.
	TSSidecar bool
	// Workflow is the run's landing policy (--workflow, ward#508): non-direct-main
	// runs export WARD_WORKFLOW + a ward.workflow label. See docs/agent-workflow.md.
	Workflow workflowMode
	// AWSHome is the aws capability's fallback ~/.aws bind source; a good host cred export
	// drops the mount and clears this, else it arms the #579 creds-less warning (ward#586).
	AWSHome string
	// ConfigEnv are the resolved `--config` overrides as WARD_* env keys (ward#616),
	// merged into wardEnv so the in-container envOr resolves them over the fleet default.
	ConfigEnv map[string]string
}

// extraRepoLogLine describes one repo that ended up in the merged grant set.
type extraRepoLogLine struct {
	Slug   string
	Reason string
}

// parseExtraRepos resolves the --repo grant (bare owner/name or clone URL):
// drops the target + dups, errors on a bad ref or workspace collision (ward#230).
func parseExtraRepos(refs []string, target targetRepo) ([]targetRepo, error) {
	var out []targetRepo
	seenSlug := map[string]bool{}
	// workspace dir name -> claiming slug; seed with the target to catch clobbers.
	seenName := map[string]string{target.Name: target.slug()}
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		repo, err := parseRepoRef(ref)
		if err != nil {
			return nil, fmt.Errorf("--repo %q: %w", ref, err)
		}
		if repo.Owner == target.Owner && repo.Name == target.Name {
			continue // the target is always cloned; naming it is a no-op
		}
		if seenSlug[repo.slug()] {
			continue
		}
		if claimed, ok := seenName[repo.Name]; ok {
			return nil, fmt.Errorf("--repo %q collides on workspace dir /workspace/%s with %s", repo.slug(), repo.Name, claimed)
		}
		seenSlug[repo.slug()] = true
		seenName[repo.Name] = repo.slug()
		out = append(out, repo)
	}
	return out, nil
}

// extraReposEnv renders the granted extra repos as a space-separated owner/name
// list for WARD_EXTRA_REPOS, the form the entrypoint word-splits. Pure.
func extraReposEnv(repos []targetRepo) string {
	slugs := make([]string, len(repos))
	for i, r := range repos {
		slugs[i] = r.slug()
	}
	return strings.Join(slugs, " ")
}

// configEnvKeys maps a `--config agent.<name>.<key>` path to the WARD_* container env
// the entrypoint reads (ward#616): the known-key allowlist, an unlisted path fails loud.
var configEnvKeys = map[string]string{
	"agent.claude.model":      "WARD_CLAUDE_MODEL",
	"agent.claude.effort":     "WARD_CLAUDE_REASONING_EFFORT",
	"agent.codex.model":       "WARD_CODEX_MODEL",
	"agent.codex.effort":      "WARD_CODEX_REASONING_EFFORT",
	"agent.codex.verbosity":   "WARD_CODEX_VERBOSITY",
	"agent.opencode.model":    "WARD_QWEN_MODEL",
	"agent.opencode.endpoint": "WARD_OLLAMA_URL",
}

// knownConfigKeys returns the sorted `--config` dotted paths, for a loud error.
func knownConfigKeys() []string {
	keys := make([]string, 0, len(configEnvKeys))
	for k := range configEnvKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// parseConfigOverrides translates repeatable `--config agent.<name>.<key>=<value>`
// flags into WARD_* container env overrides (ward#616); unknown/malformed fails loud.
func parseConfigOverrides(entries []string) (map[string]string, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	out := map[string]string{}
	for _, raw := range entries {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		path, value, ok := strings.Cut(entry, "=")
		if !ok {
			return nil, fmt.Errorf("--config %q: want agent.<name>.<key>=<value>", raw)
		}
		path = strings.TrimSpace(path)
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("--config %q: empty value (want agent.<name>.<key>=<value>)", raw)
		}
		env, known := configEnvKeys[path]
		if !known {
			return nil, fmt.Errorf("--config %q: unknown key %q (known: %s)", raw, path, strings.Join(knownConfigKeys(), ", "))
		}
		out[env] = value
	}
	return out, nil
}

// wardEnv is the non-secret WARD_* config the entrypoint reads. Everything
// here is safe to print and to record; the token never appears.
func (p upPlan) wardEnv() map[string]string {
	rec := lookupAgent(p.Mode).Record() // registry data reads (Phase 3, ward#418)
	env := map[string]string{
		// The friendly docker --name (plan.Name) so in-container tooling (the status
		// line) can show it; HOSTNAME carries only the container ID (ward#365).
		"WARD_CONTAINER_NAME": p.Name,
		// Explicit "inside a ward container" marker host-only fleet-walk scripts fence
		// on (ward#114); a host shell never has it. See docs/container-skill-surface.md.
		"WARD_CONTAINER": "1",
		"WARD_TARGET_REPO":    p.Repo.slug(),
		"WARD_TARGET_OWNER":   p.Repo.Owner,
		"WARD_TARGET_NAME":    p.Repo.Name,
		"WARD_FORGEJO_BASE":   p.ForgejoBase,
		"WARD_MODE":           string(p.Mode),
		"WARD_CONTEXT_LEVEL":  fmt.Sprintf("%d", rec.ContextLevel),
		"WARD_AGENT":          rec.Binary,
		"WARD_GITCACHE":       containerGitcacheMnt,
		"WARD_CONTEXT_SRC":    containerContextMount,
		"WARD_MIRROR_NAME":    p.Repo.mirrorName(),
		"WARD_VERSION":        p.WardVersion,
		// Terminal color: a bare TERM with no COLORTERM makes the in-container agent
		// downgrade its palette to ~mono; advertise 256-color + truecolor for color.
		"TERM":      "xterm-256color",
		"COLORTERM": "truecolor",
		// Substrate (reference repos warmed regardless of target). The entrypoint
		// has matching fallback defaults, so these keep the contract one-sourced.
		"WARD_SUBSTRATE_SEED":     containerSubstrateSeed,
		"WARD_SUBSTRATE_DEST":     containerSubstrateDest,
		"WARD_SUBSTRATE_MANIFEST": containerSubstrateManifest,
		"WARD_SUBSTRATE_TTL":      containerSubstrateTTL,
	}
	if p.Branch != "" {
		env["WARD_BRANCH"] = p.Branch
	}
	// The config role carries the per-role model/effort overlay in-container (ward#620);
	// empty (a bare `container up`) leaves today's env intact.
	if p.ConfigRole != "" {
		env["WARD_ROLE"] = p.ConfigRole
	}
	if p.Issue != 0 {
		env["WARD_TARGET_ISSUE"] = fmt.Sprintf("%d", p.Issue)
	}
	if p.WardFromSource {
		env["WARD_FROM_SOURCE"] = containerWardSrcMount
	}
	if p.Headless {
		env["WARD_HEADLESS"] = "1"
	}
	if p.Ask {
		env["WARD_ASK"] = "1"
	}
	if p.ReadOnly {
		env["WARD_READONLY"] = "1"
	}
	if p.DispatchBrokerAddr != "" {
		env[envDispatchBrokerAddr] = p.DispatchBrokerAddr
		env[envDispatchBrokerToken] = p.DispatchBrokerToken
	}
	if p.TSSidecar {
		// Per-connection proxy (never a host-wide ALL_PROXY), the box dialed by name;
		// socks5h so it resolves the tower's MagicDNS name tailnet-side (ward#349).
		env["WARD_TS_SOCKS5"] = proxySocks5Scheme + proxyBoxAddr()
		// A MagicDNS name, not a secret IP, so it rides plain (no SSM lookup; ward#337).
		env["WARD_TOWER_OLLAMA"] = towerOllamaURL()
		// The loopback forwarder's no-proxy endpoint: tools dial the tower at plain
		// localhost:<port> with no --proxy once the run starts the forwarder (ward#359).
		env["WARD_TOWER_OLLAMA_LOCAL"] = towerOllamaLocalURL()
		// Propagate the tower topology so the in-container `ward container forward`
		// --target follows the same override the host resolved (ward#395).
		env[envTowerHost] = towerMagicDNS()
		env[envTowerOllamaPort] = towerOllamaPort()
	}
	if p.GoBootstrap {
		env["WARD_USE_GO_BOOTSTRAP"] = "1"
	}
	if len(p.ExtraRepos) > 0 {
		env["WARD_EXTRA_REPOS"] = extraReposEnv(p.ExtraRepos)
	}
	// No WARD_CONTEXT_REPOS is emitted: the read-only context set is resolved in-container
	// from the fresh clone, since the host cwd may not be the target repo (ward#580).

	// A GitHub run clones off github.com + drives `gh` (ward#489); Forgejo runs emit
	// neither key, so their env is unchanged (WARD_FORGEJO_BASE stays Forgejo).
	if p.Forge == forgeGitHub {
		env["WARD_FORGE"] = p.Forge.String()
		env["WARD_CLONE_BASE"] = p.Forge.baseURL()
	}
	// A non-default landing policy rides in so the in-container reaper can refuse to
	// force-push main (ward#508); direct-main omits the key, keeping today's env intact.
	if !p.Workflow.landsOnMain() {
		env["WARD_WORKFLOW"] = string(p.Workflow.orDefault())
	}
	// --config overrides ride last so they win over any default emitted above (ward#616).
	for k, v := range p.ConfigEnv {
		env[k] = v
	}
	return env
}

// labels is the ward.* identity set a container wears for poll/reaper/sweep; issue
// rides only an engineer run, machine only when set (ward#364, docs/container.md).
func (p upPlan) labels() []string {
	role := p.Role
	if role == "" {
		role = roleSession
	}
	out := []string{
		containerLabel,
		labelRole + "=" + role,
		labelDriver + "=" + string(p.Mode),
		labelRepo + "=" + p.Repo.slug(),
	}
	if p.Machine != "" {
		out = append(out, labelMachine+"="+p.Machine)
	}
	if p.Issue > 0 {
		out = append(out, fmt.Sprintf("%s=%d", labelIssue, p.Issue))
	}
	// A non-default landing policy is stamped so poll/reaper/sweep can see it
	// without reading the container env (ward#508); direct-main stays unlabeled.
	if !p.Workflow.landsOnMain() {
		out = append(out, labelWorkflow+"="+string(p.Workflow.orDefault()))
	}
	return out
}

// dockerArgvHead is the verb + name/labels + entrypoint shared by the run and
// create argv builders.
func dockerArgvHead(verb string, p upPlan) []string {
	argv := []string{verb, "--name", p.Name}
	for _, l := range p.labels() {
		argv = append(argv, "--label", l)
	}
	argv = append(argv, "--entrypoint", containerWardAssets+"/"+containerEntrypointRel)
	// Tailnet route (mutually exclusive, off by default): --host-net shares the host's
	// namespace (ward#330), --ts-sidecar joins the shared ward-tailnet net (ward#349).
	switch {
	case p.TSSidecar:
		argv = append(argv, "--network="+tailnetNetwork())
	case p.HostNet:
		argv = append(argv, "--network=host")
	}
	// Map host.docker.internal to the host gateway so the surface's broker dial
	// works on Linux too; --network=host already resolves it, so skip it there.
	if p.DispatchBrokerAddr != "" && !p.HostNet {
		argv = append(argv, "--add-host", containerHostGateway+":host-gateway")
	}
	return argv
}

// proxyBoxAttached reports whether the standing box is among the space-separated
// container names attached to ward-tailnet (the preflight read; ward#349).
func proxyBoxAttached(names string) bool {
	for _, n := range strings.Fields(names) {
		if n == proxyBoxName() {
			return true
		}
	}
	return false
}

// dockerTailnetInspectArgv reads the names of the containers attached to the
// ward-tailnet network; it fails (non-zero) when the network does not exist (ward#349).
func dockerTailnetInspectArgv() []string {
	return []string{"network", "inspect", tailnetNetwork(),
		"--format", "{{range .Containers}}{{.Name}} {{end}}"}
}

// dockerTailnetCreateArgv creates the ward-tailnet network - the idempotent provisioning
// step (via ensureTailnetNetwork) that replaces the old hard failure (ward#597).
func dockerTailnetCreateArgv() []string {
	return []string{"network", "create", tailnetNetwork()}
}

// proxyBoxMissingWarning warns (true) when the mac-proxy box is not attached to
// ward-tailnet, so the run launches but its tower route won't resolve (ward#349, #597).
func proxyBoxMissingWarning(attachedNames string) (string, bool) {
	if proxyBoxAttached(attachedNames) {
		return "", false
	}
	return "WARNING: the standing tailnet proxy " + proxyBoxName() + " is not attached to " + tailnetNetwork() + ".\n" +
		"  The container still launches on the network, but the tailnet SOCKS5 route\n" +
		"  (the ollama tower, live-observe) will not resolve until the mac-proxy infra\n" +
		"  role is converged on this host. See docs/agent-ts-sidecar.md (ward#349, ward#597).", true
}

// hostNetTailnetWarning returns a loud warning (and true) when a --host-net run
// is unlikely to reach the tailnet on this host (ward#332; docs/agent-host-net.md).
func hostNetTailnetWarning(goos string, hasTailscale0 bool) (string, bool) {
	// Non-Linux is Docker Desktop: the run joins a LinuxKit VM netns, never a
	// tailnet node, so hasTailscale0 (the Mac's, not the VM's) is ignored here.
	if goos != "linux" {
		return "WARNING: --host-net cannot reach the tailnet on Docker Desktop.\n" +
			"  The container joins the LinuxKit VM's network namespace, not your\n" +
			"  " + goos + " host, so it inherits no tailscale0 and no MagicDNS - tailnet\n" +
			"  names (api, kai-tower-3026) will not resolve inside the run.\n" +
			"  --host-net only reaches the tailnet on a native-Linux host that is\n" +
			"  itself a tailnet node. See docs/agent-host-net.md (ward#332).", true
	}
	if !hasTailscale0 {
		return "WARNING: --host-net found no tailscale0 on this host's network namespace.\n" +
			"  The run joins this netns, so without a tailscale0 device it gets no\n" +
			"  tailnet route and MagicDNS names (api, kai-tower-3026) will not resolve.\n" +
			"  Bring this host onto the tailnet, or adopt the in-container tailscaled\n" +
			"  sidecar. See docs/agent-host-net.md (ward#332).", true
	}
	return "", false
}

// awsMountMissingWarning warns (true) when the aws capability is on but neither path
// delivered creds: export returned nothing and the ~/.aws fallback is empty (ward#586).
func awsMountMissingWarning(awsHome string, hasCreds bool) (string, bool) {
	if awsHome == "" || hasCreds {
		return "", false
	}
	return "WARNING: the aws capability is on but this host delivered no AWS credentials.\n" +
		"  ward first ran `aws configure export-credentials` (which resolves SSO, env, profile,\n" +
		"  assumed-role and IMDS creds) and it returned nothing, so ward fell back to binding\n" +
		"  " + awsHome + " read-only - but that dir holds no config or credentials file either.\n" +
		"  docker mounts the missing source as an EMPTY dir, so `aws`/`ssm` calls inside the run\n" +
		"  fail NoCredentials. Give this host an AWS identity (log into SSO, export AWS_* env, or\n" +
		"  add ~/.aws/config|credentials with SSM read/write) so --aws actually delivers creds.\n" +
		"  See docs/agent-capability.md (ward#586, ward#579).", true
}

// appendEnvAndImage appends the WARD_* env, the --env-file, the image, and the agent
// argv - the tail both builders share. The token rides --env-file, never inlined.
func appendEnvAndImage(argv []string, p upPlan, envFilePath string) []string {
	for _, k := range sortedKeys(p.wardEnv()) {
		argv = append(argv, "-e", k+"="+p.wardEnv()[k])
	}
	if envFilePath != "" {
		argv = append(argv, "--env-file", envFilePath)
	}
	argv = append(argv, p.Image)
	// Trailing args become the in-container agent's argv (entrypoint runs
	// `"$WARD_AGENT" "$@"`); empty for a bare interactive bring-up.
	return append(argv, p.AgentArgs...)
}

// dockerCreateArgv assembles `docker run` with every mount as a -v bind. Used on a
// host, where bind sources resolve on the daemon's filesystem.
func dockerCreateArgv(p upPlan, envFilePath string) []string {
	argv := dockerArgvHead("run", p)
	switch {
	case p.Capture:
		// Plain foreground `docker run`: no -d/-i/-t. Streams stdout (ward captures
		// it) while attaching no stdin, so it can't 125 through the broker (ward#606).
	case !p.Interactive:
		argv = append(argv, "-d")
	case p.TTY:
		argv = append(argv, "-it")
	default:
		// Attached, no terminal (agent/CI/pipe): keep stdin open, drop -t,
		// else docker aborts attaching stdin to a TTY-enabled container.
		argv = append(argv, "-i")
	}
	for _, m := range p.Mounts {
		argv = append(argv, "-v", m.arg())
	}
	return appendEnvAndImage(argv, p, envFilePath)
}

// dockerCreateNoBindsArgv assembles `docker create` (stopped) with only volume mounts;
// host binds are docker-cp'd in after, for an in-container dispatch (ward#323).
func dockerCreateNoBindsArgv(p upPlan, envFilePath string) []string {
	argv := dockerArgvHead("create", p)
	for _, m := range p.Mounts {
		if m.Volume {
			argv = append(argv, "-v", m.arg())
		}
	}
	return appendEnvAndImage(argv, p, envFilePath)
}

// hostBindMounts returns the non-volume mounts - the host-path binds docker-cp'd into
// the sibling when bind sources won't resolve on the daemon (ward#323).
func hostBindMounts(p upPlan) []mountSpec {
	var out []mountSpec
	for _, m := range p.Mounts {
		if !m.Volume {
			out = append(out, m)
		}
	}
	return out
}

// sortedKeys returns the map's keys in sorted order for deterministic argv.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// containerReapKeep is how many most-recently-exited ward containers the stale
// sweep keeps for post-mortem; older ones are reclaimed (docs/container-cleanup.md).
const containerReapKeep = 10

// dockerExitedListArgv builds the `docker ps` query for exited ward-managed
// containers, newest first, one name per line - the stale-sweep input (ward#272).
func dockerExitedListArgv() []string {
	return []string{"ps", "-a",
		"--filter", "label=" + containerLabel,
		"--filter", "status=exited",
		"--format", "{{.Names}}"}
}

// parseExitedContainerNames splits the `docker ps` output into non-blank names,
// newest first as docker lists them - the full exited set the sweep drains (ward#510).
func parseExitedContainerNames(psOutput string) []string {
	var names []string
	for _, line := range strings.Split(psOutput, "\n") {
		if n := strings.TrimSpace(line); n != "" {
			names = append(names, n)
		}
	}
	return names
}

// staleContainersToReap returns the exited-container names past the keep window
// (newest first, as `docker ps` lists them); blanks ignored, keep-or-fewer is nil.
func staleContainersToReap(psOutput string, keep int) []string {
	names := parseExitedContainerNames(psOutput)
	if keep < 0 {
		keep = 0
	}
	if len(names) <= keep {
		return nil
	}
	return names[keep:]
}

// dockerRmArgv builds `docker rm <names...>` (no -f: the sweep targets only
// already-exited containers, never running ones). Empty names yields nil.
func dockerRmArgv(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	return append([]string{"rm"}, names...)
}

// imageRef joins an image and tag, leaving an already-tagged or digest-pinned
// ref untouched.
func imageRef(image, tag string) string {
	if strings.Contains(image, "@") || strings.Contains(image[strings.LastIndex(image, "/")+1:], ":") {
		return image
	}
	if tag == "" {
		tag = containerImageTagDefault
	}
	return image + ":" + tag
}

// buildAgentArgv is the per-mode in-container argv builder (no setpriv prefix) +
// whether to stream-wrap output. Pure; the mode/argv switch dies in Phase 4 (#401).
func buildAgentArgv(e bootstrapEnv, seed []string) (argv []string, stream bool) {
	switch e.Mode {
	case "goose":
		if e.oneshot() {
			return append([]string{"goose", "run", "-t"}, seed...), false
		}
		return []string{"goose", "session"}, false
	case "codex":
		if e.oneshot() {
			return append([]string{"codex", "exec"}, seed...), false
		}
		return append([]string{"codex"}, seed...), false
	case "opencode", "qwen": // "qwen" is the retired alias (ward#401), still honoured
		if e.oneshot() {
			return append([]string{"opencode", "run"}, seed...), false
		}
		return []string{"opencode"}, false
	default:
		argv = []string{e.Agent}
		switch {
		case e.Ask:
			argv = append(argv, "-p")
		case e.Headless:
			argv = append(argv, "-p", "--verbose", "--output-format", "stream-json")
			stream = true
		}
		// --model rides only when resolved (ward#616); empty keeps today's bare launch.
		// Mirrors claude.go LaunchArgv. Effort is echo-only (no native claude flag).
		if e.ClaudeModel != "" {
			argv = append(argv, "--model", e.ClaudeModel)
		}
		argv = append(argv, seed...)
		return argv, stream
	}
}

// logAgentArgv emits the per-mode launch notes alongside buildAgentArgv (kept
// separate so that stays pure); the same Phase 4 mode switch (#401).
func logAgentArgv(e bootstrapEnv, seed []string) {
	switch e.Mode {
	case "goose":
		if e.oneshot() {
			blog("one-shot: goose run -t <prompt> (goose prints to this log)")
		} else if len(seed) > 0 {
			blog("interactive goose session: seed prompt is not auto-delivered (paste the issue)")
		}
	case "codex":
		if e.oneshot() {
			blog("one-shot: codex exec <prompt> (codex prints to this log)")
		}
	case "opencode", "qwen": // "qwen" is the retired alias (ward#401), still honoured
		if e.oneshot() {
			blog("one-shot: opencode run <prompt> (opencode prints to this log)")
		} else if len(seed) > 0 {
			blog("interactive opencode TUI: seed prompt is not auto-delivered (paste the issue)")
		}
	default:
		switch {
		case e.Ask:
			blog("ask: %s -p <question> (one-shot answer to this terminal)", e.Agent)
		case e.Headless:
			blog("headless: streaming %s progress to this log", e.Agent)
		}
	}
}
