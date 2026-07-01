package main

import (
	"context"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

// ward#330: --host-net appends --network=host so the run inherits the host's
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

// tailnetProbeFlags mirrors the launch flag set buildUpPlan reads, so a probe can
// exercise the consolidated --tailnet plumbing without a real surface (ward#362).
func tailnetProbeFlags() []cli.Flag {
	flags := []cli.Flag{
		&cli.StringFlag{Name: "ward-source"},
		&cli.StringFlag{Name: "ward-version"},
		&cli.StringFlag{Name: "image", Value: containerImageDefault},
		&cli.StringFlag{Name: "tag", Value: containerImageTagDefault},
		&cli.StringFlag{Name: "branch"},
		&cli.StringSliceFlag{Name: "repo"},
		&cli.BoolFlag{Name: "aws"},
	}
	flags = append(flags, tailnetFlags()...)
	return append(flags, &cli.BoolFlag{Name: "detach"})
}

// TestResolveTailnet covers ward#362: --tailnet auto-selects host-net on Linux and the
// sidecar on Docker Desktop, --tailnet-mode pins one, and it stays off unless opted in.
func TestResolveTailnet(t *testing.T) {
	run := func(args []string, goos string) (hostNet, tsSidecar bool, err error) {
		probe := &cli.Command{
			Name:  "probe",
			Flags: tailnetFlags(),
			Action: func(_ context.Context, c *cli.Command) error {
				hostNet, tsSidecar, err = resolveTailnet(c, goos)
				return nil
			},
		}
		if rerr := probe.Run(context.Background(), append([]string{"probe"}, args...)); rerr != nil {
			t.Fatalf("probe run: %v", rerr)
		}
		return hostNet, tsSidecar, err
	}

	// --tailnet auto: host-net on Linux, sidecar on Docker Desktop (non-Linux).
	if h, s, err := run([]string{"--tailnet"}, "linux"); err != nil || !h || s {
		t.Errorf("--tailnet on linux: got hostNet=%v sidecar=%v err=%v, want host-net", h, s, err)
	}
	if h, s, err := run([]string{"--tailnet"}, "darwin"); err != nil || h || !s {
		t.Errorf("--tailnet on darwin: got hostNet=%v sidecar=%v err=%v, want sidecar", h, s, err)
	}
	// --tailnet-mode pins a mechanism regardless of platform, and opts in on its own.
	if h, s, err := run([]string{"--tailnet-mode", "host-net"}, "darwin"); err != nil || !h || s {
		t.Errorf("--tailnet-mode host-net on darwin: got hostNet=%v sidecar=%v err=%v, want host-net", h, s, err)
	}
	if h, s, err := run([]string{"--tailnet-mode", "sidecar"}, "linux"); err != nil || h || !s {
		t.Errorf("--tailnet-mode sidecar on linux: got hostNet=%v sidecar=%v err=%v, want sidecar", h, s, err)
	}
	// Off by default; an unknown mode is a hard error.
	if h, s, err := run(nil, "linux"); err != nil || h || s {
		t.Errorf("default: got hostNet=%v sidecar=%v err=%v, want both off", h, s, err)
	}
	if _, _, err := run([]string{"--tailnet", "--tailnet-mode", "bogus"}, "linux"); err == nil {
		t.Error("--tailnet-mode bogus: want an error")
	}
}

// TestBuildUpPlanTailnet covers ward#362: the consolidated --tailnet sets the platform's
// mechanism and implies the ~/.aws mount, while --aws alone leaves the tailnet off.
func TestBuildUpPlanTailnet(t *testing.T) {
	run := func(args []string) upPlan {
		var got upPlan
		probe := &cli.Command{
			Name:  "probe",
			Flags: tailnetProbeFlags(),
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

	// --tailnet-mode host-net: HostNet set AND ~/.aws implied (the tailnet implies --aws).
	if p := run([]string{"--tailnet", "--tailnet-mode", "host-net"}); !p.HostNet {
		t.Error("--tailnet host-net: HostNet should be true")
	} else if !hasAWSMount(p) {
		t.Error("--tailnet should imply the ~/.aws mount (ward#362)")
	}

	// --tailnet-mode sidecar: TSSidecar set, HostNet off, and ~/.aws implied too (ward#362).
	if p := run([]string{"--tailnet", "--tailnet-mode", "sidecar"}); !p.TSSidecar {
		t.Error("--tailnet sidecar: TSSidecar should be true")
	} else if p.HostNet {
		t.Error("--tailnet sidecar must not set HostNet")
	} else if !hasAWSMount(p) {
		t.Error("--tailnet should imply the ~/.aws mount even on the sidecar route (ward#362)")
	}

	// --aws alone: the SSM mount, but no tailnet escalation.
	if p := run([]string{"--aws"}); p.HostNet || p.TSSidecar {
		t.Errorf("--aws alone must not set a tailnet route: HostNet=%v TSSidecar=%v", p.HostNet, p.TSSidecar)
	} else if !hasAWSMount(p) {
		t.Error("--aws should still mount ~/.aws")
	}

	// Neither: least-access default, no tailnet, no ~/.aws.
	if p := run(nil); p.HostNet || p.TSSidecar || hasAWSMount(p) {
		t.Errorf("default: HostNet=%v TSSidecar=%v aws-mounted=%v, want all false", p.HostNet, p.TSSidecar, hasAWSMount(p))
	}
}
