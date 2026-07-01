package main

import (
	"strings"
	"testing"
)

// TestDispatchDockerStateBlocked pins the ward#321 bring-up preflight: only an
// in-container dispatch with no docker client is blocked, with a broker-aware reason.
func TestDispatchDockerStateBlocked(t *testing.T) {
	for _, tc := range []struct {
		name        string
		state       dispatchDockerState
		wantBlocked bool
		wantSubstr  string
	}{
		{
			name:        "host with no docker is not our concern",
			state:       dispatchDockerState{inContainer: false, dockerOnPath: false},
			wantBlocked: false,
		},
		{
			name:        "container with docker client dispatches normally",
			state:       dispatchDockerState{inContainer: true, dockerOnPath: true},
			wantBlocked: false,
		},
		{
			name:        "container, no client, no broker",
			state:       dispatchDockerState{inContainer: true, dockerOnPath: false},
			wantBlocked: true,
			wantSubstr:  "no host dispatch broker is attached",
		},
		{
			name:        "container, no client, broker attached but not read-only",
			state:       dispatchDockerState{inContainer: true, dockerOnPath: false, brokerAddr: "host:1234"},
			wantBlocked: true,
			wantSubstr:  "WARD_READONLY is unset",
		},
		{
			name:        "container, no client, read-only broker forward missed",
			state:       dispatchDockerState{inContainer: true, dockerOnPath: false, brokerAddr: "host:1234", readOnly: true},
			wantBlocked: true,
			wantSubstr:  "forward did not fire",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			blocked, reason := tc.state.blocked()
			if blocked != tc.wantBlocked {
				t.Fatalf("blocked() = %v, want %v (reason %q)", blocked, tc.wantBlocked, reason)
			}
			if !blocked {
				if reason != "" {
					t.Fatalf("unblocked state returned reason %q, want empty", reason)
				}
				return
			}
			if !strings.Contains(reason, "no docker client on PATH") {
				t.Errorf("reason %q missing the core diagnostic", reason)
			}
			if tc.wantSubstr != "" && !strings.Contains(reason, tc.wantSubstr) {
				t.Errorf("reason %q missing %q", reason, tc.wantSubstr)
			}
		})
	}
}
