package main

import (
	"context"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

// resolveLaunchCreds resolves the env-file lines a launch injects for the
// selected agent harness.
func (r *Runner) resolveLaunchCreds(ctx context.Context, _ *upPlan, mode containerMode) []agentsapi.EnvLine {
	return r.resolveAgentCreds(ctx, mode)
}

// resolveDirectorStackCreds host-resolves every harness before broker launch.
// Child launches reuse those channels without access to host credential stores.
func (r *Runner) resolveDirectorStackCreds(ctx context.Context, _ *upPlan, directorMode containerMode) []agentsapi.EnvLine {
	modes := make([]containerMode, 0, len(agentModes))
	modes = append(modes, directorMode)
	for _, mode := range agentModes {
		if mode != directorMode {
			modes = append(modes, mode)
		}
	}

	seen := make(map[string]bool)
	creds := make([]agentsapi.EnvLine, 0, len(modes))
	for _, mode := range modes {
		for _, line := range r.resolveAgentCreds(ctx, mode) {
			if line.Key == "" || seen[line.Key] {
				continue
			}
			seen[line.Key] = true
			creds = append(creds, line)
		}
	}
	return creds
}
