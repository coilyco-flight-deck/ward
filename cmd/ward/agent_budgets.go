package main

import (
	"fmt"
	"time"
)

// agentRoleExecutionLimit resolves the fixed workflow's command-local budget.
func agentRoleExecutionLimit(role string) (time.Duration, bool) {
	limit := fixedRoleExecutionLimit(role)
	if limit <= 0 {
		return 0, false
	}
	return limit, true
}

// agentRunBudgetNote renders the launch-time budget block that gets folded into
// issue seeds and research prompts.
func agentRunBudgetNote(role string) string {
	limit, ok := agentRoleExecutionLimit(role)
	if !ok || limit <= 0 {
		return ""
	}
	defs, err := currentSmartDefaultsWithError()
	if err != nil {
		return ""
	}
	ttl := defs.agentReservationTTL
	if ttl <= 0 {
		ttl = bakedSmartDefaults().agentReservationTTL
	}
	return fmt.Sprintf(
		"\n\nRun budget\n- execution limit: %s\n- reservation TTL: %s\n",
		conciseDuration(limit), conciseDuration(ttl),
	)
}

// agentRunBudgetSummary renders the live countdown shown in `ward agent list`.
func agentRunBudgetSummary(role string, age time.Duration) string {
	limit, ok := agentRoleExecutionLimit(role)
	if !ok || limit <= 0 {
		return ""
	}
	remaining := limit - age
	if remaining > 0 {
		return fmt.Sprintf("%s remaining of %s limit", conciseDuration(remaining), conciseDuration(limit))
	}
	return fmt.Sprintf("expired by %s against %s limit", conciseDuration(-remaining), conciseDuration(limit))
}
