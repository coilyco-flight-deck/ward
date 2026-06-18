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

	// containerNamePrefix anchors every ward-managed container name so `ls`
	// and `down` can filter the host's container set.
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

// parseRepoRef resolves a `ward container up` arg (bare owner/name, https clone
// URL, or scp-style remote) into a targetRepo. Empty means infer from cwd.
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

// upPlan is the fully-resolved description of one `ward container up`, minus
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
	// entrypoint's `"$WARD_AGENT" "$@"`); empty for a bare `container up`.
	AgentArgs []string
	// Headless runs the in-container agent in print mode (claude -p), exporting
	// WARD_HEADLESS=1; set by `ward agent <name> headless`, which also detaches.
	Headless bool
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
	if p.WardFromSource {
		env["WARD_FROM_SOURCE"] = containerWardSrcMount
	}
	if p.Headless {
		env["WARD_HEADLESS"] = "1"
	}
	return env
}

// dockerCreateArgv assembles the `docker run` argv. The token rides --env-file
// (envFilePath), never inlined, so the argv is safe to print; "" omits it.
func dockerCreateArgv(p upPlan, envFilePath string) []string {
	argv := []string{"run", "--name", p.Name, "--label", containerLabel, "--label", "ward.repo=" + p.Repo.slug()}
	argv = append(argv, "--entrypoint", containerWardAssets+"/"+containerEntrypointRel)
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
	for _, k := range sortedKeys(p.wardEnv()) {
		argv = append(argv, "-e", k+"="+p.wardEnv()[k])
	}
	if envFilePath != "" {
		argv = append(argv, "--env-file", envFilePath)
	}
	argv = append(argv, p.Image)
	// Trailing args become the in-container agent's argv (entrypoint runs
	// `"$WARD_AGENT" "$@"`); empty for a bare interactive `container up`.
	argv = append(argv, p.AgentArgs...)
	return argv
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

// dockerExecArgv builds `docker exec -i[t] <name> <cmd...>`; tty adds -t and
// must only be set with a real terminal, as in `run`. See docs/container.md.
func dockerExecArgv(name string, tty bool, cmd []string) []string {
	argv := []string{"exec"}
	if tty {
		argv = append(argv, "-it")
	} else {
		argv = append(argv, "-i")
	}
	argv = append(argv, name)
	return append(argv, cmd...)
}

// dockerDownArgv builds `docker rm -f <name>`. The shared gitcache volume is
// never removed here - it is the point of the cache.
func dockerDownArgv(name string) []string {
	return []string{"rm", "-f", name}
}

// dockerListArgv builds `docker ps` filtered to ward-managed containers.
func dockerListArgv(all bool) []string {
	argv := []string{"ps"}
	if all {
		argv = append(argv, "-a")
	}
	return append(argv, "--filter", "label="+containerLabel,
		"--format", "table {{.Names}}\t{{.Status}}\t{{.Label \"ward.repo\"}}")
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
