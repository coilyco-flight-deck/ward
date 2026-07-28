package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	bundleFixtureAgentsPath   = "alpha.kdl"
	bundleFixtureRolesPath    = "bravo.kdl"
	bundleFixtureDefaultsPath = "charlie.kdl"
	bundleFixtureReposPath    = "delta.kdl"
	bundleFixtureTopologyPath = "echo.kdl"
	bundleAggregatePath       = "hotel.kdl"
	bundleSinglePath          = "single.kdl"
)

func writeBundleFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	files := map[string]string{
		bundleFixtureAgentsPath: `
agents {
    schema-version 2
    defaults {
        agent claude
        attribution name=example-bot email=bot@example.com
    }
    agent claude {
    }
}
`,
		bundleFixtureRolesPath: `
roles {
    role engineer {
        agent claude {
            model claude-fable-5
            reasoning-effort medium
        }
        agent codex {
            model gpt-5.4-mini
            reasoning-effort medium
        }
    }
    role director {
        guardfiles tailscale.kdl
        agent claude {
            model "claude-opus-4-8[1m]"
            reasoning-effort high
        }
        agent codex {
            model gpt-5.5
            reasoning-effort high
        }
    }
}
`,
		bundleFixtureReposPath: `
repos {
    repo-authority default=forgejo {
        trusted-owner "coilysiren"
        repo "coilysiren/*" forge=github
    }
}
`,
		bundleFixtureTopologyPath: `
topology {
    tailnet-network "net-x"
    tailnet-proxy "proxy-x:9050"
    tower-host "tower-x"
    tower-ollama-port "19090"
    substrate-seed "/seed-x"
    substrate-dest "/dest-x"
    substrate-manifest "/manifest-x"
    substrate-ttl "42"
}
`,
	}

	roleDefs, err := bakedAssets.ReadFile(roleDefinitionsKDLPath)
	if err != nil {
		t.Fatalf("read baked role definitions: %v", err)
	}
	files["roles.kdl"] = string(roleDefs)

	files[bundleFixtureDefaultsPath] = canonicalSmartDefaultsBlock(t, func(defs *smartDefaults) {
		defs.reservationRecheckDefaultMax = 9 * time.Second
		defs.agentReapIdleDefault = 90 * time.Minute
		defs.agentReapMaxCPUDefault = 7.5
		defs.containerMemoryLimit = "3g"
		defs.engineerContainerLimit = 17
		defs.engineerOpenPRBranchLimit = 8
		defs.directorMaxParallel = 13
		defs.directorLimit = 77
		defs.directorPollInterval = 45 * time.Second
		defs.reviewerTimeout = 11 * time.Minute
		defs.configBundleTTL = 15 * time.Minute
		defs.containerAssetsTTL = 3 * time.Hour
		defs.containerReadOnlyExtraRepoTTL = 48 * time.Hour
		defs.containerReapTTL = 48 * time.Hour
	})

	for name, body := range files {
		writeBundleFixtureFile(t, dir, name, body)
	}

	return dir
}

func writeSelectedBundleFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeBundleFixtureFile(t, dir, "fleet.kdl", "agents {\n    schema-version 2\n    defaults {\n        agent codex\n        attribution name=example-bot email=bot@example.com\n    }\n    agent claude {\n    }\n    agent codex {\n    }\n}\n")
	writeBundleFixtureFile(t, dir, "roles.kdl", "agent-roles {\n    role engineer {\n        tagline \"Implements a ticket end to end.\"\n        capabilities read engineering\n        modes \"A ref carries that issue detached, fire-and-forget. Freeform text files an issue first, then carries it. Detached-only - interactive work funnels to the director.\"\n        default-harness codex\n        posture code-landing\n        execution-time-limit \"90m\"\n    }\n    role director {\n        tagline \"Opens the read-only director surface. Autonomous burndown is opt-in.\"\n        capabilities read project-management\n        modes \"Attached read-only control surface over a repo's backlog (`--repo` scope). Use `--burndown` or `--drain` for the autonomous heartbeat.\"\n        default-harness codex\n        posture attached\n        execution-time-limit none\n    }\n    role qa {\n        tagline \"Inspects a candidate and posts a structured QA verdict comment.\"\n        capabilities read\n        modes \"A ref inspects the issue, branch, pull request, and checks, then posts a structured QA verdict comment. Freeform mode is not exposed.\"\n        default-harness codex\n        posture no-code\n        execution-time-limit \"30m\"\n    }\n}\n")
	return dir
}

func writeBundleFixtureFile(t *testing.T, dir, name, body string) {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(strings.TrimSpace(body)+"\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func canonicalSmartDefaultsBlock(t *testing.T, mutate func(*smartDefaults)) string {
	t.Helper()
	defs := canonicalSmartDefaults(t)
	if mutate != nil {
		mutate(&defs)
	}
	return renderSmartDefaultsBlock(defs)
}

func renderSmartDefaultsBlock(defs smartDefaults) string {
	var b strings.Builder
	b.WriteString("defaults")
	b.WriteString(" {\n")
	if defs.agentReservationTTL > 0 {
		fmt.Fprintf(&b, "    agent-reservation-ttl %q\n", conciseDuration(defs.agentReservationTTL))
	}
	if defs.reservationRecheckDefaultMax > 0 {
		fmt.Fprintf(&b, "    agent-reservation-recheck-max %q\n", conciseDuration(defs.reservationRecheckDefaultMax))
	}
	if defs.agentReapIdleDefault > 0 {
		fmt.Fprintf(&b, "    agent-reap-idle %q\n", conciseDuration(defs.agentReapIdleDefault))
	}
	if defs.agentReapMaxCPUDefault > 0 {
		fmt.Fprintf(&b, "    agent-reap-max-cpu %q\n", fmt.Sprintf("%g", defs.agentReapMaxCPUDefault))
	}
	if defs.agentImage != "" {
		fmt.Fprintf(&b, "    agent-image %q\n", defs.agentImage)
	}
	if defs.agentTag != "" {
		fmt.Fprintf(&b, "    agent-tag %q\n", defs.agentTag)
	}
	if defs.containerMemoryLimit != "" {
		fmt.Fprintf(&b, "    container-memory-limit %q\n", defs.containerMemoryLimit)
	}
	if defs.engineerContainerLimit > 0 {
		fmt.Fprintf(&b, "    engineer-container-limit %q\n", fmt.Sprintf("%d", defs.engineerContainerLimit))
	}
	if defs.engineerRepoWorkingLimit > 0 {
		fmt.Fprintf(&b, "    engineer-repo-working-limit %q\n", fmt.Sprintf("%d", defs.engineerRepoWorkingLimit))
	}
	if defs.engineerOpenPRBranchLimit > 0 {
		fmt.Fprintf(&b, "    engineer-open-pr-branch-limit %q\n", fmt.Sprintf("%d", defs.engineerOpenPRBranchLimit))
	}
	if defs.directorMaxParallel > 0 {
		fmt.Fprintf(&b, "    director-max-parallel %q\n", fmt.Sprintf("%d", defs.directorMaxParallel))
	}
	if defs.directorLimit > 0 {
		fmt.Fprintf(&b, "    director-limit %q\n", fmt.Sprintf("%d", defs.directorLimit))
	}
	if defs.directorPollInterval > 0 {
		fmt.Fprintf(&b, "    director-poll-interval %q\n", conciseDuration(defs.directorPollInterval))
	}
	if defs.reviewerTimeout > 0 {
		fmt.Fprintf(&b, "    reviewer-timeout %q\n", conciseDuration(defs.reviewerTimeout))
	}
	if defs.configBundleTTL > 0 {
		fmt.Fprintf(&b, "    config-bundle-ttl %q\n", conciseDuration(defs.configBundleTTL))
	}
	if defs.containerAssetsTTL > 0 {
		fmt.Fprintf(&b, "    container-assets-ttl %q\n", conciseDuration(defs.containerAssetsTTL))
	}
	if defs.containerReadOnlyExtraRepoTTL > 0 {
		fmt.Fprintf(&b, "    container-read-only-extra-repo-ttl %q\n", conciseDuration(defs.containerReadOnlyExtraRepoTTL))
	}
	if defs.containerReapTTL > 0 {
		fmt.Fprintf(&b, "    container-reap-ttl %q\n", conciseDuration(defs.containerReapTTL))
	}
	if defs.agentWorkflowDefault != "" {
		fmt.Fprintf(&b, "    agent-workflow default=%s {\n    }\n", defs.agentWorkflowDefault)
	}
	if defs.prMergeStyle != "" {
		fmt.Fprintf(&b, "    pr-merge-style %q\n", defs.prMergeStyle)
	}
	b.WriteString("}\n")
	return b.String()
}
