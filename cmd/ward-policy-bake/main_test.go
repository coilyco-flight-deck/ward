package main

import (
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/fleetconfig"
)

func TestMaterializeNativePolicyCombinesAOSAgentsAndRoles(t *testing.T) {
	const agents = `agents {
    schema-version 2
    defaults {
        agent codex
        attribution name=fixture-bot email=fixture@invalid.test
    }
    agent codex {
        binary codex
    }
}`
	const roles = `roles {
    role engineer {
        agent codex {
            model fixture-model
        }
    }
}`

	out, err := materializeNativePolicy([]byte(agents), []byte(roles))
	if err != nil {
		t.Fatalf("materializeNativePolicy: %v", err)
	}
	fleet, err := fleetconfig.Parse(out)
	if err != nil {
		t.Fatalf("parse materialized policy: %v", err)
	}
	if strings.TrimSpace(fleet.Defaults.Attribution.Name) == "" {
		t.Fatal("materialized policy omitted AOS attribution")
	}
	if len(fleet.Roles) == 0 {
		t.Fatal("materialized policy omitted AOS roles")
	}
}

func TestMaterializeNativePolicyRejectsExampleAttribution(t *testing.T) {
	const agents = `agents {
    schema-version 2
    defaults {
        agent codex
        attribution name=example-bot email=bot@example.com
    }
    agent codex {}
}`
	const roles = `roles {
    role engineer {}
}`

	_, err := materializeNativePolicy([]byte(agents), []byte(roles))
	if err == nil || !strings.Contains(err.Error(), "retains example attribution") {
		t.Fatalf("error = %v, want example-attribution rejection", err)
	}
}

func TestMaterializeNativePolicyRequiresBothAOSRoots(t *testing.T) {
	_, err := materializeNativePolicy([]byte("fleet {}"), []byte("roles {}"))
	if err == nil || !strings.Contains(err.Error(), `missing top-level "agents" node`) {
		t.Fatalf("error = %v, want missing agents root", err)
	}
}
