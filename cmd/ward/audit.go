package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/audit"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/config"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/scope"
	"github.com/urfave/cli/v3"
)

// auditCommand groups the read surface over ward's per-repo audit log.
// See docs/audit.md.
func auditCommand() *cli.Command {
	return &cli.Command{
		Name:  "audit",
		Usage: "Inspect this repo's ward audit log.",
		Description: `audit reads the per-repo JSONL log ward writes under
~/.ward/audit/<slug>.jsonl (slug derived from the repo's origin remote).
'path' prints the resolved file, 'tail' streams records.`,
		Commands: []*cli.Command{
			auditPathCommand(),
			auditTailCommand(),
		},
	}
}

// auditPathCommand prints the resolved audit log path for the current repo.
func auditPathCommand() *cli.Command {
	return &cli.Command{
		Name:  "path",
		Usage: "Print the resolved audit log path and exit.",
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			return r.WrapVerb(verb.Spec{
				Name: "audit.path",
				Action: func(_ context.Context, _ *cli.Command) error {
					path, err := config.DefaultAuditPath()
					if err != nil {
						return fmt.Errorf("audit path: %w", err)
					}
					fmt.Println(path)
					return nil
				},
			}, r.Audit)(ctx, c)
		},
	}
}

// auditTailCommand streams the per-repo audit log as JSONL, optionally
// filtered to a repo scope and a time window.
func auditTailCommand() *cli.Command {
	return &cli.Command{
		Name:  "tail",
		Usage: "Stream this repo's audit records as JSONL.",
		Description: `tail prints existing records and, with --follow, blocks for new
ones. --since accepts unix seconds or a duration ("5m", "1h", "7d").
--scope row-filters by repo_root: a path matches that dir and its
descendants; the literal "." (or "here") resolves to the current git
toplevel so a contributor can restrict to the current repo's rows.`,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "follow",
				Usage: "block waiting for new records after replaying history",
			},
			&cli.StringFlag{
				Name:  "since",
				Usage: "skip records older than this (unix seconds or duration like 5m / 7d)",
			},
			&cli.StringFlag{
				Name:  "scope",
				Usage: "filter by repo_root path (dir + descendants); \".\" or \"here\" = current repo",
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			return r.WrapVerb(verb.Spec{
				Name: "audit.tail",
				ArgsFunc: func(cmd *cli.Command) (map[string]string, []string) {
					return map[string]string{"--since": cmd.String("since"), "--scope": cmd.String("scope")}, nil
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					since, err := parseSince(cmd.String("since"))
					if err != nil {
						return err
					}
					path, err := config.DefaultAuditPath()
					if err != nil {
						return fmt.Errorf("audit tail: %w", err)
					}
					return tailAuditLog(ctx, path, since, resolveScope(cmd.String("scope")), cmd.Bool("follow"))
				},
			}, r.Audit)(ctx, c)
		},
	}
}

// resolveScope turns the --scope flag into a repo_root filter prefix. The
// sentinels "." and "here" resolve to the current git toplevel via scope.
func resolveScope(s string) string {
	switch s {
	case "":
		return ""
	case ".", "here":
		return scope.RepoRoot(scope.CWD())
	default:
		if abs, err := filepath.Abs(s); err == nil {
			return filepath.Clean(abs)
		}
		return filepath.Clean(s)
	}
}

// scopeMatches reports whether a row's repo_root is the filter scope or a
// descendant. The separator boundary stops "/foo" matching "/foobar".
func scopeMatches(recScope, filterScope string) bool {
	if filterScope == "" {
		return true
	}
	if recScope == filterScope {
		return true
	}
	return strings.HasPrefix(recScope, filterScope+string(filepath.Separator))
}

// parseSince accepts unix seconds, a Go duration, or an N"d" days suffix.
// Empty returns 0 (no lower bound). Ported from coily's audit tail.
func parseSince(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n, nil
	}
	if strings.HasSuffix(s, "d") {
		if days, derr := strconv.ParseInt(strings.TrimSuffix(s, "d"), 10, 64); derr == nil {
			return time.Now().Add(-time.Duration(days) * 24 * time.Hour).Unix(), nil
		}
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("--since must be unix seconds or a duration like 5m / 24h / 7d: %w", err)
	}
	return time.Now().Add(-d).Unix(), nil
}

// tailAuditLog streams the on-disk JSONL file, skipping rows older than
// since or outside scope; --follow polls for appends every 200ms.
func tailAuditLog(ctx context.Context, path string, since int64, scopeFilter string, follow bool) error {
	f, err := os.Open(path) //nolint:gosec // resolved via cli-guard/config; reading is the point
	if err != nil {
		if os.IsNotExist(err) {
			if follow {
				return waitForFile(ctx, path, since, scopeFilter)
			}
			return nil
		}
		return fmt.Errorf("audit tail: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	return streamLines(ctx, bufio.NewReader(f), since, scopeFilter, follow)
}

// waitForFile blocks until the audit file exists (under --follow), then
// streams it. Lets `tail --follow` start before the first row is written.
func waitForFile(ctx context.Context, path string, since int64, scopeFilter string) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
		if f, err := os.Open(path); err == nil { //nolint:gosec // config-resolved path
			defer func() { _ = f.Close() }()
			return streamLines(ctx, bufio.NewReader(f), since, scopeFilter, true)
		}
	}
}

// streamLines emits matching JSONL lines from r, polling on EOF when follow.
func streamLines(ctx context.Context, r *bufio.Reader, since int64, scopeFilter string, follow bool) error {
	for {
		line, err := r.ReadString('\n')
		if len(line) > 0 && rowMatches(line, since, scopeFilter) {
			_, _ = io.WriteString(os.Stdout, line)
		}
		switch err {
		case nil:
			continue
		case io.EOF:
			if !follow {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
		default:
			return err
		}
	}
}

// rowMatches applies the since + scope filters to one JSONL line. A line
// that will not parse is passed through rather than silently dropped.
func rowMatches(line string, since int64, scopeFilter string) bool {
	if since == 0 && scopeFilter == "" {
		return true
	}
	var rec audit.Record
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &rec); err != nil {
		return true
	}
	if rec.Timestamp < since {
		return false
	}
	return scopeMatches(filepath.Clean(rec.RepoRoot), scopeFilter)
}
