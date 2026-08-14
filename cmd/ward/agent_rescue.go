package main

// Durable host-owned Git rescue artifacts for engineer forge outages.

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/umbra/cli/verb"
	"github.com/urfave/cli/v3"
)

const rescueSchemaVersion = 1
const rescueSubdir = "rescues"

type rescueManifest struct {
	SchemaVersion  int                `json:"schema_version"`
	ArtifactID     string             `json:"artifact_id"`
	IssueRef       string             `json:"issue_ref"`
	RunID          string             `json:"run_id"`
	Workflow       string             `json:"workflow"`
	TerminalClass  string             `json:"terminal_failure_class"`
	CreatedAt      string             `json:"created_at"`
	ConsumedAt     string             `json:"consumed_at,omitempty"`
	ConsumedBranch string             `json:"consumed_branch,omitempty"`
	Repositories   []rescueRepository `json:"repositories"`
}

type rescueRepository struct {
	Repository          string   `json:"repository"`
	Branch              string   `json:"branch"`
	BaseCommit          string   `json:"base_commit"`
	RescuedHead         string   `json:"rescued_head"`
	Bundle              string   `json:"bundle"`
	BundleSHA256        string   `json:"bundle_sha256"`
	CommittedByBackstop bool     `json:"committed_by_residual_backstop"`
	Validation          []string `json:"validation"`
	Inventory           []string `json:"inventory"`
	Quarantined         bool     `json:"quarantined"`
	QuarantineReason    string   `json:"quarantine_reason,omitempty"`
}

func rescuesDir() string { return filepath.Join(homeDir(), ".ward", rescueSubdir) }
func rescueManifestPath(runID string) string {
	return filepath.Join(rescuesDir(), runID, "manifest.json")
}

// rescueContainerRun copies only .git into a temporary host directory.
func (r *Runner) rescueContainerRun(ctx context.Context, name string) error { //nolint:gocognit,gocyclo,cyclop // linear per-repository preservation sequence
	env := r.inspectContainerEnv(ctx, name)
	issue, _ := strconv.Atoi(strings.TrimSpace(env["WARD_TARGET_ISSUE"]))
	owner, repo := strings.TrimSpace(env["WARD_TARGET_OWNER"]), strings.TrimSpace(env["WARD_TARGET_NAME"])
	if issue <= 0 || owner == "" || repo == "" || env["WARD_AGENT_LAUNCHED"] != "1" {
		return nil
	}
	manifestPath := rescueManifestPath(name)
	if _, err := os.Stat(manifestPath); err == nil {
		return nil
	}
	primary := targetRepo{Owner: owner, Name: repo}
	all := append([]targetRepo{primary}, parseExtraReposEnv(env["WARD_EXTRA_REPOS"], owner, repo)...)
	manifest := rescueManifest{SchemaVersion: rescueSchemaVersion, ArtifactID: name, IssueRef: fmt.Sprintf("%s/%s#%d", owner, repo, issue), RunID: name, Workflow: string(workflowMode(env["WARD_WORKFLOW"]).orDefault()), TerminalClass: "forge landing unavailable", CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	for _, target := range all {
		work := grantedRepoWorkspaceDir(containerWorkspace, target)
		if target.canonicalSlug() == primary.canonicalSlug() {
			work = primaryWorkspaceDir(containerWorkspace, primary)
		}
		gitTar, err := r.dockerCapture(ctx, "cp", name+":"+work+"/.git", "-")
		if err != nil || len(gitTar) == 0 {
			continue
		}
		tmp, err := os.MkdirTemp("", "ward-rescue-git-*")
		if err != nil {
			return err
		}
		err = extractRescueGitDir(gitTar, tmp)
		if err == nil {
			entry, ok, cerr := r.createRescueRepository(ctx, tmp, target, name)
			if cerr != nil {
				err = cerr
			} else if ok {
				manifest.Repositories = append(manifest.Repositories, entry)
			}
		}
		_ = os.RemoveAll(tmp)
		if err != nil {
			return fmt.Errorf("rescue %s: %w", target.slug(), err)
		}
	}
	if len(manifest.Repositories) == 0 {
		return nil
	}
	sort.Slice(manifest.Repositories, func(i, j int) bool { return manifest.Repositories[i].Repository < manifest.Repositories[j].Repository })
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o700); err != nil {
		return err
	}
	if err := writeJSONAtomic(manifestPath, manifest); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "WARD-RESCUE: created artifact=%s manifest=%s repos=%d\n", name, manifestPath, len(manifest.Repositories))
	return nil
}

func extractRescueGitDir(data []byte, root string) error { //nolint:gocognit,gocyclo,cyclop // tar member validation is intentionally explicit
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(strings.TrimPrefix(h.Name, "./"))
		if name == "." || strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			return fmt.Errorf("unsafe git archive entry %q", h.Name)
		}
		out := filepath.Join(root, name)
		if !strings.HasPrefix(out, root+string(os.PathSeparator)) {
			return fmt.Errorf("unsafe git archive entry %q", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(out, 0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
				return err
			}
			f, e := os.OpenFile(out, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
			if e != nil {
				return e
			}
			_, e = io.Copy(f, tr) // #nosec G110 -- Git metadata stream only; no decompression and output is temporary
			cerr := f.Close()
			if e != nil {
				return e
			}
			if cerr != nil {
				return cerr
			}
		}
	}
}

func (r *Runner) createRescueRepository(ctx context.Context, gitRoot string, repo targetRepo, runID string) (rescueRepository, bool, error) {
	head := r.captureRev(ctx, gitRoot, "HEAD")
	base := r.captureRev(ctx, gitRoot, "origin/main")
	if head == "" || base == "" {
		return rescueRepository{}, false, fmt.Errorf("resolve rescue refs: head=%q base=%q", head, base)
	}
	if headOnOriginMain(ctx, r, gitRoot) {
		return rescueRepository{}, false, nil
	}
	dir := filepath.Join(rescuesDir(), runID)
	bundleName := strings.ReplaceAll(repo.slug(), "/", "__") + ".bundle"
	bundle := filepath.Join(dir, bundleName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return rescueRepository{}, false, err
	}
	if err := r.Runner.Exec(ctx, "git", "-C", gitRoot, "bundle", "create", bundle, base+"..HEAD"); err != nil {
		return rescueRepository{}, false, fmt.Errorf("create bundle: %w", err)
	}
	verify, err := r.Runner.Capture(ctx, "git", "-C", gitRoot, "bundle", "verify", bundle)
	if err != nil {
		return rescueRepository{}, false, err
	}
	data, err := os.ReadFile(bundle)
	if err != nil {
		return rescueRepository{}, false, err
	}
	sum := sha256.Sum256(data)
	inventory := rescueInventory(ctx, r, gitRoot, base+".."+head)
	q, why := rescueQuarantine(inventory)
	return rescueRepository{Repository: repo.slug(), Branch: strings.TrimSpace(stringMust(r.Runner.Capture(ctx, "git", "-C", gitRoot, "branch", "--show-current"))), BaseCommit: base, RescuedHead: head, Bundle: bundleName, BundleSHA256: hex.EncodeToString(sum[:]), CommittedByBackstop: strings.Contains(strings.ToLower(stringMust(r.Runner.Capture(ctx, "git", "-C", gitRoot, "log", "-1", "--format=%s"))), "ward-container: residual"), Validation: []string{"git bundle verify: " + firstLine(string(verify)), "sha256 verified"}, Inventory: inventory, Quarantined: q, QuarantineReason: why}, true, nil
}

func stringMust(b []byte, _ error) string { return strings.TrimSpace(string(b)) }
func rescueInventory(ctx context.Context, r *Runner, work, span string) []string {
	out, err := r.Runner.Capture(ctx, "git", "-C", work, "diff", "--name-status", "--no-renames", span)
	if err != nil {
		return nil
	}
	var inv []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(l) != "" {
			inv = append(inv, l)
		}
	}
	return inv
}
func rescueQuarantine(inv []string) (bool, string) {
	deletes := 0
	for _, v := range inv {
		fields := strings.Fields(v)
		if len(fields) >= 2 && strings.HasPrefix(fields[0], "D") {
			deletes++
		}
		if len(fields) >= 2 && (strings.HasSuffix(fields[1], ".exe") || strings.HasSuffix(fields[1], ".bin")) {
			return true, "generated binary in residual inventory"
		}
	}
	if deletes >= 20 {
		return true, fmt.Sprintf("broad deletion set (%d paths)", deletes)
	}
	return false, ""
}

func agentRecoverCommand() *cli.Command {
	return &cli.Command{Name: "recover", Usage: "Plan or apply recovery of a durable local rescue bundle for an issue.", ArgsUsage: "<owner/repo#N>", Flags: []cli.Flag{&cli.BoolFlag{Name: "apply", Usage: "create a continuation branch from current origin/main and merge the rescued bundle"}, &cli.StringFlag{Name: "work", Usage: "fresh clone to prepare (required with --apply)"}, &cli.BoolFlag{Name: "include-quarantined", Usage: "explicitly acknowledge and apply quarantined artifacts"}, &cli.BoolFlag{Name: "override-reservation", Usage: "explicitly override a live issue reservation"}}, Action: func(ctx context.Context, c *cli.Command) error {
		r := newRunner()
		return r.WrapVerb(verb.Spec{Name: "agent.recover", SkipPolicy: true, Action: func(ctx context.Context, c *cli.Command) error { return r.runAgentRecover(ctx, c) }}, r.Audit)(ctx, c)
	}, Commands: []*cli.Command{agentRescuePruneCommand()}}
}

func (r *Runner) runAgentRecover(ctx context.Context, c *cli.Command) error { //nolint:gocognit,gocyclo,cyclop // recovery safety gates intentionally remain adjacent
	ref, err := r.resolveAgentIssueRef(ctx, c.Args().First())
	if err != nil {
		return fmt.Errorf("ward agent recover: %w", err)
	}
	issue, err := r.fetchIssue(ctx, ref)
	if err != nil {
		return fmt.Errorf("ward agent recover: read current issue ownership: %w", err)
	}
	comments, err := r.fetchIssueComments(ctx, ref)
	if err != nil {
		return fmt.Errorf("ward agent recover: read current reservation: %w", err)
	}
	if !c.Bool("override-reservation") {
		if who, held := freshReservationComment(comments, time.Now().UTC(), agentReservationTTL()); held {
			return newReservationConflict("ward agent recover: issue %s is already reserved remotely (%s); wait for it to finish or pass --override-reservation to override", ref, who)
		}
	}
	m, path, err := newestRescueManifest(ref.String())
	if err != nil {
		return fmt.Errorf("ward agent recover: %w", err)
	}
	_, _ = fmt.Fprintf(c.Root().Writer, "Recovery plan: artifact=%s issue=%s remote-state=%s workflow=%s manifest=%s\n", m.ArtifactID, m.IssueRef, issue.State, m.Workflow, path)
	for _, rep := range m.Repositories {
		_, _ = fmt.Fprintf(c.Root().Writer, "- %s: %s (base %s, head %s)%s\n", rep.Repository, rep.Bundle, shortSha(rep.BaseCommit), shortSha(rep.RescuedHead), map[bool]string{true: " QUARANTINED: " + rep.QuarantineReason, false: ""}[rep.Quarantined])
	}
	if !c.Bool("apply") {
		_, _ = fmt.Fprintln(c.Root().Writer, "Read-only plan only. Re-run with --apply --work <fresh-clean-clone> after review.")
		return nil
	}
	work := strings.TrimSpace(c.String("work"))
	if work == "" || !isGitWorkTree(ctx, r, work) {
		return fmt.Errorf("ward agent recover: --apply requires a fresh git clone via --work")
	}
	if status, _ := r.Runner.Capture(ctx, "git", "-C", work, "status", "--porcelain"); strings.TrimSpace(string(status)) != "" {
		return fmt.Errorf("ward agent recover: --work must be clean")
	}
	for _, rep := range m.Repositories {
		if rep.Repository != ref.repoSlug() {
			continue
		}
		if rep.Quarantined && !c.Bool("include-quarantined") {
			return fmt.Errorf("ward agent recover: %s is quarantined (%s); review it and pass --include-quarantined explicitly", rep.Repository, rep.QuarantineReason)
		}
		bundle := filepath.Join(filepath.Dir(path), rep.Bundle)
		branch := fmt.Sprintf("issue-%d-recovery", ref.Number)
		if err := r.Runner.Exec(ctx, "git", "-C", work, "fetch", "origin"); err != nil {
			return err
		}
		if err := r.Runner.Exec(ctx, "git", "-C", work, "checkout", "-B", branch, "origin/main"); err != nil {
			return err
		}
		if err := r.Runner.Exec(ctx, "git", "-C", work, "fetch", bundle, "HEAD"); err != nil {
			return err
		}
		if err := r.Runner.Exec(ctx, "git", "-C", work, "merge", "--ff-only", "FETCH_HEAD"); err != nil {
			return err
		}
		m.ConsumedAt = time.Now().UTC().Format(time.RFC3339)
		m.ConsumedBranch = branch
		if err := writeJSONAtomic(path, m); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(c.Root().Writer, "Prepared continuation branch %s from rescue %s. Continue through the normal PR workflow.\n", branch, m.ArtifactID)
		return nil
	}
	return fmt.Errorf("ward agent recover: artifact has no primary repository entry for %s", ref.repoSlug())
}

func newestRescueManifest(issue string) (rescueManifest, string, error) {
	ents, err := os.ReadDir(rescuesDir())
	if err != nil {
		return rescueManifest{}, "", err
	}
	var best rescueManifest
	var path string
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(rescuesDir(), e.Name(), "manifest.json")
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var m rescueManifest
		if json.Unmarshal(data, &m) != nil || m.IssueRef != issue {
			continue
		}
		if path == "" || m.CreatedAt > best.CreatedAt {
			best, path = m, p
		}
	}
	if path == "" {
		return rescueManifest{}, "", fmt.Errorf("no rescue artifact for %s", issue)
	}
	return best, path, nil
}

func agentRescuePruneCommand() *cli.Command {
	return &cli.Command{Name: "prune", Usage: "Prune consumed rescue artifacts older than --older-than (ordinary reaping never deletes rescues).", Flags: []cli.Flag{&cli.DurationFlag{Name: "older-than", Value: 30 * 24 * time.Hour}, &cli.BoolFlag{Name: "confirm"}}, Action: func(_ context.Context, c *cli.Command) error {
		if !c.Bool("confirm") {
			return fmt.Errorf("ward agent recover prune: pass --confirm after reviewing rescue artifacts")
		}
		ents, _ := os.ReadDir(rescuesDir())
		cutoff := time.Now().Add(-c.Duration("older-than"))
		for _, e := range ents {
			p := filepath.Join(rescuesDir(), e.Name(), "manifest.json")
			data, er := os.ReadFile(p)
			var m rescueManifest
			if er != nil || json.Unmarshal(data, &m) != nil || m.ConsumedAt == "" {
				continue
			}
			t, _ := time.Parse(time.RFC3339, m.ConsumedAt)
			if t.Before(cutoff) {
				_ = os.RemoveAll(filepath.Dir(p))
				_, _ = fmt.Fprintf(c.Root().Writer, "pruned %s\n", m.ArtifactID)
			}
		}
		return nil
	}}
}
