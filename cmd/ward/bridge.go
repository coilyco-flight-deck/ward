package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"github.com/urfave/cli/v3"
)

// bridge.go is ward's read-only bridge surface: infrastructure publishes the
// repo-authority facts, and ward renders them as operator verbs.

type bridgeBundle struct {
	Schema int               `json:"schema"`
	Repos  []bridgeRepoFacts `json:"repos"`
}

type bridgeRepoFacts struct {
	FullName          string             `json:"full_name"`
	AuthoritativeSide string             `json:"authoritative_side"`
	MirrorTargets     []string           `json:"mirror_targets"`
	TrackerAuthority  string             `json:"tracker_authority"`
	MirrorStatus      string             `json:"mirror_status"`
	LastSyncAt        string             `json:"last_sync_at,omitempty"`
	LastSyncAge       string             `json:"last_sync_age,omitempty"`
	DivergentRefs     []bridgeDivergence `json:"divergent_refs,omitempty"`
}

type bridgeDivergence struct {
	Ref      string `json:"ref"`
	Mirror   string `json:"mirror,omitempty"`
	Remote   string `json:"remote,omitempty"`
	Upstream string `json:"upstream,omitempty"`
}

type bridgeIndex struct {
	Schema int
	Repos  map[string]bridgeRepoFacts
}

func buildBridgeOps() (*cli.Command, error) {
	src, err := selectConfigSource()
	if err != nil {
		return nil, err
	}
	return buildBridgeOpsFrom(src)
}

func buildBridgeOpsFrom(src configSource) (*cli.Command, error) {
	r := leanRunner()
	r.configAuditVersion = src.auditVersion

	idx, err := loadBridgeIndex(src.fsys, src.bridgeFacts)
	if err != nil {
		return nil, err
	}

	bridge := &cli.Command{
		Name:  "bridge",
		Usage: "read-only GitHub <=> Forgejo bridge facts published by infrastructure",
		Description: `bridge reads the infrastructure-published coordination bundle and
renders read-only repo authority, mirror status, divergence, and issue mapping
views. Ward owns the command surface. Infrastructure owns the underlying state.`,
		Commands: []*cli.Command{
			bridgeAuthoritativeSideCommand(r, idx),
			bridgeMirrorStatusCommand(r, idx),
			bridgeDivergentRefsCommand(r, idx),
			bridgeStaleSyncsCommand(r, idx),
			bridgeMapIssueCommand(r, idx),
		},
	}
	return bridge, nil
}

func loadBridgeIndex(fsys fs.FS, path string) (bridgeIndex, error) {
	raw, err := fs.ReadFile(fsys, path)
	if err != nil {
		return bridgeIndex{}, fmt.Errorf("read bridge facts: %w", err)
	}
	var bundle bridgeBundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		return bridgeIndex{}, fmt.Errorf("parse bridge facts: %w", err)
	}
	idx := bridgeIndex{
		Schema: bundle.Schema,
		Repos:  make(map[string]bridgeRepoFacts, len(bundle.Repos)),
	}
	for _, repo := range bundle.Repos {
		if slug := strings.TrimSpace(repo.FullName); slug != "" {
			idx.Repos[slug] = repo
		}
	}
	return idx, nil
}

func bridgeAuthoritativeSideCommand(r *Runner, idx bridgeIndex) *cli.Command {
	return &cli.Command{
		Name:      "authoritative-side",
		Usage:     "print the authoritative side for one owner/repo",
		ArgsUsage: "<owner/repo>",
		Action: func(ctx context.Context, c *cli.Command) error {
			return r.WrapVerb(verb.Spec{
				Name:       "ops.bridge.authoritative-side",
				SkipPolicy: true,
				ArgsFunc:   func(cmd *cli.Command) (map[string]string, []string) { return nil, cmd.Args().Slice() },
				Action: func(_ context.Context, cmd *cli.Command) error {
					repo, err := bridgeRepoArg(cmd.Args().First(), cmd.Args().Slice(), 1)
					if err != nil {
						return err
					}
					facts, err := idx.repo(repo)
					if err != nil {
						return err
					}
					return writeBridgeJSON(map[string]any{
						"repo":               repo,
						"authoritative_side": facts.AuthoritativeSide,
						"tracker_authority":  facts.TrackerAuthority,
						"mirror_targets":     facts.MirrorTargets,
					})
				},
			}, r.Audit)(ctx, c)
		},
	}
}

func bridgeMirrorStatusCommand(r *Runner, idx bridgeIndex) *cli.Command {
	return &cli.Command{
		Name:      "mirror-status",
		Usage:     "print mirror status for one repo, or every repo when omitted",
		ArgsUsage: "[owner/repo]",
		Action: func(ctx context.Context, c *cli.Command) error {
			return r.WrapVerb(verb.Spec{
				Name:       "ops.bridge.mirror-status",
				SkipPolicy: true,
				ArgsFunc:   func(cmd *cli.Command) (map[string]string, []string) { return nil, cmd.Args().Slice() },
				Action: func(_ context.Context, cmd *cli.Command) error {
					if repo := strings.TrimSpace(cmd.Args().First()); repo != "" {
						facts, err := idx.repo(repo)
						if err != nil {
							return err
						}
						return writeBridgeJSON(facts)
					}
					return writeBridgeJSON(idx.sortedRepos())
				},
			}, r.Audit)(ctx, c)
		},
	}
}

func bridgeDivergentRefsCommand(r *Runner, idx bridgeIndex) *cli.Command {
	return &cli.Command{
		Name:      "divergent-refs",
		Usage:     "print divergent refs for one owner/repo",
		ArgsUsage: "<owner/repo>",
		Action: func(ctx context.Context, c *cli.Command) error {
			return r.WrapVerb(verb.Spec{
				Name:       "ops.bridge.divergent-refs",
				SkipPolicy: true,
				ArgsFunc:   func(cmd *cli.Command) (map[string]string, []string) { return nil, cmd.Args().Slice() },
				Action: func(_ context.Context, cmd *cli.Command) error {
					repo, err := bridgeRepoArg(cmd.Args().First(), cmd.Args().Slice(), 1)
					if err != nil {
						return err
					}
					facts, err := idx.repo(repo)
					if err != nil {
						return err
					}
					return writeBridgeJSON(map[string]any{
						"repo":           repo,
						"divergent_refs": facts.DivergentRefs,
					})
				},
			}, r.Audit)(ctx, c)
		},
	}
}

func bridgeStaleSyncsCommand(r *Runner, idx bridgeIndex) *cli.Command {
	return &cli.Command{
		Name:  "stale-syncs",
		Usage: "list repos whose mirror status is stale or divergent",
		Action: func(ctx context.Context, c *cli.Command) error {
			return r.WrapVerb(verb.Spec{
				Name:       "ops.bridge.stale-syncs",
				SkipPolicy: true,
				Action: func(_ context.Context, _ *cli.Command) error {
					return writeBridgeJSON(idx.staleRepos())
				},
			}, r.Audit)(ctx, c)
		},
	}
}

func bridgeMapIssueCommand(r *Runner, idx bridgeIndex) *cli.Command {
	return &cli.Command{
		Name:      "map-issue",
		Usage:     "map an issue ref to the repo authority and tracker authority",
		ArgsUsage: "<ref>",
		Action: func(ctx context.Context, c *cli.Command) error {
			return r.WrapVerb(verb.Spec{
				Name:       "ops.bridge.map-issue",
				SkipPolicy: true,
				ArgsFunc:   func(cmd *cli.Command) (map[string]string, []string) { return nil, cmd.Args().Slice() },
				Action: func(_ context.Context, cmd *cli.Command) error {
					ref, err := parseAgentIssueRef(strings.TrimSpace(cmd.Args().First()))
					if err != nil {
						return err
					}
					if ref.Owner == "" || ref.Repo == "" {
						return fmt.Errorf("ward ops bridge map-issue: need owner/repo#N or an issue URL, got %q", cmd.Args().First())
					}
					facts, err := idx.repo(ref.repoSlug())
					if err != nil {
						return err
					}
					return writeBridgeJSON(map[string]any{
						"issue": map[string]any{
							"ref":     ref.String(),
							"forge":   ref.Forge.String(),
							"tracker": ref.trackerOrDefault().String(),
							"number":  ref.Number,
						},
						"repo": map[string]any{
							"full_name":          facts.FullName,
							"authoritative_side": facts.AuthoritativeSide,
							"tracker_authority":  facts.TrackerAuthority,
							"mirror_status":      facts.MirrorStatus,
							"last_sync_age":      facts.LastSyncAge,
						},
					})
				},
			}, r.Audit)(ctx, c)
		},
	}
}

func (idx bridgeIndex) repo(slug string) (bridgeRepoFacts, error) {
	if facts, ok := idx.Repos[strings.TrimSpace(slug)]; ok {
		return facts, nil
	}
	return bridgeRepoFacts{}, fmt.Errorf("ward ops bridge: no bridge facts for %s", slug)
}

func (idx bridgeIndex) sortedRepos() []bridgeRepoFacts {
	repos := make([]bridgeRepoFacts, 0, len(idx.Repos))
	for _, repo := range idx.Repos {
		repos = append(repos, repo)
	}
	sort.Slice(repos, func(i, j int) bool { return repos[i].FullName < repos[j].FullName })
	return repos
}

func (idx bridgeIndex) staleRepos() []bridgeRepoFacts {
	var repos []bridgeRepoFacts
	for _, repo := range idx.sortedRepos() {
		status := strings.ToLower(strings.TrimSpace(repo.MirrorStatus))
		if status == "" || status == "in_sync" {
			continue
		}
		repos = append(repos, repo)
	}
	return repos
}

func bridgeRepoArg(arg string, argv []string, want int) (string, error) {
	if strings.TrimSpace(arg) == "" {
		return "", fmt.Errorf("ward ops bridge: need <owner/repo>, got %d arg(s)", len(argv))
	}
	if want > 0 && len(argv) < want {
		return "", fmt.Errorf("ward ops bridge: need <owner/repo>, got %d arg(s)", len(argv))
	}
	return strings.TrimSpace(arg), nil
}

func writeBridgeJSON(v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("ward ops bridge: marshal: %w", err)
	}
	fmt.Println(string(raw))
	return nil
}
