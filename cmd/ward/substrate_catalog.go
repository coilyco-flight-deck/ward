package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/urfave/cli/v3"
)

// substrate_catalog.go builds the auto-generated substrate repo catalog: a
// Forgejo-derived index ward owns, infra bakes. See docs/substrate-catalog.md.

// substrateCatalogSchema is the on-disk catalog format version, stamped into the
// artifact so a reader can refuse a shape it does not understand.
const substrateCatalogSchema = 1

// substrateCatalogSeedFile is the catalog's filename inside the substrate seed
// (WARD_SUBSTRATE_SEED); the trigger writes it here, the compose path reads it.
const substrateCatalogSeedFile = "substrate-catalog.json"

// catalogEntry is one repo in the substrate catalog: its canonical Forgejo identity
// (full_name, description, topics) plus the seed tier and /substrate mount path.
type catalogEntry struct {
	FullName    string   `json:"full_name"`
	Description string   `json:"description"`
	Topics      []string `json:"topics"`
	Tier        string   `json:"tier"`
	MountPath   string   `json:"mount_path,omitempty"`
}

// substrateCatalog is the serialised artifact: a schema stamp plus the entries,
// sorted by full_name so the generated file is byte-stable run to run.
type substrateCatalog struct {
	Schema int            `json:"schema"`
	Repos  []catalogEntry `json:"repos"`
}

// ownerRepoLister lists an owner's repos. *forgejoClient satisfies it, and a fake
// stands in for tests so the builder is exercised without a live Forgejo.
type ownerRepoLister interface {
	listOwnerRepos(ctx context.Context, owner string) ([]repoBrief, error)
}

// filterSubstrateTier narrows the manifest to one seed tier (image|cache), or
// passes it through when tier is "". Keeps the public seed image-tier only; see docs.
func filterSubstrateTier(manifest []substrateRepo, tier string) ([]substrateRepo, error) {
	if tier == "" {
		return manifest, nil
	}
	if !substrateTiers[tier] {
		return nil, fmt.Errorf("substrate catalog: tier %q must be image|cache", tier)
	}
	out := make([]substrateRepo, 0, len(manifest))
	for _, repo := range manifest {
		if repo.Tier == tier {
			out = append(out, repo)
		}
	}
	return out, nil
}

// buildSubstrateCatalog lists each unique manifest owner once and emits one entry
// per manifest repo; a repo Forgejo omits falls back to its own owner/name.
func buildSubstrateCatalog(ctx context.Context, cl ownerRepoLister, manifest []substrateRepo, dest string) (substrateCatalog, error) {
	byOwner := map[string]map[string]repoBrief{}
	for _, repo := range manifest {
		if _, done := byOwner[repo.Owner]; done {
			continue
		}
		repos, err := cl.listOwnerRepos(ctx, repo.Owner)
		if err != nil {
			return substrateCatalog{}, fmt.Errorf("substrate catalog: list %s: %w", repo.Owner, err)
		}
		idx := make(map[string]repoBrief, len(repos))
		for _, rb := range repos {
			idx[rb.Name] = rb
		}
		byOwner[repo.Owner] = idx
	}
	entries := make([]catalogEntry, 0, len(manifest))
	for _, repo := range manifest {
		entry := catalogEntry{
			FullName:  repo.slug(),
			Tier:      repo.Tier,
			MountPath: dest + "/" + repo.Name,
		}
		if rb, ok := byOwner[repo.Owner][repo.Name]; ok {
			if rb.FullName != "" {
				entry.FullName = rb.FullName
			}
			entry.Description = rb.Description
			entry.Topics = rb.Topics
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].FullName < entries[j].FullName })
	return substrateCatalog{Schema: substrateCatalogSchema, Repos: entries}, nil
}

// renderSubstrateCatalog marshals the catalog to indented JSON with a trailing
// newline - small file, so readability wins over compactness.
func renderSubstrateCatalog(cat substrateCatalog) ([]byte, error) {
	b, err := json.MarshalIndent(cat, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("substrate catalog: marshal: %w", err)
	}
	return append(b, '\n'), nil
}

// containerSubstrateCatalogCommand is the hidden `ward container substrate-catalog`:
// the generator infra's seed-refresh trigger invokes with --out pointed at the seed.
func containerSubstrateCatalogCommand() *cli.Command {
	return &cli.Command{
		Name:   "substrate-catalog",
		Hidden: true,
		Usage:  "Generate the Forgejo-derived substrate repo catalog (ward#594); infra's seed-refresh trigger writes it into the substrate seed.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "out", Usage: "write the catalog JSON here (default: stdout)"},
			&cli.StringFlag{Name: "dest", Value: containerSubstrateDest, Usage: "the /substrate mount root recorded as each repo's mount_path"},
			&cli.StringFlag{Name: "tier", Usage: "restrict to one seed tier (image|cache); the public image seed passes image so private cache-tier repos never bake in"},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			return newRunner().generateSubstrateCatalog(ctx, c.String("out"), c.String("dest"), c.String("tier"))
		},
	}
}

// generateSubstrateCatalog reads the embedded manifest, optionally narrows it to one
// seed tier, builds the catalog off the host Forgejo client, and writes it to out.
func (r *Runner) generateSubstrateCatalog(ctx context.Context, out, dest, tier string) error {
	manifest, err := loadSubstrateManifest()
	if err != nil {
		return fmt.Errorf("substrate catalog: load manifest: %w", err)
	}
	manifest, err = filterSubstrateTier(manifest, tier)
	if err != nil {
		return err
	}
	cl, err := r.hostForgejoClient(ctx)
	if err != nil {
		return err
	}
	cat, err := buildSubstrateCatalog(ctx, cl, manifest, dest)
	if err != nil {
		return err
	}
	data, err := renderSubstrateCatalog(cat)
	if err != nil {
		return err
	}
	if out == "" {
		_, err = os.Stdout.Write(data)
		return err
	}
	if err := os.WriteFile(out, data, 0o644); err != nil { //nolint:gosec // a world-readable public repo index, no secrets
		return fmt.Errorf("substrate catalog: write %s: %w", out, err)
	}
	return nil
}
