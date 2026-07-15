package main

import (
	"fmt"
	"io/fs"
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
	bundleFixtureForgejoPath  = "foxtrot.kdl"
	bundleFixtureSpecLockPath = "golf.json"
	bundleAggregatePath       = "hotel.kdl"
	bundleAggregateForgejo    = "india.kdl"
	bundleAggregateSpecLock   = "juliet.json"
	bundleSinglePath          = "single.kdl"
	bundleSingleSpecLock      = "single.json"
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
        guardfiles ward-kdl.aws.guardfile.kdl ward-kdl.tailscale.guardfile.kdl
        agent claude {
            model "claude-opus-4-8[1m]"
            reasoning-effort high
        }
        agent codex {
            model gpt-5.5
            reasoning-effort high
        }
    }
    role advisor {
        guardfiles ward-kdl.aws.guardfile.kdl ward-kdl.tailscale.guardfile.kdl
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

	// Reuse the embedded Forgejo guardfile + spec lock so the split-layout fixture
	// keeps the same surface shape without reauthoring the large spec file.
	forgejoGuardfile, err := bakedAssets.ReadFile(opsForgejoGuardfilePath)
	if err != nil {
		t.Fatalf("read baked ops guardfile: %v", err)
	}
	files[bundleFixtureForgejoPath] = strings.ReplaceAll(string(forgejoGuardfile), "forgejo.swagger.v1.json", bundleFixtureSpecLockPath)
	specLock, err := bakedAssets.ReadFile(opsForgejoSpecLockPath)
	if err != nil {
		t.Fatalf("read baked ops spec lock: %v", err)
	}
	files[bundleFixtureSpecLockPath] = string(specLock)
	roleDefs, err := bakedAssets.ReadFile(roleDefinitionsGeneratedKDLPath)
	if err != nil {
		t.Fatalf("read baked role definitions: %v", err)
	}
	files["ward-kdl/ward-kdl.role-definitions.kdl"] = string(roleDefs)

	entries, err := fs.ReadDir(bakedAssets, execAssetsDir)
	if err != nil {
		t.Fatalf("read baked execassets: %v", err)
	}
	execIndex := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		srcName := filepath.Join(execAssetsDir, e.Name())
		src, err := bakedAssets.ReadFile(srcName)
		if err != nil {
			t.Fatalf("read baked %s: %v", srcName, err)
		}
		execIndex++
		files[fmt.Sprintf("exec/%02d.kdl", execIndex)] = string(src)
	}
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
		defs.containerReapKeep = 12
	})

	for name, body := range files {
		writeBundleFixtureFile(t, dir, name, body)
	}

	return dir
}

func writeAggregateBundleFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	parts := []string{
		strings.TrimSpace(`
agents {
    schema-version 2
    defaults {
        agent claude
        attribution name=example-bot email=bot@example.com
    }
    agent claude {
    }
}
`),
		strings.TrimSpace(`
roles {
    role engineer {
        agent claude {
            model claude-fable-5
            reasoning-effort medium
        }
    }
}
`),
		strings.TrimSpace(`
` + canonicalSmartDefaultsBlock(t, func(defs *smartDefaults) {
			defs.agentReservationTTL = 2 * time.Hour
			defs.reservationRecheckDefaultMax = 9 * time.Second
		}) + `
`),
		strings.TrimSpace(`
repos {
    repo-authority default=forgejo {
        trusted-owner "coilysiren"
        repo "coilysiren/*" forge=github
    }
}
`),
		strings.TrimSpace(`
topology {
    tailnet-network "net-x"
    tower-host "tower-x"
}
`),
	}
	writeBundleFixtureFile(t, dir, bundleAggregatePath, strings.Join(parts, "\n\n"))
	forgejoGuardfile, err := bakedAssets.ReadFile(opsForgejoGuardfilePath)
	if err != nil {
		t.Fatalf("read baked ops guardfile: %v", err)
	}
	writeBundleFixtureFile(t, dir, bundleAggregateForgejo, strings.ReplaceAll(string(forgejoGuardfile), "forgejo.swagger.v1.json", bundleAggregateSpecLock))
	specLock, err := bakedAssets.ReadFile(opsForgejoSpecLockPath)
	if err != nil {
		t.Fatalf("read baked ops spec lock: %v", err)
	}
	writeBundleFixtureFile(t, dir, bundleAggregateSpecLock, string(specLock))
	return dir
}

func writeSingleFileBundleFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	forgejoGuardfile, err := bakedAssets.ReadFile(opsForgejoGuardfilePath)
	if err != nil {
		t.Fatalf("read baked ops guardfile: %v", err)
	}
	specLock, err := bakedAssets.ReadFile(opsForgejoSpecLockPath)
	if err != nil {
		t.Fatalf("read baked ops spec lock: %v", err)
	}

	body := strings.Join([]string{
		`
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
		`
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
}
`,
		canonicalSmartDefaultsBlock(t, func(defs *smartDefaults) {
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
			defs.containerReapKeep = 12
		}),
		`
repos {
    repo-authority default=forgejo {
        trusted-owner "coilysiren"
        repo "coilysiren/*" forge=github
    }
}
`,
		`
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
		strings.ReplaceAll(string(forgejoGuardfile), "forgejo.swagger.v1.json", bundleSingleSpecLock),
	}, "\n\n")

	writeBundleFixtureFile(t, dir, bundleSinglePath, body)
	writeBundleFixtureFile(t, dir, bundleSingleSpecLock, string(specLock))
	return filepath.Join(dir, bundleSinglePath)
}

func writeSelectedBundleFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeBundleFixtureFile(t, dir, "ward-kdl/ward-kdl.fleet.kdl", "agents {\n    schema-version 2\n    defaults {\n        agent codex\n        attribution name=example-bot email=bot@example.com\n    }\n    agent claude {\n    }\n    agent codex {\n    }\n}\n")
	writeBundleFixtureFile(t, dir, "ward-kdl/ward-kdl.role-definitions.kdl", "agent-roles {\n    role engineer {\n        tagline \"Implements a ticket end to end.\"\n        capabilities read engineering\n        modes \"A ref carries that issue detached, fire-and-forget. Freeform text files an issue first, then carries it. Detached-only - interactive work funnels to the director.\"\n        default-harness codex\n        posture code-landing\n        execution-time-limit \"90m\"\n    }\n    role director {\n        tagline \"Opens the read-only director surface. Autonomous burndown is opt-in.\"\n        capabilities read project-management\n        modes \"Attached read-only control surface over a repo's backlog (`--repo` scope). Use `--burndown` or `--drain` for the autonomous heartbeat.\"\n        default-harness codex\n        posture attached\n        execution-time-limit none\n    }\n    role advisor {\n        tagline \"Answers without writing code.\"\n        capabilities read\n        modes \"A ref researches the issue and posts the answer as a comment. Freeform text answers inline.\"\n        default-harness codex\n        posture no-code\n        execution-time-limit \"60m\"\n    }\n    role qa {\n        tagline \"Inspects a candidate and posts a structured verdict comment.\"\n        capabilities read\n        modes \"A ref inspects the issue, branch, pull request, and checks, then posts a structured QA verdict comment. Freeform mode is not exposed.\"\n        default-harness codex\n        posture no-code\n        execution-time-limit \"30m\"\n    }\n}\n")
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
	if defs.containerReapKeep > 0 {
		fmt.Fprintf(&b, "    container-reap-keep %q\n", fmt.Sprintf("%d", defs.containerReapKeep))
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
