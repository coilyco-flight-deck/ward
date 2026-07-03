package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
	"github.com/urfave/cli/v3"
)

// TestDockerExecContainerArg pins the argv walk that finds the CONTAINER
// positional the way docker's own flag parser would, across docker exec's forms.
func TestDockerExecContainerArg(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
		ok   bool
	}{
		{"bare", []string{"engineer-abc", "bash"}, "engineer-abc", true},
		{"short bundle", []string{"-it", "engineer-abc", "bash"}, "engineer-abc", true},
		{"short single bool", []string{"-i", "engineer-abc", "sh"}, "engineer-abc", true},
		{"env short next token", []string{"-e", "K=V", "engineer-abc", "sh"}, "engineer-abc", true},
		{"env short attached", []string{"-eK=V", "engineer-abc", "sh"}, "engineer-abc", true},
		{"bundle ending in value short", []string{"-ie", "K=V", "engineer-abc", "sh"}, "engineer-abc", true},
		{"user short", []string{"-u", "root", "engineer-abc", "sh"}, "engineer-abc", true},
		{"workdir short", []string{"-w", "/tmp", "engineer-abc", "sh"}, "engineer-abc", true},
		{"env long next token", []string{"--env", "K=V", "engineer-abc", "sh"}, "engineer-abc", true},
		{"env long inline", []string{"--env=K=V", "engineer-abc", "sh"}, "engineer-abc", true},
		{"user long", []string{"--user", "1000", "engineer-abc", "sh"}, "engineer-abc", true},
		{"workdir long inline", []string{"--workdir=/srv", "engineer-abc", "sh"}, "engineer-abc", true},
		{"detach-keys long", []string{"--detach-keys", "ctrl-p", "engineer-abc", "sh"}, "engineer-abc", true},
		{"env-file long", []string{"--env-file", "/tmp/e", "engineer-abc", "sh"}, "engineer-abc", true},
		{"bool long privileged", []string{"--privileged", "engineer-abc", "sh"}, "engineer-abc", true},
		{"mixed", []string{"-it", "-u", "root", "--env=A=B", "-w", "/x", "engineer-abc", "cmd", "arg"}, "engineer-abc", true},
		{"double dash", []string{"--", "engineer-abc", "sh"}, "engineer-abc", true},
		{"double dash after flags", []string{"-it", "--", "engineer-abc", "sh"}, "engineer-abc", true},
		{"command has flags after container", []string{"engineer-abc", "ls", "-la"}, "engineer-abc", true},
		{"no container only flags", []string{"-it"}, "", false},
		{"empty", nil, "", false},
		{"only value flag no container", []string{"-e", "K=V"}, "", false},
		{"only double dash", []string{"--"}, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := dockerExecContainerArg(tc.args)
			if ok != tc.ok || got != tc.want {
				t.Errorf("dockerExecContainerArg(%q) = (%q, %v), want (%q, %v)", tc.args, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// TestGraftDockerExec asserts the exec leaf grafts onto the docker group, is a
// no-op when the group is absent, and never duplicates an existing exec.
func TestGraftDockerExec(t *testing.T) {
	r := &Runner{Runner: &shell.Runner{}}

	t.Run("grafts onto docker group", func(t *testing.T) {
		root := &cli.Command{Name: "ward", Commands: []*cli.Command{{Name: "docker"}}}
		graftDockerExec(root, r)
		docker := subCommandNamed(root, "docker")
		if subCommandNamed(docker, "exec") == nil {
			t.Fatal("exec leaf not grafted onto docker group")
		}
	})

	t.Run("no-op without docker group", func(t *testing.T) {
		root := &cli.Command{Name: "ward", Commands: []*cli.Command{{Name: "git"}}}
		graftDockerExec(root, r) // must not panic
		if subCommandNamed(root, "docker") != nil {
			t.Error("graft invented a docker group where none existed")
		}
	})

	t.Run("does not duplicate an existing exec", func(t *testing.T) {
		docker := &cli.Command{Name: "docker", Commands: []*cli.Command{{Name: "exec"}}}
		root := &cli.Command{Name: "ward", Commands: []*cli.Command{docker}}
		graftDockerExec(root, r)
		if n := countNamed(docker.Commands, "exec"); n != 1 {
			t.Errorf("expected exactly one exec leaf, got %d", n)
		}
	})
}

// fakeDockerExecRunner builds a Runner whose "docker" resolves to a script that logs
// its argv and prints wardLabel as the inspect result (no real daemon needed).
func fakeDockerExecRunner(t *testing.T, wardLabel string) (r *Runner, logPath string) {
	t.Helper()
	dir := t.TempDir()
	logPath = filepath.Join(dir, "argv.log")
	script := filepath.Join(dir, "docker")
	body := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> \"" + logPath + "\"\n" +
		// Only the inspect probe should echo the label; exec forwards run as-is.
		"case \"$1\" in inspect) printf '%s\\n' '" + wardLabel + "' ;; esac\n" +
		"exit 0\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatalf("write fake docker: %v", err)
	}
	r = &Runner{Runner: &shell.Runner{
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Resolve: func(string) (string, error) { return script, nil },
	}}
	return r, logPath
}

// TestRunDockerExecGate exercises the warded-container gate end to end against a
// fake docker: a ward=true container forwards, anything else is refused.
func TestRunDockerExecGate(t *testing.T) {
	ctx := context.Background()

	t.Run("warded container forwards to docker exec", func(t *testing.T) {
		r, logPath := fakeDockerExecRunner(t, "true")
		cmd := dockerExecCommand(r)
		if err := runViaArgs(ctx, cmd, "engineer-abc", "bash"); err != nil {
			t.Fatalf("warded exec refused: %v", err)
		}
		log := readFile(t, logPath)
		if !strings.Contains(log, "inspect") {
			t.Errorf("gate never ran the inspect probe; log:\n%s", log)
		}
		if !strings.Contains(log, "exec engineer-abc bash") {
			t.Errorf("docker exec was not forwarded; log:\n%s", log)
		}
	})

	t.Run("non-warded container is refused before exec", func(t *testing.T) {
		r, logPath := fakeDockerExecRunner(t, "") // no ward label
		cmd := dockerExecCommand(r)
		err := runViaArgs(ctx, cmd, "some-prod-db", "bash")
		if err == nil || !strings.Contains(err.Error(), "not a ward-managed container") {
			t.Fatalf("expected a ward-managed refusal, got: %v", err)
		}
		if log := readFileAllowMissing(logPath); strings.Contains(log, "exec some-prod-db") {
			t.Errorf("docker exec was forwarded despite the refusal; log:\n%s", log)
		}
	})

	t.Run("missing container arg errors", func(t *testing.T) {
		r, _ := fakeDockerExecRunner(t, "true")
		cmd := dockerExecCommand(r)
		// Only a flag, no CONTAINER positional: refused before any docker call.
		err := runViaArgs(ctx, cmd, "-it")
		if err == nil || !strings.Contains(err.Error(), "no container named") {
			t.Fatalf("expected a missing-container error, got: %v", err)
		}
	})
}

// runViaArgs drives the leaf's Action with raw args (SkipFlagParsing means every
// token lands in c.Args()), the way `ward docker exec <args>` would.
func runViaArgs(ctx context.Context, cmd *cli.Command, args ...string) error {
	root := &cli.Command{Name: "ward", Commands: []*cli.Command{
		{Name: "docker", Commands: []*cli.Command{cmd}},
	}}
	return root.Run(ctx, append([]string{"ward", "docker", "exec"}, args...))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func readFileAllowMissing(path string) string {
	b, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	if err != nil {
		return ""
	}
	return string(b)
}
