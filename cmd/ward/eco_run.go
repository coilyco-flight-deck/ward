package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// eco_run.go is the side-effecting seam behind `ward eco test`: booting the local
// throwaway EcoServer and the ssh-backed EcoExecutor. See docs/eco-test.md.

// ecoBootCommand builds the local EcoServer launch (exe, argv, env). Pure so the
// boot wiring is checkable without a Windows Steam install.
func ecoBootCommand(serverDir, worldDir string, mods []string) (exeName string, argv []string, env []string) {
	exe := "EcoServer"
	if runtime.GOOS == "windows" {
		exe = "EcoServer.exe"
	}
	exePath := filepath.Join(serverDir, exe)
	// -nogui runs headless; -storage points at the throwaway world so it is disposable.
	argv = []string{"-nogui"}
	if worldDir != "" {
		argv = append(argv, "-storage="+worldDir)
	}
	env = append(os.Environ(),
		"ECO_SMOKE_TEST=1",
		"ECO_MODS="+strings.Join(mods, ","),
	)
	return exePath, argv, env
}

// bootLocalSmoke launches the throwaway server and returns its log once ready, the
// process exits, or the timeout fires. A non-nil error blocks promote (boot failed).
func (r *Runner) bootLocalSmoke(ctx context.Context, opts ecoTestOptions) (string, error) {
	worldDir, err := os.MkdirTemp("", "ward-eco-smoke-")
	if err != nil {
		return "", fmt.Errorf("create throwaway world dir: %w", err)
	}
	defer os.RemoveAll(worldDir)

	if err := stageModSource(opts.modSrc, opts.serverDir); err != nil {
		return "", fmt.Errorf("stage working-copy mod: %w", err)
	}

	exePath, argv, env := ecoBootCommand(opts.serverDir, worldDir, opts.mods)
	if _, statErr := os.Stat(exePath); statErr != nil {
		return "", fmt.Errorf("EcoServer binary not found at %s: %w", exePath, statErr)
	}

	bootCtx, cancel := context.WithTimeout(ctx, opts.bootTimeout)
	defer cancel()

	cmd := exec.CommandContext(bootCtx, exePath, argv...) // #nosec G204 -- exe under operator-configured serverDir
	cmd.Dir = opts.serverDir
	cmd.Env = env
	var buf lockedBuffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	if err := cmd.Start(); err != nil {
		return buf.String(), fmt.Errorf("start EcoServer: %w", err)
	}

	// Watch the log for a ready marker; once seen, stop the throwaway server.
	sig := defaultSmokeSignals()
	ready := waitForReady(bootCtx, &buf, sig.readyMarkers, 500*time.Millisecond)

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	if ready {
		_ = cmd.Process.Kill()
		<-done
		return buf.String(), nil
	}

	// No ready marker: the process exited on its own or the timeout fired.
	select {
	case werr := <-done:
		if werr != nil {
			return buf.String(), fmt.Errorf("EcoServer exited before ready: %w", werr)
		}
		return buf.String(), fmt.Errorf("EcoServer exited before reaching a ready marker")
	case <-bootCtx.Done():
		_ = cmd.Process.Kill()
		<-done
		return buf.String(), fmt.Errorf("EcoServer did not reach ready within %s", opts.bootTimeout)
	}
}

// waitForReady polls buf for any ready marker until ctx is done, returning true as
// soon as one appears. The pure analyzeSmoke does the authoritative verdict.
func waitForReady(ctx context.Context, buf *lockedBuffer, markers []string, poll time.Duration) bool {
	tick := time.NewTicker(poll)
	defer tick.Stop()
	for {
		if scanReady(buf.String(), markers) {
			return true
		}
		select {
		case <-ctx.Done():
			return scanReady(buf.String(), markers)
		case <-tick.C:
		}
	}
}

// scanReady reports whether any log line matches a ready marker.
func scanReady(log string, markers []string) bool {
	for _, line := range strings.Split(log, "\n") {
		if matchAny(line, markers) {
			return true
		}
	}
	return false
}

// stageModSource overlays the working-copy Mods/ subtree onto the install for the
// smoke boot (never deletes); a no-op when modSrc has no Mods/ subtree.
func stageModSource(modSrc, serverDir string) error {
	if strings.TrimSpace(modSrc) == "" || strings.TrimSpace(serverDir) == "" {
		return nil
	}
	srcMods := filepath.Join(modSrc, "Mods")
	info, err := os.Stat(srcMods)
	if err != nil {
		//nolint:nilerr // no working-copy Mods/ to stage is a legit no-op
		return nil
	}
	if !info.IsDir() {
		return nil
	}
	return copyTree(srcMods, filepath.Join(serverDir, "Mods"))
}

// copyTree overlays src onto dst, copying files (never deleting).
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

// copyFile copies a single file, creating parent dirs.
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src) // #nosec G304 -- operator-supplied working-copy mod path
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst) // #nosec G304 -- staging under operator-configured serverDir
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// lockedBuffer is a concurrency-safe bytes.Buffer: the child's output goroutine
// writes while waitForReady reads.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// --- ssh-backed live executor -------------------------------------------------

// ecoSSHExecutor drives the infrastructure-repo promote scripts on kai-server over
// ssh; it is the production EcoExecutor (tests use a fake).
type ecoSSHExecutor struct {
	r    *Runner
	opts ecoTestOptions
}

func (r *Runner) newEcoSSHExecutor(opts ecoTestOptions) *ecoSSHExecutor {
	return &ecoSSHExecutor{r: r, opts: opts}
}

// script returns the remote path of an infra promote script.
func (e *ecoSSHExecutor) script(name string) string {
	return e.opts.scriptsDir + "/" + name
}

// ssh runs argv on the kai-server host and returns captured stdout.
func (e *ecoSSHExecutor) ssh(ctx context.Context, argv ...string) (string, error) {
	full := append([]string{e.opts.kaiHost}, argv...)
	out, err := e.r.Runner.Capture(ctx, "ssh", full...)
	return string(out), err
}

// Snapshot runs eco-server-snapshot.sh and returns the snapshot id (last stdout line).
func (e *ecoSSHExecutor) Snapshot(ctx context.Context) (string, error) {
	out, err := e.ssh(ctx, "bash", e.script("eco-server-snapshot.sh"), e.opts.remoteDir, e.opts.snapshotRoot)
	if err != nil {
		return "", fmt.Errorf("snapshot script: %w", err)
	}
	id := lastNonEmptyLine(out)
	if id == "" {
		return "", fmt.Errorf("snapshot script printed no snapshot id")
	}
	return id, nil
}

// ApplyMod runs install-eco-mod.sh per required mod; a no-op with no mods (config-only).
func (e *ecoSSHExecutor) ApplyMod(ctx context.Context) error {
	for _, mod := range e.opts.mods {
		mod = strings.TrimSpace(mod)
		if mod == "" {
			continue
		}
		if _, err := e.ssh(ctx, "bash", e.script("install-eco-mod.sh"), mod); err != nil {
			return fmt.Errorf("install-eco-mod %s: %w", mod, err)
		}
	}
	return nil
}

// Restart restarts the eco-server unit; the command is overridable per host.
func (e *ecoSSHExecutor) Restart(ctx context.Context) error {
	restart := envOr("WARD_ECO_RESTART_CMD", "sudo systemctl restart eco-server.service")
	if _, err := e.ssh(ctx, restart); err != nil {
		return fmt.Errorf("restart eco-server: %w", err)
	}
	return nil
}

// Probe runs eco-server-health-check.sh and parses its key=val output. A non-zero
// exit with output is a normal degraded signal, not a transport failure.
func (e *ecoSSHExecutor) Probe(ctx context.Context) (ecoHealth, error) {
	out, err := e.ssh(ctx, "bash", e.script("eco-server-health-check.sh"), e.opts.remoteDir)
	h := parseHealthOutput(out)
	h.Raw = out
	if err != nil && strings.TrimSpace(out) == "" {
		return h, fmt.Errorf("health-check script: %w", err)
	}
	return h, nil
}

// Rollback runs eco-server-rollback.sh (restore snapshot + restart).
func (e *ecoSSHExecutor) Rollback(ctx context.Context, snapshotID string) error {
	if strings.TrimSpace(snapshotID) == "" {
		return errNoSnapshot
	}
	if _, err := e.ssh(ctx, "bash", e.script("eco-server-rollback.sh"), snapshotID, e.opts.remoteDir); err != nil {
		return fmt.Errorf("rollback script: %w", err)
	}
	return nil
}

// parseHealthOutput reads `key=0|1` tokens (service_active, journal_clean,
// server_ready) from the health-check script's stdout into an ecoHealth.
func parseHealthOutput(out string) ecoHealth {
	var h ecoHealth
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		for _, tok := range strings.Fields(sc.Text()) {
			key, val, ok := strings.Cut(tok, "=")
			if !ok {
				continue
			}
			set := val == "1" || strings.EqualFold(val, "true") || strings.EqualFold(val, "yes")
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "service_active":
				h.ServiceActive = set
			case "journal_clean":
				h.JournalClean = set
			case "server_ready":
				h.ServerReady = set
			}
		}
	}
	return h
}

// lastNonEmptyLine returns the final non-blank line of s, trimmed.
func lastNonEmptyLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			return t
		}
	}
	return ""
}
