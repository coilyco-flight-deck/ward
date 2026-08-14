package main

import (
	"fmt"
	"strconv"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/umbra/pkg/config"
)

func loadOperatorContainerMemoryLimit() (string, error) {
	path, err := config.GlobalConfigPath()
	if err != nil {
		return "", err
	}
	var cfg wardGlobalConfig
	if oerr := config.OverlayFile(&cfg, path); oerr != nil {
		return "", oerr
	}
	return strings.TrimSpace(cfg.Container.MemoryLimit), nil
}

func resolveContainerMemorySettings() (string, string, error) {
	limit, err := resolveContainerMemoryLimit()
	if err != nil {
		return "", "", err
	}
	swap, err := resolveContainerMemorySwap(limit)
	if err != nil {
		return "", "", err
	}
	return limit, swap, nil
}

func resolveContainerMemoryLimit() (string, error) {
	if override, err := loadOperatorContainerMemoryLimit(); err != nil {
		return "", err
	} else if override != "" {
		if _, err := parseDockerMemoryBytes(override); err != nil {
			return "", fmt.Errorf("container.memory-limit: %w", err)
		}
		return override, nil
	}
	return containerMemoryLimitDefault(), nil
}

func resolveContainerMemorySwap(limit string) (string, error) {
	b, err := parseDockerMemoryBytes(limit)
	if err != nil {
		return "", err
	}
	return formatDockerMemoryBytes(b * 2), nil
}

func parseDockerMemoryBytes(raw string) (int64, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return 0, fmt.Errorf("must be non-empty")
	}
	s, mult, err := splitDockerMemorySuffix(s)
	if err != nil {
		return 0, err
	}
	if s == "" {
		return 0, fmt.Errorf("must be positive")
	}
	if strings.ContainsAny(s, ".eE") {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil || f <= 0 {
			return 0, fmt.Errorf("must be a positive memory size")
		}
		return int64(f * float64(mult)), nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("must be a positive memory size")
	}
	return n * mult, nil
}

func splitDockerMemorySuffix(s string) (string, int64, error) {
	mults := map[byte]int64{
		'b': 1,
		'k': 1 << 10,
		'm': 1 << 20,
		'g': 1 << 30,
		't': 1 << 40,
		'p': 1 << 50,
		'e': 1 << 60,
	}
	last := s[len(s)-1]
	if last >= '0' && last <= '9' || last == '.' {
		return s, 1, nil
	}
	mult, ok := mults[last]
	if !ok {
		return "", 0, fmt.Errorf("must be a positive memory size")
	}
	return strings.TrimSpace(s[:len(s)-1]), mult, nil
}

func formatDockerMemoryBytes(bytes int64) string {
	switch {
	case bytes%(1<<40) == 0:
		return fmt.Sprintf("%dt", bytes>>40)
	case bytes%(1<<30) == 0:
		return fmt.Sprintf("%dg", bytes>>30)
	case bytes%(1<<20) == 0:
		return fmt.Sprintf("%dm", bytes>>20)
	case bytes%(1<<10) == 0:
		return fmt.Sprintf("%dk", bytes>>10)
	default:
		return fmt.Sprintf("%db", bytes)
	}
}
