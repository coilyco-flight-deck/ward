package main

import (
	"context"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

// ward#330: --host-net appends --network=host so the carry inherits the host's
// tailnet route; off by default it stays absent (the least-access bridge).
func TestDockerArgvHostNet(t *testing.T) {
	// Default plan: no --network=host (least-access bridge).
	if joined := strings.Join(dockerCreateArgv(sampleUpPlan(), ""), " "); strings.Contains(joined, "--network") {
		t.Errorf("default run must not pass --network; got: %s", joined)
	}

	p := sampleUpPlan()
	p.HostNet = true
	joined := strings.Join(dockerCreateArgv(p, ""), " ")
	if !strings.Contains(joined, "--network=host") {
		t.Errorf("--host-net run must pass --network=host; got: %s", joined)
	}
	// The flag rides the shared head, so the create (no-binds) builder carries it too.
	if joined := strings.Join(dockerCreateNoBindsArgv(p, ""), " "); !strings.Contains(joined, "--network=host") {
		t.Errorf("--host-net create must pass --network=host; got: %s", joined)
	}
}

// ward#332: hostNetTailnetWarning fires on Docker Desktop (any non-Linux ward
// host) and on a Linux host with no tailscale0, and stays quiet otherwise.
func TestHostNetTailnetWarning(t *testing.T) {
	// Docker Desktop: the joined netns is the LinuxKit VM, never a tailnet node,
	// so the warning fires regardless of what the Mac/Windows host has.
	for _, goos := range []string{"darwin", "windows"} {
		for _, hasTS := range []bool{true, false} {
			msg, warn := hostNetTailnetWarning(goos, hasTS)
			if !warn {
				t.Errorf("goos=%s hasTailscale0=%v: want a warning, got none", goos, hasTS)
			}
			if !strings.Contains(msg, "Docker Desktop") || !strings.Contains(msg, goos) {
				t.Errorf("goos=%s: warning should name Docker Desktop and the host OS; got: %s", goos, msg)
			}
		}
	}

	// Native Linux, no tailscale0: ward shares the daemon's netns, so a missing
	// device means no tailnet route - warn.
	if msg, warn := hostNetTailnetWarning("linux", false); !warn {
		t.Error("linux without tailscale0: want a warning")
	} else if !strings.Contains(msg, "tailscale0") {
		t.Errorf("linux warning should name tailscale0; got: %s", msg)
	}

	// Native Linux with tailscale0: the route looks usable - stay quiet.
	if msg, warn := hostNetTailnetWarning("linux", true); warn || msg != "" {
		t.Errorf("linux with tailscale0: want no warning, got %q", msg)
	}
}

// hostNetProbeFlags mirrors the launch flag set buildUpPlan reads, so a probe
// command can exercise the --host-net plumbing without a real surface.
func hostNetProbeFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "ward-source"},
		&cli.StringFlag{Name: "ward-version"},
		&cli.StringFlag{Name: "image", Value: containerImageDefault},
		&cli.StringFlag{Name: "tag", Value: containerImageTagDefault},
		&cli.StringFlag{Name: "branch"},
		&cli.StringSliceFlag{Name: "repo", Aliases: []string{"with-repo"}},
		&cli.BoolFlag{Name: "aws"},
		hostNetFlag(),
		&cli.BoolFlag{Name: "detach"},
	}
}

// TestBuildUpPlanHostNet covers ward#330: --host-net sets HostNet and implies the
// ~/.aws mount (the tower FQDN is SSM-only), while --aws alone leaves HostNet off.
func TestBuildUpPlanHostNet(t *testing.T) {
	run := func(args []string) upPlan {
		var got upPlan
		probe := &cli.Command{
			Name:  "probe",
			Flags: hostNetProbeFlags(),
			Action: func(_ context.Context, c *cli.Command) error {
				p, err := buildUpPlan(c, targetRepo{Owner: "o", Name: "r"}, modeClaude, t.TempDir(), t.TempDir(), nil)
				if err != nil {
					return err
				}
				got = p
				return nil
			},
		}
		if err := probe.Run(context.Background(), append([]string{"probe"}, args...)); err != nil {
			t.Fatalf("probe run: %v", err)
		}
		return got
	}

	hasAWSMount := func(p upPlan) bool {
		for _, m := range p.Mounts {
			if m.Target == containerAWSMount {
				return true
			}
		}
		return false
	}

	// --host-net: HostNet set AND ~/.aws implied (the two are always wanted together).
	if p := run([]string{"--host-net"}); !p.HostNet {
		t.Error("--host-net: HostNet should be true")
	} else if !hasAWSMount(p) {
		t.Error("--host-net should imply the ~/.aws mount (tower FQDN is SSM-only)")
	}

	// --aws alone: the SSM mount, but no host network escalation.
	if p := run([]string{"--aws"}); p.HostNet {
		t.Error("--aws alone must not set HostNet")
	} else if !hasAWSMount(p) {
		t.Error("--aws should still mount ~/.aws")
	}

	// Neither: least-access default, no HostNet, no ~/.aws.
	if p := run(nil); p.HostNet || hasAWSMount(p) {
		t.Errorf("default: HostNet=%v aws-mounted=%v, want both false", p.HostNet, hasAWSMount(p))
	}
}
