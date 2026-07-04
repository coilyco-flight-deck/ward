package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"
)

// agent_context.go carries the role-neutral read-only catalog.dependsOn context grant
// (ward#573), resolved in-container from the fresh clone, not the host cwd (ward#580).

// catalogContextRepos returns the target's catalog.dependsOn as read-only context
// grants from the config discovered at start; empty on any miss (best-effort).
func catalogContextRepos(start string) []targetRepo {
	deps, err := loadRepoLocalCatalogDeps(start)
	if err != nil {
		return nil
	}
	return deps
}

// containerResolveContextCommand is the hidden `ward container resolve-context <clone>`:
// the bash entrypoint's hook to resolve the context set off the clone (ward#580).
func containerResolveContextCommand() *cli.Command {
	return &cli.Command{
		Name:            "resolve-context",
		Hidden:          true,
		Usage:           "Resolve catalog.dependsOn read-only context repos from a fresh clone (image-internal; ward#580).",
		SkipFlagParsing: true,
		Action: func(_ context.Context, c *cli.Command) error {
			work := c.Args().First()
			if work == "" {
				work = "."
			}
			target := targetRepo{Owner: os.Getenv("WARD_TARGET_OWNER"), Name: os.Getenv("WARD_TARGET_NAME")}
			extra := parseExtraReposEnv(os.Getenv("WARD_EXTRA_REPOS"), target.Owner, target.Name)
			repos, _ := resolveCatalogContextRepos(work, target, extra)
			fmt.Println(extraReposEnv(repos))
			return nil
		},
	}
}

// resolveCatalogContextRepos resolves catalog.dependsOn from the fresh clone at work,
// deduped against the target, the writable grants, and /substrate (ward#580).
func resolveCatalogContextRepos(work string, target targetRepo, extra []targetRepo) ([]targetRepo, []extraRepoLogLine) {
	return resolveContextRepos(catalogContextRepos(work), extra, target, substrateContextSkipSet())
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

// --- substrate inventory (ward#593): label the mounted reference repos ---------

// substrateInventoryHeader turns the unlabeled /substrate mount into a "read these
// first" pointer so a session reads it before interrogating the operator (ward#593).
const substrateInventoryHeader = `

---

## Reference repos mounted read-only under /substrate (read these BEFORE asking)

These cross-cutting reference repos are already checked out read-only in this
container. They are your first-line context for any infra / deploy / hosting /
config / DNS / domain question: the answer to "do I have a public IP", "my own
domain", "a Caddy route", "a Funnel", "an existing subdomain" lives in these
files, not in the operator's head. **Read or grep the relevant repo before you
ask a human a factual question whose answer is in a mounted repo** - naming the
gap is not closing it, and "discoverable in the clone" only helps if you look.

`

// containerSubstrateInventoryCommand is the hidden `ward container
// substrate-inventory [dest]`: bash and Go compose share one block source (ward#593).
func containerSubstrateInventoryCommand() *cli.Command {
	return &cli.Command{
		Name:            "substrate-inventory",
		Hidden:          true,
		Usage:           "Render the read-these-first pointer at the mounted /substrate repos (image-internal; ward#593).",
		SkipFlagParsing: true,
		Action: func(_ context.Context, c *cli.Command) error {
			dest := c.Args().First()
			if dest == "" {
				dest = envOr("WARD_SUBSTRATE_DEST", "/substrate")
			}
			if block := substrateInventoryBlock(dest); block != "" {
				fmt.Print(block)
			}
			return nil
		},
	}
}

// substrateInventoryBlock renders one bullet per repo warmed under dest with a
// self-sourced tagline; "" when none, so a substrate-less run appends nothing.
func substrateInventoryBlock(dest string) string {
	entries, err := os.ReadDir(dest)
	if err != nil {
		return ""
	}
	var lines []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		repo := filepath.Join(dest, entry.Name())
		line := "- **" + repo + "**"
		if tag := substrateRepoTagline(repo); tag != "" {
			line += " - " + tag
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return ""
	}
	return substrateInventoryHeader + strings.Join(lines, "\n") + "\n"
}

// substrateRepoTagline extracts a one-line summary from a repo's own
// README/AGENTS/FEATURES, so each repo self-describes and the label never drifts.
func substrateRepoTagline(repo string) string {
	for _, name := range []string{"README.md", "AGENTS.md", "docs/FEATURES.md"} {
		if tag := taglineFromFile(filepath.Join(repo, name)); tag != "" {
			return tag
		}
	}
	return ""
}

// taglineFromFile returns a markdown file's first prose line (headings, fences,
// and badge/HTML noise skipped), else its first heading text; "" on a read miss.
func taglineFromFile(path string) string {
	f, err := os.Open(path) // #nosec G304 -- bind-mounted substrate reference repo
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	inFence := false
	heading := ""
	for scanned := 0; sc.Scan() && scanned < 60; scanned++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if heading == "" {
				heading = strings.TrimSpace(strings.TrimLeft(line, "# "))
			}
			continue
		}
		if isMarkdownNoise(line) {
			continue
		}
		return truncate(collapseSpaces(line), 200)
	}
	return truncate(collapseSpaces(heading), 200)
}

// isMarkdownNoise reports whether a line is non-prose chrome a tagline should
// skip: images, badges/link-ref defs, HTML, blockquotes, and table rows.
func isMarkdownNoise(line string) bool {
	switch line[0] {
	case '!', '[', '<', '>', '|':
		return true
	}
	return false
}

// collapseSpaces folds any internal whitespace run to a single space so a wrapped
// markdown line renders as one clean tagline.
func collapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
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
