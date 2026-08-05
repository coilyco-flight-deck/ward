package main

import (
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"
)

const verificationFixtureFlagName = "verification-fixture"

func verificationFixtureFlag() cli.Flag {
	return &cli.BoolFlag{
		Name:  verificationFixtureFlagName,
		Usage: "run only against an issue admitted by agent.verification.fixtures; forces remote-branch-only work and stamps verification evidence",
	}
}

func verificationFixtureRequested(c *cli.Command) bool {
	return c != nil && c.Bool(verificationFixtureFlagName)
}

func normalizeVerificationFixtureRules(in []verificationFixtureRule) ([]verificationFixtureRule, error) {
	out := make([]verificationFixtureRule, 0, len(in))
	seen := map[string]bool{}
	for i, raw := range in {
		rule := verificationFixtureRule{
			Repository: strings.TrimSpace(raw.Repository),
			IssueLabel: strings.TrimSpace(raw.IssueLabel),
		}
		if !validWorkflowRepoSlug(rule.Repository) {
			return nil, fmt.Errorf("entry %d repository %q must be owner/name", i+1, raw.Repository)
		}
		if rule.IssueLabel == "" {
			return nil, fmt.Errorf("entry %d issue-label must not be empty", i+1)
		}
		key := strings.ToLower(rule.Repository + "\x00" + rule.IssueLabel)
		if seen[key] {
			return nil, fmt.Errorf("entry %d duplicates %s with issue label %q", i+1, rule.Repository, rule.IssueLabel)
		}
		seen[key] = true
		out = append(out, rule)
	}
	return out, nil
}

func validateVerificationFixtureTarget(c *cli.Command, ref agentIssueRef, issue *Issue) error {
	if !verificationFixtureRequested(c) {
		return nil
	}
	if ref.MergeRequest {
		return fmt.Errorf("--%s requires an issue ref, not a pull request ref", verificationFixtureFlagName)
	}
	defs, err := currentSmartDefaultsWithError()
	if err != nil {
		return err
	}
	slug := ref.repoSlug()
	var required []string
	for _, rule := range defs.verificationFixtures {
		if !strings.EqualFold(rule.Repository, slug) {
			continue
		}
		required = append(required, rule.IssueLabel)
		if issue != nil && issueHasModeLabel(issue.Labels, rule.IssueLabel) {
			return nil
		}
	}
	if len(required) == 0 {
		return fmt.Errorf("--%s target %s is not admitted by agent.verification.fixtures", verificationFixtureFlagName, slug)
	}
	return fmt.Errorf("--%s issue %s requires one configured fixture label: %s",
		verificationFixtureFlagName, ref, strings.Join(required, ", "))
}

func validateVerificationFixtureEngineerOptions(c *cli.Command) error {
	if !verificationFixtureRequested(c) {
		return nil
	}
	if c.Bool("pr") {
		return fmt.Errorf("--%s does not accept pull request input", verificationFixtureFlagName)
	}
	if strings.TrimSpace(c.String("branch")) != "" {
		return fmt.Errorf("--%s owns the deterministic issue branch; --branch is not allowed", verificationFixtureFlagName)
	}
	if len(c.StringSlice("repo")) > 0 {
		return fmt.Errorf("--%s does not allow additional writable repositories", verificationFixtureFlagName)
	}
	if c.Bool("override-capacity") {
		return fmt.Errorf("--%s does not allow --override-capacity", verificationFixtureFlagName)
	}
	return nil
}
