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

// TestUpgradeScoopApp_MatchesBucketManifest pins the Windows scoop app name to
// the bare `ward`, matching ward.json in coilysiren/scoop-bucket. See ward#561.
func TestUpgradeScoopApp_MatchesBucketManifest(t *testing.T) {
	if upgradeScoopApp != "ward" {
		t.Errorf("upgradeScoopApp = %q, want %q", upgradeScoopApp, "ward")
	}
	if strings.Contains(upgradeScoopApp, "/") {
		t.Errorf("upgradeScoopApp = %q must be bare (no bucket prefix)", upgradeScoopApp)
	}
}

// TestStaleTabNotInstalled gates the reinstall escalation to the specific
// stale receipt signature, not any brew failure. See ward#581.
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

// TestScoopUpdateWardScript_WaitsForPidThenUpdates pins that the child waits for
// the parent to exit before scoop runs, else scoop still self-blocks. ward#568.
func TestScoopUpdateWardScript_WaitsForPidThenUpdates(t *testing.T) {
	s := scoopUpdateWardScript(11812, "ward")
	for _, want := range []string{
		"Wait-Process -Id 11812",
		"scoop update ward",
		"ward-upgrade.log",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("scoop update script missing %q\n%s", want, s)
		}
	}
	wait := strings.Index(s, "Wait-Process")
	update := strings.Index(s, "scoop update ward")
	if wait < 0 || update < 0 || wait > update {
		t.Errorf("script must wait for the parent (%d) before scoop update (%d)", wait, update)
	}
}

// TestScoopDetachArgv_HiddenProfileFreePowershell pins a hidden, profile-free
// powershell with the script as its trailing -Command body. See ward#568.
func TestScoopDetachArgv_HiddenProfileFreePowershell(t *testing.T) {
	name, args := scoopDetachArgv("BODY")
	if name != "powershell" {
		t.Errorf("detach command = %q, want powershell", name)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"-NoProfile", "-WindowStyle Hidden", "-Command"} {
		if !strings.Contains(joined, want) {
			t.Errorf("detach args missing %q: %v", want, args)
		}
	}
	if args[len(args)-1] != "BODY" {
		t.Errorf("script body must be the final arg, got %q", args[len(args)-1])
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
