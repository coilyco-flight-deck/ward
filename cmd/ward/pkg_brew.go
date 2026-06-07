package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/audit"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/egress"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/exitcode"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/verb"
	"github.com/urfave/cli/v3"
)

// pkgBrewCommand is the single brew entry point, ported at parity from
// coily's `coily pkg brew`. Runner built lazily in the Action (newRunner).
func pkgBrewCommand() *cli.Command {
	return &cli.Command{
		Name:  "brew",
		Usage: "Scoped wrapper around brew. Mirrors brew's argv shape.",
		Description: `brew is the single entry point for every brew verb. Mutating verbs
that operate on a named formula default to primary-org tap scope and
require --allow-untapped otherwise. Tap mutations default to
primary-org taps. Touch-everything verbs (cleanup, autoremove,
services cleanup) require --allow-untapped. Read-only verbs, brew
bundle, and brew update pass through verbatim.

  formula-scoped:    install, uninstall, upgrade, reinstall,
                     link, unlink, pin, unpin
  tap-scoped:        tap, untap
  touch-everything:  cleanup, autoremove, services cleanup
  services-scoped:   services start, stop, restart, run, kill
  passthrough:       update, search, info, list, deps, leaves,
                     outdated, doctor, config, commands, bundle,
                     services list, services info, any other`,
		SkipFlagParsing: true,
		Action: func(ctx context.Context, c *cli.Command) error {
			return newRunner().pkgBrewDispatch(ctx, c)
		},
	}
}

// pkgBrewDispatch routes by argv to a scoped action or the passthrough
// fallback, building a verb.Spec with the per-subverb audit name.
func (r *Runner) pkgBrewDispatch(ctx context.Context, c *cli.Command) error {
	raw := c.Args().Slice()
	auditName, action, hook := r.classifyBrewInvocation(raw)
	spec := verb.Spec{
		Name:       auditName,
		SkipPolicy: true,
		ArgsFunc: func(_ *cli.Command) (map[string]string, []string) {
			return nil, raw
		},
		Action:     action,
		OnComplete: hook,
	}
	return r.WrapVerb(spec, r.Audit)(ctx, c)
}

// brewFormulaScopedVerbs are mutating verbs whose positionals are formula
// names; they default-allow primary-org taps.
var brewFormulaScopedVerbs = map[string]bool{
	"install":   true,
	"uninstall": true,
	"upgrade":   true,
	"reinstall": true,
	"link":      true,
	"unlink":    true,
	"pin":       true,
	"unpin":     true,
}

// brewTapScopedVerbs mutate the registered-tap set; the gate is "starts
// with a primary org".
var brewTapScopedVerbs = map[string]bool{
	"tap":   true,
	"untap": true,
}

// brewTouchEverythingVerbs operate on every keg/service; the gate is
// "--allow-untapped was typed".
var brewTouchEverythingVerbs = map[string]bool{
	"cleanup":    true,
	"autoremove": true,
}

// brewServicesFormulaScopedSubs take a formula positional and follow the
// formula-scoped rule.
var brewServicesFormulaScopedSubs = map[string]bool{
	"start":   true,
	"stop":    true,
	"restart": true,
	"run":     true,
	"kill":    true,
}

// brewServicesTouchEverythingSubs touch every service; --allow-untapped
// required.
var brewServicesTouchEverythingSubs = map[string]bool{
	"cleanup": true,
}

// classifyBrewInvocation picks the audit name + action template. The hook
// closure attaches egress + stderr-tail to the audit row after exec.
func (r *Runner) classifyBrewInvocation(raw []string) (string, cli.ActionFunc, func(*audit.Record)) {
	if len(raw) == 0 {
		// Bare `ward pkg brew` -> passthrough (brew prints its top-level help).
		return "pkg.brew", r.brewPassthroughAction(nil), nil
	}
	verbName := raw[0]
	rest := raw[1:]
	switch {
	case verbName == "services":
		return r.classifyBrewServices(rest)
	case brewFormulaScopedVerbs[verbName]:
		action, hook := r.brewFormulaScopedAction([]string{verbName}, rest)
		return "pkg.brew." + verbName, action, hook
	case brewTapScopedVerbs[verbName]:
		action, hook := r.brewTapScopedAction([]string{verbName}, rest)
		return "pkg.brew." + verbName, action, hook
	case brewTouchEverythingVerbs[verbName]:
		action, hook := r.brewTouchEverythingAction([]string{verbName}, rest)
		return "pkg.brew." + verbName, action, hook
	default:
		return "pkg.brew." + verbName, r.brewPassthroughAction(raw), nil
	}
}

// classifyBrewServices is the `services <sub>` arm. Bare `services` and
// `services <unknown>` fall through to passthrough so brew's help fires.
func (r *Runner) classifyBrewServices(rest []string) (string, cli.ActionFunc, func(*audit.Record)) {
	if len(rest) == 0 {
		return "pkg.brew.services", r.brewPassthroughAction([]string{"services"}), nil
	}
	sub := rest[0]
	tail := rest[1:]
	prefix := []string{"services", sub}
	switch {
	case brewServicesFormulaScopedSubs[sub]:
		action, hook := r.brewFormulaScopedAction(prefix, tail)
		return "pkg.brew.services." + sub, action, hook
	case brewServicesTouchEverythingSubs[sub]:
		action, hook := r.brewTouchEverythingAction(prefix, tail)
		return "pkg.brew.services." + sub, action, hook
	default:
		// services list, services info, anything else: passthrough.
		return "pkg.brew.services." + sub, r.brewPassthroughAction(append([]string{"services"}, rest...)), nil
	}
}

// brewFormulaScopedAction scope-checks formula positionals against the
// primary-org taps. prefix is the consumed verb chain; rest is the tail.
func (r *Runner) brewFormulaScopedAction(prefix, rest []string) (cli.ActionFunc, func(*audit.Record)) {
	var rows []audit.EgressRow
	tail := newBrewTail()
	action := func(ctx context.Context, _ *cli.Command) error {
		allow, forward, formulae := splitBrewArgs(rest)
		verbLabel := strings.Join(prefix, " ")
		if !allow {
			// Bare `brew upgrade` (no formula) touches every keg; gate it.
			if len(formulae) == 0 {
				if len(prefix) == 1 && prefix[0] == "upgrade" {
					return exitcode.New(exitcode.PolicyDenied, "policy_denied",
						fmt.Errorf("brew upgrade with no formula upgrades every keg on the system; pass --allow-untapped to confirm"),
						"add --allow-untapped to confirm an upgrade-everything run, or name specific formulae").
						WithReason("bare `brew upgrade` touches every installed keg, turning a single-intent invocation into a global state shift on a long-lived host; --allow-untapped is the explicit opt-in")
				}
				// Fall through: brew will surface its own usage error.
			}
			for _, f := range formulae {
				if !brewInTapScope(f, r.primaryOrgs()) {
					return exitcode.New(exitcode.PolicyDenied, "policy_denied",
						fmt.Errorf("brew %s %q is outside the primary-org taps (%s); pass --allow-untapped to confirm", verbLabel, f, strings.Join(r.primaryOrgs(), ", ")),
						"qualify the formula with a <primary-org>/<tap>/ prefix (e.g. coilysiren/tap/, coilyco-flight-deck/o2r/), or add --allow-untapped to confirm an off-tap formula").
						WithReason("brew state should live under a primary-org tap by default so the install graph is reviewable from first-party repos; --allow-untapped is the explicit opt-out for one-offs")
				}
			}
		}
		captured, execErr := r.execBrew(ctx, prefix, forward, tail)
		rows = captured
		return execErr
	}
	hook := makeBrewHook(&rows, tail)
	return action, hook
}

// brewTapScopedAction scopes `brew tap` / `brew untap` on the tap name.
// Bare `brew tap` (no positional) lists taps and passes through.
func (r *Runner) brewTapScopedAction(prefix, rest []string) (cli.ActionFunc, func(*audit.Record)) {
	var rows []audit.EgressRow
	tail := newBrewTail()
	action := func(ctx context.Context, _ *cli.Command) error {
		allow, forward, positionals := splitBrewArgs(rest)
		verbLabel := strings.Join(prefix, " ")
		if !allow && len(positionals) > 0 {
			for _, t := range positionals {
				if !brewTapPositionalAllowed(t, r.primaryOrgs()) {
					return exitcode.New(exitcode.PolicyDenied, "policy_denied",
						fmt.Errorf("brew %s %q is outside the primary orgs (%s); pass --allow-untapped to confirm", verbLabel, t, strings.Join(r.primaryOrgs(), ", ")),
						"name a <primary-org>/<repo> tap or a forgejo.coilysiren.me/<primary-org>/<repo> URL, or add --allow-untapped to confirm an off-org tap").
						WithReason("brew-tap state should be a primary-org tap by default so the registered-tap graph is reviewable from the fleet; --allow-untapped is the explicit opt-out")
				}
			}
		}
		captured, execErr := r.execBrew(ctx, prefix, forward, tail)
		rows = captured
		return execErr
	}
	hook := makeBrewHook(&rows, tail)
	return action, hook
}

// brewTouchEverythingAction requires --allow-untapped before forwarding
// (cleanup / autoremove / services cleanup touch every item).
func (r *Runner) brewTouchEverythingAction(prefix, rest []string) (cli.ActionFunc, func(*audit.Record)) {
	var rows []audit.EgressRow
	tail := newBrewTail()
	action := func(ctx context.Context, _ *cli.Command) error {
		allow, forward, _ := splitBrewArgs(rest)
		verbLabel := strings.Join(prefix, " ")
		if !allow {
			return exitcode.New(exitcode.PolicyDenied, "policy_denied",
				fmt.Errorf("brew %s touches every installed item in this category; pass --allow-untapped to confirm", verbLabel),
				"add --allow-untapped to confirm a touch-everything run").
				WithReason("touch-everything brew verbs turn a single invocation into a global state shift; --allow-untapped is the explicit opt-in")
		}
		captured, execErr := r.execBrew(ctx, prefix, forward, tail)
		rows = captured
		return execErr
	}
	hook := makeBrewHook(&rows, tail)
	return action, hook
}

// brewPassthroughAction forwards raw argv to brew with no scope check
// (read-only verbs, brew bundle, brew update, unrecognized verbs).
func (r *Runner) brewPassthroughAction(raw []string) cli.ActionFunc {
	var rows []audit.EgressRow
	tail := newBrewTail()
	return func(ctx context.Context, _ *cli.Command) error {
		// --allow-untapped is a ward-side flag; consume it so it never reaches brew.
		_, forward, _ := splitBrewArgs(raw)
		captured, execErr := r.execBrewRaw(ctx, forward, tail)
		rows = captured
		_ = rows
		return execErr
	}
}

// makeBrewHook returns an OnComplete that attaches captured egress rows and
// a stderr tail (on non-zero exit) to the audit row.
func makeBrewHook(rows *[]audit.EgressRow, tail *brewTail) func(*audit.Record) {
	return func(rec *audit.Record) {
		if len(*rows) > 0 {
			rec.Egress = *rows
		}
		if rec.ExitCode != 0 {
			if s := strings.TrimSpace(tail.String()); s != "" {
				rec.StderrTail = s
			}
		}
	}
}

// scopedTapFormulae are the bare formula names the primary-org taps
// publish; default-allowed because brew installs them under the user tap.
var scopedTapFormulae = map[string]bool{
	"coily":         true,
	"ward":          true,
	"repo-recall":   true,
	"arize-phoenix": true,
}

const brewAllowFlag = "--allow-untapped"

// splitBrewArgs walks argv once: pops --allow-untapped from the forwarded
// args and collects non-flag tokens as positionals for the scope check.
func splitBrewArgs(raw []string) (allow bool, forward, positionals []string) {
	forward = make([]string, 0, len(raw))
	positionals = make([]string, 0, len(raw))
	for _, a := range raw {
		if a == brewAllowFlag {
			allow = true
			continue
		}
		forward = append(forward, a)
		if !strings.HasPrefix(a, "-") {
			positionals = append(positionals, a)
		}
	}
	return allow, forward, positionals
}

// brewTapPositionalAllowed accepts a `<primary-org>/<repo>` tap name or a
// forgejo.coilysiren.me/<primary-org>/ source URL.
func brewTapPositionalAllowed(t string, orgs []string) bool {
	for _, o := range orgs {
		if strings.HasPrefix(t, o+"/") {
			return true
		}
		if strings.HasPrefix(t, "https://forgejo.coilysiren.me/"+o+"/") {
			return true
		}
	}
	return false
}

// brewInTapScope is true for a <primary-org>/<tap>/<formula> qualified name
// or a bare formula a primary-org tap publishes.
func brewInTapScope(f string, orgs []string) bool {
	for _, o := range orgs {
		if strings.HasPrefix(f, o+"/") {
			parts := strings.Split(f, "/")
			if len(parts) == 3 && parts[1] != "" && parts[2] != "" {
				return true
			}
		}
	}
	return scopedTapFormulae[f]
}

// execBrew forwards `brew <prefix...> <forward...>` with egress proxy +
// stderr tail wired in.
func (r *Runner) execBrew(ctx context.Context, prefix, forward []string, tail *brewTail) ([]audit.EgressRow, error) {
	full := append([]string{}, prefix...)
	full = append(full, forward...)
	return r.execBrewRaw(ctx, full, tail)
}

// execBrewRaw forwards `brew <argv...>` verbatim under the egress proxy.
// ModeObserve: brew's `go build` egress is wide/unstable, so observe-only.
func (r *Runner) execBrewRaw(ctx context.Context, argv []string, tail *brewTail) ([]audit.EgressRow, error) {
	p := egress.New(nil, egress.ModeObserve)
	proxyURL, err := p.Start(ctx)
	if err != nil {
		return nil, fmt.Errorf("egress: start proxy: %w", err)
	}
	shadow := *r.Runner
	shadow.Env = append([]string(nil), r.Runner.Env...)
	shadow.Env = append(shadow.Env,
		"HTTPS_PROXY="+proxyURL,
		"HTTP_PROXY="+proxyURL,
		"https_proxy="+proxyURL,
		"http_proxy="+proxyURL,
	)
	if r.Runner.Stderr != nil {
		shadow.Stderr = io.MultiWriter(r.Runner.Stderr, tail)
	} else {
		shadow.Stderr = tail
	}
	execErr := shadow.Exec(ctx, "brew", argv...)
	rows := p.Stop()
	return rows, execErr
}

// brewTail is a fixed-size last-N-bytes ring for the stderr tail.
type brewTail struct {
	cap int
	buf []byte
}

func newBrewTail() *brewTail { return &brewTail{cap: audit.MaxStderrTailBytes} }

func (t *brewTail) String() string { return string(t.buf) }

func (t *brewTail) Write(p []byte) (int, error) {
	n := len(p)
	if t.cap <= 0 {
		return n, nil
	}
	if len(p) >= t.cap {
		t.buf = append(t.buf[:0], p[len(p)-t.cap:]...)
		return n, nil
	}
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.cap; over > 0 {
		t.buf = t.buf[over:]
	}
	return n, nil
}
