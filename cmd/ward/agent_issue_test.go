package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/broker"
)

func TestAgentIssueCreateUsesBrokerAndDoesNotDispatch(t *testing.T) {
	sock := shortBrokerSocket(t)
	ln, err := newBrokerListener(sock, os.Getgid())
	if err != nil {
		t.Fatalf("newBrokerListener: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	fake := &fakeExecutor{result: broker.Result{Number: 1592, URL: "https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1592"}}
	srv, err := broker.NewServer(ln, fake, (&Runner{}).writeTierAuthorizer())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Serve(ctx) }()
	t.Setenv(envBrokerSocket, sock)

	bodyPath := writeIssueBodyFile(t, "body from the director")
	cmd := parseCommandForTest(t, agentIssueCreateCommand().Flags, []string{
		"create", "coilyco-flight-deck/ward",
		"--title", "file only",
		"--body-file", bodyPath,
	})

	origLaunch := dispatchBrokerLaunch
	t.Cleanup(func() { dispatchBrokerLaunch = origLaunch })
	dispatchBrokerLaunch = func(context.Context, dispatchBrokerRequest) error {
		t.Fatal("issue create must not dispatch a worker")
		return nil
	}

	var out bytes.Buffer
	r := &Runner{Runner: &shell.Runner{Stdout: &out}}
	if err := r.runAgentIssueCreate(ctx, cmd); err != nil {
		t.Fatalf("runAgentIssueCreate: %v", err)
	}

	if !fake.fileCalled {
		t.Fatal("broker FileIssue was not called")
	}
	if fake.dispatchCalled {
		t.Fatal("broker Dispatch was called; issue create must not request launch credentials")
	}
	if fake.fileTarget != (broker.Target{Owner: "coilyco-flight-deck", Repo: "ward"}) {
		t.Fatalf("target = %+v, want coilyco-flight-deck/ward", fake.fileTarget)
	}
	if fake.fileTitle != "file only" || fake.fileBody != "body from the director" {
		t.Fatalf("file payload = title %q body %q", fake.fileTitle, fake.fileBody)
	}
	for _, want := range []string{
		"issue: coilyco-flight-deck/ward#1592",
		"url: https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1592",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("stdout missing %q:\n%s", want, out.String())
		}
	}
}

func TestAgentIssueCreateRequiresBrokerAccess(t *testing.T) {
	t.Setenv(envBrokerSocket, "")
	_, err := (&Runner{}).brokerFileIssue(context.Background(), targetRepo{Owner: "coilyco-flight-deck", Name: "ward"}, "title", "body")
	if err == nil {
		t.Fatal("brokerFileIssue without broker unexpectedly succeeded")
	}
	got := classifyBrokerIssueCreateError(err).Error()
	for _, want := range []string{"missing broker access", envBrokerSocket, "read-only director surface"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing broker error lacks %q: %s", want, got)
		}
	}
}

func TestAgentIssueCreateClassifiesBrokerRefusalAndForgeFailure(t *testing.T) {
	refusal := classifyBrokerIssueCreateError(errors.Join(errBrokerOutOfTier, errors.New("broker: owner not in allowlist"))).Error()
	if !strings.Contains(refusal, "broker refusal") || !strings.Contains(refusal, "broker refused") {
		t.Fatalf("refusal was not classified distinctly: %s", refusal)
	}

	forgeErr := classifyBrokerIssueCreateError(errors.New("broker: create issue coilyco/ward: forgejo POST returned 500")).Error()
	if !strings.Contains(forgeErr, "Forgejo/API failure") {
		t.Fatalf("Forgejo/API error was not classified distinctly: %s", forgeErr)
	}
}

func TestAgentCommandIncludesIssueCreate(t *testing.T) {
	issue := commandNamed(agentCommand().Commands, "issue")
	if issue == nil {
		t.Fatal("ward agent missing issue command")
	}
	if commandNamed(issue.Commands, "create") == nil {
		t.Fatal("ward agent issue missing create command")
	}
	if commandNamed(issue.Commands, "approve") == nil {
		t.Fatal("ward agent issue missing approve command")
	}
	if commandNamed(agentCommand().Commands, "approval-plan") == nil {
		t.Fatal("ward agent missing approval-plan command")
	}
}

func writeIssueBodyFile(t *testing.T, body string) string {
	t.Helper()
	path := t.TempDir() + "/issue.md"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write body file: %v", err)
	}
	return path
}
