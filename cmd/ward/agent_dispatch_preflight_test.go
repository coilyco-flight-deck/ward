package main

import (
	"net"
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

func TestDispatchDockerStateBlockedDistinguishesBrokerReachability(t *testing.T) {
	t.Run("reachable broker", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen broker: %v", err)
		}
		defer ln.Close()
		blocked, reason := (dispatchDockerState{inContainer: true, dockerOnPath: false, brokerAddr: ln.Addr().String(), readOnly: true}).blocked()
		if !blocked {
			t.Fatal("blocked() = false, want true")
		}
		if !strings.Contains(reason, "forward did not fire") {
			t.Fatalf("reachable-broker reason %q, want the forward-missed branch", reason)
		}
	})
	t.Run("unreachable broker", func(t *testing.T) {
		blocked, reason := (dispatchDockerState{inContainer: true, dockerOnPath: false, brokerAddr: "127.0.0.1:1", readOnly: true}).blocked()
		if !blocked {
			t.Fatal("blocked() = false, want true")
		}
		if !strings.Contains(reason, "unreachable") {
			t.Fatalf("unreachable-broker reason %q, want the unreachable branch", reason)
		}
	})
}
