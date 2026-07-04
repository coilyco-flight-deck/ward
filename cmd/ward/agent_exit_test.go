package main

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/exitcode"
)

// dispatchContractDoc is the committed page the machine-readable contract lives on.
const dispatchContractDoc = "../../docs/agent-dispatch-contract.md"

// TestDispatchContractDocumented is the ward#485 drift guard: every dispatch exit
// code and every meta.json outcome must appear on the contract page (ward#485).
func TestDispatchContractDocumented(t *testing.T) {
	raw, err := os.ReadFile(dispatchContractDoc)
	if err != nil {
		t.Fatalf("read %s: %v", dispatchContractDoc, err)
	}
	doc := string(raw)

	for _, ec := range dispatchExitCodes {
		if code := fmt.Sprintf("`%d`", ec.Code); !strings.Contains(doc, code) {
			t.Errorf("%s does not document dispatch exit code %s (kind %q)", dispatchContractDoc, code, ec.Kind)
		}
		if !strings.Contains(doc, ec.Kind) {
			t.Errorf("%s does not document dispatch exit-code kind %q (code %d)", dispatchContractDoc, ec.Kind, ec.Code)
		}
	}

	for _, outcome := range reapOutcomeValues {
		if !strings.Contains(doc, "`"+outcome+"`") {
			t.Errorf("%s does not document meta.json outcome %q", dispatchContractDoc, outcome)
		}
	}
}

// TestDispatchExitCodesDistinct guards the contract's core promise: an author can
// tell every refusal class apart by exit code alone (no two share a code).
func TestDispatchExitCodesDistinct(t *testing.T) {
	seen := map[int]string{}
	for _, ec := range dispatchExitCodes {
		if prior, ok := seen[ec.Code]; ok {
			t.Errorf("dispatch exit code %d is shared by %q and %q", ec.Code, prior, ec.Kind)
		}
		seen[ec.Code] = ec.Kind
	}
}

// TestRefusalErrorsCarryDispatchCodes pins that the two headless refusal classes a
// scripted supervisor actually hits are Coded, so main.go exits their code (not 1).
func TestRefusalErrorsCarryDispatchCodes(t *testing.T) {
	r := &Runner{}
	if coded := exitcode.From(r.untrustedOwnerErr("warded", "evilcorp")); coded == nil {
		t.Error("untrustedOwnerErr is not a Coded error; main.go would exit generic 1")
	} else if coded.Code() != dispatchUntrustedOwner {
		t.Errorf("untrustedOwnerErr code = %d, want %d", coded.Code(), dispatchUntrustedOwner)
	}

	conflict := newReservationConflict("issue reserved")
	if coded := exitcode.From(conflict); coded == nil {
		t.Error("reservation conflict is not a Coded error; main.go would exit generic 1")
	} else if coded.Code() != dispatchReservationConflict {
		t.Errorf("reservation conflict code = %d, want %d", coded.Code(), dispatchReservationConflict)
	}
	// The conflict must stay recoverable as a conflict for the in-process director.
	if !isReservationConflict(conflict) {
		t.Error("Coded reservation conflict no longer matches isReservationConflict")
	}

	// The closed-issue re-dispatch guard (ward#600) must carry its own code and read
	// as a terminal decline the director marks failed, not a deferral it retries.
	closed := dispatchDeclineErr(dispatchIssueClosed, "issue-closed", "issue a/b#1 is closed")
	if coded := exitcode.From(closed); coded == nil {
		t.Error("issue-closed decline is not a Coded error; main.go would exit generic 1")
	} else if coded.Code() != dispatchIssueClosed {
		t.Errorf("issue-closed decline code = %d, want %d", coded.Code(), dispatchIssueClosed)
	}
	if !isDispatchDecline(closed) {
		t.Error("issue-closed decline should classify as a director decline (failed, not deferred)")
	}
}
