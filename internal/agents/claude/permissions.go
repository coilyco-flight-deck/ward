package claude

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

type settings struct {
	TUI              string            `json:"tui"`
	DeniedMCPServers []deniedMCPServer `json:"deniedMcpServers"`
	Permissions      permissions       `json:"permissions"`
	StatusLine       *statusLine       `json:"statusLine,omitempty"`
}

type deniedMCPServer struct {
	ServerName string `json:"serverName"`
}

type permissions struct {
	DefaultMode string   `json:"defaultMode"`
	Deny        []string `json:"deny,omitempty"`
}

type statusLine struct {
	Type            string `json:"type"`
	Command         string `json:"command"`
	Padding         int    `json:"padding"`
	RefreshInterval int    `json:"refreshInterval"`
}

// ComposePermissions writes Claude's native policy. No other harness implements
// this capability, so core never creates Claude settings for another adapter.
func (a Agent) ComposePermissions(rc agentsapi.RunCtx) error {
	out := filepath.Join(rc.AgentHome, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	buf, err := composeSettings(record.StatusLine)
	if err != nil {
		return err
	}
	if err := os.WriteFile(out, buf, 0o644); err != nil { // #nosec G306 -- permission policy, not a secret.
		return err
	}
	rc.Log("wrote claude permission policy to %s", out)
	return nil
}

func composeSettings(withStatusLine bool) ([]byte, error) {
	cfg := settings{
		TUI:              "fullscreen",
		DeniedMCPServers: []deniedMCPServer{{ServerName: "claude-in-chrome"}},
		Permissions:      permissions{DefaultMode: "bypassPermissions"},
	}
	if withStatusLine {
		cfg.StatusLine = &statusLine{
			Type: "command", Command: "ward agent dispatch-health --line",
			Padding: 1, RefreshInterval: 5,
		}
	}
	buf, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(buf, '\n'), nil
}
