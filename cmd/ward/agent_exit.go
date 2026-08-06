package main

import (
	"fmt"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/exitcode"
)

// agent_exit.go is the machine-readable dispatch contract for `ward agent` /
// `warded` (ward#485). See docs/agent-dispatch-broker.md.

// Dispatch exit codes. main.go routes any exitcode.Coded error to its Code().
// 0/1/2 track the cli-guard contract; 3-5 are ward-agent-specific.
const (
	// dispatchLaunched: a container detached (or --print / fail-open pre-flight).
	dispatchLaunched = exitcode.Success
	// dispatchLaunchFailure: bring-up failed, issue unresolved, or a bare subprocess code.
	dispatchLaunchFailure = exitcode.Generic
	// dispatchUntrustedOwner: the owner trust gate refused the ref (agent-lifecycle.md).
	dispatchUntrustedOwner = exitcode.PolicyDenied
	// dispatchReservationConflict: another live run holds the issue (reservation).
	dispatchReservationConflict = 3
	// dispatchNoGo: interactive pre-flight NO-GO, or a bounced WRONG-REPO (pre-flight).
	dispatchNoGo = 4
	// dispatchWrongRepo: interactive pre-flight blind-fired elsewhere (agent-lifecycle.md).
	dispatchWrongRepo = 5
	// dispatchIssueClosed: the target issue is already closed, so nothing launched -
	// the re-dispatch guard against an already-landed issue (ward#600).
	dispatchIssueClosed = 6
	// dispatchModeCeiling: the issue is explicitly labeled interactive, so the
	// code-landing dispatch is refused (ward#663).
	dispatchModeCeiling = 7
)

// dispatchExitCode pairs a code with a stable kind token the drift test matches.
type dispatchExitCode struct {
	Code int
	Kind string
}

// dispatchExitCodes is the drift-test source of truth for the contract page,
// in code order; a new code without a documented row fails the build.
var dispatchExitCodes = []dispatchExitCode{
	{dispatchLaunched, "launched"},
	{dispatchLaunchFailure, "launch-failure"},
	{dispatchUntrustedOwner, "untrusted-owner"},
	{dispatchReservationConflict, "reservation-conflict"},
	{dispatchNoGo, "no-go"},
	{dispatchWrongRepo, "wrong-repo"},
	{dispatchIssueClosed, "issue-closed"},
	{dispatchModeCeiling, "mode-ceiling"},
}

// dispatchDeclineErr builds a Coded refusal so main.go exits with code: the
// pre-flight declined to launch (NO-GO / WRONG-REPO), not a run-time failure.
func dispatchDeclineErr(code int, kind, format string, args ...any) error {
	return exitcode.New(code, kind, fmt.Errorf(format, args...), "")
}

// isDispatchDecline reports whether err is a stable per-issue refusal rather
// than an infrastructure or launch failure.
func isDispatchDecline(err error) bool {
	coded := exitcode.From(err)
	if coded == nil {
		return false
	}
	switch coded.Code() {
	case dispatchNoGo, dispatchWrongRepo, dispatchUntrustedOwner, dispatchIssueClosed, dispatchModeCeiling:
		return true
	default:
		return false
	}
}
