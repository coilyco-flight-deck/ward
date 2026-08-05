package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"
)

// smartDefaults contains Ward product mechanics. The defaults are typed Go
// values. Supported consumer overrides are loaded from YAML at the edge.
type smartDefaults struct {
	agentReservationTTL           time.Duration
	reservationRecheckDefaultMax  time.Duration
	agentReapIdleDefault          time.Duration
	agentReapMaxCPUDefault        float64
	agentImage                    string
	agentTag                      string
	containerMemoryLimit          string
	engineerContainerLimit        int
	engineerRepoWorkingLimit      int
	engineerOpenPRBranchLimit     int
	directorMaxParallel           int
	directorLimit                 int
	reviewerTimeout               time.Duration
	gitRefCacheTTL                time.Duration
	containerAssetsTTL            time.Duration
	containerReadOnlyExtraRepoTTL time.Duration
	containerReapTTL              time.Duration
	agentWorkflowDefault          workflowMode
	agentWorkflowRepos            map[string]workflowMode
	prMergeStyle                  string
	routeIntakeRepo               targetRepo
	trustedOwners                 []string
	repoAuthorityDefault          forge
	repoAuthorityRules            []repoAuthorityRule
}

type repoAuthorityRule struct {
	Pattern    string
	Forge      forge
	Tracker    tracker
	TrackerSet bool
	Landing    forge
	LandingSet bool
	Mirrors    []forge
}

type operatorPreferences struct {
	DefaultHarness string
	AgentImage     string
	AgentTag       string
	Workflow       workflowMode
	WorkflowRepos  map[string]workflowMode
	MaxParallel    int
	DirectorLimit  int
}

func defaultSmartDefaults() smartDefaults {
	return smartDefaults{
		agentReservationTTL:           3 * time.Hour,
		reservationRecheckDefaultMax:  15 * time.Second,
		agentReapIdleDefault:          time.Hour,
		agentReapMaxCPUDefault:        5,
		agentImage:                    "forgejo.coilysiren.me/coilyco-flight-deck/ward",
		agentTag:                      "release",
		containerMemoryLimit:          "2g",
		engineerContainerLimit:        12,
		engineerRepoWorkingLimit:      3,
		engineerOpenPRBranchLimit:     6,
		directorMaxParallel:           6,
		directorLimit:                 50,
		reviewerTimeout:               8 * time.Minute,
		gitRefCacheTTL:                10 * time.Minute,
		containerAssetsTTL:            time.Hour,
		containerReadOnlyExtraRepoTTL: 24 * time.Hour,
		containerReapTTL:              48 * time.Hour,
		agentWorkflowDefault:          workflowDirectToMain,
		agentWorkflowRepos:            map[string]workflowMode{},
		routeIntakeRepo:               targetRepo{Owner: "coilysiren", Name: "inbox"},
		trustedOwners:                 []string{"coilysiren", "coilyco-flight-deck", "coilyco-gaming"},
		repoAuthorityDefault:          forgeForgejo,
		repoAuthorityRules: []repoAuthorityRule{
			{Pattern: "coilysiren/*", Forge: forgeGitHub},
			{Pattern: "coilyco-flight-deck/*", Forge: forgeForgejo},
			{Pattern: "coilyco-gaming/*", Forge: forgeForgejo},
		},
	}
}

func bakedSmartDefaults() smartDefaults {
	return cloneSmartDefaults(defaultSmartDefaults())
}

func currentSmartDefaults() smartDefaults {
	defs, err := currentSmartDefaultsWithError()
	if err != nil {
		return bakedSmartDefaults()
	}
	return defs
}

func currentSmartDefaultsWithError() (smartDefaults, error) {
	defs := bakedSmartDefaults()
	prefs, err := loadOperatorPreferences()
	if err != nil {
		return smartDefaults{}, err
	}
	applyOperatorPreferences(&defs, prefs)
	if err := applyRepoRuntimeConfig(&defs); err != nil {
		return smartDefaults{}, err
	}
	if defs.agentReservationTTL <= fixedRoleExecutionLimit(roleEngineer) {
		return smartDefaults{}, fmt.Errorf("agent reservation TTL %s must exceed engineer execution limit %s",
			conciseDuration(defs.agentReservationTTL), conciseDuration(fixedRoleExecutionLimit(roleEngineer)))
	}
	return defs, nil
}

func smartDefaultsGuard(surface string) cli.BeforeFunc {
	return func(ctx context.Context, _ *cli.Command) (context.Context, error) {
		if _, err := currentSmartDefaultsWithError(); err != nil {
			return ctx, fmt.Errorf("%s: %w", surface, err)
		}
		return ctx, nil
	}
}

func loadOperatorPreferences() (operatorPreferences, error) {
	cfg, err := loadWardGlobalConfigWithRedactionValidation()
	if err != nil {
		return operatorPreferences{}, err
	}
	out := operatorPreferences{
		DefaultHarness: strings.TrimSpace(cfg.DefaultHarness),
		AgentImage:     strings.TrimSpace(cfg.Agent.Image),
		AgentTag:       strings.TrimSpace(cfg.Agent.ReleaseChannel),
		MaxParallel:    cfg.Director.MaxParallel,
		DirectorLimit:  cfg.Director.Limit,
	}
	if raw := strings.TrimSpace(cfg.Agent.Workflow.Default); raw != "" {
		out.Workflow, err = parseWorkflow(raw)
		if err != nil {
			return operatorPreferences{}, fmt.Errorf("agent.workflow.default: %w", err)
		}
	}
	out.WorkflowRepos = map[string]workflowMode{}
	for slug, raw := range cfg.Agent.Workflow.Repositories {
		if !validWorkflowRepoSlug(slug) {
			return operatorPreferences{}, fmt.Errorf("agent.workflow.repositories key %q must be owner/name", slug)
		}
		mode, parseErr := parseWorkflow(raw)
		if parseErr != nil {
			return operatorPreferences{}, fmt.Errorf("agent.workflow.repositories[%s]: %w", slug, parseErr)
		}
		out.WorkflowRepos[slug] = mode
	}
	if out.DefaultHarness != "" && launchAgentByName(builtInLaunchConfig(), out.DefaultHarness).Name == "" {
		return operatorPreferences{}, fmt.Errorf("default-harness %q must be one of %s", out.DefaultHarness, strings.Join(frontierAgentNames(), "|"))
	}
	return out, nil
}

func applyOperatorPreferences(defs *smartDefaults, prefs operatorPreferences) {
	if prefs.AgentImage != "" {
		defs.agentImage = prefs.AgentImage
	}
	if prefs.AgentTag != "" {
		defs.agentTag = prefs.AgentTag
	}
	if prefs.Workflow != "" {
		defs.agentWorkflowDefault = prefs.Workflow
	}
	for slug, mode := range prefs.WorkflowRepos {
		defs.agentWorkflowRepos[slug] = mode
	}
	if prefs.MaxParallel > 0 {
		defs.directorMaxParallel = prefs.MaxParallel
	}
	if prefs.DirectorLimit > 0 {
		defs.directorLimit = prefs.DirectorLimit
	}
}

type repoRuntimeConfig struct {
	Agent struct {
		Workflow string `yaml:"workflow"`
		Image    string `yaml:"image"`
		Channel  string `yaml:"release-channel"`
	} `yaml:"agent"`
}

func applyRepoRuntimeConfig(defs *smartDefaults) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	configPath, err := discoverConfig(cwd)
	if err != nil {
		if errors.Is(err, errNoConfig) {
			return nil
		}
		return err
	}
	body, err := os.ReadFile(configPath) // #nosec G304 -- discovered .ward/ward.yaml is the intended input.
	if err != nil {
		return fmt.Errorf("read repository Ward config %s: %w", configPath, err)
	}
	var cfg repoRuntimeConfig
	if err := yaml.Unmarshal(body, &cfg); err != nil {
		return fmt.Errorf("parse repository Ward config %s: %w", configPath, err)
	}
	if raw := strings.TrimSpace(cfg.Agent.Workflow); raw != "" {
		mode, parseErr := parseWorkflow(raw)
		if parseErr != nil {
			return fmt.Errorf("repository agent.workflow: %w", parseErr)
		}
		defs.agentWorkflowDefault = mode
	}
	if value := strings.TrimSpace(cfg.Agent.Image); value != "" {
		defs.agentImage = value
	}
	if value := strings.TrimSpace(cfg.Agent.Channel); value != "" {
		defs.agentTag = value
	}
	return nil
}

func cloneSmartDefaults(in smartDefaults) smartDefaults {
	out := in
	out.agentWorkflowRepos = make(map[string]workflowMode, len(in.agentWorkflowRepos))
	for slug, mode := range in.agentWorkflowRepos {
		out.agentWorkflowRepos[slug] = mode
	}
	out.trustedOwners = append([]string{}, in.trustedOwners...)
	out.repoAuthorityRules = append([]repoAuthorityRule{}, in.repoAuthorityRules...)
	return out
}

func repoPatternSpecificity(pattern string) int {
	score := 0
	for _, r := range pattern {
		switch r {
		case '*', '?', '[':
		default:
			score++
		}
	}
	return score
}

func validWorkflowRepoSlug(s string) bool {
	owner, repo, ok := strings.Cut(strings.TrimSpace(s), "/")
	return ok && owner != "" && repo != "" && !strings.Contains(repo, "/")
}

func (d smartDefaults) trustedOwnerList() []string {
	return append([]string{}, d.trustedOwners...)
}

type repoAuthority struct {
	Checkout forge
	Landing  forge
	Tracker  tracker
	Mirrors  []forge
}

func (d smartDefaults) authorityForRepo(owner, repo string) repoAuthority {
	slug := owner + "/" + repo
	if rule, ok := d.repoAuthorityRuleForSlug(slug); ok {
		out := repoAuthority{Checkout: rule.Forge, Landing: rule.Forge, Tracker: trackerFromForge(rule.Forge), Mirrors: append([]forge{}, rule.Mirrors...)}
		if rule.TrackerSet {
			out.Tracker = rule.Tracker
		}
		if rule.LandingSet {
			out.Landing = rule.Landing
		}
		return out
	}
	return repoAuthority{Checkout: d.repoAuthorityDefault, Landing: d.repoAuthorityDefault, Tracker: trackerFromForge(d.repoAuthorityDefault)}
}

func (d smartDefaults) repoAuthorityRuleForSlug(slug string) (repoAuthorityRule, bool) {
	var best repoAuthorityRule
	bestSpecificity := -1
	for _, rule := range d.repoAuthorityRules {
		if ok, _ := path.Match(rule.Pattern, slug); ok {
			if specificity := repoPatternSpecificity(rule.Pattern); specificity > bestSpecificity {
				best, bestSpecificity = rule, specificity
			}
		}
	}
	return best, bestSpecificity >= 0
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
func agentReapMaxCPUDefault() float64     { return currentSmartDefaults().agentReapMaxCPUDefault }
func agentImageDefault() string {
	if value := strings.TrimSpace(currentSmartDefaults().agentImage); value != "" {
		return value
	}
	return containerImageDefault
}
func agentTagDefault() string {
	if value := strings.TrimSpace(currentSmartDefaults().agentTag); value != "" {
		return value
	}
	return containerImageTagDefault
}
func containerMemoryLimitDefault() string {
	if value := strings.TrimSpace(currentSmartDefaults().containerMemoryLimit); value != "" {
		return value
	}
	return "2g"
}
func engineerContainerLimitDefault() int { return currentSmartDefaults().engineerContainerLimit }
func engineerRepoWorkingLimitDefault() int {
	if value := currentSmartDefaults().engineerRepoWorkingLimit; value > 0 {
		return value
	}
	return 3
}
func engineerOpenPRBranchLimitDefault() int {
	return currentSmartDefaults().engineerOpenPRBranchLimit
}
func directorMaxParallelDefault() int       { return currentSmartDefaults().directorMaxParallel }
func directorLimitDefault() int             { return currentSmartDefaults().directorLimit }
func reviewerTimeoutDefault() time.Duration { return currentSmartDefaults().reviewerTimeout }
func gitRefCacheTTLDefault() time.Duration  { return bakedSmartDefaults().gitRefCacheTTL }
func containerAssetsTTL() time.Duration     { return currentSmartDefaults().containerAssetsTTL }
func containerReadOnlyExtraRepoTTL() time.Duration {
	return currentSmartDefaults().containerReadOnlyExtraRepoTTL
}
func containerReapTTL() time.Duration { return currentSmartDefaults().containerReapTTL }
func prMergeStyleDefault() string     { return strings.TrimSpace(currentSmartDefaults().prMergeStyle) }
