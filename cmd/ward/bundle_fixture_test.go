package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		bundleFixtureDefaultsPath: `
defaults {
    agent-reservation-ttl "3h"
    agent-reservation-recheck-max "9s"
    agent-reap-idle "90m"
    agent-reap-max-cpu "7.5"
    engineer-container-limit "17"
    engineer-repo-working-limit "3"
    engineer-open-pr-branch-limit "8"
    director-max-parallel "13"
    director-limit "77"
    director-poll-interval "45s"
    reviewer-timeout "11m"
    config-bundle-ttl "900"
    container-assets-ttl "3h"
    container-read-only-extra-repo-ttl "48h"
    container-reap-keep "12"
    agent-workflow default="merge-remote-main" {
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
defaults {
    agent-reservation-ttl "2h"
    agent-reservation-recheck-max "9s"
}
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
		`
defaults {
    agent-reservation-ttl "3h"
    agent-reservation-recheck-max "9s"
    agent-reap-idle "90m"
    agent-reap-max-cpu "7.5"
    engineer-container-limit "17"
    engineer-open-pr-branch-limit "8"
    director-max-parallel "13"
    director-limit "77"
    director-poll-interval "45s"
    reviewer-timeout "11m"
    config-bundle-ttl "900"
    container-assets-ttl "3h"
    container-read-only-extra-repo-ttl "48h"
    container-reap-keep "12"
    agent-workflow default="merge-remote-main" {
    }
}
`,
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
