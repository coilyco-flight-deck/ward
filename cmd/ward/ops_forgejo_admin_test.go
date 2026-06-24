package main

import (
	"context"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/execverb"
	"github.com/urfave/cli/v3"
)

// TestForgejoAdminExecGrafted asserts the exec-dialect admin/doctor slice
// (ward#81) grafts onto the same `ops forgejo` group as the REST surface.
func TestForgejoAdminExecGrafted(t *testing.T) {
	forgejo, err := buildForgejoOps()
	if err != nil {
		t.Fatalf("buildForgejoOps: %v", err)
	}
	admin := commandNamed(forgejo.Commands, "admin")
	if admin == nil {
		t.Fatalf("forgejo group missing grafted admin command; got %v", commandNames(forgejo.Commands))
	}
	user := commandNamed(admin.Commands, "user")
	if user == nil {
		t.Fatalf("admin missing user group; got %v", commandNames(admin.Commands))
	}
	for _, want := range []string{"list", "create"} {
		if commandNamed(user.Commands, want) == nil {
			t.Errorf("admin user missing %q; got %v", want, commandNames(user.Commands))
		}
	}
	auth := commandNamed(admin.Commands, "auth")
	if auth == nil || commandNamed(auth.Commands, "list") == nil {
		t.Errorf("admin auth list not mounted; got %v", commandNames(admin.Commands))
	}
	doctor := commandNamed(forgejo.Commands, "doctor")
	if doctor == nil || commandNamed(doctor.Commands, "check") == nil {
		t.Errorf("doctor check not mounted; got %v", commandNames(forgejo.Commands))
	}
}

// TestForgejoAdminCoexistsWithREST asserts the graft adds the remote-exec
// children without displacing the REST resources already on the group.
func TestForgejoAdminCoexistsWithREST(t *testing.T) {
	forgejo, err := buildForgejoOps()
	if err != nil {
		t.Fatalf("buildForgejoOps: %v", err)
	}
	for _, want := range []string{"issue", "admin", "doctor"} {
		if commandNamed(forgejo.Commands, want) == nil {
			t.Errorf("forgejo group missing %q; got %v", want, commandNames(forgejo.Commands))
		}
	}
}

// execCapture records the resolved invocation instead of running it, so the
// embedded guardfile's transport and flag policy can be asserted offline.
type execCapture struct {
	bin  string
	argv []string
}

func (cp *execCapture) run(_ context.Context, bin string, argv, _ []string) error {
	cp.bin = bin
	cp.argv = argv
	return nil
}

// buildAdminCapture mounts the embedded admin guardfile against a capturing
// runner, returning the root command and the capture sink.
func buildAdminCapture(t *testing.T) (*cli.Command, *execCapture) {
	t.Helper()
	gfBytes, err := opsAssets.ReadFile(opsForgejoAdminGuardfilePath)
	if err != nil {
		t.Fatalf("read embedded admin guardfile: %v", err)
	}
	gf, err := execverb.Parse(gfBytes)
	if err != nil {
		t.Fatalf("parse admin guardfile: %v", err)
	}
	cp := &execCapture{}
	root := &cli.Command{Name: "ward"}
	if err := execverb.Mount(root, execverb.Config{Guardfile: gf, Run: cp.run}); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	return root, cp
}

// TestForgejoAdminTransportFixed asserts every leaf rides the pinned ssh +
// kubectl-exec argv-prefix, with the granted subcommand and caller args after.
func TestForgejoAdminTransportFixed(t *testing.T) {
	root, cp := buildAdminCapture(t)
	if err := root.Run(context.Background(),
		[]string{"ward", "ops", "forgejo", "admin", "user", "list"}); err != nil {
		t.Fatalf("run admin user list: %v", err)
	}
	if cp.bin != "ssh" {
		t.Errorf("bin = %q, want ssh", cp.bin)
	}
	want := "kai@kai-server k3s kubectl -n forgejo exec deploy/forgejo -- forgejo admin user list"
	if got := strings.Join(cp.argv, " "); got != want {
		t.Errorf("argv = %q,\nwant   %q", got, want)
	}
}

// TestForgejoDoctorReadOnly asserts doctor check allows the diagnostic flags but
// refuses --fix (the mutating one), keeping the leaf read-only.
func TestForgejoDoctorReadOnly(t *testing.T) {
	root, cp := buildAdminCapture(t)
	if err := root.Run(context.Background(),
		[]string{"ward", "ops", "forgejo", "doctor", "check", "--run", "storages"}); err != nil {
		t.Fatalf("doctor check --run refused: %v", err)
	}
	if got := strings.Join(cp.argv, " "); !strings.HasSuffix(got, "forgejo doctor check --run storages") {
		t.Errorf("argv = %q, want the doctor check suffix", got)
	}

	cp.bin = ""
	if err := root.Run(context.Background(),
		[]string{"ward", "ops", "forgejo", "doctor", "check", "--fix"}); err == nil {
		t.Fatal("doctor check --fix should be refused by the allow-flag policy")
	}
	if cp.bin != "" {
		t.Errorf("refused --fix still executed: %s %v", cp.bin, cp.argv)
	}
}
