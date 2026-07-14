package main

import (
	"context"
	"runtime"
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
// exercise the config-driven tailnet plumbing without a real surface (ward#362).
func tailnetProbeFlags() []cli.Flag {
	flags := []cli.Flag{
		&cli.StringFlag{Name: "ward-source"},
		&cli.StringFlag{Name: "ward-version"},
		&cli.StringFlag{Name: "image", Value: containerImageDefault},
		&cli.StringFlag{Name: "tag", Value: containerImageTagDefault},
		&cli.StringFlag{Name: "branch"},
		&cli.StringSliceFlag{Name: "repo"},
		&cli.BoolFlag{Name: "no-tailnet"},
	}
	return append(flags, &cli.BoolFlag{Name: "detach"})
}

// TestResolveTailnet covers ward#362: a requested tailnet route selects host-net on
// Linux and the sidecar on Docker Desktop.
func TestResolveTailnet(t *testing.T) {
	for _, tc := range []struct {
		goos     string
		wantHost bool
		wantSide bool
	}{
		{goos: "linux", wantHost: true},
		{goos: "darwin", wantSide: true},
	} {
		h, s := resolveTailnetMechanism(tc.goos, true)
		if h != tc.wantHost || s != tc.wantSide {
			t.Errorf("resolveTailnetMechanism(%s, true) = hostNet=%v sidecar=%v, want hostNet=%v sidecar=%v",
				tc.goos, h, s, tc.wantHost, tc.wantSide)
		}
	}
	if h, s := resolveTailnetMechanism("linux", false); h || s {
		t.Errorf("resolveTailnetMechanism(linux, false) = hostNet=%v sidecar=%v, want both off", h, s)
	}
}

// TestBuildUpPlanTailnet covers ward#362: the role's guardfile set selects the
// platform mechanism and implies the ~/.aws mount.
func TestBuildUpPlanTailnet(t *testing.T) {
	run := func(role string, args []string) upPlan {
		var got upPlan
		probe := &cli.Command{
			Name:  "probe",
			Flags: tailnetProbeFlags(),
			Action: func(_ context.Context, c *cli.Command) error {
				p, err := buildUpPlan(c, targetRepo{Owner: "o", Name: "r"}, modeClaude, role, t.TempDir(), t.TempDir(), nil, false)
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

	// Director carries the live-observe set.
	p := run(roleDirector, nil)
	if runtime.GOOS == "linux" {
		if !p.HostNet || p.TSSidecar {
			t.Errorf("director role on linux should resolve host-net, got HostNet=%v TSSidecar=%v", p.HostNet, p.TSSidecar)
		}
	} else if !p.TSSidecar || p.HostNet {
		t.Errorf("director role on %s should resolve the sidecar, got HostNet=%v TSSidecar=%v", runtime.GOOS, p.HostNet, p.TSSidecar)
	}
	if !hasAWSMount(p) {
		t.Error("director role should imply the ~/.aws mount")
	}

	// Advisor can opt out.
	if p := run(roleAdvisor, []string{"--no-tailnet"}); p.HostNet || p.TSSidecar {
		t.Errorf("--no-tailnet must keep the plan isolated: HostNet=%v TSSidecar=%v", p.HostNet, p.TSSidecar)
	} else if hasAWSMount(p) {
		t.Error("--no-tailnet should keep ~/.aws out of the plan")
	}

	// Engineer stays least-access.
	if p := run(roleEngineer, nil); p.HostNet || p.TSSidecar || hasAWSMount(p) {
		t.Errorf("engineer default: HostNet=%v TSSidecar=%v aws-mounted=%v, want all false", p.HostNet, p.TSSidecar, hasAWSMount(p))
	}
}
