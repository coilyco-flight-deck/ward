package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func engineerCountDockerStub(t *testing.T, count int) string {
	t.Helper()
	stub := filepath.Join(t.TempDir(), "docker")
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("if [ \"$1\" = ps ] && [ \"$2\" = --format ] && [ \"$3\" = '{{.Names}}' ] && [ \"$4\" = --filter ] && [ \"$5\" = label=ward=true ] && [ \"$6\" = --filter ] && [ \"$7\" = label=ward.role=engineer ]; then\n")
	for i := 0; i < count; i++ {
		fmt.Fprintf(&b, "  printf '%%s\\n' %s\n", shellQuote(fmt.Sprintf("engineer-%02d", i+1)))
	}
	b.WriteString("  exit 0\n")
	b.WriteString("fi\n")
	b.WriteString("printf '%s\\n' \"unexpected docker args: $*\" >&2\n")
	b.WriteString("exit 1\n")
	if err := os.WriteFile(stub, []byte(b.String()), 0o755); err != nil { //nolint:gosec
		t.Fatalf("write docker stub: %v", err)
	}
	t.Setenv("PATH", filepath.Dir(stub)+string(os.PathListSeparator)+os.Getenv("PATH"))
	return stub
}

func engineerRepoAndGlobalCountDockerStub(t *testing.T, repo string, repoCount, globalCount int) string {
	t.Helper()
	stub := filepath.Join(t.TempDir(), "docker")
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("if [ \"$1\" = ps ] && [ \"$2\" = --format ] && [ \"$3\" = '{{.Names}}' ] && [ \"$4\" = --filter ] && [ \"$5\" = label=ward=true ] && [ \"$6\" = --filter ] && [ \"$7\" = label=ward.role=engineer ] && [ \"$8\" = --filter ] && [ \"$9\" = label=ward.repo=" + repo + " ]; then\n")
	for i := 0; i < repoCount; i++ {
		fmt.Fprintf(&b, "  printf '%%s\\n' %s\n", shellQuote(fmt.Sprintf("engineer-%02d", i+1)))
	}
	b.WriteString("  exit 0\n")
	b.WriteString("fi\n")
	b.WriteString("if [ \"$1\" = ps ] && [ \"$2\" = --format ] && [ \"$3\" = '{{.Names}}' ] && [ \"$4\" = --filter ] && [ \"$5\" = label=ward=true ] && [ \"$6\" = --filter ] && [ \"$7\" = label=ward.role=engineer ]; then\n")
	for i := 0; i < globalCount; i++ {
		fmt.Fprintf(&b, "  printf '%%s\\n' %s\n", shellQuote(fmt.Sprintf("engineer-%02d", i+1)))
	}
	b.WriteString("  exit 0\n")
	b.WriteString("fi\n")
	b.WriteString("printf '%s\\n' \"unexpected docker args: $*\" >&2\n")
	b.WriteString("exit 1\n")
	if err := os.WriteFile(stub, []byte(b.String()), 0o755); err != nil { //nolint:gosec
		t.Fatalf("write docker stub: %v", err)
	}
	t.Setenv("PATH", filepath.Dir(stub)+string(os.PathListSeparator)+os.Getenv("PATH"))
	return stub
}

func stubCommandInPath(t *testing.T, name string) string {
	t.Helper()
	stub := filepath.Join(t.TempDir(), name)
	script := "#!/bin/sh\nprintf '%s\\n' \"unexpected " + name + " invocation: $*\" >&2\nexit 1\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil { //nolint:gosec
		t.Fatalf("write %s stub: %v", name, err)
	}
	t.Setenv("PATH", filepath.Dir(stub)+string(os.PathListSeparator)+os.Getenv("PATH"))
	return stub
}

type issueThreadAuthorityRow struct {
	Number   int
	Title    string
	Body     string
	Labels   []string
	Comments []issueComment
}

func issueThreadAuthorityServer(t *testing.T, rows []issueThreadAuthorityRow) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/issues", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("type") {
		case "issues":
			issues := make([]map[string]any, 0, len(rows))
			for _, row := range rows {
				labels := make([]map[string]any, 0, len(row.Labels))
				for _, label := range row.Labels {
					labels = append(labels, map[string]any{"name": label})
				}
				issues = append(issues, map[string]any{
					"number":       row.Number,
					"title":        row.Title,
					"body":         row.Body,
					"html_url":     fmt.Sprintf("https://forgejo.example/coilyco-flight-deck/ward/issues/%d", row.Number),
					"labels":       labels,
					"state":        "open",
					"pull_request": nil,
				})
			}
			_ = json.NewEncoder(w).Encode(issues)
		case "pulls":
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		default:
			t.Fatalf("unexpected issue feed query: %s", r.URL.RawQuery)
		}
	})
	for _, row := range rows {
		row := row
		mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/issues/"+fmt.Sprint(row.Number)+"/comments", func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(row.Comments)
		})
	}
	return httptest.NewServer(mux)
}

func TestEngineerContainerLimitBelowAndAtLimit(t *testing.T) {
	limit := engineerContainerLimitDefault()
	t.Run("below limit", func(t *testing.T) {
		r, _, _ := bufRunner(engineerCountDockerStub(t, limit-1))
		if err := r.enforceEngineerContainerLimit(context.Background(), "ward agent engineer", false); err != nil {
			t.Fatalf("enforceEngineerContainerLimit below limit: %v", err)
		}
	})

	t.Run("at limit", func(t *testing.T) {
		r, _, _ := bufRunner(engineerCountDockerStub(t, limit))
		err := r.enforceEngineerContainerLimit(context.Background(), "ward agent engineer", false)
		if err == nil {
			t.Fatal("enforceEngineerContainerLimit at limit: want error, got nil")
		}
		var capErr *engineerCapacityError
		if !errors.As(err, &capErr) {
			t.Fatalf("enforceEngineerContainerLimit at limit returned %T, want *engineerCapacityError", err)
		}
		if !isEngineerCapacityError(err) {
			t.Fatal("enforceEngineerContainerLimit at limit should classify as engineer capacity backpressure")
		}
		for _, want := range []string{
			"global engineer limit is reached",
			"running",
			fmt.Sprintf("limit %d", limit),
			"ward agent reap",
		} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("enforceEngineerContainerLimit at limit missing %q: %v", want, err)
			}
		}
	})
}

// TestEngineerContainerLimitOverrideCapacity covers ward#1045: --override-capacity
// grants one loud launch past the OOM ceiling and never stacks past limit+1.
func TestEngineerContainerLimitOverrideCapacity(t *testing.T) {
	limit := engineerContainerLimitDefault()

	t.Run("at limit with override launches loudly", func(t *testing.T) {
		r, _, _ := bufRunner(engineerCountDockerStub(t, limit))
		var err error
		stderr := captureTestStderr(t, func() {
			err = r.enforceEngineerContainerLimit(context.Background(), "ward agent engineer", true)
		})
		if err != nil {
			t.Fatalf("enforceEngineerContainerLimit at limit with override: %v", err)
		}
		for _, want := range []string{
			"WARNING",
			fmt.Sprintf("(%d/%d)", limit+1, limit),
			"OOM",
			"--override-capacity",
		} {
			if !strings.Contains(stderr, want) {
				t.Errorf("override warning missing %q: %q", want, stderr)
			}
		}
	})

	t.Run("already over the ceiling refuses even with override", func(t *testing.T) {
		r, _, _ := bufRunner(engineerCountDockerStub(t, limit+1))
		var err error
		stderr := captureTestStderr(t, func() {
			err = r.enforceEngineerContainerLimit(context.Background(), "ward agent engineer", true)
		})
		if err == nil {
			t.Fatal("enforceEngineerContainerLimit over limit with override: want error, got nil")
		}
		if !isEngineerCapacityError(err) {
			t.Fatalf("over-limit override refusal should classify as capacity backpressure: %T %v", err, err)
		}
		if !strings.Contains(stderr, "exactly one launch past the ceiling") {
			t.Errorf("over-limit override refusal should explain the one-launch bound: %q", stderr)
		}
	})

	t.Run("below limit with override stays quiet", func(t *testing.T) {
		r, _, _ := bufRunner(engineerCountDockerStub(t, limit-1))
		var err error
		stderr := captureTestStderr(t, func() {
			err = r.enforceEngineerContainerLimit(context.Background(), "ward agent engineer", true)
		})
		if err != nil {
			t.Fatalf("enforceEngineerContainerLimit below limit with override: %v", err)
		}
		if strings.Contains(stderr, "WARNING") {
			t.Errorf("below-limit override should not warn: %q", stderr)
		}
	})
}

func TestEngineerContainerLimitFromBundleOverride(t *testing.T) {
	dir := t.TempDir()
	defaultsBody := `defaults {
    agent-reservation-ttl "3h"
    agent-reservation-recheck-max "15s"
    agent-reap-idle "1h"
    agent-reap-max-cpu "5.0"
    engineer-container-limit "15"
    engineer-repo-working-limit "3"
    engineer-open-pr-branch-limit "6"
    director-max-parallel "10"
    director-limit "50"
    director-poll-interval "30s"
    reviewer-timeout "8m"
    config-bundle-ttl "600s"
    container-assets-ttl "1h"
    container-read-only-extra-repo-ttl "24h"
    container-reap-keep "10"
}
workflow default=merge-remote-main {
    repo "coilyco-flight-deck/ward" workflow=pull-request-and-merge
}
`
	reposBody := `repos {
    repo-authority default=forgejo {
        trusted-owner coilysiren
        repo "coilyco-flight-deck/*" forge=forgejo
    }
}`
	if err := os.WriteFile(filepath.Join(dir, bundleFixtureDefaultsPath), []byte(defaultsBody), 0o644); err != nil {
		t.Fatalf("write defaults bundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, bundleFixtureReposPath), []byte(reposBody), 0o644); err != nil {
		t.Fatalf("write repos bundle: %v", err)
	}
	t.Setenv(wardConfigRefEnv, "file://"+dir)
	if got := engineerContainerLimitDefault(); got != 15 {
		t.Fatalf("engineerContainerLimitDefault() = %d, want 15", got)
	}

	r, _, _ := bufRunner(engineerCountDockerStub(t, 14))
	if err := r.enforceEngineerContainerLimit(context.Background(), "ward agent engineer", false); err != nil {
		t.Fatalf("enforceEngineerContainerLimit below overridden limit: %v", err)
	}

	r, _, _ = bufRunner(engineerCountDockerStub(t, 15))
	err := r.enforceEngineerContainerLimit(context.Background(), "ward agent engineer", false)
	if err == nil {
		t.Fatal("enforceEngineerContainerLimit at overridden limit: want error, got nil")
	}
	if !strings.Contains(err.Error(), "limit 15") {
		t.Fatalf("enforceEngineerContainerLimit overridden limit error = %v", err)
	}
}

func TestLaunchRepoEngineerBackpressureIgnoresStaleDockerWhenIssueThreadIsClear(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	oldBase := forgejoBaseURL
	defer func() { forgejoBaseURL = oldBase }()
	srv := issueThreadAuthorityServer(t, nil)
	defer srv.Close()
	forgejoBaseURL = srv.URL

	r, _, _ := bufRunner(engineerRepoAndGlobalCountDockerStub(t, "coilyco-flight-deck/ward", 0, engineerContainerLimitDefault()))
	if err := r.launchRepoEngineerBackpressureCheck(context.Background(), "lbl", agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward"}, false); err != nil {
		t.Fatalf("launchRepoEngineerBackpressureCheck with clear issue thread and stale docker: %v", err)
	}
}

func repoBackpressureDockerStub(t *testing.T, repo string, runningNames []string) string {
	t.Helper()
	stub := filepath.Join(t.TempDir(), "docker")
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("if [ \"$1\" = ps ] && [ \"$2\" = --format ] && [ \"$3\" = '{{.Names}}' ] && [ \"$4\" = --filter ] && [ \"$5\" = label=ward=true ] && [ \"$6\" = --filter ] && [ \"$7\" = label=ward.role=engineer ]; then\n")
	for _, name := range runningNames {
		fmt.Fprintf(&b, "  printf '%%s\\n' %s\n", shellQuote(name))
	}
	b.WriteString("  exit 0\n")
	b.WriteString("fi\n")
	b.WriteString("if [ \"$1\" = inspect ]; then\n")
	b.WriteString("  case \"$2\" in\n")
	for _, name := range runningNames {
		issue := strings.TrimPrefix(name, "engineer-codex-ward-")
		payload := []map[string]any{{
			"Name": "/" + name,
			"Config": map[string]any{
				"Labels": map[string]string{
					containerLabel: "true",
					labelRole:      roleEngineer,
					labelDriver:    string(modeCodex),
					labelRepo:      repo,
					"ward.issue":   issue,
				},
				"Env": []string{
					"WARD_TARGET_OWNER=coilyco-flight-deck",
					"WARD_TARGET_NAME=ward",
					"WARD_TARGET_REPO=" + repo,
					"WARD_TARGET_ISSUE=" + issue,
					"WARD_BRANCH=issue-" + issue,
					"WARD_MODE=codex",
				},
			},
			"State": map[string]any{
				"Status":    "running",
				"StartedAt": "2026-07-14T00:00:00Z",
			},
		}}
		jsonBytes, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal docker inspect payload: %v", err)
		}
		fmt.Fprintf(&b, "    %s) printf '%%s\\n' %s; exit 0 ;;\n", shellQuote(name), shellQuote(string(jsonBytes)))
	}
	b.WriteString("  esac\n")
	b.WriteString("fi\n")
	b.WriteString("if [ \"$1\" = ps ]; then\n")
	b.WriteString("  case \"$*\" in\n")
	b.WriteString("    *name=*) exit 0 ;;\n")
	b.WriteString("  esac\n")
	b.WriteString("fi\n")
	b.WriteString("printf '%s\\n' \"unexpected docker args: $*\" >&2\n")
	b.WriteString("exit 1\n")
	if err := os.WriteFile(stub, []byte(b.String()), 0o755); err != nil { //nolint:gosec
		t.Fatalf("write docker stub: %v", err)
	}
	return stub
}

func staleRepoReservation(t *testing.T, ref agentIssueRef, container string, at time.Time) {
	t.Helper()
	path, err := agentReservationPath(ref)
	if err != nil {
		t.Fatalf("agentReservationPath: %v", err)
	}
	if err := writeAgentReservation(path, agentReservation{
		Owner:     ref.Owner,
		Repo:      ref.Repo,
		Number:    ref.Number,
		Mode:      string(modeCodex),
		Container: container,
		Branch:    "issue-" + fmt.Sprint(ref.Number),
		Host:      "test-host",
		PID:       1234,
		At:        at,
	}); err != nil {
		t.Fatalf("writeAgentReservation: %v", err)
	}
}

func reservationIssueComment(body string, at time.Time) issueComment {
	comment := issueComment{Body: body, CreatedAt: at}
	comment.User.Login = "coilyco-ops"
	return comment
}

func TestLaunchRepoEngineerBackpressureOverrideReservationRecoversStalePrelaunch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	oldBase := forgejoBaseURL
	defer func() { forgejoBaseURL = oldBase }()
	now := time.Now().UTC()
	rows := []issueThreadAuthorityRow{
		{
			Number:   101,
			Title:    "active 1",
			Body:     "body",
			Labels:   []string{"P0", "headless"},
			Comments: []issueComment{reservationIssueComment(reservationCommentBody(modeCodex, "engineer-codex-ward-101", "host-1", now.Add(-time.Minute), "", nil), now.Add(-time.Minute))},
		},
		{
			Number:   102,
			Title:    "active 2",
			Body:     "body",
			Labels:   []string{"P0", "headless"},
			Comments: []issueComment{reservationIssueComment(reservationCommentBody(modeCodex, "engineer-codex-ward-102", "host-2", now.Add(-time.Minute), "", nil), now.Add(-time.Minute))},
		},
		{
			Number:   103,
			Title:    "active 3",
			Body:     "body",
			Labels:   []string{"P0", "headless"},
			Comments: []issueComment{reservationIssueComment(reservationCommentBody(modeCodex, "engineer-codex-ward-103", "host-3", now.Add(-time.Minute), "", nil), now.Add(-time.Minute))},
		},
	}
	srv := issueThreadAuthorityServer(t, rows)
	defer srv.Close()
	forgejoBaseURL = srv.URL

	staleRepoReservation(t, agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 201}, "engineer-codex-ward-201", now.Add(-3*agentLaunchConfirmationTTL()))
	staleRepoReservation(t, agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 202}, "engineer-codex-ward-202", now.Add(-3*agentLaunchConfirmationTTL()))
	dockerStub := repoBackpressureDockerStub(t, "coilyco-flight-deck/ward", []string{"engineer-codex-ward-101"})

	r, _, _ := bufRunner(dockerStub)
	if err := r.launchRepoEngineerBackpressureCheck(context.Background(), "lbl", agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward"}, false); err == nil {
		t.Fatal("launchRepoEngineerBackpressureCheck without override should fail when stale prelaunch holds fill the repo limit")
	} else {
		for _, want := range []string{"running", "stale prelaunch", "override-reservation"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("repo backpressure error missing %q: %v", want, err)
			}
		}
	}
	if err := r.launchRepoEngineerBackpressureCheck(context.Background(), "lbl", agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward"}, true); err != nil {
		t.Fatalf("launchRepoEngineerBackpressureCheck with override-reservation should recover stale prelaunch holds: %v", err)
	}
}

func TestLaunchRepoEngineerBackpressureOverrideReservationStillRespectsRealRunningCapacity(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	oldBase := forgejoBaseURL
	defer func() { forgejoBaseURL = oldBase }()
	now := time.Now().UTC()
	rows := []issueThreadAuthorityRow{
		{
			Number:   301,
			Title:    "active 1",
			Body:     "body",
			Labels:   []string{"P0", "headless"},
			Comments: []issueComment{reservationIssueComment(reservationCommentBody(modeCodex, "engineer-codex-ward-301", "host-1", now.Add(-time.Minute), "", nil), now.Add(-time.Minute))},
		},
		{
			Number:   302,
			Title:    "active 2",
			Body:     "body",
			Labels:   []string{"P0", "headless"},
			Comments: []issueComment{reservationIssueComment(reservationCommentBody(modeCodex, "engineer-codex-ward-302", "host-2", now.Add(-time.Minute), "", nil), now.Add(-time.Minute))},
		},
		{
			Number:   303,
			Title:    "active 3",
			Body:     "body",
			Labels:   []string{"P0", "headless"},
			Comments: []issueComment{reservationIssueComment(reservationCommentBody(modeCodex, "engineer-codex-ward-303", "host-3", now.Add(-time.Minute), "", nil), now.Add(-time.Minute))},
		},
		{
			Number: 304,
			Title:  "stale extra",
			Body:   "body",
			Labels: []string{"P0", "headless"},
		},
	}
	srv := issueThreadAuthorityServer(t, rows)
	defer srv.Close()
	forgejoBaseURL = srv.URL

	staleRepoReservation(t, agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 304}, "engineer-codex-ward-304", now.Add(-3*agentLaunchConfirmationTTL()))
	dockerStub := repoBackpressureDockerStub(t, "coilyco-flight-deck/ward", []string{"engineer-codex-ward-301", "engineer-codex-ward-302", "engineer-codex-ward-303"})

	r, _, _ := bufRunner(dockerStub)
	err := r.launchRepoEngineerBackpressureCheck(context.Background(), "lbl", agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward"}, true)
	if err == nil {
		t.Fatal("launchRepoEngineerBackpressureCheck with override-reservation should still refuse when real running capacity is already full")
	}
	for _, want := range []string{"running", "stale prelaunch", "--override-capacity"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("real-capacity error missing %q: %v", want, err)
		}
	}
}

func TestActiveEngineerLaunchCountIgnoresLaunchIntents(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	oldBase := forgejoBaseURL
	defer func() { forgejoBaseURL = oldBase }()
	now := time.Now().UTC()
	rows := []issueThreadAuthorityRow{
		{
			Number: 883,
			Title:  "clear issue",
			Body:   "body",
			Labels: []string{"P0", "headless"},
		},
		{
			Number:   884,
			Title:    "reserved issue",
			Body:     "body",
			Labels:   []string{"P0", "headless"},
			Comments: []issueComment{{Body: reservationCommentBody(modeClaude, "engineer-claude-ward-884", "box", now.Add(-time.Minute), "", nil), CreatedAt: now.Add(-time.Minute)}},
		},
	}
	srv := issueThreadAuthorityServer(t, rows)
	defer srv.Close()
	forgejoBaseURL = srv.URL

	r, _, _ := bufRunner(engineerRepoAndGlobalCountDockerStub(t, "coilyco-flight-deck/ward", engineerRepoWorkingLimitDefault()-1, 0))
	path, err := agentReservationPath(agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 885})
	if err != nil {
		t.Fatalf("agentReservationPath: %v", err)
	}
	reservationPath, err := agentReservationPath(agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 883})
	if err != nil {
		t.Fatalf("agentReservationPath: %v", err)
	}
	if err := writeAgentReservation(reservationPath, agentReservation{
		Owner:     "coilyco-flight-deck",
		Repo:      "ward",
		Number:    883,
		Mode:      "codex",
		Container: "reserved-engineer-03",
		Branch:    "main",
		Host:      "test-host",
		PID:       1234,
		At:        time.Now().UTC(),
	}); err != nil {
		t.Fatalf("writeAgentReservation: %v", err)
	}

	count, err := r.activeEngineerLaunchCountForRepo(context.Background(), agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward"})
	if err != nil {
		t.Fatalf("activeEngineerLaunchCountForRepo: %v", err)
	}
	if count != 1 {
		t.Fatalf("activeEngineerLaunchCountForRepo = %d, want 1 from issue state", count)
	}
	if err := r.launchRepoEngineerBackpressureCheck(context.Background(), "lbl", agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward"}, false); err != nil {
		t.Fatalf("launchRepoEngineerBackpressureCheck should ignore launch intents, got %v", err)
	}
	if _, ok, _ := readAgentReservation(path); ok {
		t.Fatal("launchRepoEngineerBackpressureCheck should not touch the target issue reservation")
	}
}

func TestActiveEngineerLaunchCountUsesFreshLaunchRows(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Now().UTC()
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 885}
	path, err := agentReservationPath(ref)
	if err != nil {
		t.Fatalf("agentReservationPath: %v", err)
	}
	if err := writeAgentReservation(path, agentReservation{
		Owner:     ref.Owner,
		Repo:      ref.Repo,
		Number:    ref.Number,
		Mode:      string(modeCodex),
		Container: "engineer-codex-ward-885",
		Branch:    "issue-885",
		Host:      "test-host",
		PID:       1234,
		At:        now,
	}); err != nil {
		t.Fatalf("writeAgentReservation: %v", err)
	}

	r, _, _ := bufRunner(repoBackpressureDockerStub(t, ref.repoSlug(), nil))
	count, err := r.activeEngineerLaunchCountForRepo(context.Background(), ref)
	if err != nil {
		t.Fatalf("activeEngineerLaunchCountForRepo fresh: %v", err)
	}
	if count != 1 {
		t.Fatalf("activeEngineerLaunchCountForRepo fresh = %d, want 1", count)
	}

	if err := writeAgentReservation(path, agentReservation{
		Owner:     ref.Owner,
		Repo:      ref.Repo,
		Number:    ref.Number,
		Mode:      string(modeCodex),
		Container: "engineer-codex-ward-885",
		Branch:    "issue-885",
		Host:      "test-host",
		PID:       1234,
		At:        now.Add(-3 * agentLaunchConfirmationTTL()),
	}); err != nil {
		t.Fatalf("rewrite stale reservation: %v", err)
	}
	count, err = r.activeEngineerLaunchCountForRepo(context.Background(), ref)
	if err != nil {
		t.Fatalf("activeEngineerLaunchCountForRepo stale: %v", err)
	}
	if count != 0 {
		t.Fatalf("activeEngineerLaunchCountForRepo stale = %d, want 0", count)
	}
}

func TestActiveEngineerLaunchCountFallsBackSafelyForNonForgejoTrackers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	oldBase := forgejoBaseURL
	defer func() { forgejoBaseURL = oldBase }()
	forgejoSrv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Fatalf("Forgejo client must not be called for non-Forgejo refs: %s %s", r.Method, r.URL.Path)
	}))
	defer forgejoSrv.Close()
	forgejoBaseURL = forgejoSrv.URL

	cases := []struct {
		name string
		ref  agentIssueRef
		plan func(t *testing.T)
	}{
		{
			name: "github",
			ref:  agentIssueRef{Owner: "acme", Repo: "ward", Number: 77, Forge: forgeGitHub, Tracker: trackerGitHub},
			plan: func(t *testing.T) {
				stubCommandInPath(t, "gh")
			},
		},
		{
			name: "gitlab",
			ref:  agentIssueRef{Owner: "acme", Repo: "ward", Number: 77, Forge: forgeGitLab, Tracker: trackerGitLab},
		},
		{
			name: "shortcut",
			ref: agentIssueRef{
				Owner:             "acme",
				Repo:              "ward",
				Number:            77,
				Tracker:           trackerShortcut,
				URL:               "https://app.shortcut.com/acme/story/77/fix-it",
				ShortcutWorkspace: "acme",
			},
			plan: func(t *testing.T) {
				t.Setenv(shortcutTokenEnv, "secret")
			},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if tc.plan != nil {
				tc.plan(t)
			}
			r, _, _ := bufRunner(engineerCountDockerStub(t, 0))

			count, err := r.activeEngineerLaunchCountForRepo(context.Background(), tc.ref)
			if err != nil {
				t.Fatalf("activeEngineerLaunchCountForRepo(%s): %v", tc.name, err)
			}
			if count != 0 {
				t.Fatalf("activeEngineerLaunchCountForRepo(%s) = %d, want 0 from the safe Docker fallback", tc.name, count)
			}

			err = r.launchRepoEngineerBackpressureCheck(context.Background(), "lbl", tc.ref, false)
			if err != nil {
				t.Fatalf("launchRepoEngineerBackpressureCheck(%s) should use the safe fallback: %v", tc.name, err)
			}

			if _, err := r.activeEngineerLaunchCountFromIssueThread(context.Background(), tc.ref); !isRepoIssueScanUnsupported(err) {
				t.Fatalf("activeEngineerLaunchCountFromIssueThread(%s) error = %v, want explicit unsupported tracker error", tc.name, err)
			}
		})
	}
}
