package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/umbra/pkg/config"
	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"
)

var directorScopeSetupAttached = terminalAttached

func (r *Runner) resolveDirectorScope(ctx context.Context, c *cli.Command, label string) ([]string, error) {
	explicit := parseScopeRepos(c.String("repo"), "")
	orgs := dedupeSlugs(c.StringSlice("org"))
	if len(explicit) == 0 && len(orgs) == 0 {
		return r.resolveDirectorDefaultScope(ctx, label)
	}
	expanded, err := r.expandOrgScopes(ctx, label, orgs)
	if err != nil {
		return nil, err
	}
	repos := mergeScopeRepos(explicit, expanded)
	if len(repos) == 0 {
		return nil, fmt.Errorf("%s: --repo/--org scope resolved to no repos", label)
	}
	return repos, nil
}

func (r *Runner) resolveDirectorDefaultScope(ctx context.Context, label string) ([]string, error) {
	orgs, repos, err := loadDirectorDefaultScope()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	if len(orgs) == 0 && len(repos) == 0 && directorScopeSetupAttached() {
		orgs, repos, err = r.promptDirectorDefaultScope(label)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", label, err)
		}
	}
	if len(orgs) == 0 && len(repos) == 0 {
		return nil, fmt.Errorf("%s: no --repo/--org given and no director.default-scope in ~/.ward/config.yaml", label)
	}
	expanded, err := r.expandOrgScopes(ctx, label, orgs)
	if err != nil {
		return nil, err
	}
	resolved := mergeScopeRepos(repos, expanded)
	if len(resolved) == 0 {
		return nil, fmt.Errorf("%s: director.default-scope resolved to no repos", label)
	}
	return resolved, nil
}

type directorScopeKind string

const (
	directorScopeRepo   directorScopeKind = "repo"
	directorScopeOrg    directorScopeKind = "org"
	directorScopeCancel directorScopeKind = "cancel"
)

func (r *Runner) promptDirectorDefaultScope(label string) (orgs, repos []string, err error) {
	reader := bufio.NewReader(r.gateIn())
	w := r.gateErr()
	_, _ = fmt.Fprintf(w, "%s: no --repo/--org given and no director.default-scope in ~/.ward/config.yaml\n\n", label)
	_, _ = fmt.Fprintln(w, "Choose a default director scope to save:")
	_, _ = fmt.Fprintln(w, "  1) Repo scope (owner/name, comma-separated)")
	_, _ = fmt.Fprintln(w, "  2) Org scope (owner, comma-separated)")
	_, _ = fmt.Fprintln(w, "  3) Cancel")
	_, _ = fmt.Fprint(w, "Selection [1-3]: ")
	choice, err := readDirectorScopeChoice(reader)
	if err != nil {
		return nil, nil, err
	}
	if choice == directorScopeCancel {
		return nil, nil, errors.New("director scope setup canceled")
	}
	prompt := "Repo scope"
	if choice == directorScopeOrg {
		prompt = "Org scope"
	}
	_, _ = fmt.Fprintf(w, "%s: ", prompt)
	entries, err := readDirectorScopeEntries(reader, choice)
	if err != nil {
		return nil, nil, err
	}
	if err := writeDirectorDefaultScope(entries); err != nil {
		return nil, nil, err
	}
	path, _ := config.GlobalConfigPath()
	if path != "" {
		_, _ = fmt.Fprintf(w, "%s: saved director.default-scope to %s\n\n", label, path)
	}
	orgs, repos = partitionScopeEntries(entries)
	return orgs, repos, nil
}

func readDirectorScopeChoice(reader *bufio.Reader) (directorScopeKind, error) {
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read scope selection: %w", err)
	}
	switch strings.TrimSpace(line) {
	case "1", "repo", "repos", "r":
		return directorScopeRepo, nil
	case "2", "org", "orgs", "o":
		return directorScopeOrg, nil
	case "3", "cancel", "c", "q", "quit":
		return directorScopeCancel, nil
	default:
		return "", fmt.Errorf("invalid scope selection %q (want 1, 2, or 3)", strings.TrimSpace(line))
	}
}

func readDirectorScopeEntries(reader *bufio.Reader, kind directorScopeKind) ([]string, error) {
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read scope value: %w", err)
	}
	entries := dedupeSlugs(strings.Split(line, ","))
	if len(entries) == 0 {
		return nil, errors.New("empty director.default-scope")
	}
	for _, entry := range entries {
		hasSlash := strings.Contains(entry, "/")
		switch {
		case kind == directorScopeRepo && !hasSlash:
			return nil, fmt.Errorf("repo scope entry %q must be owner/name", entry)
		case kind == directorScopeOrg && hasSlash:
			return nil, fmt.Errorf("org scope entry %q must be an owner, not owner/name", entry)
		}
	}
	return entries, nil
}

func writeDirectorDefaultScope(entries []string) error {
	path, err := config.GlobalConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir %s: %w", filepath.Dir(path), err)
	}
	doc, mapping, err := loadOrCreateGlobalConfigDocument(path)
	if err != nil {
		return err
	}
	director := mappingChildMapping(mapping, "director")
	setMappingValue(director, "default-scope", scopeEntriesNode(entries))
	return writeYAMLDocument(path, doc)
}

func loadOrCreateGlobalConfigDocument(path string) (*yaml.Node, *yaml.Node, error) {
	body, err := os.ReadFile(path) // #nosec G304 -- ~/.ward/config.yaml is the intended operator-local input.
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, nil, fmt.Errorf("read %s: %w", path, err)
		}
		doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
		return doc, doc.Content[0], nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	mapping, hasMapping, err := documentMapping(&doc, path)
	if err != nil {
		return nil, nil, err
	}
	if !hasMapping {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
		mapping = doc.Content[0]
	}
	return &doc, mapping, nil
}

func mappingChildMapping(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value != key {
			continue
		}
		child := mapping.Content[i+1]
		if child.Kind != yaml.MappingNode {
			child.Kind = yaml.MappingNode
			child.Tag = "!!map"
			child.Value = ""
			child.Content = nil
		}
		return child
	}
	child := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	mapping.Content = append(mapping.Content, scalarNode(key), child)
	return child
}

func setMappingValue(mapping *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1] = value
			return
		}
	}
	mapping.Content = append(mapping.Content, scalarNode(key), value)
}

func scopeEntriesNode(entries []string) *yaml.Node {
	node := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, entry := range dedupeSlugs(entries) {
		node.Content = append(node.Content, scalarNode(entry))
	}
	return node
}

func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

type wardGlobalConfig struct {
	DefaultHarness string `yaml:"default-harness"`
	Director       struct {
		DefaultScope []string `yaml:"default-scope"`
		MaxParallel  int      `yaml:"max-parallel"`
		Limit        int      `yaml:"limit"`
	} `yaml:"director"`
	Agent struct {
		Image          string `yaml:"image"`
		ReleaseChannel string `yaml:"release-channel"`
		Redaction      struct {
			EnvNames []string `yaml:"env-names"`
			Patterns []string `yaml:"patterns"`
		} `yaml:"redaction"`
		Workflow struct {
			Default      string            `yaml:"default"`
			Repositories map[string]string `yaml:"repositories"`
		} `yaml:"workflow"`
		Review struct {
			Skip []string `yaml:"skip"`
		} `yaml:"review"`
	} `yaml:"agent"`
	Container struct {
		MemoryLimit string `yaml:"memory-limit"`
		StagingDir  string `yaml:"staging-dir"`
	} `yaml:"container"`
}

func loadWardGlobalConfig() (wardGlobalConfig, error) {
	path, err := config.GlobalConfigPath()
	if err != nil {
		return wardGlobalConfig{}, err
	}
	var cfg wardGlobalConfig
	if err := config.OverlayFile(&cfg, path); err != nil {
		return wardGlobalConfig{}, err
	}
	return cfg, nil
}

func loadDirectorDefaultScope() (orgs, repos []string, err error) {
	cfg, err := loadWardGlobalConfig()
	if err != nil {
		return nil, nil, err
	}
	orgs, repos = partitionScopeEntries(cfg.Director.DefaultScope)
	return orgs, repos, nil
}

func loadReviewSkips() ([]string, error) {
	cfg, err := loadWardGlobalConfig()
	if err != nil {
		return nil, err
	}
	return cfg.Agent.Review.Skip, nil
}

func partitionScopeEntries(entries []string) (orgs, repos []string) {
	for _, entry := range dedupeSlugs(entries) {
		if strings.Contains(entry, "/") {
			repos = append(repos, entry)
		} else {
			orgs = append(orgs, entry)
		}
	}
	return orgs, repos
}

func (r *Runner) expandOrgScopes(ctx context.Context, label string, orgs []string) ([]string, error) {
	if len(orgs) == 0 {
		return nil, nil
	}
	client := r.hostForgejoClient(ctx)
	var out []string
	for _, org := range orgs {
		repos, err := client.listOwnerRepos(ctx, org)
		if err != nil {
			return nil, fmt.Errorf("%s: cannot expand --org %q: %w", label, org, err)
		}
		slugs := orgReposToSlugs(org, repos)
		if len(slugs) == 0 {
			return nil, fmt.Errorf("%s: --org %q expanded to no repos (unknown org, or only archived/empty repos)", label, org)
		}
		out = append(out, slugs...)
	}
	return out, nil
}

func orgReposToSlugs(org string, repos []repoBrief) []string {
	var slugs []string
	for _, repo := range repos {
		if repo.Archived || repo.Empty {
			continue
		}
		slugs = append(slugs, org+"/"+repo.Name)
	}
	return slugs
}

func parseScopeRepos(raw, fallback string) []string {
	if strings.TrimSpace(raw) == "" {
		raw = fallback
	}
	return dedupeSlugs(strings.Split(raw, ","))
}

func mergeScopeRepos(lists ...[]string) []string {
	var all []string
	for _, list := range lists {
		all = append(all, list...)
	}
	return dedupeSlugs(all)
}

func dedupeSlugs(values []string) []string {
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		found := false
		for _, existing := range out {
			if existing == value {
				found = true
				break
			}
		}
		if !found {
			out = append(out, value)
		}
	}
	return out
}
