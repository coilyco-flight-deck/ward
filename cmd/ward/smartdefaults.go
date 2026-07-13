package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	kdl "github.com/calico32/kdl-go"
	"github.com/urfave/cli/v3"
)

// smartDefaults is ward-owned runtime policy data. It starts from the baked
// neutral default and can be overridden by the selected config bundle.
type smartDefaults struct {
	agentReservationTTL           time.Duration
	reservationRecheckDefaultMax  time.Duration
	agentReapIdleDefault          time.Duration
	agentReapMaxCPUDefault        float64
	engineerContainerLimit        int
	engineerOpenPRBranchLimit     int
	directorMaxParallel           int
	directorLimit                 int
	directorPollInterval          time.Duration
	reviewerTimeout               time.Duration
	configBundleTTL               time.Duration
	containerAssetsTTL            time.Duration
	containerReadOnlyExtraRepoTTL time.Duration
	containerReapKeep             int
	agentWorkflowDefault          workflowMode
	agentWorkflowRepos            map[string]workflowMode
	trustedOwners                 []string
	repoAuthorityDefault          forge
	repoAuthorityRules            []repoAuthorityRule
	burndownConfigured            bool
	burndownDefault               bool
	burndownRepos                 map[string]bool
}

type repoAuthorityRule struct {
	Pattern string
	Forge   forge
}

var smartDefaultsCache struct {
	sync.Mutex
	ref         string
	initialized bool
	defaults    smartDefaults
	err         error
}

func bakedSmartDefaults() smartDefaults {
	return smartDefaults{
		agentReservationTTL:           3 * time.Hour,
		reservationRecheckDefaultMax:  15 * time.Second,
		agentReapIdleDefault:          time.Hour,
		agentReapMaxCPUDefault:        5.0,
		engineerContainerLimit:        12,
		engineerOpenPRBranchLimit:     6,
		directorMaxParallel:           10,
		directorLimit:                 50,
		directorPollInterval:          30 * time.Second,
		reviewerTimeout:               8 * time.Minute,
		configBundleTTL:               600 * time.Second,
		containerAssetsTTL:            time.Hour,
		containerReadOnlyExtraRepoTTL: 24 * time.Hour,
		containerReapKeep:             10,
		agentWorkflowDefault:          defaultWorkflow,
		agentWorkflowRepos:            map[string]workflowMode{},
		repoAuthorityDefault:          forgeForgejo,
		trustedOwners:                 []string{},
	}
}

// currentSmartDefaults returns ward's runtime policy, caching the baked parse.
func currentSmartDefaults() smartDefaults {
	defs, _ := currentSmartDefaultsWithError()
	return defs
}

func currentSmartDefaultsWithError() (smartDefaults, error) {
	ref := strings.TrimSpace(os.Getenv(wardConfigRefEnv))
	if ref == "" {
		return loadCurrentSmartDefaults()
	}

	smartDefaultsCache.Lock()
	defer smartDefaultsCache.Unlock()
	if smartDefaultsCache.initialized && smartDefaultsCache.ref == ref {
		return smartDefaultsCache.defaults, smartDefaultsCache.err
	}

	defs, err := loadCurrentSmartDefaults()
	smartDefaultsCache.ref = ref
	smartDefaultsCache.initialized = true
	smartDefaultsCache.defaults = defs
	smartDefaultsCache.err = err
	return defs, err
}

// loadCurrentSmartDefaults resolves the config source and parses its defaults.
// Fail-closed values must name the serving source, not just the value.
func loadCurrentSmartDefaults() (smartDefaults, error) {
	defs := bakedSmartDefaults()
	src, err := selectConfigSource()
	if err == nil {
		if defs, err = loadSmartDefaultsFrom(src); err != nil {
			err = fmt.Errorf("%w [config source: %s]", err, src.sourceDesc())
		}
	}
	return defs, err
}

// smartDefaultsGuardExemptVerbs must stay reachable with the config bundle
// absent or rolled back (ward#1067): the native PR-workflow meta verb.
var smartDefaultsGuardExemptVerbs = map[string]bool{"pr": true}

func smartDefaultsGuard(surface string) cli.BeforeFunc {
	return func(ctx context.Context, c *cli.Command) (context.Context, error) {
		if smartDefaultsGuardExemptVerbs[strings.TrimSpace(c.Args().First())] {
			return ctx, nil
		}
		if _, err := currentSmartDefaultsWithError(); err != nil {
			return ctx, fmt.Errorf("%s: %w", surface, err)
		}
		return ctx, nil
	}
}

func loadSmartDefaultsFrom(src configSource) (smartDefaults, error) {
	if src.defaultsKDL != "" {
		b, err := fs.ReadFile(src.fsys, src.defaultsKDL)
		if err != nil {
			return smartDefaults{}, fmt.Errorf("read smart defaults %s: %w", src.defaultsKDL, err)
		}
		defs, err := parseSmartDefaults(b)
		if err != nil {
			return smartDefaults{}, err
		}
		if err := validateReservationTTLInvariant(defs); err != nil {
			return smartDefaults{}, err
		}
		return defs, nil
	}
	return loadBundleSmartDefaultsFrom(src)
}

func loadBundleSmartDefaultsFrom(src configSource) (smartDefaults, error) {
	files, err := loadBundleKDLFiles(src)
	if err != nil {
		return smartDefaults{}, err
	}
	defs := bakedSmartDefaults()
	defaultsFile, defaultsNode, err := findUniqueNamedBundleNode(files, "top-level `defaults` block", "defaults", "smart-defaults")
	if err != nil {
		return smartDefaults{}, err
	}
	if err := parseBundleDefaultsNode(defaultsFile.path, defaultsNode, &defs); err != nil {
		return smartDefaults{}, err
	}
	reposFile, reposNode, err := findUniqueNamedBundleNode(files, "top-level `repos` or `repo-authority` block", "repos", "repo-authority")
	if err != nil {
		return smartDefaults{}, err
	}
	if err := parseBundleReposNode(reposFile.path, reposNode, &defs); err != nil {
		return smartDefaults{}, err
	}
	if err := validateReservationTTLInvariant(defs); err != nil {
		return smartDefaults{}, err
	}
	return defs, nil
}

func parseSmartDefaults(src []byte) (smartDefaults, error) {
	doc, err := kdl.ParseString(string(src))
	if err != nil {
		return smartDefaults{}, fmt.Errorf("smart defaults: parse KDL: %w", err)
	}
	defs := bakedSmartDefaults()
	seenDefaults := false
	seenAuthority := false
	for _, n := range doc.Nodes {
		if err := applySmartDefaultsTopLevelNode(&defs, &seenDefaults, &seenAuthority, n); err != nil {
			return smartDefaults{}, err
		}
	}
	if !seenDefaults {
		return smartDefaults{}, fmt.Errorf("smart defaults: missing top-level `smart-defaults` block (fail-closed)")
	}
	if !seenAuthority {
		return smartDefaults{}, fmt.Errorf("smart defaults: missing top-level `repo-authority` block (fail-closed)")
	}
	return defs, nil
}

func parseBundleDefaultsNode(srcPath string, n *kdl.Node, defs *smartDefaults) error {
	if n.Name() != "defaults" && n.Name() != "smart-defaults" {
		return unknownSmartDefaultsNode("top-level", n.Name(), "defaults")
	}
	if len(n.Arguments()) != 0 {
		return fmt.Errorf("smart defaults: `defaults` takes no arguments (fail-closed)")
	}
	if len(n.Properties()) != 0 {
		return fmt.Errorf("smart defaults: `defaults` takes no properties (fail-closed)")
	}
	for _, c := range n.Children().Nodes {
		if err := applySmartDefaultNode(defs, c); err != nil {
			return fmt.Errorf("%s: %w", srcPath, err)
		}
	}
	return nil
}

func parseBundleReposNode(srcPath string, n *kdl.Node, defs *smartDefaults) error {
	switch n.Name() {
	case "repos":
		if len(n.Arguments()) != 0 {
			return fmt.Errorf("repo authority: `repos` takes no arguments (fail-closed)")
		}
		if len(n.Properties()) != 0 {
			return fmt.Errorf("repo authority: `repos` takes no properties (fail-closed)")
		}
		var repoAuthoritySeen, burndownSeen bool
		for _, c := range n.Children().Nodes {
			if err := applyBundleReposChild(defs, srcPath, c, &repoAuthoritySeen, &burndownSeen); err != nil {
				return err
			}
		}
		if !repoAuthoritySeen {
			return fmt.Errorf("repo authority: missing top-level `repo-authority` block (fail-closed)")
		}
		return nil
	case "repo-authority":
		return applyRepoAuthority(defs, n)
	default:
		return unknownSmartDefaultsNode("top-level", n.Name(), "repos")
	}
}

func applySmartDefaultsTopLevelNode(defs *smartDefaults, seenDefaults, seenAuthority *bool, n *kdl.Node) error {
	switch n.Name() {
	case "smart-defaults":
		if *seenDefaults {
			return fmt.Errorf("smart defaults: duplicate top-level `smart-defaults` block (fail-closed)")
		}
		*seenDefaults = true
		if len(n.Arguments()) != 0 {
			return fmt.Errorf("smart defaults: `smart-defaults` takes no arguments (fail-closed)")
		}
		if len(n.Properties()) != 0 {
			return fmt.Errorf("smart defaults: `smart-defaults` takes no properties (fail-closed)")
		}
		for _, c := range n.Children().Nodes {
			if err := applySmartDefaultNode(defs, c); err != nil {
				return err
			}
		}
	case "repo-authority":
		if *seenAuthority {
			return fmt.Errorf("smart defaults: duplicate top-level `repo-authority` block (fail-closed)")
		}
		*seenAuthority = true
		if err := applyRepoAuthority(defs, n); err != nil {
			return err
		}
	default:
		return unknownSmartDefaultsNode("top-level", n.Name(), "smart-defaults | repo-authority")
	}
	return nil
}

func applySmartDefaultNode(defs *smartDefaults, n *kdl.Node) error { //nolint:gocognit,gocyclo,cyclop,funlen
	switch n.Name() {
	case "agent-reservation-ttl":
		v, err := smartDefaultsDurationArg(n, "smart-defaults > agent-reservation-ttl")
		if err != nil {
			return err
		}
		defs.agentReservationTTL = v
	case "agent-reservation-recheck-max":
		v, err := smartDefaultsDurationArg(n, "smart-defaults > agent-reservation-recheck-max")
		if err != nil {
			return err
		}
		defs.reservationRecheckDefaultMax = v
	case "agent-reap-idle":
		v, err := smartDefaultsDurationArg(n, "smart-defaults > agent-reap-idle")
		if err != nil {
			return err
		}
		defs.agentReapIdleDefault = v
	case "agent-reap-max-cpu":
		v, err := smartDefaultsFloatArg(n, "smart-defaults > agent-reap-max-cpu")
		if err != nil {
			return err
		}
		defs.agentReapMaxCPUDefault = v
	case "engineer-container-limit":
		v, err := smartDefaultsIntArg(n, "smart-defaults > engineer-container-limit")
		if err != nil {
			return err
		}
		defs.engineerContainerLimit = v
	case "engineer-open-pr-branch-limit":
		v, err := smartDefaultsIntArg(n, "smart-defaults > engineer-open-pr-branch-limit")
		if err != nil {
			return err
		}
		defs.engineerOpenPRBranchLimit = v
	case "director-max-parallel":
		v, err := smartDefaultsIntArg(n, "smart-defaults > director-max-parallel")
		if err != nil {
			return err
		}
		defs.directorMaxParallel = v
	case "director-limit":
		v, err := smartDefaultsIntArg(n, "smart-defaults > director-limit")
		if err != nil {
			return err
		}
		defs.directorLimit = v
	case "director-poll-interval":
		v, err := smartDefaultsDurationArg(n, "smart-defaults > director-poll-interval")
		if err != nil {
			return err
		}
		defs.directorPollInterval = v
	case "reviewer-timeout":
		v, err := smartDefaultsDurationArg(n, "smart-defaults > reviewer-timeout")
		if err != nil {
			return err
		}
		defs.reviewerTimeout = v
	case "config-bundle-ttl":
		v, err := smartDefaultsDurationOrSecondsArg(n, "smart-defaults > config-bundle-ttl")
		if err != nil {
			return err
		}
		defs.configBundleTTL = v
	case "container-assets-ttl":
		v, err := smartDefaultsDurationArg(n, "smart-defaults > container-assets-ttl")
		if err != nil {
			return err
		}
		defs.containerAssetsTTL = v
	case "container-read-only-extra-repo-ttl":
		v, err := smartDefaultsDurationArg(n, "smart-defaults > container-read-only-extra-repo-ttl")
		if err != nil {
			return err
		}
		defs.containerReadOnlyExtraRepoTTL = v
	case "container-reap-keep":
		v, err := smartDefaultsIntArg(n, "smart-defaults > container-reap-keep")
		if err != nil {
			return err
		}
		defs.containerReapKeep = v
	case "agent-workflow":
		if err := applySmartDefaultWorkflow(defs, n); err != nil {
			return err
		}
	default:
		return unknownSmartDefaultsNode("smart-defaults body", n.Name(),
			"agent-reservation-ttl | agent-reservation-recheck-max | agent-reap-idle | agent-reap-max-cpu | engineer-container-limit | engineer-open-pr-branch-limit | director-max-parallel | director-limit | director-poll-interval | reviewer-timeout | config-bundle-ttl | container-assets-ttl | container-read-only-extra-repo-ttl | container-reap-keep | agent-workflow")
	}
	return nil
}

func applyRepoAuthority(defs *smartDefaults, n *kdl.Node) error {
	if len(n.Arguments()) != 0 {
		return fmt.Errorf("smart defaults: repo-authority takes no arguments (fail-closed)")
	}
	if err := applyRepoAuthorityDefault(defs, n); err != nil {
		return err
	}
	if err := requireRepoAuthorityProperties(n); err != nil {
		return err
	}
	if defs.trustedOwners == nil {
		defs.trustedOwners = []string{}
	}
	if defs.repoAuthorityRules == nil {
		defs.repoAuthorityRules = []repoAuthorityRule{}
	}
	seenOwners := map[string]bool{}
	seenPatterns := map[string]bool{}
	for _, c := range n.Children().Nodes {
		if err := applyRepoAuthorityChild(defs, c, seenOwners, seenPatterns); err != nil {
			return err
		}
	}
	return nil
}

func applyRepoAuthorityChild(defs *smartDefaults, c *kdl.Node, seenOwners, seenPatterns map[string]bool) error {
	switch c.Name() {
	case "trusted-owner":
		owner, err := smartDefaultsStringArg(c, "repo-authority > trusted-owner")
		if err != nil {
			return err
		}
		if strings.Contains(owner, "/") {
			return fmt.Errorf("smart defaults: repo-authority > trusted-owner %q must be an owner only (fail-closed)", owner)
		}
		if seenOwners[owner] {
			return fmt.Errorf("smart defaults: repo-authority > trusted-owner %q repeated (fail-closed)", owner)
		}
		seenOwners[owner] = true
		defs.trustedOwners = append(defs.trustedOwners, owner)
	case "repo":
		pattern, err := smartDefaultsStringArg(c, "repo-authority > repo")
		if err != nil {
			return err
		}
		if !validRepoAuthorityPattern(pattern) {
			return fmt.Errorf("smart defaults: repo-authority > repo %q must be owner/repo or owner/* (fail-closed)", pattern)
		}
		if seenPatterns[pattern] {
			return fmt.Errorf("smart defaults: repo-authority > repo %q repeated (fail-closed)", pattern)
		}
		forge, err := smartDefaultsForgeProp(c, "forge", "repo-authority > repo forge")
		if err != nil {
			return err
		}
		seenPatterns[pattern] = true
		defs.repoAuthorityRules = append(defs.repoAuthorityRules, repoAuthorityRule{Pattern: pattern, Forge: forge})
	default:
		return unknownSmartDefaultsNode("repo-authority body", c.Name(), "trusted-owner | repo")
	}
	return nil
}

func applyRepoAuthorityDefault(defs *smartDefaults, n *kdl.Node) error {
	v, ok, err := smartDefaultsStringProp(n, "default", "repo-authority default")
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("smart defaults: repo-authority missing `default` property (fail-closed)")
	}
	if v != "forgejo" && v != "github" {
		return fmt.Errorf("smart defaults: repo-authority default must be forgejo or github (fail-closed)")
	}
	defs.repoAuthorityDefault = parseForge(v)
	return nil
}

func requireRepoAuthorityProperties(n *kdl.Node) error {
	for prop := range n.Properties() {
		if prop != "default" {
			return fmt.Errorf("smart defaults: repo-authority unknown property %q (want default; fail-closed)", prop)
		}
	}
	return nil
}

func smartDefaultsForgeProp(n *kdl.Node, name, label string) (forge, error) {
	raw, ok, err := smartDefaultsStringProp(n, name, label)
	if err != nil {
		return forgeForgejo, err
	}
	if !ok {
		return forgeForgejo, fmt.Errorf("smart defaults: %s missing (fail-closed)", label)
	}
	if raw != "forgejo" && raw != "github" {
		return forgeForgejo, fmt.Errorf("smart defaults: %s must be forgejo or github (fail-closed)", label)
	}
	return parseForge(raw), nil
}

func applyBundleReposChild(defs *smartDefaults, srcPath string, c *kdl.Node, repoAuthoritySeen, burndownSeen *bool) error {
	switch c.Name() {
	case "repo-authority":
		if *repoAuthoritySeen {
			return fmt.Errorf("repo authority: duplicate top-level `repo-authority` block (fail-closed)")
		}
		*repoAuthoritySeen = true
		if err := applyRepoAuthority(defs, c); err != nil {
			return fmt.Errorf("%s: %w", srcPath, err)
		}
	case "burndown":
		if *burndownSeen {
			return fmt.Errorf("burndown: duplicate `burndown` block (fail-closed)")
		}
		*burndownSeen = true
		if err := applyBurndown(defs, c); err != nil {
			return fmt.Errorf("%s: %w", srcPath, err)
		}
	default:
		return unknownSmartDefaultsNode("repos body", c.Name(), "repo-authority | burndown")
	}
	return nil
}

// applyBurndown reads the burndown repo-exclusion block (ward#1105).
// `default` sets the fleet-wide posture and each `repo` overrides it.
func applyBurndown(defs *smartDefaults, n *kdl.Node) error {
	if len(n.Arguments()) != 0 {
		return fmt.Errorf("smart defaults: burndown takes no arguments (fail-closed)")
	}
	def, ok, err := smartDefaultsBoolProp(n, "default", "burndown default")
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("smart defaults: burndown missing `default` property (fail-closed)")
	}
	defs.burndownConfigured = true
	defs.burndownDefault = def
	if defs.burndownRepos == nil {
		defs.burndownRepos = map[string]bool{}
	}
	for _, c := range n.Children().Nodes {
		if c.Name() != "repo" {
			return unknownSmartDefaultsNode("burndown body", c.Name(), "repo")
		}
		if err := applyBurndownRepo(defs, c); err != nil {
			return err
		}
	}
	return nil
}

func applyBurndownRepo(defs *smartDefaults, c *kdl.Node) error {
	args := c.Arguments()
	if len(args) != 2 {
		return fmt.Errorf("smart defaults: burndown > repo expects `repo \"owner/name\" #true|#false`, got %d value(s) (fail-closed)", len(args))
	}
	if args[0].Kind() != kdl.String {
		return fmt.Errorf("smart defaults: burndown > repo slug must be a string (fail-closed)")
	}
	slug := strings.TrimSpace(args[0].String())
	if !validRepoAuthorityPattern(slug) || strings.Contains(slug, "*") {
		return fmt.Errorf("smart defaults: burndown > repo %q must be an exact owner/name (fail-closed)", slug)
	}
	if _, dup := defs.burndownRepos[slug]; dup {
		return fmt.Errorf("smart defaults: burndown > repo %q repeated (fail-closed)", slug)
	}
	enabled, err := smartDefaultsBoolValue(args[1], "burndown > repo "+slug)
	if err != nil {
		return err
	}
	defs.burndownRepos[slug] = enabled
	return nil
}

// burndownEnabled reports whether the director may burn a repo's backlog down.
// An absent burndown block excludes nothing, so it cannot silently drain scope.
func (d smartDefaults) burndownEnabled(slug string) bool {
	if !d.burndownConfigured {
		return true
	}
	if v, ok := d.burndownRepos[strings.TrimSpace(slug)]; ok {
		return v
	}
	return d.burndownDefault
}

func validRepoAuthorityPattern(s string) bool {
	s = strings.TrimSpace(s)
	owner, repo, ok := strings.Cut(s, "/")
	return ok && owner != "" && repo != "" && !strings.Contains(owner, "/") && !strings.Contains(repo, "/") && !strings.ContainsAny(s, " \t#")
}

func applySmartDefaultWorkflow(defs *smartDefaults, n *kdl.Node) error {
	if len(n.Arguments()) != 0 {
		return fmt.Errorf("smart defaults: smart-defaults > agent-workflow takes no arguments (fail-closed)")
	}
	if err := applySmartDefaultWorkflowDefault(defs, n); err != nil {
		return err
	}
	if err := requireSmartDefaultWorkflowProperties(n); err != nil {
		return err
	}
	if defs.agentWorkflowRepos == nil {
		defs.agentWorkflowRepos = map[string]workflowMode{}
	}
	for _, c := range n.Children().Nodes {
		repo, wf, err := parseSmartDefaultWorkflowRepo(c)
		if err != nil {
			return err
		}
		if _, dup := defs.agentWorkflowRepos[repo]; dup {
			return fmt.Errorf("smart defaults: smart-defaults > agent-workflow > repo %q repeated (fail-closed)", repo)
		}
		defs.agentWorkflowRepos[repo] = wf
	}
	return nil
}

func validateReservationTTLInvariant(defs smartDefaults) error {
	cat, err := cachedBuiltInAgentRoleCatalog()
	if err != nil {
		return fmt.Errorf("smart defaults: load embedded role catalog: %w", err)
	}
	ttl := defs.agentReservationTTL
	for _, name := range cat.Order {
		def, ok := cat.Definitions[name]
		if !ok || !def.ExecutionLimitSet || def.ExecutionTimeLimit <= 0 {
			continue
		}
		if ttl <= def.ExecutionTimeLimit {
			return fmt.Errorf("smart defaults: agent-reservation-ttl %s must exceed role %q execution-time-limit %s (fail-closed)",
				conciseDuration(ttl), name, conciseDuration(def.ExecutionTimeLimit))
		}
	}
	return nil
}

func applySmartDefaultWorkflowDefault(defs *smartDefaults, n *kdl.Node) error {
	v, ok, err := smartDefaultsStringProp(n, "default", "smart-defaults > agent-workflow default")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	wf, err := parseWorkflow(v)
	if err != nil {
		return fmt.Errorf("smart defaults: smart-defaults > agent-workflow default: %w (fail-closed)", err)
	}
	defs.agentWorkflowDefault = wf
	return nil
}

func requireSmartDefaultWorkflowProperties(n *kdl.Node) error {
	for prop := range n.Properties() {
		if prop != "default" {
			return fmt.Errorf("smart defaults: smart-defaults > agent-workflow unknown property %q (want default; fail-closed)", prop)
		}
	}
	return nil
}

func parseSmartDefaultWorkflowRepo(c *kdl.Node) (string, workflowMode, error) {
	if c.Name() != "repo" {
		return "", workflowMode(""), unknownSmartDefaultsNode("smart-defaults > agent-workflow body", c.Name(), "repo")
	}
	repo, err := smartDefaultsStringArg(c, "smart-defaults > agent-workflow > repo")
	if err != nil {
		return "", workflowMode(""), err
	}
	if !validWorkflowRepoSlug(repo) {
		return "", workflowMode(""), fmt.Errorf("smart defaults: smart-defaults > agent-workflow > repo %q must be owner/name (fail-closed)", repo)
	}
	wf, err := parseSmartDefaultRepoWorkflow(c, repo)
	return repo, wf, err
}

func parseSmartDefaultRepoWorkflow(c *kdl.Node, repo string) (workflowMode, error) {
	wfRaw, ok, err := smartDefaultsStringProp(c, "workflow", "smart-defaults > agent-workflow > repo workflow")
	if err != nil {
		return workflowMode(""), err
	}
	if !ok {
		return workflowMode(""), fmt.Errorf("smart defaults: smart-defaults > agent-workflow > repo %q missing workflow property (fail-closed)", repo)
	}
	for prop := range c.Properties() {
		if prop != "workflow" {
			return workflowMode(""), fmt.Errorf("smart defaults: smart-defaults > agent-workflow > repo unknown property %q (want workflow; fail-closed)", prop)
		}
	}
	wf, err := parseWorkflow(wfRaw)
	if err != nil {
		return workflowMode(""), fmt.Errorf("smart defaults: smart-defaults > agent-workflow > repo %q: %w (fail-closed)", repo, err)
	}
	return wf, nil
}

func (d smartDefaults) trustedOwnerList() []string {
	out := make([]string, 0, len(d.trustedOwners))
	out = append(out, d.trustedOwners...)
	return out
}

func (d smartDefaults) forgeForRepo(owner, repo string) forge {
	slug := owner + "/" + repo
	for _, rule := range d.repoAuthorityRules {
		if ok, _ := path.Match(rule.Pattern, slug); ok {
			return rule.Forge
		}
	}
	return d.repoAuthorityDefault
}

func firstTrustedOwner(owners []string) string {
	for _, owner := range owners {
		if strings.TrimSpace(owner) != "" {
			return owner
		}
	}
	return ""
}

func trustedOwnerExtras(owners []string) []string {
	if len(owners) <= 1 {
		return nil
	}
	extra := make([]string, 0, len(owners)-1)
	for _, owner := range owners[1:] {
		if strings.TrimSpace(owner) != "" {
			extra = append(extra, owner)
		}
	}
	return extra
}

func smartDefaultsBoolProp(n *kdl.Node, name, label string) (bool, bool, error) {
	v, ok := n.Properties()[name]
	if !ok {
		return false, false, nil
	}
	b, err := smartDefaultsBoolValue(v, label)
	if err != nil {
		return false, true, err
	}
	return b, true, nil
}

// smartDefaultsBoolValue insists on a KDL v2 boolean (#true / #false). A bare
// true / false lexes as an identifier, so it never reaches here as a bool.
func smartDefaultsBoolValue(v kdl.Value, label string) (bool, error) {
	if v.Kind() != kdl.Bool {
		return false, fmt.Errorf("smart defaults: %s must be #true or #false (fail-closed)", label)
	}
	return v.Bool(), nil
}

func smartDefaultsStringProp(n *kdl.Node, name, label string) (string, bool, error) {
	v, ok := n.Properties()[name]
	if !ok {
		return "", false, nil
	}
	if v.Kind() != kdl.String {
		return "", true, fmt.Errorf("smart defaults: %s must be a string (fail-closed)", label)
	}
	out := strings.TrimSpace(v.String())
	if out == "" {
		return "", true, fmt.Errorf("smart defaults: %s must be non-empty (fail-closed)", label)
	}
	return out, true, nil
}

func validWorkflowRepoSlug(s string) bool {
	owner, repo, ok := strings.Cut(strings.TrimSpace(s), "/")
	return ok && owner != "" && repo != "" && !strings.Contains(repo, "/") && !strings.ContainsAny(s, " \t#")
}

func smartDefaultsStringArg(n *kdl.Node, label string) (string, error) {
	args := n.Arguments()
	if len(args) != 1 {
		return "", fmt.Errorf("smart defaults: %s expects exactly one value, got %d (fail-closed)", label, len(args))
	}
	if args[0].Kind() != kdl.String {
		return "", fmt.Errorf("smart defaults: %s must be a string (fail-closed)", label)
	}
	v := strings.TrimSpace(args[0].String())
	if v == "" {
		return "", fmt.Errorf("smart defaults: %s must be non-empty (fail-closed)", label)
	}
	return v, nil
}

func smartDefaultsDurationArg(n *kdl.Node, label string) (time.Duration, error) {
	raw, err := smartDefaultsStringArg(n, label)
	if err != nil {
		return 0, err
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("smart defaults: %s must be a positive duration (fail-closed)", label)
	}
	return d, nil
}

func smartDefaultsDurationOrSecondsArg(n *kdl.Node, label string) (time.Duration, error) {
	raw, err := smartDefaultsStringArg(n, label)
	if err != nil {
		return 0, err
	}
	if d, err := time.ParseDuration(raw); err == nil {
		if d <= 0 {
			return 0, fmt.Errorf("smart defaults: %s must be positive (fail-closed)", label)
		}
		return d, nil
	}
	sec, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || sec <= 0 {
		return 0, fmt.Errorf("smart defaults: %s must be positive seconds or duration (fail-closed)", label)
	}
	return time.Duration(sec) * time.Second, nil
}

func smartDefaultsFloatArg(n *kdl.Node, label string) (float64, error) {
	raw, err := smartDefaultsStringArg(n, label)
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("smart defaults: %s must be a positive number (fail-closed)", label)
	}
	return v, nil
}

func smartDefaultsIntArg(n *kdl.Node, label string) (int, error) {
	raw, err := smartDefaultsStringArg(n, label)
	if err != nil {
		return 0, err
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("smart defaults: %s must be a positive integer (fail-closed)", label)
	}
	return v, nil
}

func unknownSmartDefaultsNode(where, name, want string) error {
	return fmt.Errorf("smart defaults: %s: unknown node %q (want %s; fail-closed)", where, name, want)
}

func conciseDuration(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", int64(d/time.Hour))
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", int64(d/time.Minute))
	case d%time.Second == 0:
		return fmt.Sprintf("%ds", int64(d/time.Second))
	default:
		return d.String()
	}
}

func agentReservationTTL() time.Duration { return currentSmartDefaults().agentReservationTTL }

func reservationRecheckDefaultMax() time.Duration {
	return currentSmartDefaults().reservationRecheckDefaultMax
}

func agentReapIdleDefault() time.Duration { return currentSmartDefaults().agentReapIdleDefault }

func agentReapMaxCPUDefault() float64 { return currentSmartDefaults().agentReapMaxCPUDefault }

func engineerContainerLimitDefault() int { return currentSmartDefaults().engineerContainerLimit }

func engineerOpenPRBranchLimitDefault() int { return currentSmartDefaults().engineerOpenPRBranchLimit }

func directorMaxParallelDefault() int { return currentSmartDefaults().directorMaxParallel }

func directorLimitDefault() int { return currentSmartDefaults().directorLimit }

func directorPollIntervalDefault() time.Duration { return currentSmartDefaults().directorPollInterval }

func reviewerTimeoutDefault() time.Duration { return currentSmartDefaults().reviewerTimeout }

func configBundleTTLDefault() time.Duration { return bakedSmartDefaults().configBundleTTL }

func containerAssetsTTL() time.Duration { return currentSmartDefaults().containerAssetsTTL }

func containerReadOnlyExtraRepoTTL() time.Duration {
	return currentSmartDefaults().containerReadOnlyExtraRepoTTL
}

func containerReapKeep() int { return currentSmartDefaults().containerReapKeep }
