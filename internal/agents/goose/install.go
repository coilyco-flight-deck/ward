package goose

import "github.com/coilyco-flight-deck/ward/internal/agentsapi"

// Install is a best-effort no-op for goose. The harness is expected to be
// present already, so bootstrap just gets a required hook to call.
func (a Agent) Install(agentsapi.RunCtx) error { return nil }
