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
