package main

import (
	"errors"
	"fmt"
	"strings"
)

// dispatchPartialLaunchError marks a run that launched a container but could not
// post the reservation marker that keeps the issue visibly reserved.
type dispatchPartialLaunchError struct {
	ref         agentIssueRef
	container   string
	cause       error
	remediation string
}

func newDispatchPartialLaunchError(ref agentIssueRef, container string, cause error) error {
	return &dispatchPartialLaunchError{
		ref:         ref,
		container:   strings.TrimSpace(container),
		cause:       cause,
		remediation: partialLaunchRemediationText(ref, container),
	}
}

func (e *dispatchPartialLaunchError) Error() string {
	if e == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "dispatch broker: partial-launch for %s", e.ref)
	if container := strings.TrimSpace(e.container); container != "" {
		fmt.Fprintf(&b, " (container %s)", container)
	}
	if cause := strings.TrimSpace(firstLine(errorText(e.cause))); cause != "" {
		fmt.Fprintf(&b, ": %s", cause)
	}
	if remediation := strings.TrimSpace(e.remediation); remediation != "" {
		fmt.Fprintf(&b, "; remediation: %s", remediation)
	}
	return b.String()
}

func (e *dispatchPartialLaunchError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func isPartialLaunchError(err error) bool {
	var e *dispatchPartialLaunchError
	return errors.As(err, &e)
}

func partialLaunchRemediationText(ref agentIssueRef, container string) string {
	return fmt.Sprintf(
		"issue %s is missing the reservation-held marker; re-post the reservation comment or stop and re-dispatch %s",
		ref, emptyDefault(strings.TrimSpace(container), "the container"),
	)
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
