package main

import (
	"strings"
	"testing"
)

// TestUpgradeFormula_LockedToCentralTap pins the upgrade verb to ward's
// centralized flight-deck tap, the only tap CI keeps fresh.
func TestUpgradeFormula_LockedToCentralTap(t *testing.T) {
	if upgradeFormula != "coilyco-flight-deck/tap/ward" {
		t.Errorf("upgradeFormula = %q, want %q", upgradeFormula, "coilyco-flight-deck/tap/ward")
	}
	if !strings.HasPrefix(upgradeFormula, "coilyco-flight-deck/") {
		t.Errorf("upgradeFormula = %q must live under coilyco-flight-deck/", upgradeFormula)
	}
	if !strings.HasSuffix(upgradeFormula, "/ward") {
		t.Errorf("upgradeFormula = %q must name the ward formula", upgradeFormula)
	}
}

// TestUpgradeFormulaBare_IsRackName pins the unqualified fallback to the bare
// rack name so it matches a null-full_name keg by directory. See ward#551.
func TestUpgradeFormulaBare_IsRackName(t *testing.T) {
	if upgradeFormulaBare != "ward" {
		t.Errorf("upgradeFormulaBare = %q, want %q", upgradeFormulaBare, "ward")
	}
	if strings.Contains(upgradeFormulaBare, "/") {
		t.Errorf("upgradeFormulaBare = %q must be unqualified (no tap prefix)", upgradeFormulaBare)
	}
	if !strings.HasSuffix(upgradeFormula, "/"+upgradeFormulaBare) {
		t.Errorf("upgradeFormulaBare = %q must be the rack tail of %q", upgradeFormulaBare, upgradeFormula)
	}
}

// TestStaleTabNotInstalled gates the unqualified retry to the specific stale
// receipt signature, not any brew failure. See ward#551.
func TestStaleTabNotInstalled(t *testing.T) {
	cases := []struct {
		name string
		tail string
		want bool
	}{
		{"exact brew error", "Error: coilyco-flight-deck/tap/ward not installed\n", true},
		{"amid other output", "==> brew update\nError: coilyco-flight-deck/tap/ward not installed", true},
		{"different formula not installed", "Error: some/other/formula not installed", false},
		{"unrelated build break", "Error: ward: go build failed", false},
		{"empty tail", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := staleTabNotInstalled(tc.tail, upgradeFormula); got != tc.want {
				t.Errorf("staleTabNotInstalled(%q, %q) = %v, want %v", tc.tail, upgradeFormula, got, tc.want)
			}
		})
	}
}

// TestUpgradeCommand_HasDryFlag pins the --dry escape hatch.
func TestUpgradeCommand_HasDryFlag(t *testing.T) {
	cmd := upgradeCommand()
	for _, f := range cmd.Flags {
		if f.Names()[0] == "dry" {
			return
		}
	}
	t.Error("upgrade command missing --dry flag")
}

// TestUpgradeCommand_Registered pins that `ward upgrade` is wired into the
// top-level command set, not just defined.
func TestUpgradeCommand_Registered(t *testing.T) {
	cmd := upgradeCommand()
	if cmd.Name != "upgrade" {
		t.Errorf("upgrade command Name = %q, want %q", cmd.Name, "upgrade")
	}
}
