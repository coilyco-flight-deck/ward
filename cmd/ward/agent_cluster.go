package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/config"
	"github.com/urfave/cli/v3"
)

type collaborationClusterContainer struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Cluster string `json:"cluster"`
	Role    string `json:"role"`
	Harness string `json:"harness"`
	PeerID  string `json:"peer_id,omitempty"`
}

func agentClusterCommand() *cli.Command {
	return &cli.Command{
		Name:  "cluster",
		Usage: "Launch and manage repository-independent collaboration clusters.",
		Commands: []*cli.Command{
			agentClusterStartCommand(),
			agentClusterListCommand(),
			agentClusterStatusCommand(),
			agentClusterLogsCommand(),
			agentClusterStopCommand(),
		},
	}
}

func agentClusterStartCommand() *cli.Command {
	flags := agentHarnessFlags()
	flags = append(flags, agentImageFlags()...)
	flags = append(flags, &cli.BoolFlag{Name: "print", Usage: "render the broker-only cluster plan and run nothing"})
	return &cli.Command{
		Name:  "start",
		Usage: "Start a supervised broker-only collaboration cluster and print its id.",
		Flags: flags,
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			return r.WrapVerb(verb.Spec{
				Name:       "agent.cluster.start",
				SkipPolicy: true,
				Action:     func(ctx context.Context, cmd *cli.Command) error { return r.runClusterStart(ctx, cmd) },
			}, r.Audit)(ctx, c)
		},
	}
}

func agentClusterListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List supervised collaboration clusters.",
		Flags: []cli.Flag{&cli.BoolFlag{Name: "json", Usage: "emit JSON"}},
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			return r.WrapVerb(verb.Spec{
				Name:       "agent.cluster.list",
				SkipPolicy: true,
				Action:     func(ctx context.Context, cmd *cli.Command) error { return r.runClusterList(ctx, cmd) },
			}, r.Audit)(ctx, c)
		},
	}
}

func agentClusterStatusCommand() *cli.Command {
	return &cli.Command{
		Name:      "status",
		Usage:     "Show every broker, director, and peer in one cluster.",
		ArgsUsage: "<cluster-id>",
		Flags:     []cli.Flag{&cli.BoolFlag{Name: "json", Usage: "emit JSON"}},
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			return r.WrapVerb(verb.Spec{
				Name:       "agent.cluster.status",
				SkipPolicy: true,
				Action:     func(ctx context.Context, cmd *cli.Command) error { return r.runClusterStatus(ctx, cmd) },
			}, r.Audit)(ctx, c)
		},
	}
}

func agentClusterLogsCommand() *cli.Command {
	return &cli.Command{
		Name:      "logs",
		Usage:     "Read logs for every container in one cluster.",
		ArgsUsage: "<cluster-id>",
		Flags:     []cli.Flag{&cli.IntFlag{Name: "tail", Value: 100, Usage: "lines per container"}},
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			return r.WrapVerb(verb.Spec{
				Name:       "agent.cluster.logs",
				SkipPolicy: true,
				Action:     func(ctx context.Context, cmd *cli.Command) error { return r.runClusterLogs(ctx, cmd) },
			}, r.Audit)(ctx, c)
		},
	}
}

func agentClusterStopCommand() *cli.Command {
	return &cli.Command{
		Name:      "stop",
		Usage:     "Stop and remove exactly one collaboration cluster.",
		ArgsUsage: "<cluster-id>",
		Flags:     []cli.Flag{&cli.BoolFlag{Name: "print", Usage: "show the scoped cleanup without changing state"}},
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			return r.WrapVerb(verb.Spec{
				Name:       "agent.cluster.stop",
				SkipPolicy: true,
				Action:     func(ctx context.Context, cmd *cli.Command) error { return r.runClusterStop(ctx, cmd) },
			}, r.Audit)(ctx, c)
		},
	}
}

func (r *Runner) runClusterStart(ctx context.Context, c *cli.Command) error {
	mode, err := agentHarness(c)
	if err != nil {
		return fmt.Errorf("ward agent cluster start: %w", err)
	}
	clusterID := mintClusterID(mode)
	stack, err := prepareDirectorStackAssets(ctx, clusterID, c.String("ward-source"), strings.TrimSpace(c.String("ward-version")))
	if err != nil {
		return err
	}
	plan, err := brokerOnlyClusterPlan(c, mode, stack)
	if err != nil {
		return fmt.Errorf("ward agent cluster start: %w", err)
	}
	if c.Bool("print") {
		return json.NewEncoder(agentCommandWriter(c)).Encode(map[string]any{
			"cluster_id": clusterID,
			"harness":    mode,
			"project":    stack.Project,
			"services":   []string{"broker"},
		})
	}
	creds := r.resolveDirectorStackCreds(ctx, &plan, mode)
	envFile, cleanupEnv, err := writeAgentEnvFile(creds)
	if err != nil {
		return fmt.Errorf("ward agent cluster start: write broker launch environment: %w", err)
	}
	defer cleanupEnv()
	if err := r.runBrokerOnlyCluster(ctx, plan, stack, envFile); err != nil {
		return err
	}
	writef(agentCommandWriter(c), "%s\n", clusterID)
	return nil
}

func brokerOnlyClusterPlan(c *cli.Command, mode containerMode, stack directorStack) (upPlan, error) {
	wardVersion, wardVersionSource, err := resolveLaunchWardVersion(c)
	if err != nil {
		return upPlan{}, err
	}
	memoryLimit, memorySwap, err := resolveContainerMemorySettings()
	if err != nil {
		return upPlan{}, err
	}
	mounts := leastAccessMounts("", mountOpts{AssetsDir: stack.AssetsDir, WardSource: c.String("ward-source")})
	mounts = append(mounts, dockerSockMount())
	return upPlan{
		Image:             imageRef(c.String("image"), c.String("tag")),
		Name:              stack.BrokerName,
		Role:              "broker",
		Mode:              mode,
		ForgejoBase:       forgejoBaseURL,
		Mounts:            mounts,
		WardVersion:       wardVersion,
		WardVersionSource: wardVersionSource,
		WardFromSource:    c.String("ward-source") != "",
		MemoryLimit:       memoryLimit,
		MemorySwap:        memorySwap,
		ClusterID:         stack.Project,
	}, nil
}

func (r *Runner) runBrokerOnlyCluster(ctx context.Context, plan upPlan, stack directorStack, sourceEnvFile string) error {
	token, err := newDispatchBrokerToken()
	if err != nil {
		return fmt.Errorf("ward collaboration cluster: mint broker token: %w", err)
	}
	if err := appendLaunchEnvSecret(sourceEnvFile, envDispatchBrokerToken, token); err != nil {
		return fmt.Errorf("ward collaboration cluster: write broker token: %w", err)
	}
	envBody, err := os.ReadFile(sourceEnvFile) // #nosec G304 -- Ward-owned launch environment.
	if err != nil {
		return fmt.Errorf("ward collaboration cluster: read broker environment: %w", err)
	}
	if err := writePrivateFile(stack.EnvPath, envBody); err != nil {
		return fmt.Errorf("ward collaboration cluster: persist broker environment: %w", err)
	}
	defer cleanupDirectorStackEnvFiles(stack)
	globalDir, err := config.GlobalDir()
	if err != nil {
		return fmt.Errorf("ward collaboration cluster: resolve persistent state: %w", err)
	}
	body, err := renderBrokerOnlyStackCompose(plan, stack, stack.EnvPath, globalDir)
	if err != nil {
		return err
	}
	if err := os.WriteFile(stack.ComposePath, body, 0o600); err != nil {
		return fmt.Errorf("ward collaboration cluster: write Compose file: %w", err)
	}
	if err := r.dockerExec(ctx, "compose", "version"); err != nil {
		return fmt.Errorf("ward collaboration cluster: Docker Compose plugin is required: %w", err)
	}
	commands := directorStackComposeArgs(stack)
	if err := r.dockerExec(ctx, commands.BrokerUp...); err != nil {
		return fmt.Errorf("ward collaboration cluster: start broker: %w", err)
	}
	return nil
}

func (r *Runner) runClusterList(ctx context.Context, c *cli.Command) error {
	containers, err := r.clusterContainers(ctx, "", true)
	if err != nil {
		return fmt.Errorf("ward agent cluster list: %w", err)
	}
	return writeClusterContainers(c, containers, c.Bool("json"))
}

func (r *Runner) runClusterStatus(ctx context.Context, c *cli.Command) error {
	clusterID, err := requiredClusterArg("status", c.Args().First())
	if err != nil {
		return err
	}
	containers, err := r.clusterContainers(ctx, clusterID, false)
	if err != nil {
		return fmt.Errorf("ward agent cluster status: %w", err)
	}
	if len(containers) == 0 {
		return fmt.Errorf("ward agent cluster status: cluster %s is not running", clusterID)
	}
	return writeClusterContainers(c, containers, c.Bool("json"))
}

func (r *Runner) runClusterLogs(ctx context.Context, c *cli.Command) error {
	clusterID, err := requiredClusterArg("logs", c.Args().First())
	if err != nil {
		return err
	}
	tail := c.Int("tail")
	if tail < 0 {
		return fmt.Errorf("ward agent cluster logs: --tail must be non-negative")
	}
	containers, err := r.clusterContainers(ctx, clusterID, false)
	if err != nil {
		return fmt.Errorf("ward agent cluster logs: %w", err)
	}
	if len(containers) == 0 {
		return fmt.Errorf("ward agent cluster logs: cluster %s is not running", clusterID)
	}
	for _, container := range containers {
		writef(r.Runner.Stderr, "===== ward agent cluster logs: %s =====\n", container.Name)
		if err := r.dockerExec(ctx, "logs", "--tail", strconv.Itoa(tail), container.Name); err != nil {
			return fmt.Errorf("ward agent cluster logs: %s: %w", container.Name, err)
		}
	}
	return nil
}

func (r *Runner) runClusterStop(ctx context.Context, c *cli.Command) error { //nolint:gocyclo,cyclop // exact-scope cleanup reports each independently recoverable failure
	clusterID, err := requiredClusterArg("stop", c.Args().First())
	if err != nil {
		return err
	}
	containers, err := r.clusterContainers(ctx, clusterID, false)
	if err != nil {
		return fmt.Errorf("ward agent cluster stop: %w", err)
	}
	if len(containers) == 0 {
		return fmt.Errorf("ward agent cluster stop: cluster %s is not running", clusterID)
	}
	names := make([]string, 0, len(containers))
	for _, container := range containers {
		names = append(names, container.Name)
	}
	sort.Strings(names)
	if c.Bool("print") {
		writef(agentCommandWriter(c), "cluster %s: would stop and remove %s\n", clusterID, strings.Join(names, ", "))
		return nil
	}
	stopArgs := append([]string{"stop", "--time", "30"}, names...)
	if err := r.dockerExec(ctx, stopArgs...); err != nil {
		return fmt.Errorf("ward agent cluster stop: stop %s: %w", clusterID, err)
	}
	removeArgs := append([]string{"rm"}, names...)
	if err := r.dockerExec(ctx, removeArgs...); err != nil {
		return fmt.Errorf("ward agent cluster stop: remove %s: %w", clusterID, err)
	}
	stack, err := resolveDirectorStack(clusterID)
	if err != nil {
		return err
	}
	if err := r.dockerExec(ctx, "compose", "-p", clusterID, "-f", stack.ComposePath, "down", "--remove-orphans"); err != nil {
		return fmt.Errorf("ward agent cluster stop: remove Compose project %s: %w", clusterID, err)
	}
	if err := removeClusterStatePath(stack.Dir); err != nil {
		return fmt.Errorf("ward agent cluster stop: remove state for %s: %w", clusterID, err)
	}
	peerRegistry, err := dispatchPeerAdmissionsPath(clusterID)
	if err != nil {
		return err
	}
	if err := os.Remove(peerRegistry); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("ward agent cluster stop: remove peer registry for %s: %w", clusterID, err)
	}
	writef(agentCommandWriter(c), "%s\n", clusterID)
	return nil
}

func requiredClusterArg(action, raw string) (string, error) {
	clusterID := strings.TrimSpace(raw)
	if !validClusterID(clusterID) {
		return "", fmt.Errorf("ward agent cluster %s: a canonical cluster id is required", action)
	}
	return clusterID, nil
}

func clusterDockerListArgs(clusterID string, brokersOnly bool) []string {
	args := []string{"ps", "-a", "--filter", "label=" + labelCluster}
	if clusterID != "" {
		args = []string{"ps", "-a", "--filter", "label=" + labelCluster + "=" + clusterID}
	}
	if brokersOnly {
		args = append(args, "--filter", "label="+labelRole+"=broker")
	}
	return append(args, "--format", `{{.Names}}\t{{.Status}}\t{{.Label "ward.cluster"}}\t{{.Label "ward.role"}}\t{{.Label "ward.driver"}}\t{{.Label "ward.peer"}}`)
}

func (r *Runner) clusterContainers(ctx context.Context, clusterID string, brokersOnly bool) ([]collaborationClusterContainer, error) {
	out, err := r.dockerCapture(ctx, clusterDockerListArgs(clusterID, brokersOnly)...)
	if err != nil {
		return nil, err
	}
	var containers []collaborationClusterContainer
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 6)
		if len(parts) != 6 || !validClusterID(strings.TrimSpace(parts[2])) {
			return nil, fmt.Errorf("unexpected Docker cluster row %q", line)
		}
		containers = append(containers, collaborationClusterContainer{
			Name: strings.TrimSpace(parts[0]), Status: strings.TrimSpace(parts[1]),
			Cluster: strings.TrimSpace(parts[2]), Role: strings.TrimSpace(parts[3]),
			Harness: strings.TrimSpace(parts[4]), PeerID: strings.TrimSpace(parts[5]),
		})
	}
	sort.Slice(containers, func(i, j int) bool {
		if containers[i].Cluster == containers[j].Cluster {
			return containers[i].Name < containers[j].Name
		}
		return containers[i].Cluster < containers[j].Cluster
	})
	return containers, nil
}

func writeClusterContainers(c *cli.Command, containers []collaborationClusterContainer, jsonOut bool) error {
	w := agentCommandWriter(c)
	if jsonOut {
		return json.NewEncoder(w).Encode(containers)
	}
	for _, container := range containers {
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", container.Cluster, container.Role, container.Harness, container.PeerID, container.Status, container.Name); err != nil {
			return err
		}
	}
	return nil
}

func removeClusterStatePath(path string) error {
	if filepath.Base(path) == "." || filepath.Base(path) == string(filepath.Separator) {
		return fmt.Errorf("refusing broad cluster state path %q", path)
	}
	return os.RemoveAll(path)
}
