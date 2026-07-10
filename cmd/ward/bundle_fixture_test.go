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
	neutralBundleForgejoGuardfilePath = "ops.forgejo.kdl"
	compatBundleForgejoGuardfilePath  = "guardfile.forgejo.kdl"
)

func writeBundleFixture(t *testing.T) string {
	return writeBundleFixtureWithForgejoGuardfile(t, neutralBundleForgejoGuardfilePath)
}

func writeCompatBundleFixture(t *testing.T) string {
	return writeBundleFixtureWithForgejoGuardfile(t, compatBundleForgejoGuardfilePath)
}

func writeBundleFixtureWithForgejoGuardfile(t *testing.T, forgejoGuardfilePath string) string {
	t.Helper()
	dir := t.TempDir()

	files := map[string]string{
		bundleOpsManifestPath: fmt.Sprintf(`
ops {
    forgejo "%s"
}
`, forgejoGuardfilePath),
		bundleAgentsKDLPath: `
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
		bundleRolesKDLPath: `
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
        guardfile guardfile.aws.kdl
        guardfile guardfile.tailscale.kdl
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
        guardfile guardfile.aws.kdl
        guardfile guardfile.tailscale.kdl
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
		bundleDefaultsKDLPath: `
defaults {
    agent-reservation-ttl "2h"
    agent-reservation-recheck-max "9s"
    agent-reap-idle "90m"
    agent-reap-max-cpu "7.5"
    engineer-container-limit "17"
    director-max-parallel "13"
    director-limit "77"
    director-poll-interval "45s"
    reviewer-timeout "11m"
    config-bundle-ttl "900"
    container-assets-ttl "3h"
    container-read-only-extra-repo-ttl "48h"
    container-reap-keep "12"
    agent-workflow default="direct-main" {
    }
}
`,
		bundleReposKDLPath: `
repos {
    repo-authority default=forgejo {
        trusted-owner "example-owner"
        repo "example-owner/*" forge=github
    }
}
	`,
		bundleTopologyKDLPath: `
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
		bundleForgejoSpecLockPath: "",
	}

	// Reuse the embedded Forgejo guardfile + spec lock so the split-layout fixture
	// keeps the same surface shape without reauthoring the large spec file.
	forgejoGuardfile, err := bakedAssets.ReadFile(opsForgejoGuardfilePath)
	if err != nil {
		t.Fatalf("read baked ops guardfile: %v", err)
	}
	files[forgejoGuardfilePath] = string(forgejoGuardfile)
	specLock, err := bakedAssets.ReadFile(opsForgejoSpecLockPath)
	if err != nil {
		t.Fatalf("read baked ops spec lock: %v", err)
	}
	files[bundleForgejoSpecLockPath] = string(specLock)

	if forgejoGuardfilePath == compatBundleForgejoGuardfilePath {
		files["guardfile.forgejo.write.kdl"] = strings.TrimSpace(`
wrap ward-kdl ops forgejo {
    inherit guardfile.forgejo.read.kdl
}
`)
	}

	entries, err := fs.ReadDir(bakedAssets, execAssetsDir)
	if err != nil {
		t.Fatalf("read baked execassets: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		srcName := filepath.Join(execAssetsDir, e.Name())
		src, err := bakedAssets.ReadFile(srcName)
		if err != nil {
			t.Fatalf("read baked %s: %v", srcName, err)
		}
		files[bundleGuardfileName(e.Name())] = string(src)
	}

	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.TrimSpace(body)+"\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	return dir
}

func bundleGuardfileName(name string) string {
	if !strings.HasPrefix(name, "ward-kdl.") || !strings.HasSuffix(name, ".guardfile.kdl") {
		return name
	}
	trimmed := strings.TrimSuffix(strings.TrimPrefix(name, "ward-kdl."), ".guardfile.kdl")
	return fmt.Sprintf("guardfile.%s.kdl", trimmed)
}
