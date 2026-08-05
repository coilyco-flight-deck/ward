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

// catalogContextRepo is one resolved read-only catalog dependency: the owner/name
// dedup key plus, for an external (non-Forgejo) dep, its honored URL + host (ward#612).
type catalogContextRepo struct {
	targetRepo
	// CloneURL is the dep's clone URL (host + transport) for an external dep; "" =>
	// Forgejo, cloned via CloneBase over HTTPS like every other mirror.
	CloneURL string
	// Host is the external dep's git host (e.g. github.com); "" for a Forgejo dep.
	Host string
}

// external reports whether this dep names a non-Forgejo host, so it must clone over
// its own transport off a host-side seeded gitcache mirror, never CloneBase.
func (c catalogContextRepo) external() bool { return c.CloneURL != "" }

// catalogContextRepos returns the target's catalog.dependsOn as read-only context
// grants from the config discovered at start; empty on any miss (best-effort).
func catalogContextRepos(start string) []catalogContextRepo {
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
			fmt.Println(contextReposEnv(repos))
			return nil
		},
	}
}

// resolveCatalogContextRepos resolves catalog.dependsOn from the fresh clone at work,
// deduped against the target, the writable grants, and /substrate (ward#580).
func resolveCatalogContextRepos(work string, target targetRepo, extra []targetRepo) ([]catalogContextRepo, []extraRepoLogLine) {
	return resolveContextRepos(catalogContextRepos(work), extra, target, substrateContextSkipSet())
}

// loadRepoLocalCatalogDeps parses catalog.dependsOn (config discovered at start) into a
// deduped dep list, honoring a full clone URL (ward#612); bad refs skip.
func loadRepoLocalCatalogDeps(start string) ([]catalogContextRepo, error) {
	path, err := discoverConfig(start)
	if err != nil {
		//nolint:nilerr // missing config is best-effort, not a hard failure
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
	var out []catalogContextRepo
	seen := map[string]bool{}
	for _, dep := range doc.Catalog.DependsOn {
		repo, err := parseCatalogDep(dep)
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

// resolveContextRepos folds catalog deps into the read-only set, dropping the target,
// the writable grants, and /substrate (Forgejo-only) repos (ward#573).
func resolveContextRepos(auto []catalogContextRepo, explicit []targetRepo, target targetRepo, substrate map[string]bool) ([]catalogContextRepo, []extraRepoLogLine) {
	seen := map[string]bool{target.slug(): true}
	for _, repo := range explicit {
		seen[repo.slug()] = true
	}
	var out []catalogContextRepo
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
		if !repo.external() && substrate[slug] {
			// Already present read-only under /substrate; the agent reads it there.
			continue
		}
		out = append(out, repo)
		reason := "read-only catalog dependency"
		if repo.external() {
			reason = "read-only external catalog dependency (" + repo.Host + ", host-side seeded)"
		}
		notes = append(notes, extraRepoLogLine{Slug: slug, Reason: reason})
	}
	return out, notes
}

// parseCatalogDep resolves one catalog.dependsOn entry, honoring a full clone URL: bare
// owner/name or a Forgejo-host URL is internal, any other host external (ward#612).
func parseCatalogDep(dep string) (catalogContextRepo, error) {
	dep = strings.TrimSpace(dep)
	repo, err := parseRepoRef(dep)
	if err != nil {
		return catalogContextRepo{}, err
	}
	out := catalogContextRepo{targetRepo: repo}
	host := cloneRefHost(dep)
	if host != "" && !strings.EqualFold(host, forgejoCanonicalHost()) {
		out.Host = host
		out.CloneURL = normalizeExternalCloneURL(dep, host, repo)
	}
	return out, nil
}

// forgejoCanonicalHost is ward's home Forgejo host, the classification pivot: a dep on
// this host (or a bare owner/name) is internal; any other host is external (ward#612).
func forgejoCanonicalHost() string { return forgejoHostFromBase(forgejoBaseURL) }

// cloneRefHost extracts the git host from a clone ref, "" for a bare owner/name: scheme
// URLs (ssh/https), scp-style git@host:path, or a host/owner/name (dotted first segment).
func cloneRefHost(ref string) string {
	ref = strings.TrimSpace(ref)
	if i := strings.Index(ref, "://"); i >= 0 {
		rest := ref[i+3:]
		if at := strings.IndexByte(rest, '@'); at >= 0 {
			rest = rest[at+1:]
		}
		rest = rest[:strings.IndexFunc(rest+"/", func(r rune) bool { return r == '/' || r == ':' })]
		return rest
	}
	// scp-style: [user@]host:path, the host before the first colon and no scheme.
	if at := strings.IndexByte(ref, '@'); at >= 0 && strings.Contains(ref, ":") {
		rest := ref[at+1:]
		if c := strings.IndexByte(rest, ':'); c >= 0 {
			return rest[:c]
		}
	}
	// Bare path form: host/owner/name has a dotted (host-looking) first segment.
	if parts := strings.Split(ref, "/"); len(parts) >= 3 && strings.Contains(parts[0], ".") {
		return parts[0]
	}
	return ""
}

// normalizeExternalCloneURL honors an external dep's declared transport (scheme/scp),
// else synthesizes the sanctioned ssh URL for a bare host/owner/name (ward#612).
func normalizeExternalCloneURL(dep, host string, repo targetRepo) string {
	if strings.Contains(dep, "://") {
		return dep
	}
	if at := strings.IndexByte(dep, '@'); at >= 0 && strings.Contains(dep, ":") {
		return dep // scp-style git@host:owner/name.git
	}
	return "ssh://git@" + host + "/" + repo.Owner + "/" + repo.Name + ".git"
}

// contextReposEnv renders WARD_CONTEXT_REPOS: a Forgejo dep as owner/name, external as
// owner/name=<cloneURL> so its transport survives the env word-split (ward#612).
func contextReposEnv(repos []catalogContextRepo) string {
	toks := make([]string, len(repos))
	for i, r := range repos {
		toks[i] = r.slug()
		if r.external() {
			toks[i] += "=" + r.CloneURL
		}
	}
	return strings.Join(toks, " ")
}

// parseContextReposEnv inverts contextReposEnv (dropping blanks, target, dups, bad refs);
// owner/name=<cloneURL> restores an external dep's transport (ward#612).
func parseContextReposEnv(raw, targetOwner, targetName string) []catalogContextRepo {
	var out []catalogContextRepo
	seen := map[string]bool{}
	for _, tok := range strings.Fields(raw) {
		slug, cloneURL, external := strings.Cut(tok, "=")
		owner, name, ok := splitOwnerName(slug)
		if !ok {
			continue
		}
		if owner == targetOwner && name == targetName {
			continue
		}
		if seen[owner+"/"+name] {
			continue
		}
		seen[owner+"/"+name] = true
		dep := catalogContextRepo{targetRepo: targetRepo{Owner: owner, Name: name}}
		if external {
			dep.CloneURL = cloneURL
			dep.Host = cloneRefHost(cloneURL)
		}
		out = append(out, dep)
	}
	return out
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

// substrateInventoryBlock renders one bullet per repo warmed under dest, using
// only that repo's local documentation to describe it; "" when none.
func substrateInventoryBlock(dest string) string {
	return renderSubstrateInventory(dest)
}

// renderSubstrateInventory is the pure block builder: one bullet per mounted repo,
// each described by its README/AGENTS/FEATURES tagline. "" when none.
func renderSubstrateInventory(dest string) string {
	entries, err := os.ReadDir(dest)
	if err != nil {
		return ""
	}
	var lines []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		lines = append(lines, inventoryLine(filepath.Join(dest, entry.Name())))
	}
	if len(lines) == 0 {
		return ""
	}
	return substrateInventoryHeader + strings.Join(lines, "\n") + "\n"
}

// inventoryLine renders one read-these-first bullet from the mounted path and the
// repo's own local documentation, retaining a bare path when no tagline exists.
func inventoryLine(repo string) string {
	line := "- **" + repo + "**"
	if desc := substrateRepoTagline(repo); desc != "" {
		line += " - " + desc
	}
	return line
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
