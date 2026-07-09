package main

import "testing"

func TestSemanticCapabilitiesForRole(t *testing.T) {
	cases := []struct {
		role string
		want string
	}{
		{roleAdvisor, "read"},
		{roleQA, "read"},
		{roleDirector, "read + project-management"},
		{roleEngineer, "read + engineering"},
		{roleOps, "read + ops"},
		{roleAdmin, "read + project-management + engineering + ops + admin"},
		{"nonexistent", ""},
	}
	for _, tc := range cases {
		t.Run(tc.role, func(t *testing.T) {
			if got := semanticCapabilitiesForRole(tc.role).String(); got != tc.want {
				t.Fatalf("semanticCapabilitiesForRole(%q) = %q, want %q", tc.role, got, tc.want)
			}
		})
	}
}

func TestSemanticCapabilitiesCompose(t *testing.T) {
	base := semanticCapabilitiesForRole(roleAdvisor)
	got := semanticCapabilitiesCompose(base, semanticCapabilityEngineering, semanticCapabilityProjectManagement)
	if want := "read + project-management + engineering"; got.String() != want {
		t.Fatalf("compose = %q, want %q", got.String(), want)
	}
	if !base.Has(semanticCapabilityRead) || base.Has(semanticCapabilityEngineering) {
		t.Fatalf("base preset mutated by compose: %+v", base)
	}
}

func TestSemanticCapabilitiesFromNames(t *testing.T) {
	got, err := semanticCapabilitiesFromNames("read", "engineering", "project-management")
	if err != nil {
		t.Fatalf("semanticCapabilitiesFromNames: %v", err)
	}
	if want := "read + project-management + engineering"; got.String() != want {
		t.Fatalf("from names = %q, want %q", got.String(), want)
	}
	if _, err := semanticCapabilitiesFromNames("read", "write-everything"); err == nil {
		t.Fatal("unknown capability name did not fail")
	}
}

func TestAgentRosterRowsRenderCapabilities(t *testing.T) {
	rows, err := agentRosterRows()
	if err != nil {
		t.Fatalf("agentRosterRows: %v", err)
	}
	want := map[string]string{
		"engineer": "read + engineering",
		"director": "read + project-management",
		"advisor":  "read",
		"qa":       "read",
	}
	for _, row := range rows {
		if row.Capabilities != want[row.Role] {
			t.Fatalf("row %q capabilities = %q, want %q", row.Role, row.Capabilities, want[row.Role])
		}
	}
}
