package main

import "os"

// containerTopology holds typed product defaults. Per-launch environment
// inputs take precedence.
type containerTopology struct {
	TailnetNetwork    string
	TailnetProxy      string
	TowerHost         string
	TowerOllamaPort   string
	SubstrateSeed     string
	SubstrateDest     string
	SubstrateManifest string
	SubstrateTTL      string
}

var containerTopologyDefaults = containerTopology{
	TailnetNetwork:    defaultTailnetNetwork,
	TailnetProxy:      defaultTailnetProxy,
	TowerHost:         defaultTowerHost,
	TowerOllamaPort:   defaultTowerOllamaPort,
	SubstrateSeed:     containerSubstrateSeed,
	SubstrateDest:     containerSubstrateDest,
	SubstrateManifest: containerSubstrateManifest,
	SubstrateTTL:      containerSubstrateTTL,
}

func envOrBundleOr(key, value, def string) string {
	if configured := os.Getenv(key); configured != "" {
		return configured
	}
	if value != "" {
		return value
	}
	return def
}

func currentContainerTopology() containerTopology {
	return containerTopologyDefaults
}
