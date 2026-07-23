package main

import (
	"testing"
	"testing/fstest"
)

func TestLoadBundleFleetConfigPreservesSplitRoleIntentRoutes(t *testing.T) {
	const (
		intent  = "example-intent"
		harness = "example-harness"
	)
	src := configSource{
		fsys: fstest.MapFS{
			"agents.kdl": {
				Data: []byte(`
agents {
    schema-version 2
    agent example {
        binary example
    }
}
`),
			},
			"roles.kdl": {
				Data: []byte(`
roles {
    role example {
        intent ` + intent + ` {
            harness ` + harness + `
        }
    }
}
`),
			},
		},
		execDir: ".",
	}

	fleet, err := loadBundleFleetConfigFrom(src)
	if err != nil {
		t.Fatalf("load split fleet bundle: %v", err)
	}
	if len(fleet.Roles) != 1 {
		t.Fatalf("roles = %d, want 1", len(fleet.Roles))
	}
	routes := fleet.Roles[0].IntentRoutes
	if len(routes) != 1 {
		t.Fatalf("intent routes = %d, want 1", len(routes))
	}
	if routes[0].Intent != intent || routes[0].Harness != harness {
		t.Fatalf("intent route = %+v, want %s -> %s", routes[0], intent, harness)
	}
}
