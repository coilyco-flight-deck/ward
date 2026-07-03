package main

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// agent_context.go carries the role-neutral read-only auto-context grant (ward#573):
// every warded role mounts the target's catalog.dependsOn read-only reference clones.

// catalogContextRepos returns the target's catalog.dependsOn as read-only context
// grants from the config discovered at start; empty on any miss (best-effort).
func catalogContextRepos(start string) []targetRepo {
	deps, err := loadRepoLocalCatalogDeps(start)
	if err != nil {
		return nil
	}
	return deps
}

// loadRepoLocalCatalogDeps parses catalog.dependsOn out of the repo-local config
// discovered from start into a de-duplicated targetRepo list; unparseable refs skip.
func loadRepoLocalCatalogDeps(start string) ([]targetRepo, error) {
	path, err := discoverConfig(start)
	if err != nil {
		return nil, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("catalog context: read %s: %w", filepath.Base(path), err)
	}
	var doc struct {
		Catalog struct {
			DependsOn []string `yaml:"dependsOn"`
		} `yaml:"catalog"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("catalog context: parse %s: %w", filepath.Base(path), err)
	}
	var out []targetRepo
	seen := map[string]bool{}
	for _, dep := range doc.Catalog.DependsOn {
		repo, err := parseRepoRef(dep)
		if err != nil {
			continue
		}
		if seen[repo.slug()] {
			continue
		}
		seen[repo.slug()] = true
		out = append(out, repo)
	}
	return out, nil
}

// resolveContextRepos folds catalog deps into the read-only context set (first-seen
// order), dropping the target, the writable grants, and /substrate repos (ward#573).
func resolveContextRepos(auto, explicit []targetRepo, target targetRepo, substrate map[string]bool) ([]targetRepo, []extraRepoLogLine) {
	seen := map[string]bool{target.slug(): true}
	for _, repo := range explicit {
		seen[repo.slug()] = true
	}
	var out []targetRepo
	var notes []extraRepoLogLine
	for _, repo := range auto {
		if repo.Owner == "" || repo.Name == "" {
			continue
		}
		slug := repo.slug()
		if seen[slug] {
			continue
		}
		seen[slug] = true
		if substrate[slug] {
			// Already present read-only under /substrate; the agent reads it there.
			continue
		}
		out = append(out, repo)
		notes = append(notes, extraRepoLogLine{Slug: slug, Reason: "read-only catalog dependency"})
	}
	return out, notes
}

// substrateContextSkipSet is the set of substrate-manifest slugs a catalog dep is
// deduped against; a manifest read error yields an empty set (no dedup, no failure).
func substrateContextSkipSet() map[string]bool {
	repos, err := loadSubstrateManifest()
	if err != nil {
		return nil
	}
	skip := make(map[string]bool, len(repos))
	for _, repo := range repos {
		skip[repo.slug()] = true
	}
	return skip
}
