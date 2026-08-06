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
	b.WriteString("if [ \"$1\" = inspect ] && [ $# -eq 2 ]; then\n")
	b.WriteString("  case \"$2\" in\n")
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("engineer-%02d", i+1)
		fmt.Fprintf(&b, "    %s)\n", name)
		fmt.Fprintf(&b, "      cat <<'JSON'\n")
		fmt.Fprintf(&b, "[{\"Name\":\"/%s\",\"Config\":{\"Labels\":{\"ward\":\"true\",\"ward.role\":\"engineer\"},\"Env\":[\"WARD_TARGET_OWNER=coilyco-flight-deck\",\"WARD_TARGET_NAME=ward\",\"WARD_TARGET_REPO=coilyco-flight-deck/ward\",\"WARD_TARGET_ISSUE=%d\",\"WARD_BRANCH=issue-%d\",\"WARD_MODE=codex\"]},\"State\":{\"Status\":\"running\",\"StartedAt\":\"2026-07-10T00:00:00Z\"}}]\n", name, i+1, i+1)
		b.WriteString("JSON\n")
		b.WriteString("      exit 0\n")
		b.WriteString("      ;;\n")
	}
	b.WriteString("  esac\n")
	b.WriteString("fi\n")
	b.WriteString("printf '%s\\n' \"unexpected docker args: $*\" >&2\n")
	b.WriteString("exit 1\n")
	writeTestShellCommand(t, stub, b.String())
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
	writeTestShellCommand(t, stub, b.String())
	t.Setenv("PATH", filepath.Dir(stub)+string(os.PathListSeparator)+os.Getenv("PATH"))
	return stub
}

func engineerCapacityDelayedVisibilityDockerStub(t *testing.T, running []string) (stub string, statePath string, pendingPath string, visiblePath string) {
	t.Helper()
	dir := t.TempDir()
	statePath = filepath.Join(dir, "running.txt")
	pendingPath = filepath.Join(dir, "pending.txt")
	visiblePath = filepath.Join(dir, "visible.txt")
	if err := os.WriteFile(statePath, []byte(strings.Join(running, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("seed running state: %v", err)
	}
	stub = filepath.Join(dir, "docker")
	script := "#!/bin/sh\n" +
		"state=" + shellQuote(testShellPath(statePath)) + "\n" +
		"pending=" + shellQuote(testShellPath(pendingPath)) + "\n" +
		"visible=" + shellQuote(testShellPath(visiblePath)) + "\n" +
		"cmd=$1\n" +
		"shift\n" +
		"case \"$cmd\" in\n" +
		"  run)\n" +
		"    name=\n" +
		"    while [ $# -gt 0 ]; do\n" +
		"      case \"$1\" in\n" +
		"        --name)\n" +
		"          name=$2\n" +
		"          shift 2\n" +
		"          ;;\n" +
		"        --name=*)\n" +
		"          name=${1#--name=}\n" +
		"          shift\n" +
		"          ;;\n" +
		"        *)\n" +
		"          shift\n" +
		"          ;;\n" +
		"      esac\n" +
		"    done\n" +
		"    if [ -n \"$name\" ] && ! grep -Fxq \"$name\" \"$state\" 2>/dev/null && ! grep -Fxq \"$name\" \"$pending\" 2>/dev/null; then\n" +
		"      printf '%s\\n' \"$name\" >> \"$pending\"\n" +
		"    fi\n" +
		"    printf '%s\\n' deadbeefcontainerid\n" +
		"    ;;\n" +
		"  ps)\n" +
		"    if [ \"$1\" = --format ] && [ \"$2\" = '{{.Names}}' ]; then\n" +
		"      if [ \"$3\" = --filter ] && [ \"$4\" = label=ward=true ] && [ \"$5\" = --filter ] && [ \"$6\" = label=ward.role=engineer ]; then\n" +
		"        if [ \"$7\" = --filter ] && [ \"${8#name=^}\" != \"$8\" ]; then\n" +
		"          target=$(printf '%s' \"$8\" | sed 's/^name=\\^//; s/\\$$//')\n" +
		"          if grep -Fxq \"$target\" \"$state\" 2>/dev/null; then\n" +
		"            printf '%s\\n' \"$target\"\n" +
		"          elif [ -f \"$visible\" ] && grep -Fxq \"$target\" \"$pending\" 2>/dev/null; then\n" +
		"            printf '%s\\n' \"$target\"\n" +
		"          fi\n" +
		"          exit 0\n" +
		"        fi\n" +
		"        if [ -f \"$visible\" ]; then\n" +
		"          cat \"$state\" \"$pending\" 2>/dev/null\n" +
		"        else\n" +
		"          cat \"$state\" 2>/dev/null\n" +
		"        fi\n" +
		"        exit 0\n" +
		"      fi\n" +
		"    fi\n" +
		"    exit 0\n" +
		"    ;;\n" +
		"  *)\n" +
		"    exit 0\n" +
		"    ;;\n" +
		"esac\n"
	writeTestShellCommand(t, stub, script)
	t.Setenv("PATH", filepath.Dir(stub)+string(os.PathListSeparator)+os.Getenv("PATH"))
	return stub, statePath, pendingPath, visiblePath
}

func stubCommandInPath(t *testing.T, name string) string {
	t.Helper()
	stub := filepath.Join(t.TempDir(), name)
	script := "#!/bin/sh\nprintf '%s\\n' \"unexpected " + name + " invocation: $*\" >&2\nexit 1\n"
	writeTestShellCommand(t, stub, script)
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
	t.Setenv("WARD_TARGET_OWNER", "")
	t.Setenv("WARD_TARGET_REPO", "")
	t.Setenv("WARD_READONLY", "")
	limit := bakedSmartDefaults().engineerContainerLimit
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
			"active launches",
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
	t.Setenv("WARD_TARGET_OWNER", "")
	t.Setenv("WARD_TARGET_REPO", "")
	t.Setenv("WARD_READONLY", "")
	limit := bakedSmartDefaults().engineerContainerLimit

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

func TestEngineerCapacityLockWaitsForVisibleContainerBeforeRelease(t *testing.T) {
	setTestHome(t, t.TempDir())
	t.Setenv("WARD_TARGET_OWNER", "")
	t.Setenv("WARD_TARGET_REPO", "")
	t.Setenv("WARD_READONLY", "")
	limit := bakedSmartDefaults().engineerContainerLimit
	if limit < 2 {
		t.Fatalf("engineer limit = %d, want at least 2 for the race fixture", limit)
	}

	running := make([]string, 0, limit-1)
	for i := 0; i < limit-1; i++ {
		running = append(running, fmt.Sprintf("engineer-%02d", i+1))
	}
	stub, _, pendingPath, visiblePath := engineerCapacityDelayedVisibilityDockerStub(t, running)
	r, _, _ := bufRunner(stub)

	plan := sampleUpPlan()
	plan.Interactive = false
	plan.Name = fmt.Sprintf("engineer-claude-ward-%d", limit)
	plan.Repo = targetRepo{Owner: "coilyco-flight-deck", Name: "ward"}
	plan.Issue = 1016
	plan.Branch = "issue-1016"

	firstStarted := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		if err := os.WriteFile(pendingPath, []byte(plan.Name+"\n"), 0o644); err != nil {
			firstDone <- err
			return
		}
		close(firstStarted)
		firstDone <- r.engineerLaunchVisible(t.Context(), plan.Name)
	}()

	select {
	case <-firstStarted:
	case err := <-firstDone:
		if err != nil {
			t.Fatalf("first launch failed before waiting for visibility: %v", err)
		}
		t.Fatal("first launch returned before reaching the visibility wait")
	case <-time.After(2 * time.Second):
		t.Fatal("first launch did not reach the visibility wait")
	}

	if err := r.enforceEngineerContainerLimit(t.Context(), "ward agent engineer", false); err != nil {
		t.Fatalf("capacity check while launch is still invisible: %v", err)
	}

	if err := os.WriteFile(visiblePath, []byte("1"), 0o644); err != nil {
		t.Fatalf("mark container visible: %v", err)
	}
	if err := <-firstDone; err != nil {
		t.Fatalf("first launch visibility wait: %v", err)
	}
	err := r.enforceEngineerContainerLimit(t.Context(), "ward agent engineer", false)
	if err == nil {
		t.Fatal("capacity check after visibility: want error, got nil")
	}
	var capErr *engineerCapacityError
	if !errors.As(err, &capErr) {
		t.Fatalf("capacity check after visibility returned %T, want *engineerCapacityError", err)
	}
	if !isEngineerCapacityError(err) {
		t.Fatal("capacity check after visibility should classify as engineer capacity backpressure")
	}
}

func TestLaunchRepoEngineerBackpressureIgnoresStaleDockerWhenIssueThreadIsClear(t *testing.T) {
	setTestHome(t, t.TempDir())
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

func TestLaunchRepoEngineerBackpressureOverrideCapacity(t *testing.T) {
	setTestHome(t, t.TempDir())
	oldBase := forgejoBaseURL
	defer func() { forgejoBaseURL = oldBase }()
	now := time.Now().UTC()
	limit := engineerRepoWorkingLimitDefault()
	rows := make([]issueThreadAuthorityRow, 0, limit)
	for i := 0; i < limit; i++ {
		rows = append(rows, issueThreadAuthorityRow{
			Number:   i + 1,
			Title:    fmt.Sprintf("held issue %d", i+1),
			Body:     "body",
			Labels:   []string{"P0", "headless"},
			Comments: []issueComment{reservationIssueComment(reservationCommentBody(modeCodex, fmt.Sprintf("engineer-codex-ward-%d", i+1), "box", now.Add(-time.Minute), "", nil), now.Add(-time.Minute))},
		})
	}
	srv := issueThreadAuthorityServer(t, rows)
	defer srv.Close()
	forgejoBaseURL = srv.URL

	r, _, _ := bufRunner(stubCommandInPath(t, "docker"))
	var err error
	stderr := captureTestStderr(t, func() {
		err = r.launchRepoEngineerBackpressureCheck(context.Background(), "lbl", agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward"}, true)
	})
	if err != nil {
		t.Fatalf("launchRepoEngineerBackpressureCheck at limit with override-capacity: %v", err)
	}
	for _, want := range []string{
		"WARNING",
		fmt.Sprintf("(%d/%d)", limit+1, limit),
		"--override-capacity",
		"repo engineer ceiling",
	} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("override-capacity warning missing %q: %q", want, stderr)
		}
	}

	rows = append(rows, issueThreadAuthorityRow{
		Number:   limit + 1,
		Title:    "over limit issue",
		Body:     "body",
		Labels:   []string{"P0", "headless"},
		Comments: []issueComment{reservationIssueComment(reservationCommentBody(modeCodex, fmt.Sprintf("engineer-codex-ward-%d", limit+1), "box", now.Add(-time.Minute), "", nil), now.Add(-time.Minute))},
	})
	srv2 := issueThreadAuthorityServer(t, rows)
	defer srv2.Close()
	forgejoBaseURL = srv2.URL

	stderr = captureTestStderr(t, func() {
		err = r.launchRepoEngineerBackpressureCheck(context.Background(), "lbl", agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward"}, true)
	})
	if err == nil {
		t.Fatal("launchRepoEngineerBackpressureCheck over limit with override-capacity: want error, got nil")
	}
	var capErr *engineerRepoWorkingBackpressureError
	if !errors.As(err, &capErr) {
		t.Fatalf("over-limit override-capacity error = %T %v, want *engineerRepoWorkingBackpressureError", err, err)
	}
	if !strings.Contains(stderr, "exactly one launch past the repo ceiling") {
		t.Fatalf("override-capacity refusal note missing from stderr: %q", stderr)
	}
}

func repoReservation(t *testing.T, ref agentIssueRef, container string, at time.Time) {
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

func terminalIssueComment(status string, at time.Time) issueComment {
	comment := issueComment{Body: "WARD-WORKFLOW: " + status, CreatedAt: at}
	comment.User.Login = "coilyco-ops"
	return comment
}

func TestActiveEngineerLaunchCountUsesIssueThreadAuthority(t *testing.T) {
	setTestHome(t, t.TempDir())
	oldBase := forgejoBaseURL
	defer func() { forgejoBaseURL = oldBase }()
	now := time.Now().UTC()
	rows := []issueThreadAuthorityRow{
		{
			Number:   1,
			Title:    "held issue",
			Body:     "body",
			Labels:   []string{"P0", "headless"},
			Comments: []issueComment{reservationIssueComment(reservationCommentBody(modeCodex, "engineer-codex-ward-1", "box", now.Add(-time.Minute), "", nil), now.Add(-time.Minute))},
		},
		{
			Number: 2,
			Title:  "clear issue",
			Body:   "body",
			Labels: []string{"P0", "headless"},
		},
	}
	srv := issueThreadAuthorityServer(t, rows)
	defer srv.Close()
	forgejoBaseURL = srv.URL

	r, _, _ := bufRunner(stubCommandInPath(t, "docker"))
	count, err := r.activeEngineerLaunchCountForRepo(context.Background(), agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward"})
	if err != nil {
		t.Fatalf("activeEngineerLaunchCountForRepo: %v", err)
	}
	if count != 1 {
		t.Fatalf("activeEngineerLaunchCountForRepo = %d, want 1 from issue state", count)
	}

	rows[0].Comments = nil
	srv2 := issueThreadAuthorityServer(t, rows)
	defer srv2.Close()
	forgejoBaseURL = srv2.URL
	count, err = r.activeEngineerLaunchCountForRepo(context.Background(), agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward"})
	if err != nil {
		t.Fatalf("activeEngineerLaunchCountForRepo after release: %v", err)
	}
	if count != 0 {
		t.Fatalf("activeEngineerLaunchCountForRepo after release = %d, want 0", count)
	}
}

func TestActiveEngineerLaunchCountIgnoresLaunchIntents(t *testing.T) {
	setTestHome(t, t.TempDir())
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
			Comments: []issueComment{machineComment(reservationCommentBody(modeClaude, "engineer-claude-ward-884", "box", now.Add(-time.Minute), "", nil), now.Add(-time.Minute))},
		},
	}
	srv := issueThreadAuthorityServer(t, rows)
	defer srv.Close()
	forgejoBaseURL = srv.URL

	r, _, _ := bufRunner(stubCommandInPath(t, "docker"))
	path, err := agentReservationPath(agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 885})
	if err != nil {
		t.Fatalf("agentReservationPath: %v", err)
	}
	repoReservation(t, agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 883}, "reserved-engineer-03", time.Now().UTC())

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

func TestActiveEngineerLaunchCountIgnoresTerminalGhostRowsAndCacheClears(t *testing.T) {
	setTestHome(t, t.TempDir())
	oldBase := forgejoBaseURL
	defer func() { forgejoBaseURL = oldBase }()
	now := time.Now().UTC()
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward"}
	rows := []issueThreadAuthorityRow{
		{
			Number:   101,
			Title:    "active 1",
			Body:     "body",
			Labels:   []string{"P0", "headless"},
			Comments: []issueComment{reservationIssueComment(reservationCommentBody(modeCodex, "engineer-codex-ward-101", "host-1", now.Add(-2*time.Minute), "", nil), now.Add(-2*time.Minute))},
		},
		{
			Number:   102,
			Title:    "active 2",
			Body:     "body",
			Labels:   []string{"P0", "headless"},
			Comments: []issueComment{reservationIssueComment(reservationCommentBody(modeCodex, "engineer-codex-ward-102", "host-2", now.Add(-2*time.Minute), "", nil), now.Add(-2*time.Minute))},
		},
		{
			Number: 103,
			Title:  "terminal ghost",
			Body:   "body",
			Labels: []string{"P0", "headless"},
			Comments: []issueComment{
				reservationIssueComment(reservationCommentBody(modeCodex, "engineer-codex-ward-103", "host-3", now.Add(-3*time.Minute), "", nil), now.Add(-3*time.Minute)),
				terminalIssueComment("done", now.Add(-time.Minute)),
			},
		},
	}
	srv := issueThreadAuthorityServer(t, rows)
	defer srv.Close()
	forgejoBaseURL = srv.URL

	ghostRef := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 103}
	repoReservation(t, ghostRef, "engineer-codex-ward-103", now)

	r, _, _ := bufRunner(stubCommandInPath(t, "docker"))
	count, err := r.activeEngineerLaunchCountForRepo(context.Background(), ref)
	if err != nil {
		t.Fatalf("activeEngineerLaunchCountForRepo terminal ghost: %v", err)
	}
	if count != 2 {
		t.Fatalf("activeEngineerLaunchCountForRepo terminal ghost = %d, want 2", count)
	}

	if err := r.launchRepoEngineerBackpressureCheck(context.Background(), "lbl", ref, false); err != nil {
		t.Fatalf("launchRepoEngineerBackpressureCheck should ignore the terminal ghost row: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(os.Getenv("HOME"), ".ward")); err != nil {
		t.Fatalf("remove cache folder: %v", err)
	}
	count, err = r.activeEngineerLaunchCountForRepo(context.Background(), ref)
	if err != nil {
		t.Fatalf("activeEngineerLaunchCountForRepo after cache clear: %v", err)
	}
	if count != 2 {
		t.Fatalf("activeEngineerLaunchCountForRepo after cache clear = %d, want 2", count)
	}
	if err := r.launchRepoEngineerBackpressureCheck(context.Background(), "lbl", ref, true); err != nil {
		t.Fatalf("launchRepoEngineerBackpressureCheck with override-capacity should still ignore the terminal ghost row: %v", err)
	}
}

func TestActiveEngineerLaunchCountIgnoresLocalCleanupNeededReservation(t *testing.T) {
	setTestHome(t, t.TempDir())
	oldBase := forgejoBaseURL
	defer func() { forgejoBaseURL = oldBase }()
	now := time.Now().UTC()
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1502}
	rows := []issueThreadAuthorityRow{
		{
			Number:   ref.Number,
			Title:    "failed local launch",
			Body:     "body",
			Labels:   []string{"P0", "headless"},
			Comments: []issueComment{reservationIssueComment(reservationCommentBody(modeCodex, "engineer-codex-ward-1502", "test-host", now.Add(-time.Minute), "", nil), now.Add(-time.Minute))},
		},
	}
	srv := issueThreadAuthorityServer(t, rows)
	defer srv.Close()
	forgejoBaseURL = srv.URL
	repoReservation(t, ref, "engineer-codex-ward-1502", now.Add(-time.Minute))

	r, _, _ := bufRunner(engineerRepoAndGlobalCountDockerStub(t, ref.repoSlug(), 0, 0))
	count, err := r.activeEngineerLaunchCountForRepo(context.Background(), ref)
	if err != nil {
		t.Fatalf("activeEngineerLaunchCountForRepo cleanup-needed row: %v", err)
	}
	if count != 0 {
		t.Fatalf("activeEngineerLaunchCountForRepo cleanup-needed row = %d, want 0", count)
	}
	listRows, err := r.agentListRows(context.Background())
	if err != nil {
		t.Fatalf("agentListRows cleanup-needed row: %v", err)
	}
	if len(listRows) != 1 || agentLaunchRowClass(listRows[0]) != agentLaunchRowCleanupNeeded {
		t.Fatalf("agentListRows cleanup-needed row = %+v, want one cleanup-needed row", listRows)
	}
}

func TestActiveEngineerLaunchCountFallsBackSafelyForNonForgejoTrackers(t *testing.T) {
	setTestHome(t, t.TempDir())
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
