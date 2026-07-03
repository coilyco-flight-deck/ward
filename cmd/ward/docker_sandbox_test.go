package main

import (
	"context"
	"errors"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/sandbox"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
)

// TestDockerExecSuspendsSandbox locks the ward#540 fix: docker must never ride
// cli-guard's brew jail (a snap-provided docker breaks inside it, exit 64).
func TestDockerExecSuspendsSandbox(t *testing.T) {
	spec := &sandbox.Spec{SelfExe: "/nonexistent/ward", Tools: []string{"brew"}}

	for _, tc := range []struct {
		name string
		call func(r *Runner) error
	}{
		{"dockerExec", func(r *Runner) error {
			return r.dockerExec(context.Background(), "ps")
		}},
		{"dockerCapture", func(r *Runner) error {
			_, err := r.dockerCapture(context.Background(), "ps")
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var duringResolve *sandbox.Spec
			resolved := false
			r := &Runner{Runner: &shell.Runner{Sandbox: spec}}
			// The resolver runs inside Exec/Capture after suspendSandbox fires, so it
			// observes the suspended (nil) sandbox. Erroring stops the real exec.
			r.Runner.Resolve = func(string) (string, error) {
				resolved = true
				duringResolve = r.Runner.Sandbox
				return "", errors.New("stop before exec")
			}

			_ = tc.call(r)

			if !resolved {
				t.Fatal("resolver never ran; cannot observe the sandbox state")
			}
			if duringResolve != nil {
				t.Errorf("sandbox not suspended during docker call: got %+v, want nil", duringResolve)
			}
			if r.Runner.Sandbox != spec {
				t.Errorf("sandbox not restored after docker call: got %+v, want the original spec", r.Runner.Sandbox)
			}
		})
	}
}
