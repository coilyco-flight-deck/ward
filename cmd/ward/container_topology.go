package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"sync"

	kdl "github.com/calico32/kdl-go"
)

// container_topology.go resolves the staged container-topology middle tier.
// Edge bundle selection stays on the configsource seam; bring-up stays baked.

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

var containerTopologyCache struct {
	sync.Mutex
	initialized bool
	topo        containerTopology
	err         error
}

// envOrBundleOr resolves a container-topology value with the staged precedence:
// host env > runtime value > baked neutral default.
func envOrBundleOr(key, bundle, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	if bundle != "" {
		return bundle
	}
	return def
}

func currentContainerTopology() containerTopology {
	topo, _ := currentContainerTopologyWithError()
	return topo
}

func currentContainerTopologyWithError() (containerTopology, error) {
	containerTopologyCache.Lock()
	defer containerTopologyCache.Unlock()
	if containerTopologyCache.initialized {
		return containerTopologyCache.topo, containerTopologyCache.err
	}

	src := coreRuntimeConfigSource()
	topo, err := loadContainerTopologyFrom(src)
	containerTopologyCache.initialized = true
	containerTopologyCache.topo = topo
	containerTopologyCache.err = err
	return topo, err
}

func loadContainerTopologyFrom(src configSource) (containerTopology, error) {
	topo := containerTopologyDefaults
	b, err := fs.ReadFile(src.fsys, src.topologyKDL)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return topo, nil
		}
		return topo, fmt.Errorf("read container topology %s: %w", src.topologyKDL, err)
	}
	parsed, err := parseContainerTopology(b)
	if err != nil {
		return topo, err
	}
	return parsed, nil
}

func parseContainerTopology(src []byte) (containerTopology, error) { //nolint:funlen,gocognit,gocyclo,cyclop
	doc, err := kdl.ParseString(string(src))
	if err != nil {
		return containerTopology{}, fmt.Errorf("container topology: parse KDL: %w", err)
	}
	topo := containerTopologyDefaults
	seenTopology := false
	for _, n := range doc.Nodes {
		switch n.Name() {
		case "topology":
			if seenTopology {
				return containerTopology{}, fmt.Errorf("container topology: duplicate top-level `topology` block (fail-closed)")
			}
			seenTopology = true
			if len(n.Arguments()) != 0 {
				return containerTopology{}, fmt.Errorf("container topology: `topology` takes no arguments (fail-closed)")
			}
			if len(n.Properties()) != 0 {
				return containerTopology{}, fmt.Errorf("container topology: `topology` takes no properties (fail-closed)")
			}
			for _, c := range n.Children().Nodes {
				switch c.Name() {
				case "tailnet-network":
					v, err := singleTopologyStringArg(c, "topology > tailnet-network")
					if err != nil {
						return containerTopology{}, err
					}
					topo.TailnetNetwork = v
				case "tailnet-proxy":
					v, err := singleTopologyStringArg(c, "topology > tailnet-proxy")
					if err != nil {
						return containerTopology{}, err
					}
					topo.TailnetProxy = v
				case "tower-host":
					v, err := singleTopologyStringArg(c, "topology > tower-host")
					if err != nil {
						return containerTopology{}, err
					}
					topo.TowerHost = v
				case "tower-ollama-port":
					v, err := singleTopologyStringArg(c, "topology > tower-ollama-port")
					if err != nil {
						return containerTopology{}, err
					}
					topo.TowerOllamaPort = v
				case "substrate-seed":
					v, err := singleTopologyStringArg(c, "topology > substrate-seed")
					if err != nil {
						return containerTopology{}, err
					}
					topo.SubstrateSeed = v
				case "substrate-dest":
					v, err := singleTopologyStringArg(c, "topology > substrate-dest")
					if err != nil {
						return containerTopology{}, err
					}
					topo.SubstrateDest = v
				case "substrate-manifest":
					v, err := singleTopologyStringArg(c, "topology > substrate-manifest")
					if err != nil {
						return containerTopology{}, err
					}
					topo.SubstrateManifest = v
				case "substrate-ttl":
					v, err := singleTopologyStringArg(c, "topology > substrate-ttl")
					if err != nil {
						return containerTopology{}, err
					}
					topo.SubstrateTTL = v
				default:
					return containerTopology{}, unknownTopologyNode("topology body", c.Name(), "tailnet-network | tailnet-proxy | tower-host | tower-ollama-port | substrate-seed | substrate-dest | substrate-manifest | substrate-ttl")
				}
			}
		default:
			return containerTopology{}, unknownTopologyNode("top-level", n.Name(), "topology")
		}
	}
	if !seenTopology {
		return containerTopology{}, fmt.Errorf("container topology: missing top-level `topology` block (fail-closed)")
	}
	return topo, nil
}

func singleTopologyStringArg(n *kdl.Node, label string) (string, error) {
	args := n.Arguments()
	if len(args) != 1 {
		return "", fmt.Errorf("container topology: %s expects exactly one value, got %d (fail-closed)", label, len(args))
	}
	if args[0].Kind() != kdl.String {
		return "", fmt.Errorf("container topology: %s must be a string (fail-closed)", label)
	}
	v := args[0].String()
	if strings.TrimSpace(v) == "" {
		return "", fmt.Errorf("container topology: %s must be non-empty (fail-closed)", label)
	}
	return v, nil
}

func unknownTopologyNode(where, name, want string) error {
	return fmt.Errorf("container topology: %s: unknown node %q (want %s; fail-closed)", where, name, want)
}
