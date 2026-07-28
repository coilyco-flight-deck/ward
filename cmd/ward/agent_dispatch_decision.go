package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"strings"
)

const dispatchDecisionPrefix = "ward dispatch decision"

func logDispatchDecision(w io.Writer, component, checkpoint, format string, args ...any) {
	if w == nil {
		return
	}
	component = strings.TrimSpace(component)
	checkpoint = strings.TrimSpace(checkpoint)
	if component == "" {
		component = "host"
	}
	if checkpoint == "" {
		checkpoint = "checkpoint"
	}
	detail := strings.TrimSpace(fmt.Sprintf(format, args...))
	if detail == "" {
		_, _ = fmt.Fprintf(w, "%s: component=%s checkpoint=%s\n", dispatchDecisionPrefix, component, checkpoint)
		return
	}
	_, _ = fmt.Fprintf(w, "%s: component=%s checkpoint=%s %s\n", dispatchDecisionPrefix, component, checkpoint, detail)
}

func dispatchDecisionWriter(active bool) io.Writer {
	if !active {
		return nil
	}
	return os.Stderr
}

func brokeredDispatchRequestID() string {
	return strings.TrimSpace(os.Getenv(envDispatchRequestID))
}

func brokeredDispatchActive() bool {
	return brokeredDispatchRequestID() != ""
}

func seedSummary(seed string) string {
	lines := 0
	for _, line := range strings.Split(seed, "\n") {
		if strings.TrimSpace(line) != "" {
			lines++
		}
	}
	sum := sha256.Sum256([]byte(seed))
	return fmt.Sprintf("%d bytes, %d non-empty lines, sha256=%x", len(seed), lines, sum[:6])
}

func summarizedSeedLogBlock(seed string, requestID string) string {
	target := "the in-container agent argv"
	if strings.TrimSpace(requestID) != "" {
		target = "the in-container agent argv for dispatch request " + requestID
	}
	return fmt.Sprintf("----- seeded prompt summary -----\nseed omitted from this host decision log; it still rides in %s.\nsummary: %s\n----- end -----\n",
		target, seedSummary(seed))
}
