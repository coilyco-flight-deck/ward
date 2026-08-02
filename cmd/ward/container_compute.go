package main

// container_compute.go holds the pure, testable computation behind `ward
// container`; cmd/ward/container.go owns side effects. See docs/container.md.

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/urfave/cli/v3"
)

const (
	// containerImageDefault and friends are product mechanics compiled into the
	// binary; supported operator overrides are applied at the command edge.
	containerImageDefault    = "forgejo.coilysiren.me/coilyco-flight-deck/ward"
	containerImageTagDefault = "release"

	// envAgentImage / envAgentTag pin the dev-base image + tag once for every
	// `ward agent` dispatch; an explicit --image/--tag still overrides (ward#312).
	envAgentImage = "WARD_AGENT_IMAGE"
	envAgentTag   = "WARD_AGENT_TAG"

	// envAgentVersion pins the ward release the host stages for the container,
	// independent of the dev-base image tag; --ward-version overrides it per run (ward#312).
	envAgentVersion = "WARD_AGENT_VERSION"

	// envAgentVersionSource records whether the container's ward version came from an
	// explicit operator pin, the host ward default, or in-container latest resolution.
	envAgentVersionSource = "WARD_VERSION_SOURCE"

	// containerWardAssets is where Ward's materialized entrypoint + doctrine are
	// bind-mounted, read-only. The image bakes none of this in.
	containerWardAssets     = "/opt/ward"
	containerEntrypointPath = "/opt/ward-shell-entrypoint.sh"
	containerEntrypointRel  = "entrypoint.sh"

	// containerWardSrcMount is where --ward-source mounts a ward checkout, so
	// the host stages the ward binary from it instead of downloading the release.
	containerWardSrcMount = "/opt/ward-src"

	// containerContextMount holds the read-only host cwd (operating context):
	// the only default host bind, a sibling of containerWardAssets (not nested).
	containerContextMount = "/opt/ward-context"

	// containerContextBundle is the fixed, read-only handoff path for an
	// already-materialized generic context bundle (ward#1511).
	containerContextBundle = "/opt/ward-context-bundle"
	containerContextTools  = containerContextBundle + "/bin"

	// containerGitcacheVol is a shared named volume of bare mirrors (never a
	// host dir) so fresh clones are cheap and never land in the host repo tree.
	containerGitcacheVol = "ward-gitcache"
	containerGitcacheMnt = "/gitcache"

	// containerAgentLogsMount is where the host agent-log drain binds read-only in a
	// director surface container; surface-only opt-in (ward#525, redacted source ward#526).
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
	labelRole                = "ward.role"
	labelDriver              = "ward.driver"
	labelCluster             = "ward.cluster"
	labelRepo                = "ward.repo"
	labelIssue               = "ward.issue"
	labelMachine             = "ward.machine"
	labelWorkflow            = "ward.workflow"
	labelVerificationFixture = "ward.verification-fixture"

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
	defaultTailnetProxy = "tailscale-proxy:1055"

	// proxySocks5Scheme is socks5h (not socks5): the proxy resolves the tower's
	// MagicDNS name tailnet-side, so the run dials by name (ward#337; the doc).
	proxySocks5Scheme = "socks5h://"

	// envTowerHost overrides the tower's MagicDNS node name, dialed by name through
	// the proxy (ward#337).
	envTowerHost     = "WARD_TOWER_HOST"
	defaultTowerHost = "kai-tower-3026"

	// envTowerOllamaPort overrides the port the tower serves ollama on.
	envTowerOllamaPort     = "WARD_TOWER_OLLAMA_PORT"
	defaultTowerOllamaPort = "11434"
)

// tailnetNetwork resolves the shared docker network name (env > bundle > baked default;
// ward#395). The bundle only fills the slot the baked literal fills now.
func tailnetNetwork() string {
	topo := currentContainerTopology()
	return envOrBundleOr(envTailnetNetwork, topo.TailnetNetwork, defaultTailnetNetwork)
}

// proxyBoxAddr resolves the SOCKS5 box's host:port endpoint (env > bundle > default;
// ward#395).
func proxyBoxAddr() string {
	topo := currentContainerTopology()
	return envOrBundleOr(envTailnetProxy, topo.TailnetProxy, defaultTailnetProxy)
}

// proxyBoxName is the proxy box's container name - the host half of proxyBoxAddr, used
// by the attach preflight. A port-less override falls back to the whole value.
func proxyBoxName() string {
	if host, _, err := net.SplitHostPort(proxyBoxAddr()); err == nil {
		return host
	}
	return proxyBoxAddr()
}

// towerMagicDNS resolves the tower's MagicDNS node name (env > bundle > baked default;
// ward#395).
func towerMagicDNS() string {
	topo := currentContainerTopology()
	return envOrBundleOr(envTowerHost, topo.TowerHost, defaultTowerHost)
}

// towerOllamaPort resolves the port the tower serves ollama on (env > bundle > default;
// ward#395).
func towerOllamaPort() string {
	topo := currentContainerTopology()
	return envOrBundleOr(envTowerOllamaPort, topo.TowerOllamaPort, defaultTowerOllamaPort)
}

// towerOllamaURL is the by-name tower endpoint a --ts-sidecar run dials through the
// proxy (ward#337; the doc).
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
// container composes.
type containerMode string

// container roles lead the name + the ward.role label (ward#364). director is a host
// loop, not a container, but its surface container runs as roleDirector (ward#353).
const (
	roleEngineer = "engineer"
	roleQA       = "qa"
	roleOps      = "ops"
	roleAdmin    = "admin"
	roleSession  = "session"
	// roleDirector labels the director session for reporting. It grants no
	// credentials, mounts, network access, or broker authority.
	roleDirector = "director"
)

// agentBinary is the in-container command each mode launches.
func (m containerMode) agentBinary() string {
	return mustAgentAdapter(m).Binary
}

// hostPreflightArgv is the host one-shot argv asking this mode's agent prompt,
// plus whether one exists (claude+goose yes, codex/qwen not yet). See docs/agent.md.
func (m containerMode) hostPreflightArgv(prompt string) ([]string, bool) { //nolint:unparam // ward#418 flipped the varying-prompt callers to the registry; only the contract test's constant prompt remains until Phase 4 deletes this switch
	return mustAgentAdapter(m).preflightArgv(prompt)
}

// contextLevel maps a mode onto the least-access ladder (2=full, 1=scoped, 0=minimal);
// see docs/container.md. goose is full (level 2) like claude, mirrored to its hints file.
func (m containerMode) contextLevel() int {
	return mustAgentAdapter(m).ContextLevel
}

// parseMode validates a --mode value against the embedded fleet roster.
func parseMode(s string) (containerMode, error) {
	m, err := cachedAgentManifest()
	if err != nil {
		return "", err
	}
	for _, a := range m.Agents {
		if a.Name == s {
			return containerMode(a.Name), nil
		}
	}
	return "", fmt.Errorf("unknown --mode %q: want a fleet agent name", s)
}

// targetRepo is a forgejo owner/name pair the container clones and works.
type targetRepo struct {
	Owner string
	Name  string
}

func (t targetRepo) slug() string { return t.Owner + "/" + t.Name }

// canonicalSlug is the identity key used at the grant boundary. Forge paths are
// case-insensitive, while the original spelling remains available for display.
func (t targetRepo) canonicalSlug() string { return strings.ToLower(t.slug()) }

// primaryWorkspaceDir preserves the primary checkout's long-standing, obvious cwd.
// Additional grants use grantedRepoWorkspaceDir so equal basenames can coexist.
func primaryWorkspaceDir(root string, repo targetRepo) string {
	return rootedPathJoin(root, repo.Name)
}

// grantedRepoWorkspaceDir gives every non-primary checkout an owner-qualified,
// normalized location under the workspace.
func grantedRepoWorkspaceDir(root string, repo targetRepo) string {
	return rootedPathJoin(root, repo.Owner, repo.Name)
}

func rootedPathJoin(root string, elements ...string) string {
	if filepath.VolumeName(root) != "" {
		return filepath.Join(append([]string{root}, elements...)...)
	}
	return path.Join(append([]string{root}, elements...)...)
}

// workspaceMapping renders the resolved primary + extra checkout locations for
// launch plans and seeded agent context.
func workspaceMapping(primary targetRepo, extra []targetRepo) []string {
	paths := []string{primary.slug() + " -> " + primaryWorkspaceDir(containerWorkspace, primary)}
	for _, repo := range extra {
		paths = append(paths, repo.slug()+" -> "+grantedRepoWorkspaceDir(containerWorkspace, repo))
	}
	return paths
}

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
		return normalizedTargetRepo(m[1], m[2], ref)
	}
	if m := repoPathRe.FindStringSubmatch(ref); m != nil {
		return normalizedTargetRepo(m[1], m[2], ref)
	}
	return targetRepo{}, fmt.Errorf("cannot parse repo ref %q: want owner/name or a forgejo clone URL", ref)
}

// normalizedTargetRepo rejects path components which filepath.Clean would fold.
// Repository refs are identifiers, never relative filesystem paths.
func normalizedTargetRepo(owner, name, ref string) (targetRepo, error) {
	if owner == "." || owner == ".." || name == "." || name == ".." {
		return targetRepo{}, fmt.Errorf("cannot parse repo ref %q: dot segments are not allowed", ref)
	}
	return targetRepo{Owner: owner, Name: name}, nil
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

// issueScopedContainerName builds the issue-scoped container name shape:
// <role>-<driver>-<repo>-<N>. Engineer and issue-scoped runs use it.
func issueScopedContainerName(role string, mode containerMode, repo targetRepo, issue int) string {
	return fmt.Sprintf("%s-%s-%s-%d", role, mode, safeRepoName(repo), issue)
}

// containerRoleName builds the role-led container name: engineer uses the
// issue-scoped shape, and other roles use <role>-<driver>-<suffix>.
func containerRoleName(role string, mode containerMode, repo targetRepo, issue int, suffix string) string {
	if role == roleEngineer {
		return issueScopedContainerName(role, mode, repo, issue)
	}
	return fmt.Sprintf("%s-%s-%s", role, mode, suffix)
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
	// AssetsDir holds Ward's materialized entrypoint + doctrine, written to a
	// per-run staging dir and mounted read-only. Always set in practice.
	AssetsDir string
	// WardSource, when non-empty, mounts a local ward checkout (--ward-source)
	// so the container builds ward from source instead of downloading.
	WardSource string
	// AgentLogsDir, when non-empty, mounts a host agent-log drain read-only at
	// containerAgentLogsMount (ward#525); the surface passes the REDACTED tree (ward#526).
	AgentLogsDir string
	// ContextBundle, when non-empty, mounts one validated generic context bundle
	// read-only. The bundle cannot declare authority (ward#1511).
	ContextBundle string
}

// leastAccessMounts is the default set: cwd + assets read-only and the gitcache
// volume. The target repo is never mounted; the optional mounts are opt-ins.
func leastAccessMounts(hostCwd string, opts mountOpts) []mountSpec {
	mounts := []mountSpec{
		{Source: containerGitcacheVol, Target: containerGitcacheMnt, ReadOnly: false, Volume: true},
	}
	if strings.TrimSpace(hostCwd) != "" {
		mounts = append(mounts, mountSpec{Source: hostCwd, Target: containerContextMount, ReadOnly: true, Volume: false})
	}
	if opts.AssetsDir != "" {
		mounts = append(mounts, mountSpec{Source: opts.AssetsDir, Target: containerWardAssets, ReadOnly: true, Volume: false})
		mounts = append(mounts, mountSpec{Source: filepath.Join(opts.AssetsDir, containerEntrypointRel), Target: containerEntrypointPath, ReadOnly: true, Volume: false})
	}
	if opts.WardSource != "" {
		mounts = append(mounts, mountSpec{Source: opts.WardSource, Target: containerWardSrcMount, ReadOnly: true, Volume: false})
	}
	if opts.AgentLogsDir != "" {
		mounts = append(mounts, mountSpec{Source: opts.AgentLogsDir, Target: containerAgentLogsMount, ReadOnly: true, Volume: false})
	}
	if opts.ContextBundle != "" {
		mounts = append(mounts, mountSpec{Source: opts.ContextBundle, Target: containerContextBundle, ReadOnly: true, Volume: false})
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
	// Role leads the name + the ward.role label (engineer/qa/session).
	Role string
	// ConfigRole carries the workflow label in-container as WARD_ROLE
	// (ward#620). Consumers must treat it as opaque metadata.
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
	// Capture marks an attached foreground run ward reads stdout back from:
	// streams stdout, attaches no stdin (ward#411, #606).
	Capture bool
	// WardVersion pins the ward release the entrypoint downloads (matches the
	// launcher); "dev" or "" tells the entrypoint to resolve the latest release.
	WardVersion string
	// WardVersionSource records explicit pin, host ward, or latest resolution.
	WardVersionSource string
	// WardFromSource is set when --ward-source mounted a checkout: the
	// host staging builds ward from it instead of downloading.
	WardFromSource bool
	// MemoryLimit and MemorySwap cap the container's cgroup memory ceiling.
	MemoryLimit string
	MemorySwap  string
	// AgentArgs ride after the image as the in-container agent's argv (the
	// entrypoint's `"$WARD_AGENT" "$@"`); empty for a bare interactive bring-up.
	AgentArgs []string
	// Headless runs the in-container agent in print mode (claude -p), exporting
	// WARD_HEADLESS=1; set by the detached `ward agent engineer` run.
	Headless bool
	// Ask runs the in-container agent one-shot, attached (claude -p plain, no
	// stream-json); exports WARD_ASK=1 for one-shot attached runs.
	Ask bool
	// ExtraRepos are additional writable repos this run was granted to clone +
	// operate against (--repo, ward#230); see docs/container-multi-repo.md.
	ExtraRepos []targetRepo
	// Issue is the carried issue number (0 for a bare `container up`), exported as
	// WARD_TARGET_ISSUE so the reaper can release a pre-launch hold (ward#264).
	Issue int
	// ReadOnly marks a read-only surface session (the director's drain surface, ward#293,
	// ward#353): exports WARD_READONLY=1. See docs/agent-surface.md.
	ReadOnly bool
	// DispatchBrokerAddr, when set, exports WARD_DISPATCH_BROKER_ADDR.
	DispatchBrokerAddr string
	// ClusterID is the stable harness-scoped collaboration identity shared by
	// the broker, optional director, and every attached peer.
	ClusterID string
	// DispatchBrokerToken exports WARD_DISPATCH_BROKER_TOKEN, the per-launch secret
	// the surface echoes back so the broker service authenticates the dial.
	DispatchBrokerToken string
	// DispatchBrokerNetwork joins a broker-launched sibling to the broker's
	// Compose network so the broker DNS name remains reachable.
	DispatchBrokerNetwork string
	// AgentID is the broker-authenticated peer identity exposed to the harness.
	AgentID string
	// DispatchRequestID is the broker-minted idempotency key carried as an env
	// value and Docker label on the sibling container.
	DispatchRequestID string
	// HostNet joins the container to the host network (--network=host) so a run
	// inherits the host's tailnet route (--host-net, ward#330). docs/agent-host-net.md.
	HostNet bool
	// TSSidecar attaches the run to the shared ward-tailnet network so it reaches
	// the standing tailscale-proxy box (--ts-sidecar, ward#349). docs/agent-ts-sidecar.md.
	TSSidecar bool
	// Workflow is the run's landing policy (--workflow, ward#508): non-merge-remote-main
	// runs export WARD_WORKFLOW + a ward.workflow label. See docs/agent-workflow.md.
	Workflow workflowMode
	// VerificationFixture marks a run constrained to a deployment-admitted
	// disposable issue. It grants no authority by itself.
	VerificationFixture bool
	// SkipPreflight mirrors --skip-preflight into the container launch gate so host
	// preflight/review and in-container smoke probes share the same escape hatch.
	SkipPreflight bool
	// SkipSmokeTest carries the narrow --skip-smoke-test escape hatch without
	// bypassing any host-side or review preflight work.
	SkipSmokeTest bool
	// ReviewClass pins the pre-landing review panel's autonomy class into the
	// container (WARD_REVIEW_CLASS, ward#134). See docs/dispatch-review.md.
	ReviewClass string
	// ConfigEnv are resolved config-derived WARD_* env keys: selected fleet attribution
	// plus `--config` overrides (ward#616), applied over the baked default in wardEnv.
	ConfigEnv map[string]string
	// ContextBundle is the resolved host path for the optional read-only mount.
	// The container env gets the fixed target path instead (ward#1511).
	ContextBundle string
	// ContextTools records whether the validated bundle has a non-empty bin
	// directory. Its fixed container path is appended to the agent PATH.
	ContextTools bool
}

const (
	wardVersionSourceExplicit = "explicit pin"
	wardVersionSourceHost     = "host ward"
	wardVersionSourceLatest   = "latest (resolved in-container)"
)

// wardVersionLaunchLabel renders the startup-visible ward version summary.
func wardVersionLaunchLabel(version, source string) string {
	version = strings.TrimSpace(version)
	source = strings.TrimSpace(source)
	switch source {
	case wardVersionSourceExplicit, wardVersionSourceHost:
		if version == "" {
			return source
		}
		return source + " " + version
	case wardVersionSourceLatest:
		return wardVersionSourceLatest
	}
	if version == "" || version == "dev" {
		return wardVersionSourceLatest
	}
	return wardVersionSourceHost + " " + version
}

// resolveWardVersionSource classifies the resolved version for startup logs and
// child-engineer forwarding.
func resolveWardVersionSource(c *cli.Command, version string) string {
	version = strings.TrimSpace(version)
	if version == "" || version == "dev" {
		return wardVersionSourceLatest
	}
	if c.IsSet("ward-version") {
		return wardVersionSourceExplicit
	}
	return wardVersionSourceHost
}

// extraRepoLogLine describes one repo that ended up in the merged grant set.
type extraRepoLogLine struct {
	Slug   string
	Reason string
}

// parseExtraRepos resolves --repo grants, dropping target and canonical duplicates.
// Every extra gets an owner-qualified workspace path, so basenames may overlap.
func parseExtraRepos(refs []string, target targetRepo) ([]targetRepo, error) {
	var out []targetRepo
	seenSlug := map[string]bool{}
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		repo, err := parseRepoRef(ref)
		if err != nil {
			return nil, fmt.Errorf("--repo %q: %w", ref, err)
		}
		if repo.canonicalSlug() == target.canonicalSlug() {
			continue // the target is always cloned; naming it is a no-op
		}
		if seenSlug[repo.canonicalSlug()] {
			continue
		}
		seenSlug[repo.canonicalSlug()] = true
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
	"agent.goose.model":       "WARD_GOOSE_MODEL",
	"agent.opencode.model":    "WARD_OPENCODE_MODEL",
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
		return map[string]string{}, nil
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

func resolveLaunchConfigEnv(configEntries []string, cwd string, mode containerMode) (map[string]string, error) {
	configEnv, err := parseConfigOverrides(configEntries)
	if err != nil {
		return nil, err
	}
	launch, err := loadLaunchConfig()
	if err != nil {
		return nil, fmt.Errorf("load typed harness config: %w", err)
	}
	configEnv = addHarnessConfigEnv(configEnv, launch)
	configEnv = addAgentIdentityConfigEnv(configEnv, mode)
	configEnv = addLaunchAttributionConfigEnv(configEnv, launch, cwd)
	return configEnv, nil
}

// addHarnessConfigEnv projects only explicit environment settings into the
// container. Role values never select models, reasoning, endpoints, or identity.
func addHarnessConfigEnv(env map[string]string, launch launchConfig) map[string]string {
	if env == nil {
		env = map[string]string{}
	}
	_ = launch
	addConfigEnvDefault(env, "WARD_CLAUDE_MODEL")
	addConfigEnvDefault(env, "WARD_CLAUDE_REASONING_EFFORT")
	addConfigEnvDefault(env, "WARD_CODEX_MODEL")
	addConfigEnvDefault(env, "WARD_CODEX_REASONING_EFFORT")
	addConfigEnvDefault(env, "WARD_CODEX_VERBOSITY")
	addConfigEnvDefault(env, "WARD_OPENCODE_MODEL")
	addConfigEnvDefault(env, "WARD_OLLAMA_URL")
	addConfigEnvDefault(env, "WARD_GOOSE_MODEL")
	return env
}

func addAgentIdentityConfigEnv(env map[string]string, mode containerMode) map[string]string {
	if env == nil {
		env = map[string]string{}
	}
	identity := mode.defaultAgentIdentity()
	if strings.TrimSpace(env[envAgentDisplayName]) == "" {
		env[envAgentDisplayName] = identity.Name
	}
	if strings.TrimSpace(env[envAgentPronouns]) == "" && strings.TrimSpace(identity.Pronouns) != "" {
		env[envAgentPronouns] = identity.Pronouns
	}
	return env
}

func addConfigEnvDefault(env map[string]string, key string) {
	if strings.TrimSpace(env[key]) != "" {
		return
	}
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		env[key] = value
		return
	}
}

func validateLocalHarnessConfig(mode containerMode, model, endpoint string) error {
	if strings.TrimSpace(model) == "" {
		switch mode {
		case modeGoose:
			return fmt.Errorf("missing agent.goose.model for goose: set it in baked policy, WARD_GOOSE_MODEL, or --config agent.goose.model=<model>")
		case modeOpencode:
			return fmt.Errorf("missing agent.opencode.model for opencode: set it in baked policy, WARD_OPENCODE_MODEL, or --config agent.opencode.model=<model>")
		case modeClaude, modeCodex:
			return nil
		}
	}
	if mode == modeOpencode && strings.TrimSpace(endpoint) == "" {
		return fmt.Errorf("missing agent.opencode.endpoint for opencode: set it in baked policy, WARD_OLLAMA_URL, or --config agent.opencode.endpoint=<url>")
	}
	return nil
}

// addLaunchAttributionConfigEnv projects the typed fallback commit identity into
// the explicit bootstrap env vars, falling back to git config before the placeholder.
func addLaunchAttributionConfigEnv(env map[string]string, launch launchConfig, cwd string) map[string]string {
	if env == nil {
		env = map[string]string{}
	}
	attribution := launch.Attribution
	if strings.TrimSpace(env["WARD_GIT_NAME"]) == "" {
		env["WARD_GIT_NAME"] = resolveLaunchGitIdentity(cwd, attribution.Name, "user.name")
	}
	if strings.TrimSpace(env["WARD_GIT_EMAIL"]) == "" {
		env["WARD_GIT_EMAIL"] = resolveLaunchGitIdentity(cwd, attribution.Email, "user.email")
	}
	if containsExamplePlaceholder(env["WARD_GIT_NAME"]) || containsExamplePlaceholder(env["WARD_GIT_EMAIL"]) {
		warnLaunchGitIdentityPlaceholder(env["WARD_GIT_NAME"], env["WARD_GIT_EMAIL"])
	}
	return env
}

var launchGitIdentityPlaceholderWarnOnce sync.Once

func warnLaunchGitIdentityPlaceholder(name, email string) {
	launchGitIdentityPlaceholderWarnOnce.Do(func() {
		writef(os.Stderr, "ward container: launch git identity still uses the baked placeholder %q <%s>; set git user.name/user.email or WARD_GIT_* to replace it\n",
			strings.TrimSpace(name), strings.TrimSpace(email))
	})
}

func validateLandingLaunchGitIdentity(env map[string]string) error {
	name := strings.TrimSpace(env["WARD_GIT_NAME"])
	email := strings.TrimSpace(env["WARD_GIT_EMAIL"])
	if name == "" || email == "" {
		return fmt.Errorf("launch git identity is incomplete (%q <%s>); set git user.name/user.email or WARD_GIT_NAME/WARD_GIT_EMAIL before launching a landing-capable engineer run; %s",
			name, email, setupNextStep)
	}
	if containsExamplePlaceholder(name) || containsExamplePlaceholder(email) {
		return fmt.Errorf("launch git identity still uses the baked placeholder %q <%s>; set git user.name/user.email or WARD_GIT_NAME/WARD_GIT_EMAIL before launching a landing-capable engineer run; %s",
			name, email, setupNextStep)
	}
	return nil
}

// resolveLaunchGitIdentity prefers a real fleet attribution, then repo-local git
// config, then global git config, and finally keeps the baked placeholder.
func resolveLaunchGitIdentity(cwd, fallback, key string) string {
	if v := strings.TrimSpace(fallback); v != "" && !containsExamplePlaceholder(v) {
		return v
	}
	if v := strings.TrimSpace(gitConfigValue(cwd, false, key)); v != "" {
		return v
	}
	if v := strings.TrimSpace(gitConfigValue("", true, key)); v != "" {
		return v
	}
	return strings.TrimSpace(fallback)
}

// gitConfigValue reads one git config key from the requested scope.
func gitConfigValue(cwd string, global bool, key string) string {
	var argv []string
	switch {
	case global:
		argv = []string{"config", "--global", "--get", key}
	case strings.TrimSpace(cwd) != "":
		argv = []string{"-C", cwd, "config", "--local", "--get", key}
	default:
		return ""
	}
	out, err := exec.Command("git", argv...).CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// launchEnvAllowlist copies locale/time context while keeping the raw host env denied.
// Terminal capability stays local to Docker's PTY so Codex redraws reliably (ward#1512).
func launchEnvAllowlist() map[string]string {
	env := map[string]string{}
	for _, key := range []string{
		"LANG",
		"TZ",
	} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			env[key] = v
		}
	}
	for _, raw := range os.Environ() {
		key, value, ok := strings.Cut(raw, "=")
		if !ok || !strings.HasPrefix(key, "LC_") {
			continue
		}
		if value = strings.TrimSpace(value); value != "" {
			env[key] = value
		}
	}
	// The container owns this pseudo-terminal; keep Ward's known-good profile.
	env["TERM"] = "xterm-256color"
	env["COLORTERM"] = "truecolor"
	return env
}

// correlationEnv renders the stable, non-secret run envelope that the agent
// process and any request-header adapter can see.
func (p upPlan) correlationEnv() map[string]string {
	env := map[string]string{
		"WARD_RUN_ID":         p.Name,
		"WARD_CONTAINER_NAME": p.Name,
		"WARD_HARNESS":        string(p.Mode),
		"WARD_TARGET_REPO":    p.Repo.slug(),
		"WARD_TARGET_OWNER":   p.Repo.Owner,
		"WARD_TARGET_NAME":    p.Repo.Name,
		"WARD_CONTEXT_LEVEL":  fmt.Sprintf("%d", lookupAgent(p.Mode).Record().ContextLevel),
		"WARD_VERSION":        p.WardVersion,
	}
	if p.ConfigRole != "" {
		env["WARD_ROLE"] = p.ConfigRole
	} else if p.Role != "" {
		env["WARD_ROLE"] = p.Role
	}
	if p.Repo.Owner != "" && p.Repo.Name != "" {
		if p.Issue > 0 {
			env["WARD_ISSUE_REF"] = fmt.Sprintf("%s#%d", p.Repo.slug(), p.Issue)
		}
	}
	if threadID := harnessThreadID(p.Mode); threadID != "" {
		env["WARD_THREAD_ID"] = threadID
	}
	return env
}

// wardEnv is the non-secret WARD_* config the entrypoint reads. Everything
// here is safe to print and to record; the token never appears.
func (p upPlan) wardEnv() map[string]string { //nolint:gocyclo,cyclop,gocognit,funlen
	topo := currentContainerTopology()
	rec := lookupAgent(p.Mode).Record() // registry data reads (Phase 3, ward#418)
	env := map[string]string{
		// The friendly docker --name (plan.Name) so in-container tooling (the status
		// line) can show it; HOSTNAME carries only the container ID (ward#365).
		"WARD_CONTAINER_NAME": p.Name,
		// Explicit "inside a ward container" marker host-only fleet-walk scripts fence
		// on (ward#114); a host shell never has it. See docs/container-skill-surface.md.
		"WARD_CONTAINER":      "1",
		"WARD_TARGET_REPO":    p.Repo.slug(),
		"WARD_TARGET_OWNER":   p.Repo.Owner,
		"WARD_TARGET_NAME":    p.Repo.Name,
		"WARD_RUN_ID":         p.Name,
		"WARD_FORGEJO_BASE":   p.ForgejoBase,
		"WARD_MODE":           string(p.Mode),
		"WARD_HARNESS":        string(p.Mode),
		"WARD_CONTEXT_LEVEL":  fmt.Sprintf("%d", rec.ContextLevel),
		"WARD_AGENT":          rec.Binary,
		"WARD_AGENT_HOME":     "/home/ubuntu/.ward",
		"WARD_GITCACHE":       containerGitcacheMnt,
		"WARD_CONTEXT_SRC":    containerContextMount,
		"WARD_MIRROR_NAME":    p.Repo.mirrorName(),
		"WARD_VERSION":        p.WardVersion,
		"WARD_VERSION_SOURCE": wardVersionLaunchLabel(p.WardVersion, p.WardVersionSource),
		envHostGOOS:           launchHostGOOS(),
		// Substrate (reference repos warmed regardless of target). The entrypoint
		// has matching fallback defaults, so these keep the contract one-sourced.
		"WARD_SUBSTRATE_SEED":     envOrBundleOr("WARD_SUBSTRATE_SEED", topo.SubstrateSeed, containerSubstrateSeed),
		"WARD_SUBSTRATE_DEST":     envOrBundleOr("WARD_SUBSTRATE_DEST", topo.SubstrateDest, containerSubstrateDest),
		"WARD_SUBSTRATE_MANIFEST": envOrBundleOr("WARD_SUBSTRATE_MANIFEST", topo.SubstrateManifest, containerSubstrateManifest),
		"WARD_SUBSTRATE_TTL":      envOrBundleOr("WARD_SUBSTRATE_TTL", topo.SubstrateTTL, containerSubstrateTTL),
	}
	if p.Branch != "" {
		env["WARD_BRANCH"] = p.Branch
	}
	// The workflow label is descriptive metadata only. A bare `container up`
	// leaves it empty.
	if p.ConfigRole != "" {
		env["WARD_ROLE"] = p.ConfigRole
	}
	if p.Issue != 0 {
		env["WARD_TARGET_ISSUE"] = fmt.Sprintf("%d", p.Issue)
		env["WARD_ISSUE_REF"] = fmt.Sprintf("%s#%d", p.Repo.slug(), p.Issue)
	}
	if p.WardFromSource {
		env["WARD_FROM_SOURCE"] = containerWardSrcMount
	}
	if p.ContextBundle != "" {
		env["WARD_CONTEXT_BUNDLE"] = containerContextBundle
	}
	if p.ContextTools {
		env["WARD_CONTEXT_TOOLS"] = containerContextTools
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
	if p.ClusterID != "" {
		env[envClusterID] = p.ClusterID
	}
	if p.AgentID != "" {
		env[envAgentID] = p.AgentID
	}
	if p.DispatchRequestID != "" {
		env[envDispatchRequestID] = p.DispatchRequestID
	}
	if p.TSSidecar {
		// Per-connection proxy (never a host-wide ALL_PROXY), the box dialed by name;
		// socks5h so it resolves the tower's MagicDNS name tailnet-side (ward#349).
		env["WARD_TS_SOCKS5"] = proxySocks5Scheme + proxyBoxAddr()
		// A MagicDNS name, not a secret IP, so it rides plain (ward#337).
		env["WARD_TOWER_OLLAMA"] = towerOllamaURL()
		// The loopback forwarder's no-proxy endpoint: tools dial the tower at plain
		// localhost:<port> with no --proxy once the run starts the forwarder (ward#359).
		env["WARD_TOWER_OLLAMA_LOCAL"] = towerOllamaLocalURL()
		// Propagate the tower topology so the in-container `ward container forward`
		// --target follows the same override the host resolved (ward#395).
		env[envTowerHost] = towerMagicDNS()
		env[envTowerOllamaPort] = towerOllamaPort()
	}
	if len(p.ExtraRepos) > 0 {
		env["WARD_EXTRA_REPOS"] = extraReposEnv(p.ExtraRepos)
	}
	// No WARD_CONTEXT_REPOS is emitted: the read-only context set is resolved in-container
	// from the fresh clone, since the host cwd may not be the target repo (ward#580).

	// GitHub and GitLab runs clone off their own forge + drive their host-side
	// issue clients, so the clone base rides only for non-Forgejo forges.
	if p.Forge != forgeForgejo {
		env["WARD_FORGE"] = p.Forge.String()
		env["WARD_CLONE_BASE"] = p.Forge.baseURL()
	}
	// A non-default landing policy rides in so the reaper can refuse to
	// force-push main (ward#508); merge-remote-main omits the key.
	if !p.Workflow.landsOnMain() {
		env["WARD_WORKFLOW"] = string(p.Workflow.orDefault())
	}
	for k, v := range p.correlationEnv() {
		env[k] = v
	}
	if p.SkipPreflight || p.SkipSmokeTest {
		env["WARD_SMOKE_TEST_SKIP"] = "1"
	}
	// The review panel's autonomy class rides in so the in-container gate reads it
	// deterministically, never from the (untrusted) worker (ward#134).
	if p.ReviewClass != "" {
		env[reviewClassEnv] = p.ReviewClass
	}
	p.addVerificationFixtureEnv(env)
	// --config overrides ride last so they win over any default emitted above (ward#616).
	for k, v := range p.ConfigEnv {
		env[k] = v
	}
	return env
}

func (p upPlan) addVerificationFixtureEnv(env map[string]string) {
	if p.VerificationFixture {
		env["WARD_VERIFICATION_FIXTURE"] = "1"
	}
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
	if p.ClusterID != "" {
		out = append(out, labelCluster+"="+p.ClusterID)
	}
	if p.Machine != "" {
		out = append(out, labelMachine+"="+p.Machine)
	}
	if p.Issue > 0 {
		out = append(out, fmt.Sprintf("%s=%d", labelIssue, p.Issue))
	}
	if p.DispatchRequestID != "" {
		out = append(out, labelDispatchRequest+"="+p.DispatchRequestID)
	}
	// A non-default landing policy is stamped so poll/reaper/sweep can see it
	// without reading the container env (ward#508); merge-remote-main stays unlabeled.
	if !p.Workflow.landsOnMain() {
		out = append(out, labelWorkflow+"="+string(p.Workflow.orDefault()))
	}
	if p.VerificationFixture {
		out = append(out, labelVerificationFixture+"=true")
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
	argv = append(argv, "--entrypoint", containerEntrypointPath)
	// Tailnet route (mutually exclusive, off by default): --host-net shares the host's
	// namespace (ward#330), --ts-sidecar joins the shared ward-tailnet net (ward#349).
	switch {
	case p.DispatchBrokerNetwork != "":
		argv = append(argv, "--network="+p.DispatchBrokerNetwork)
	case p.TSSidecar:
		argv = append(argv, "--network="+tailnetNetwork())
	case p.HostNet:
		argv = append(argv, "--network=host")
	}
	if p.MemoryLimit != "" {
		argv = append(argv, "--memory="+p.MemoryLimit)
	}
	if p.MemorySwap != "" {
		argv = append(argv, "--memory-swap="+p.MemorySwap)
	}
	// Map host.docker.internal to the host gateway so the surface's broker dial
	// works on Linux too; --network=host already resolves it, so skip it there.
	if strings.HasPrefix(p.DispatchBrokerAddr, containerHostGateway+":") && !p.HostNet {
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

// proxyBoxMissingWarning warns (true) when the tailscale-proxy box is not attached to
// ward-tailnet, so the run launches but its tower route won't resolve (ward#349, #597).
func proxyBoxMissingWarning(attachedNames string) (string, bool) {
	if proxyBoxAttached(attachedNames) {
		return "", false
	}
	return "WARNING: the standing tailnet proxy " + proxyBoxName() + " is not attached to " + tailnetNetwork() + ".\n" +
		"  The container still launches on the network, but the tailnet SOCKS5 route\n" +
		"  (the ollama tower, live-observe) will not resolve until the tailscale-proxy infra\n" +
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

// appendEnvAndImage appends the WARD_* env, the --env-file, the image, and the agent
// argv - the tail both builders share. The token rides --env-file, never inlined.
func appendEnvAndImage(argv []string, p upPlan, envFilePath string) []string {
	env := p.wardEnv()
	launchEnv := launchEnvAllowlist()
	for _, k := range sortedKeys(env) {
		argv = append(argv, "-e", k+"="+env[k])
	}
	for _, k := range sortedKeys(launchEnv) {
		argv = append(argv, "-e", k+"="+launchEnv[k])
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

// exitedContainerSnapshot is the minimal data needed to decide whether one
// stopped container has aged past the retention window.
type exitedContainerSnapshot struct {
	Name       string
	FinishedAt time.Time
	HasFinish  bool
}

// staleContainersToReap returns the exited-container names older than the TTL.
// Containers with an unreadable finish time are skipped best-effort.
func staleContainersToReap(now time.Time, containers []exitedContainerSnapshot, ttl time.Duration) []string {
	if ttl < 0 {
		ttl = 0
	}
	var stale []string
	for _, c := range containers {
		if !c.HasFinish {
			continue
		}
		if now.Sub(c.FinishedAt) >= ttl {
			stale = append(stale, c.Name)
		}
	}
	return stale
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
	if image == "" {
		image = agentImageDefault()
	}
	if tag == "" {
		tag = agentTagDefault()
	}
	return image + ":" + tag
}

// buildAgentArgv is the per-mode in-container argv builder (no setpriv prefix) +
// whether to stream-wrap output. Pure; the mode/argv switch dies in Phase 4 (#401).
func buildAgentArgv(e bootstrapEnv, seed []string) (argv []string, stream bool) {
	return mustAgentAdapter(containerMode(e.Mode)).launchArgv(e.Headless, e.Ask, e.ClaudeModel, seed)
}

// logAgentArgv emits the per-mode launch notes alongside buildAgentArgv (kept
// separate so that stays pure); the same Phase 4 mode switch (#401).
func logAgentArgv(e bootstrapEnv, seed []string) {
	adapter := mustAgentAdapter(containerMode(e.Mode))
	switch {
	case e.Ask && len(adapter.Argv.Preflight) > 0:
		blog("one-shot: %s <prompt> (prompt on stdin; prints to this log)", strings.Join(adapter.Argv.Preflight, " "))
	case e.oneshot() && adapter.Name == string(modeGoose):
		blog("one-shot: %s <prompt> (prompt on stdin; prints to this log)", strings.Join(adapter.Argv.Headless, " "))
	case e.oneshot():
		blog("one-shot: %s <prompt> (prints to this log)", strings.Join(adapter.Argv.Headless, " "))
	case len(seed) > 0:
		blog("interactive %s session: seed prompt is not auto-delivered (paste the issue)", adapter.Binary)
	}
}
