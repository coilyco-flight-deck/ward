package main

import (
	"context"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

// ward#333: --ts-sidecar joins the carry to a userspace SOCKS5 sidecar's netns
// (--network=container:<name>-ts); off by default it stays absent.
func TestDockerArgvTSSidecar(t *testing.T) {
	// Default plan: no --network at all.
	if joined := strings.Join(dockerCreateArgv(sampleUpPlan(), ""), " "); strings.Contains(joined, "--network") {
		t.Errorf("default run must not pass --network; got: %s", joined)
	}

	p := sampleUpPlan()
	p.TSSidecar = true
	want := "--network=container:" + tsSidecarName(p.Name)
	joined := strings.Join(dockerCreateArgv(p, ""), " ")
	if !strings.Contains(joined, want) {
		t.Errorf("--ts-sidecar run must pass %q; got: %s", want, joined)
	}
	// It is the sidecar netns, never the host network.
	if strings.Contains(joined, "--network=host") {
		t.Errorf("--ts-sidecar must not pass --network=host; got: %s", joined)
	}
	// The flag rides the shared head, so the no-binds (create) builder carries it too.
	if j := strings.Join(dockerCreateNoBindsArgv(p, ""), " "); !strings.Contains(j, want) {
		t.Errorf("--ts-sidecar create must pass %q; got: %s", want, j)
	}
}

// TestTSSidecarRunArgv covers the userspace sidecar's `docker run -d` shape: detached,
// named off the carry, labelled, userspace + loopback SOCKS5, auth via env-file only.
func TestTSSidecarRunArgv(t *testing.T) {
	argv := tsSidecarRunArgv("ward-eco-app-deadbeef", "coilyco-gaming/eco-app", "/tmp/ward-ts-env-xyz")
	joined := strings.Join(argv, " ")

	if argv[0] != "run" || argv[1] != "-d" {
		t.Errorf("sidecar argv must start `run -d`; got: %v", argv[:2])
	}
	for _, want := range []string{
		"--name ward-eco-app-deadbeef-ts",
		"--label " + containerLabel,
		"--label " + tsSidecarLabel,
		"--label ward.repo=coilyco-gaming/eco-app",
		"--hostname " + tsSidecarHostname,
		"-e TS_USERSPACE=true",
		"-e TS_SOCKS5_SERVER=" + tsSidecarSocks5Host,
		"--env-file /tmp/ward-ts-env-xyz",
		tsSidecarImage,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("sidecar argv missing %q; got: %s", want, joined)
		}
	}
	// Userspace mode escalates nothing: no TUN device, no NET_ADMIN.
	for _, forbidden := range []string{"/dev/net/tun", "NET_ADMIN", "--privileged"} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("sidecar argv must not contain %q; got: %s", forbidden, joined)
		}
	}
	// The auth key never rides argv - it lives in the --env-file only.
	if strings.Contains(joined, "TS_AUTHKEY") {
		t.Errorf("sidecar argv must not inline TS_AUTHKEY; got: %s", joined)
	}
	// The loopback bind is loopback (the carry shares the netns), never 0.0.0.0.
	if strings.Contains(joined, "0.0.0.0:1055") {
		t.Errorf("sidecar SOCKS5 must bind loopback, not 0.0.0.0; got: %s", joined)
	}
}

// TestTSSidecarWardEnv: a --ts-sidecar carry is told the per-connection SOCKS5 proxy
// address (never a host-wide ALL_PROXY); a default carry is told nothing.
func TestTSSidecarWardEnv(t *testing.T) {
	p := sampleUpPlan()
	if _, ok := p.wardEnv()["WARD_TS_SOCKS5"]; ok {
		t.Error("default carry must not set WARD_TS_SOCKS5")
	}
	p.TSSidecar = true
	env := p.wardEnv()
	if got := env["WARD_TS_SOCKS5"]; got != "socks5://"+tsSidecarSocks5Host {
		t.Errorf("WARD_TS_SOCKS5 = %q, want socks5://%s", got, tsSidecarSocks5Host)
	}
	// Per-connection only: never a blanket ALL_PROXY (the proxy reaches the tailnet,
	// not the public internet, so routing everything through it would break egress).
	for k := range env {
		if strings.EqualFold(k, "ALL_PROXY") {
			t.Errorf("a --ts-sidecar carry must not set %s (per-connection, not host-wide)", k)
		}
	}
}

// TestCredEnvLinesTower: the tower endpoint rides the env-file base64'd, like the
// other endpoints, since the tailnet IP is SSM-held.
func TestCredEnvLinesTower(t *testing.T) {
	lines := credEnvLines(agentCreds{TowerOllamaHost: "http://100.64.0.1:11434"})
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "WARD_TOWER_OLLAMA_B64=") {
		t.Errorf("expected WARD_TOWER_OLLAMA_B64 line; got: %v", lines)
	}
	// The plaintext endpoint must not appear (it is base64'd).
	if strings.Contains(joined, "100.64.0.1") {
		t.Errorf("tower endpoint must be base64'd, not plaintext; got: %v", lines)
	}
	if got := credEnvLines(agentCreds{}); len(got) != 0 {
		t.Errorf("no creds -> no lines; got: %v", got)
	}
}

// TestOrphanedSidecars: a sidecar whose carry is gone is reclaimed; one whose carry
// still exists is left alone; a non-sidecar carry is never touched.
func TestOrphanedSidecars(t *testing.T) {
	ps := strings.Join([]string{
		"ward-eco-app-live-0001",    // a live carry
		"ward-eco-app-live-0001-ts", // its sidecar - carry present, keep
		"ward-eco-app-dead-0002-ts", // sidecar whose carry is gone - orphan
		"ward-eco-app-plain-0003",   // a carry with no sidecar - ignore
	}, "\n")
	got := orphanedSidecars(ps)
	if len(got) != 1 || got[0] != "ward-eco-app-dead-0002-ts" {
		t.Errorf("orphanedSidecars = %v, want [ward-eco-app-dead-0002-ts]", got)
	}
	if got := orphanedSidecars(""); got != nil {
		t.Errorf("empty input -> nil; got: %v", got)
	}
	if argv := dockerForceRmArgv(nil); argv != nil {
		t.Errorf("dockerForceRmArgv(nil) must be nil; got: %v", argv)
	}
	if argv := dockerForceRmArgv([]string{"a", "b"}); strings.Join(argv, " ") != "rm -f a b" {
		t.Errorf("dockerForceRmArgv = %v, want [rm -f a b]", argv)
	}
}

// tsSidecarProbeFlags mirrors buildUpPlan's launch flag set with both network
// escalations registered, so a probe can exercise the --ts-sidecar plumbing.
func tsSidecarProbeFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "ward-source"},
		&cli.StringFlag{Name: "ward-version"},
		&cli.StringFlag{Name: "image", Value: containerImageDefault},
		&cli.StringFlag{Name: "tag", Value: containerImageTagDefault},
		&cli.StringFlag{Name: "branch"},
		&cli.StringSliceFlag{Name: "repo", Aliases: []string{"with-repo"}},
		&cli.BoolFlag{Name: "aws"},
		hostNetFlag(),
		tsSidecarFlag(),
		&cli.BoolFlag{Name: "detach"},
	}
}

// TestBuildUpPlanTSSidecar covers ward#333: --ts-sidecar sets TSSidecar, implies the
// ~/.aws mount, and is mutually exclusive with --host-net.
func TestBuildUpPlanTSSidecar(t *testing.T) {
	run := func(args []string) (upPlan, error) {
		var got upPlan
		var perr error
		probe := &cli.Command{
			Name:  "probe",
			Flags: tsSidecarProbeFlags(),
			Action: func(_ context.Context, c *cli.Command) error {
				got, perr = buildUpPlan(c, targetRepo{Owner: "o", Name: "r"}, modeClaude, t.TempDir(), t.TempDir(), nil)
				return nil
			},
		}
		if err := probe.Run(context.Background(), append([]string{"probe"}, args...)); err != nil {
			t.Fatalf("probe run: %v", err)
		}
		return got, perr
	}

	hasAWSMount := func(p upPlan) bool {
		for _, m := range p.Mounts {
			if m.Target == containerAWSMount {
				return true
			}
		}
		return false
	}

	// --ts-sidecar: TSSidecar set, HostNet off, AND ~/.aws implied.
	if p, err := run([]string{"--ts-sidecar"}); err != nil {
		t.Fatalf("--ts-sidecar: unexpected error: %v", err)
	} else if !p.TSSidecar {
		t.Error("--ts-sidecar: TSSidecar should be true")
	} else if p.HostNet {
		t.Error("--ts-sidecar must not set HostNet")
	} else if !hasAWSMount(p) {
		t.Error("--ts-sidecar should imply the ~/.aws mount (auth key + tower IP are SSM-only)")
	}

	// --host-net + --ts-sidecar: mutually exclusive, a hard error.
	if _, err := run([]string{"--host-net", "--ts-sidecar"}); err == nil {
		t.Error("--host-net + --ts-sidecar must be a mutual-exclusion error")
	}

	// --aws alone: the SSM mount, but neither network escalation.
	if p, err := run([]string{"--aws"}); err != nil {
		t.Fatalf("--aws: unexpected error: %v", err)
	} else if p.TSSidecar || p.HostNet {
		t.Errorf("--aws alone: TSSidecar=%v HostNet=%v, want both false", p.TSSidecar, p.HostNet)
	} else if !hasAWSMount(p) {
		t.Error("--aws should still mount ~/.aws")
	}
}
