package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/config"
	"github.com/urfave/cli/v3"
)

// agent_tab.go is the sidequest spawn seam for `ward agent work <ref> --new-tab`:
// queue a {ref,mode} entry + fire Warp. See docs/agent.md, ward#174.

// Defaults for the agent <-> Warp seam. The agentic-os shim reads from the queue
// dir, the URI fires warp(preview)://(tab_config|launch)/<name>.
const (
	defaultAgentTabLaunchName = "claude-agent-work"
	defaultAgentTabChannel    = "preview"
	defaultAgentTabSurface    = "tab"
)

// agentTabQueueSchemaVersion lets the shim reject queue entries it does not
// understand once the schema evolves. Bump when adding required fields.
const agentTabQueueSchemaVersion = 1

// defaultAgentTabQueueDir derives /tmp/<base>-agent-queue from the consumer's
// app dir (config.BaseName), e.g. /tmp/ward-agent-queue.
func defaultAgentTabQueueDir() string {
	return filepath.Join("/tmp", config.BaseName()+"-agent-queue")
}

// agentTabQueueEntry is the JSON payload --new-tab writes for the shim, which
// reads ref + mode and runs the agent; title is cosmetic (the tab header).
type agentTabQueueEntry struct {
	SchemaVersion int    `json:"schema_version"`
	Ref           string `json:"ref"`
	Mode          string `json:"mode"`
	Title         string `json:"title"`
}

// agentTabChannelScheme maps the --channel flag to the URL scheme that lands in
// that Warp build. `warp://` opens Stable, `warppreview://` opens Preview.
func agentTabChannelScheme(channel string) (string, error) {
	switch channel {
	case "preview":
		return "warppreview", nil
	case "stable":
		return "warp", nil
	default:
		return "", fmt.Errorf("invalid --channel %q (valid values: preview | stable)", channel)
	}
}

// agentTabSurfacePath maps the --surface flag to the URI path segment Warp uses
// to pick a new-tab fire (tab_config) vs a new-window fire (launch).
func agentTabSurfacePath(surface string) (string, error) {
	switch surface {
	case "tab":
		return "tab_config", nil
	case "window":
		return "launch", nil
	default:
		return "", fmt.Errorf("invalid --surface %q (valid values: tab | window)", surface)
	}
}

// agentTabURL builds the warp(preview)://(tab_config|launch)/<name> URL fired by
// `open`. Channel picks the scheme, surface picks the path.
func agentTabURL(channel, surface, launchName string) (string, error) {
	scheme, err := agentTabChannelScheme(channel)
	if err != nil {
		return "", err
	}
	path, err := agentTabSurfacePath(surface)
	if err != nil {
		return "", err
	}
	return scheme + "://" + path + "/" + launchName, nil
}

// agentTabFlags are the --new-tab seam overrides exposed only on the interactive
// `work` surface. They mirror the retired dispatch-interactive flags.
func agentTabFlags() []cli.Flag {
	return []cli.Flag{
		&cli.BoolFlag{Name: "new-tab", Usage: "spawn the work into its own Warp tab (running `ward agent work <ref> --driver <mode>`) instead of launching the container in this terminal - the sidequest spawn path"},
		&cli.StringFlag{Name: "queue-dir", Value: defaultAgentTabQueueDir(), Usage: "override the --new-tab queue directory. Must match the path the Warp shim reads."},
		&cli.StringFlag{Name: "launch-name", Value: defaultAgentTabLaunchName, Usage: "override the Warp config name fired via warp(preview)://(tab_config|launch)/<name>."},
		&cli.StringFlag{Name: "channel", Value: defaultAgentTabChannel, Usage: "Warp channel to fire the --new-tab URL into. `preview` (default) or `stable`."},
		&cli.StringFlag{Name: "surface", Value: defaultAgentTabSurface, Usage: "URI path that picks tab vs window for --new-tab. `tab` (default) or `window`."},
	}
}

// runAgentNewTab queues the resolved ref + mode and fires the Warp URI so a
// fresh tab runs the agent. The ref is pre-validated; --print fires nothing.
func (r *Runner) runAgentNewTab(ctx context.Context, c *cli.Command, mode containerMode, w resolvedWork) error {
	label := agentCmdline(mode, "work") + " --new-tab"
	queueDir := c.String("queue-dir")
	launchName := c.String("launch-name")
	channel := c.String("channel")
	surface := c.String("surface")

	url, err := agentTabURL(channel, surface, launchName)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}

	entry := agentTabQueueEntry{
		SchemaVersion: agentTabQueueSchemaVersion,
		Ref:           w.Ref.String(),
		Mode:          string(mode),
		Title:         w.Title,
	}

	if c.Bool("print") {
		entryJSON, _ := json.MarshalIndent(entry, "", "  ")
		fmt.Printf("# %s --new-tab (--print)\n", agentCmdline(mode, "work"))
		fmt.Printf("issue:       %s\n", w.Ref)
		fmt.Printf("queue-dir:   %s\n", queueDir)
		fmt.Printf("channel:     %s\n", channel)
		fmt.Printf("surface:     %s\n", surface)
		fmt.Printf("tab-command: %s %s\n", agentCmdline(mode, "work"), w.Ref)
		fmt.Printf("warp-url:    %s\n", url)
		fmt.Printf("----- queue entry -----\n%s\n----- end -----\n", string(entryJSON))
		return nil
	}

	queuePath, err := writeAgentTabQueueEntry(queueDir, entry)
	if err != nil {
		return fmt.Errorf("%s: write queue entry under %s: %w", label, queueDir, err)
	}

	if err := r.Runner.Exec(ctx, "open", url); err != nil {
		printAgentTabFallback(mode, w.Ref, url, queuePath, err)
		return nil
	}
	fmt.Fprintf(os.Stderr, "%s: opened a Warp tab for %s (%q)\n", label, w.Ref, w.Title)
	return nil
}

// writeAgentTabQueueEntry atomically writes one JSON file named
// <unix-nanos>-<8hex>.json (mode 0600); the nanos prefix gives the shim FIFO order.
func writeAgentTabQueueEntry(dir string, entry agentTabQueueEntry) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("mkdir queue dir: %w", err)
	}
	payload, err := json.Marshal(entry)
	if err != nil {
		return "", fmt.Errorf("marshal queue entry: %w", err)
	}
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		return "", fmt.Errorf("rand suffix: %w", err)
	}
	name := fmt.Sprintf("%d-%s.json", time.Now().UnixNano(), hex.EncodeToString(suffix))
	finalPath := filepath.Join(dir, name)
	tmpPath := finalPath + ".tmp"
	if err := os.WriteFile(tmpPath, payload, 0o600); err != nil {
		return "", fmt.Errorf("write tmp queue entry: %w", err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("rename tmp to final: %w", err)
	}
	return finalPath, nil
}

// printAgentTabFallback emits the paste-in-a-tab command when the Warp fire
// fails; the queue entry is left in place for the shim to consume on retry.
func printAgentTabFallback(mode containerMode, ref fmt.Stringer, url, queuePath string, openErr error) {
	fmt.Fprintf(os.Stderr, "%s --new-tab: %s did not fire (%v).\n", agentCmdline(mode, "work"), url, openErr)
	fmt.Fprintf(os.Stderr, "Queue entry left at %s for the shim to consume on retry.\n", queuePath)
	fmt.Fprintf(os.Stderr, "Or run this in a new tab yourself:\n\n  %s %s\n\n", agentCmdline(mode, "work"), ref)
}
