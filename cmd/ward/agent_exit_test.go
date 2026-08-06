package main

import (
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/exitcode"
)

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
