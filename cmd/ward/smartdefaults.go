package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	kdl "github.com/calico32/kdl-go"
	"github.com/urfave/cli/v3"
)

// smartDefaults is the launch-selected runtime policy bundle. It starts from the
// baked neutral default and can be overridden by WARD_CONFIG_REF bundles.
type smartDefaults struct {
	agentReservationTTL           time.Duration
	reservationRecheckDefaultMax  time.Duration
	agentReapIdleDefault          time.Duration
	agentReapMaxCPUDefault        float64
	directorMaxParallel           int
	directorLimit                 int
	directorPollInterval          time.Duration
	reviewerTimeout               time.Duration
	configBundleTTL               time.Duration
	containerAssetsTTL            time.Duration
	containerReadOnlyExtraRepoTTL time.Duration
	containerReapKeep             int
}

var smartDefaultsCache struct {
	sync.Mutex
	ref         string
	initialized bool
	defaults    smartDefaults
	err         error
}

func bakedSmartDefaults() smartDefaults {
	return smartDefaults{
		agentReservationTTL:           time.Hour,
		reservationRecheckDefaultMax:  15 * time.Second,
		agentReapIdleDefault:          time.Hour,
		agentReapMaxCPUDefault:        5.0,
		directorMaxParallel:           10,
		directorLimit:                 50,
		directorPollInterval:          30 * time.Second,
		reviewerTimeout:               8 * time.Minute,
		configBundleTTL:               600 * time.Second,
		containerAssetsTTL:            time.Hour,
		containerReadOnlyExtraRepoTTL: 24 * time.Hour,
		containerReapKeep:             10,
	}
}

// currentSmartDefaults returns the launch-selected runtime policy, caching it by
// WARD_CONFIG_REF so the bundle is parsed once per selection.
func currentSmartDefaults() smartDefaults {
	defs, _ := currentSmartDefaultsWithError()
	return defs
}

func currentSmartDefaultsWithError() (smartDefaults, error) {
	ref := strings.TrimSpace(os.Getenv(wardConfigRefEnv))

	smartDefaultsCache.Lock()
	defer smartDefaultsCache.Unlock()
	if smartDefaultsCache.initialized && smartDefaultsCache.ref == ref {
		return smartDefaultsCache.defaults, smartDefaultsCache.err
	}

	defs := bakedSmartDefaults()
	src, err := selectConfigSource()
	if err == nil {
		defs, err = loadSmartDefaultsFrom(src)
	}
	smartDefaultsCache.ref = ref
	smartDefaultsCache.initialized = true
	smartDefaultsCache.defaults = defs
	smartDefaultsCache.err = err
	return defs, err
}

func smartDefaultsGuard(surface string) cli.BeforeFunc {
	return func(ctx context.Context, c *cli.Command) (context.Context, error) {
		if _, err := currentSmartDefaultsWithError(); err != nil {
			return ctx, fmt.Errorf("%s: %w", surface, err)
		}
		return ctx, nil
	}
}

func loadSmartDefaultsFrom(src configSource) (smartDefaults, error) {
	b, err := fs.ReadFile(src.fsys, src.defaultsKDL)
	if err != nil {
		return smartDefaults{}, fmt.Errorf("read smart defaults %s: %w", src.defaultsKDL, err)
	}
	return parseSmartDefaults(b)
}

func parseSmartDefaults(src []byte) (smartDefaults, error) {
	doc, err := kdl.ParseString(string(src))
	if err != nil {
		return smartDefaults{}, fmt.Errorf("smart defaults: parse KDL: %w", err)
	}
	defs := bakedSmartDefaults()
	seen := false
	for _, n := range doc.Nodes {
		switch n.Name() {
		case "smart-defaults":
			if seen {
				return smartDefaults{}, fmt.Errorf("smart defaults: duplicate top-level `smart-defaults` block (fail-closed)")
			}
			seen = true
			if len(n.Arguments()) != 0 {
				return smartDefaults{}, fmt.Errorf("smart defaults: `smart-defaults` takes no arguments (fail-closed)")
			}
			if len(n.Properties()) != 0 {
				return smartDefaults{}, fmt.Errorf("smart defaults: `smart-defaults` takes no properties (fail-closed)")
			}
			for _, c := range n.Children().Nodes {
				if err := applySmartDefaultNode(&defs, c); err != nil {
					return smartDefaults{}, err
				}
			}
		default:
			return smartDefaults{}, unknownSmartDefaultsNode("top-level", n.Name(), "smart-defaults")
		}
	}
	if !seen {
		return smartDefaults{}, fmt.Errorf("smart defaults: missing top-level `smart-defaults` block (fail-closed)")
	}
	return defs, nil
}

func applySmartDefaultNode(defs *smartDefaults, n *kdl.Node) error {
	switch n.Name() {
	case "agent-reservation-ttl":
		v, err := smartDefaultsDurationArg(n, "smart-defaults > agent-reservation-ttl")
		if err != nil {
			return err
		}
		defs.agentReservationTTL = v
	case "agent-reservation-recheck-max":
		v, err := smartDefaultsDurationArg(n, "smart-defaults > agent-reservation-recheck-max")
		if err != nil {
			return err
		}
		defs.reservationRecheckDefaultMax = v
	case "agent-reap-idle":
		v, err := smartDefaultsDurationArg(n, "smart-defaults > agent-reap-idle")
		if err != nil {
			return err
		}
		defs.agentReapIdleDefault = v
	case "agent-reap-max-cpu":
		v, err := smartDefaultsFloatArg(n, "smart-defaults > agent-reap-max-cpu")
		if err != nil {
			return err
		}
		defs.agentReapMaxCPUDefault = v
	case "director-max-parallel":
		v, err := smartDefaultsIntArg(n, "smart-defaults > director-max-parallel")
		if err != nil {
			return err
		}
		defs.directorMaxParallel = v
	case "director-limit":
		v, err := smartDefaultsIntArg(n, "smart-defaults > director-limit")
		if err != nil {
			return err
		}
		defs.directorLimit = v
	case "director-poll-interval":
		v, err := smartDefaultsDurationArg(n, "smart-defaults > director-poll-interval")
		if err != nil {
			return err
		}
		defs.directorPollInterval = v
	case "reviewer-timeout":
		v, err := smartDefaultsDurationArg(n, "smart-defaults > reviewer-timeout")
		if err != nil {
			return err
		}
		defs.reviewerTimeout = v
	case "config-bundle-ttl":
		v, err := smartDefaultsDurationOrSecondsArg(n, "smart-defaults > config-bundle-ttl")
		if err != nil {
			return err
		}
		defs.configBundleTTL = v
	case "container-assets-ttl":
		v, err := smartDefaultsDurationArg(n, "smart-defaults > container-assets-ttl")
		if err != nil {
			return err
		}
		defs.containerAssetsTTL = v
	case "container-read-only-extra-repo-ttl":
		v, err := smartDefaultsDurationArg(n, "smart-defaults > container-read-only-extra-repo-ttl")
		if err != nil {
			return err
		}
		defs.containerReadOnlyExtraRepoTTL = v
	case "container-reap-keep":
		v, err := smartDefaultsIntArg(n, "smart-defaults > container-reap-keep")
		if err != nil {
			return err
		}
		defs.containerReapKeep = v
	default:
		return unknownSmartDefaultsNode("smart-defaults body", n.Name(),
			"agent-reservation-ttl | agent-reservation-recheck-max | agent-reap-idle | agent-reap-max-cpu | director-max-parallel | director-limit | director-poll-interval | reviewer-timeout | config-bundle-ttl | container-assets-ttl | container-read-only-extra-repo-ttl | container-reap-keep")
	}
	return nil
}

func smartDefaultsStringArg(n *kdl.Node, label string) (string, error) {
	args := n.Arguments()
	if len(args) != 1 {
		return "", fmt.Errorf("smart defaults: %s expects exactly one value, got %d (fail-closed)", label, len(args))
	}
	if args[0].Kind() != kdl.String {
		return "", fmt.Errorf("smart defaults: %s must be a string (fail-closed)", label)
	}
	v := strings.TrimSpace(args[0].String())
	if v == "" {
		return "", fmt.Errorf("smart defaults: %s must be non-empty (fail-closed)", label)
	}
	return v, nil
}

func smartDefaultsDurationArg(n *kdl.Node, label string) (time.Duration, error) {
	raw, err := smartDefaultsStringArg(n, label)
	if err != nil {
		return 0, err
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("smart defaults: %s must be a positive duration (fail-closed)", label)
	}
	return d, nil
}

func smartDefaultsDurationOrSecondsArg(n *kdl.Node, label string) (time.Duration, error) {
	raw, err := smartDefaultsStringArg(n, label)
	if err != nil {
		return 0, err
	}
	if d, err := time.ParseDuration(raw); err == nil {
		if d <= 0 {
			return 0, fmt.Errorf("smart defaults: %s must be positive (fail-closed)", label)
		}
		return d, nil
	}
	sec, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || sec <= 0 {
		return 0, fmt.Errorf("smart defaults: %s must be positive seconds or duration (fail-closed)", label)
	}
	return time.Duration(sec) * time.Second, nil
}

func smartDefaultsFloatArg(n *kdl.Node, label string) (float64, error) {
	raw, err := smartDefaultsStringArg(n, label)
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("smart defaults: %s must be a positive number (fail-closed)", label)
	}
	return v, nil
}

func smartDefaultsIntArg(n *kdl.Node, label string) (int, error) {
	raw, err := smartDefaultsStringArg(n, label)
	if err != nil {
		return 0, err
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("smart defaults: %s must be a positive integer (fail-closed)", label)
	}
	return v, nil
}

func unknownSmartDefaultsNode(where, name, want string) error {
	return fmt.Errorf("smart defaults: %s: unknown node %q (want %s; fail-closed)", where, name, want)
}

func conciseDuration(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", int64(d/time.Hour))
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", int64(d/time.Minute))
	case d%time.Second == 0:
		return fmt.Sprintf("%ds", int64(d/time.Second))
	default:
		return d.String()
	}
}

func agentReservationTTL() time.Duration { return currentSmartDefaults().agentReservationTTL }

func reservationRecheckDefaultMax() time.Duration {
	return currentSmartDefaults().reservationRecheckDefaultMax
}

func agentReapIdleDefault() time.Duration { return currentSmartDefaults().agentReapIdleDefault }

func agentReapMaxCPUDefault() float64 { return currentSmartDefaults().agentReapMaxCPUDefault }

func directorMaxParallelDefault() int { return currentSmartDefaults().directorMaxParallel }

func directorLimitDefault() int { return currentSmartDefaults().directorLimit }

func directorPollIntervalDefault() time.Duration { return currentSmartDefaults().directorPollInterval }

func reviewerTimeoutDefault() time.Duration { return currentSmartDefaults().reviewerTimeout }

func configBundleTTLDefault() time.Duration { return currentSmartDefaults().configBundleTTL }

func containerAssetsTTL() time.Duration { return currentSmartDefaults().containerAssetsTTL }

func containerReadOnlyExtraRepoTTL() time.Duration {
	return currentSmartDefaults().containerReadOnlyExtraRepoTTL
}

func containerReapKeep() int { return currentSmartDefaults().containerReapKeep }
