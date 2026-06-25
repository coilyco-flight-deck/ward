package main

// container_compute.go holds the pure, testable computation behind `ward
// container`; cmd/ward/container.go owns side effects. See docs/container.md.

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
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

	// containerAWSMount is where ~/.aws lands under --aws (broad SSM read,
	// off by default; the forgejo token is injected single-purpose instead).
	containerAWSMount = "/root/.aws"

	// containerDockerSock is the host docker socket bound into an explore session so
	// it can dispatch sibling runs; same path both sides (ward#315). docs/agent-explore.md.
	containerDockerSock = "/var/run/docker.sock"

	// containerNamePrefix anchors every ward-managed container name so a
	// `docker ps` filter can pick ward's containers out of the host's set.
	containerNamePrefix = "ward"

	// containerLabel marks ward-managed containers for filtering.
	containerLabel = "ward.container=1"

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

const (
	// tsSidecarImage is the upstream tailscale image the carry's userspace SOCKS5
	// sidecar runs (no /dev/net/tun, no NET_ADMIN, no route; ward#333).
	tsSidecarImage = "tailscale/tailscale:latest"

	// tsSidecarSocks5Host is the loopback SOCKS5 the userspace sidecar serves; the
	// carry reaches it over a shared netns, so it binds loopback not 0.0.0.0.
	tsSidecarSocks5Host = "127.0.0.1:1055"

	// tsSidecarSocks5Scheme is socks5h (not socks5): the proxy resolves the tower's
	// MagicDNS name tailnet-side, so the carry dials by name (ward#337; the doc).
	tsSidecarSocks5Scheme = "socks5h://"

	// tsSidecarHostname is the tailnet node name the sidecar registers under - the
	// tag:proxy node the /coilysiren/mac-proxy/ts-authkey key is minted for.
	tsSidecarHostname = "mac-proxy"

	// tsSidecarSuffix names a carry's sidecar off the carry, so the carry joins its
	// netns deterministically and the orphan sweep can pair the two back up.
	tsSidecarSuffix = "-ts"

	// tsSidecarLabel marks the userspace sidecars so the orphan sweep can find one
	// whose carry is gone and reclaim it (ward#333).
	tsSidecarLabel = "ward.sidecar=ts"

	// towerMagicDNSName is the tower's MagicDNS node name; a --ts-sidecar carry dials
	// it by name through the proxy (resolved tailnet-side), no SSM IP lookup (ward#337).
	towerMagicDNSName = "kai-tower-3026"

	// towerOllamaPort is the port kai-tower-3026 serves ollama on over the tailnet.
	towerOllamaPort = "11434"

	// towerOllamaURL is the by-name endpoint a --ts-sidecar carry dials through the
	// proxy; a constant, no per-launch SSM IP lookup (ward#337; the doc).
	towerOllamaURL = "http://" + towerMagicDNSName + ":" + towerOllamaPort
)

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
	modeClaude containerMode = "claude"
	modeCodex  containerMode = "codex"
	modeQwen   containerMode = "qwen"
	modeGoose  containerMode = "goose"
)

// visionCapable reports whether the harness can take multimodal blocks; the
// local ollama harnesses (qwen/goose) can't, so read_image 400s them (ward#157).
func (m containerMode) visionCapable() bool {
	switch m {
	case modeQwen, modeGoose:
		return false
	case modeClaude, modeCodex:
		return true
	default:
		return true
	}
}

// agentBinary is the in-container command each mode launches.
func (m containerMode) agentBinary() string {
	switch m {
	case modeCodex:
		return "codex"
	case modeQwen:
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
func (m containerMode) hostPreflightArgv(prompt string) ([]string, bool) {
	switch m {
	case modeClaude:
		return []string{m.agentBinary(), "-p", prompt}, true
	case modeGoose:
		return []string{m.agentBinary(), "run", "-t", prompt}, true
	case modeCodex, modeQwen:
		return nil, false
	default:
		return nil, false
	}
}

// contextLevel maps a mode onto the least-access ladder (2=full, 1=scoped, 0=minimal);
// see docs/container.md. goose is full (level 2) like claude, mirrored to its hints file.
func (m containerMode) contextLevel() int {
	switch m {
	case modeQwen:
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

// parseMode validates a --mode value.
func parseMode(s string) (containerMode, error) {
	switch containerMode(s) {
	case modeClaude:
		return modeClaude, nil
	case modeCodex:
		return modeCodex, nil
	case modeQwen:
		return modeQwen, nil
	case modeGoose:
		return modeGoose, nil
	default:
		return "", fmt.Errorf("unknown --mode %q: want claude|codex|qwen|goose", s)
	}
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

// containerName builds the unique per-run name ward-<repo>-<rand>; the injected
// random suffix lets many runs against one repo coexist (the default mode).
func containerName(repo targetRepo, randSuffix string) string {
	return fmt.Sprintf("%s-%s-%s", containerNamePrefix, safeRepoName(repo), randSuffix)
}

// agentContainerName names an `ward agent` container ward-<repo>-issue-<N>-<mode>-<rand>,
// so `docker ps` shows repo/issue/harness; suffix lets runs coexist. See docs/agent.md.
func agentContainerName(repo targetRepo, mode containerMode, issue int, randSuffix string) string {
	return fmt.Sprintf("%s-%s-issue-%d-%s-%s", containerNamePrefix, safeRepoName(repo), issue, mode, randSuffix)
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
	// AWSHome, when non-empty, mounts ~/.aws read-only (--aws): the broad SSM
	// read surface, off by default.
	AWSHome string
	// WardSource, when non-empty, mounts a local ward checkout (--ward-source)
	// so the container builds ward from source instead of downloading.
	WardSource string
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
	Image       string
	Name        string
	Repo        targetRepo
	Mode        containerMode
	Branch      string
	ForgejoBase string
	HostCwd     string
	Mounts      []mountSpec
	// Interactive attaches the run (stdin kept open); false means --detach (-d).
	Interactive bool
	// TTY allocates a pseudo-terminal (-t), auto-detected: true only with a real
	// terminal, since docker rejects -t against non-terminal stdin. See docs.
	TTY bool
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
	// WARD_HEADLESS=1; set by `ward agent headless`, which also detaches.
	Headless bool
	// Ask runs the in-container agent one-shot, attached (claude -p plain, no
	// stream-json); exports WARD_ASK=1, set by `ward agent ask`.
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
	// ReadOnly marks a read-only scratch session (`ward agent explore`, ward#293):
	// exports WARD_READONLY=1. See docs/agent-explore.md for what it enforces.
	ReadOnly bool
	// HostNet joins the container to the host network (--network=host) so a carry
	// inherits the host's tailnet route (--host-net, ward#330). docs/agent-host-net.md.
	HostNet bool
	// TSSidecar joins the carry to a userspace tailscale SOCKS5 sidecar's netns for
	// tailnet reach on Docker Desktop (--ts-sidecar, ward#333). docs/agent-ts-sidecar.md.
	TSSidecar bool
}

// tsSidecarName derives the userspace tailscale sidecar's container name from the
// carry's name, so the carry can join its netns deterministically (ward#333).
func tsSidecarName(carry string) string { return carry + tsSidecarSuffix }

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

// wardEnv is the non-secret WARD_* config the entrypoint reads. Everything
// here is safe to print and to record; the token never appears.
func (p upPlan) wardEnv() map[string]string {
	env := map[string]string{
		"WARD_TARGET_REPO":   p.Repo.slug(),
		"WARD_TARGET_OWNER":  p.Repo.Owner,
		"WARD_TARGET_NAME":   p.Repo.Name,
		"WARD_FORGEJO_BASE":  p.ForgejoBase,
		"WARD_MODE":          string(p.Mode),
		"WARD_CONTEXT_LEVEL": fmt.Sprintf("%d", p.Mode.contextLevel()),
		"WARD_AGENT":         p.Mode.agentBinary(),
		"WARD_GITCACHE":      containerGitcacheMnt,
		"WARD_CONTEXT_SRC":   containerContextMount,
		"WARD_MIRROR_NAME":   p.Repo.mirrorName(),
		"WARD_VERSION":       p.WardVersion,
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
	if p.TSSidecar {
		// Per-connection proxy for the tower only, never a host-wide ALL_PROXY; socks5h
		// so the proxy resolves the tower's MagicDNS name tailnet-side (ward#337).
		env["WARD_TS_SOCKS5"] = tsSidecarSocks5Scheme + tsSidecarSocks5Host
		// A MagicDNS name, not a secret IP, so it rides plain (no SSM lookup; ward#337).
		env["WARD_TOWER_OLLAMA"] = towerOllamaURL
	}
	if p.GoBootstrap {
		env["WARD_USE_GO_BOOTSTRAP"] = "1"
	}
	if len(p.ExtraRepos) > 0 {
		env["WARD_EXTRA_REPOS"] = extraReposEnv(p.ExtraRepos)
	}
	return env
}

// dockerArgvHead is the verb + name/labels + entrypoint shared by the run and
// create argv builders.
func dockerArgvHead(verb string, p upPlan) []string {
	argv := []string{
		verb, "--name", p.Name,
		"--label", containerLabel, "--label", "ward.repo=" + p.Repo.slug(),
		"--entrypoint", containerWardAssets + "/" + containerEntrypointRel,
	}
	// Tailnet route (mutually exclusive, off by default): --host-net shares the
	// host's namespace (ward#330), --ts-sidecar the sidecar's (ward#333).
	switch {
	case p.TSSidecar:
		argv = append(argv, "--network=container:"+tsSidecarName(p.Name))
	case p.HostNet:
		argv = append(argv, "--network=host")
	}
	return argv
}

// tsSidecarRunArgv builds the `docker run -d` argv for the userspace tailscale
// SOCKS5 sidecar a --ts-sidecar carry joins (TS_AUTHKEY rides the env-file; ward#333).
func tsSidecarRunArgv(carryName, repoSlug, envFilePath string) []string {
	argv := []string{
		"run", "-d",
		"--name", tsSidecarName(carryName),
		"--label", containerLabel,
		"--label", tsSidecarLabel,
		"--label", "ward.repo=" + repoSlug,
		"--hostname", tsSidecarHostname,
		"-e", "TS_USERSPACE=true",
		"-e", "TS_SOCKS5_SERVER=" + tsSidecarSocks5Host,
	}
	if envFilePath != "" {
		argv = append(argv, "--env-file", envFilePath)
	}
	return append(argv, tsSidecarImage)
}

// hostNetTailnetWarning returns a loud warning (and true) when a --host-net carry
// is unlikely to reach the tailnet on this host (ward#332; docs/agent-host-net.md).
func hostNetTailnetWarning(goos string, hasTailscale0 bool) (string, bool) {
	// Non-Linux is Docker Desktop: the carry joins a LinuxKit VM netns, never a
	// tailnet node, so hasTailscale0 (the Mac's, not the VM's) is ignored here.
	if goos != "linux" {
		return "WARNING: --host-net cannot reach the tailnet on Docker Desktop.\n" +
			"  The container joins the LinuxKit VM's network namespace, not your\n" +
			"  " + goos + " host, so it inherits no tailscale0 and no MagicDNS - tailnet\n" +
			"  names (api, kai-tower-3026) will not resolve inside the carry.\n" +
			"  --host-net only reaches the tailnet on a native-Linux host that is\n" +
			"  itself a tailnet node. See docs/agent-host-net.md (ward#332).", true
	}
	if !hasTailscale0 {
		return "WARNING: --host-net found no tailscale0 on this host's network namespace.\n" +
			"  The carry joins this netns, so without a tailscale0 device it gets no\n" +
			"  tailnet route and MagicDNS names (api, kai-tower-3026) will not resolve.\n" +
			"  Bring this host onto the tailnet, or adopt the in-container tailscaled\n" +
			"  sidecar. See docs/agent-host-net.md (ward#332).", true
	}
	return "", false
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

// staleContainersToReap returns the exited-container names past the keep window
// (newest first, as `docker ps` lists them); blanks ignored, keep-or-fewer is nil.
func staleContainersToReap(psOutput string, keep int) []string {
	var names []string
	for _, line := range strings.Split(psOutput, "\n") {
		if n := strings.TrimSpace(line); n != "" {
			names = append(names, n)
		}
	}
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

// dockerWardListArgv lists every ward-managed container by name (running + exited),
// the input the orphan-sidecar sweep pairs carries against sidecars (ward#333).
func dockerWardListArgv() []string {
	return []string{"ps", "-a",
		"--filter", "label=" + containerLabel,
		"--format", "{{.Names}}"}
}

// orphanedSidecars returns the -ts sidecar names whose carry is no longer among
// the ward containers (dockerWardListArgv's output, one name per line; ward#333).
func orphanedSidecars(psOutput string) []string {
	present := map[string]bool{}
	var names []string
	for _, line := range strings.Split(psOutput, "\n") {
		if n := strings.TrimSpace(line); n != "" {
			present[n] = true
			names = append(names, n)
		}
	}
	var orphans []string
	for _, n := range names {
		carry := strings.TrimSuffix(n, tsSidecarSuffix)
		if carry == n {
			continue // not a sidecar name
		}
		if !present[carry] {
			orphans = append(orphans, n)
		}
	}
	return orphans
}

// dockerForceRmArgv builds `docker rm -f <names...>` - the orphan-sidecar sweep
// targets still-running userspace sidecars, so it forces. Empty names yields nil.
func dockerForceRmArgv(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	return append([]string{"rm", "-f"}, names...)
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
