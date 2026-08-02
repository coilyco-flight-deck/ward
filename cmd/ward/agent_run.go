package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/config"
	"github.com/urfave/cli/v3"
)

// agentRunCommand is the role-agnostic launch surface. The role selects composed
// context only. The command's read-only one-shot lifecycle supplies authority.
func agentRunCommand() *cli.Command {
	flags := agentHarnessFlags()
	flags = append(flags,
		&cli.StringFlag{Name: "role", Required: true, Usage: "safe composed-role slug used to select context"},
		&cli.StringFlag{Name: "agent-id", Hidden: true, Usage: "compatibility override for the broker-minted peer id"},
		&cli.StringFlag{Name: "cluster", Usage: "existing collaboration cluster id to attach to"},
		&cli.StringFlag{Name: "repo", Usage: "owner/repo to clone for read-only context (default: infer from cwd)"},
		configFlag(),
	)
	flags = append(flags, agentImageFlags()...)
	flags = append(flags,
		&cli.BoolFlag{Name: "print", Usage: "render the generic launch plan and run nothing"},
		&cli.BoolFlag{Name: "no-pull", Hidden: true, Usage: "skip the image pull"},
	)
	return &cli.Command{
		Name:      "run",
		Usage:     "Run arbitrary composed context in a read-only one-shot agent.",
		ArgsUsage: "<work>",
		Flags:     flags,
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			mode, err := surfaceDispatchMode(c)
			if err != nil {
				return fmt.Errorf("ward agent run: %w", err)
			}
			return r.WrapVerb(verb.Spec{
				Name:       "agent." + string(mode) + ".run",
				SkipPolicy: true,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return r.runGenericAgent(ctx, cmd, mode)
				},
			}, r.Audit)(ctx, c)
		},
	}
}

func (r *Runner) runGenericAgent(ctx context.Context, c *cli.Command, mode containerMode) error { //nolint:gocyclo,cyclop
	role := strings.TrimSpace(c.String("role"))
	if !validComposedRole(role) {
		return fmt.Errorf("ward agent run: invalid --role %q: want a lowercase slug", role)
	}
	agentID := strings.TrimSpace(c.String("agent-id"))
	if agentID != "" && !validDispatchAgentID(agentID) {
		return fmt.Errorf("ward agent run: invalid --agent-id %q", agentID)
	}
	work := strings.TrimSpace(strings.Join(c.Args().Slice(), " "))
	if work == "" {
		return fmt.Errorf("ward agent run: work text is required")
	}
	if forwarded, err := r.maybeForwardGenericAgent(ctx, c, mode, role, agentID, work); forwarded {
		return err
	}
	clusterID := strings.TrimSpace(c.String("cluster"))
	if clusterID == "" && strings.TrimSpace(os.Getenv(envChildBrokerAddr)) != "" {
		clusterID = strings.TrimSpace(os.Getenv(envClusterID))
	}
	if clusterID != "" && !validClusterID(clusterID) {
		return fmt.Errorf("ward agent run: invalid --cluster %q", clusterID)
	}
	if clusterID != "" && strings.TrimSpace(c.String("repo")) == "" {
		return r.runCollaborationAgent(ctx, c, mode, role, agentID, clusterID, work)
	}

	repo, cwd, err := r.resolveTarget(ctx, strings.TrimSpace(c.String("repo")))
	if err != nil {
		return fmt.Errorf("ward agent run: %w", err)
	}
	if !r.ownerAllowed(repo.Owner) {
		return r.untrustedOwnerErr("ward agent run", repo.Owner)
	}
	assetsDir, cleanupAssets, err := writeContainerAssets(ctx, r, c.String("ward-source"), strings.TrimSpace(c.String("ward-version")))
	if err != nil {
		return fmt.Errorf("ward agent run: %w", err)
	}
	defer cleanupAssets()

	plan, err := buildUpPlan(c, repo, mode, role, cwd, assetsDir, []string{work}, false)
	if err != nil {
		return fmt.Errorf("ward agent run: %w", err)
	}
	plan.ReadOnly = true
	plan.Headless = true
	plan.Interactive = false
	plan.TTY = false
	plan.DispatchRequestID = strings.TrimSpace(os.Getenv(envDispatchRequestID))
	if plan.ClusterID == "" {
		plan.ClusterID = clusterID
	}
	if plan.AgentID == "" {
		plan.AgentID = agentID
	}
	if plan.AgentID == "" {
		plan.AgentID = role + "-" + dictatableID()
	}
	if err := r.maybeAttachRunningDispatchBroker(ctx, &plan); err != nil {
		return fmt.Errorf("ward agent run: %w", err)
	}
	if plan.DispatchBrokerAddr != "" {
		plan.AgentArgs = []string{work + "\n\n" + genericPeerMessagingPrompt(plan.AgentID)}
	}

	if c.Bool("print") {
		out := c.Root().Writer
		if out == nil {
			out = os.Stdout
		}
		return json.NewEncoder(out).Encode(map[string]any{
			"agent_id":  plan.AgentID,
			"role":      plan.Role,
			"repo":      plan.Repo.slug(),
			"read_only": true,
			"command":   dockerCreateArgv(plan, "<env-file>"),
		})
	}
	if err := r.prelaunchDispatch(ctx, c, plan, "ward agent run"); err != nil {
		return err
	}
	launchCreds := r.resolveLaunchCreds(ctx, &plan, mode)
	envFile, cleanupEnv, err := r.writeTokenEnvFile(ctx, planDispatchTarget(plan), plan.Forge, launchCreds)
	if err != nil {
		return fmt.Errorf("ward agent run: %w", err)
	}
	defer cleanupEnv()
	fmt.Fprintf(os.Stderr, "ward agent run: launching %s as role %s on %s (container %s)\n", plan.AgentID, role, repo.slug(), plan.Name)
	if err := r.createAgentContainer(ctx, plan, envFile); err != nil {
		return err
	}
	writef(agentCommandWriter(c), "%s\n", plan.AgentID)
	return nil
}

func (r *Runner) runCollaborationAgent(ctx context.Context, c *cli.Command, mode containerMode, role, agentID, clusterID, work string) error {
	assetsDir, cleanupAssets, err := writeContainerAssets(ctx, r, c.String("ward-source"), strings.TrimSpace(c.String("ward-version")))
	if err != nil {
		return fmt.Errorf("ward agent run: %w", err)
	}
	defer cleanupAssets()
	plan, err := buildCollaborationPlan(c, mode, role, agentID, clusterID, work, assetsDir)
	if err != nil {
		return fmt.Errorf("ward agent run: %w", err)
	}
	if c.Bool("print") {
		stack, err := r.runningDispatchBrokerStack(ctx, plan.ClusterID)
		if err != nil {
			return fmt.Errorf("ward agent run: %w", err)
		}
		previewID := plan.AgentID
		if previewID == "" {
			previewID = role + "-<ab12>"
		}
		plan.AgentID = previewID
		plan.Name = config.SanitizeSlug(previewID + "-" + plan.ClusterID)
		applyDispatchBrokerAttachment(&plan, stack, "<broker-minted-peer-capability>")
		plan.AgentArgs = []string{work + "\n\n" + genericPeerMessagingPrompt(plan.AgentID)}
		return json.NewEncoder(agentCommandWriter(c)).Encode(map[string]any{
			"agent_id":      plan.AgentID,
			"cluster_id":    plan.ClusterID,
			"role":          plan.Role,
			"repository":    nil,
			"collaboration": true,
			"command":       dockerCreateArgv(plan, "<agent-credential-env-file>"),
		})
	}
	if err := r.admitRunningDispatchBrokerPeer(ctx, &plan); err != nil {
		return fmt.Errorf("ward agent run: %w", err)
	}
	plan.AgentArgs = []string{work + "\n\n" + genericPeerMessagingPrompt(plan.AgentID)}
	failAdmission := func(cause error) error {
		if statusErr := r.finishRunningDispatchBrokerPeer(ctx, plan, dispatchPeerStatusFailed); statusErr != nil {
			return fmt.Errorf("%w (also failed to retire broker peer admission: %v)", cause, statusErr)
		}
		return cause
	}
	if err := r.prelaunchDispatch(ctx, c, plan, "ward agent run"); err != nil {
		return failAdmission(err)
	}
	creds := r.resolveLaunchCreds(ctx, &plan, mode)
	envFile, cleanupEnv, err := writeAgentEnvFile(creds)
	if err != nil {
		return failAdmission(fmt.Errorf("ward agent run: write collaboration environment: %w", err))
	}
	defer cleanupEnv()
	fmt.Fprintf(os.Stderr, "ward agent run: launching %s as role %s in cluster %s without a repository target (container %s)\n", plan.AgentID, role, clusterID, plan.Name)
	if err := r.createAgentContainer(ctx, plan, envFile); err != nil {
		return failAdmission(err)
	}
	if err := r.finishRunningDispatchBrokerPeer(ctx, plan, dispatchPeerStatusActive); err != nil {
		return fmt.Errorf("ward agent run: mark broker peer %s active: %w", plan.AgentID, err)
	}
	writef(agentCommandWriter(c), "%s\n", plan.AgentID)
	return nil
}

func buildCollaborationPlan(c *cli.Command, mode containerMode, role, agentID, clusterID, work, assetsDir string) (upPlan, error) {
	if !validClusterID(clusterID) {
		return upPlan{}, fmt.Errorf("invalid collaboration cluster id %q", clusterID)
	}
	contextBundle, err := resolveContextBundle(c.String("context-bundle"), role, mode)
	if err != nil {
		return upPlan{}, err
	}
	if contextBundle.Root == "" {
		return upPlan{}, fmt.Errorf("--context-bundle is required for a repository-free collaboration peer")
	}
	wardVersion, wardVersionSource, err := resolveLaunchWardVersion(c)
	if err != nil {
		return upPlan{}, err
	}
	configEnv, err := resolveLaunchConfigEnv(c.StringSlice("config"), resolveInvokeCWD(), mode)
	if err != nil {
		return upPlan{}, err
	}
	localModel := configEnv["WARD_OPENCODE_MODEL"]
	if mode == modeGoose {
		localModel = configEnv["WARD_GOOSE_MODEL"]
	}
	if err := validateLocalHarnessConfig(mode, localModel, configEnv["WARD_OLLAMA_URL"]); err != nil {
		return upPlan{}, err
	}
	memoryLimit, memorySwap, err := resolveContainerMemorySettings()
	if err != nil {
		return upPlan{}, err
	}
	requestID := strings.TrimSpace(os.Getenv(envDispatchRequestID))
	if requestID == "" {
		requestID = newDispatchBrokerRequestID()
	}
	machine := randHex()
	return upPlan{
		Image:      imageRef(c.String("image"), c.String("tag")),
		Name:       config.SanitizeSlug(emptyDefault(agentID, role+"-pending") + "-" + clusterID),
		Role:       role,
		ConfigRole: role,
		Machine:    machine,
		Mode:       mode,
		Mounts: leastAccessMounts("", mountOpts{
			AssetsDir: assetsDir, WardSource: c.String("ward-source"), ContextBundle: contextBundle.Root,
		}),
		Headless:          true,
		Interactive:       false,
		TTY:               false,
		WardVersion:       wardVersion,
		WardVersionSource: wardVersionSource,
		WardFromSource:    c.String("ward-source") != "",
		MemoryLimit:       memoryLimit,
		MemorySwap:        memorySwap,
		AgentArgs:         []string{work},
		ConfigEnv:         configEnv,
		ContextBundle:     contextBundle.Root,
		ContextTools:      contextBundle.HasTools,
		ClusterID:         clusterID,
		Collaboration:     true,
		AgentID:           agentID,
		DispatchRequestID: requestID,
	}, nil
}

func (r *Runner) runningDispatchBrokerStack(ctx context.Context, clusterID string) (directorStack, error) {
	stack, err := resolveDirectorStack(clusterID)
	if err != nil {
		return directorStack{}, err
	}
	running, err := r.dockerCapture(ctx, "inspect", "--format", "{{.State.Running}}", stack.BrokerName)
	if err != nil || strings.TrimSpace(string(running)) != "true" {
		return directorStack{}, fmt.Errorf("collaboration cluster %s has no running broker", clusterID)
	}
	return stack, nil
}

func (r *Runner) admitRunningDispatchBrokerPeer(ctx context.Context, plan *upPlan) error {
	if plan == nil || !plan.Collaboration || !validClusterID(plan.ClusterID) {
		return fmt.Errorf("broker peer admission requires a repository-free collaboration plan")
	}
	stack, err := r.runningDispatchBrokerStack(ctx, plan.ClusterID)
	if err != nil {
		return err
	}
	args := []string{
		"exec", stack.BrokerName, "/usr/local/bin/ward", "container", "dispatch-broker-peer-admit",
		"--role", plan.Role, "--request-id", plan.DispatchRequestID,
	}
	if plan.AgentID != "" {
		args = append(args, "--agent-id", plan.AgentID)
	}
	out, err := r.dockerCapture(ctx, args...)
	if err != nil {
		return fmt.Errorf("attach to broker %s: admit peer: %w", stack.BrokerName, err)
	}
	var response dispatchBrokerPeerAdmissionResponse
	if err := json.Unmarshal(out, &response); err != nil {
		return fmt.Errorf("attach to broker %s: decode peer admission: %w", stack.BrokerName, err)
	}
	if response.ClusterID != plan.ClusterID || response.RequestID != plan.DispatchRequestID || !validDispatchAgentID(response.PeerID) || response.Capability == "" {
		return fmt.Errorf("attach to broker %s: invalid peer admission response", stack.BrokerName)
	}
	plan.AgentID = response.PeerID
	plan.Name = config.SanitizeSlug(plan.AgentID + "-" + plan.ClusterID)
	applyDispatchBrokerAttachment(plan, stack, response.Capability)
	return nil
}

func (r *Runner) finishRunningDispatchBrokerPeer(ctx context.Context, plan upPlan, status string) error {
	stack, err := resolveDirectorStack(plan.ClusterID)
	if err != nil {
		return err
	}
	return r.dockerExec(ctx,
		"exec", stack.BrokerName, "/usr/local/bin/ward", "container", "dispatch-broker-peer-status",
		"--role", plan.Role, "--request-id", plan.DispatchRequestID, "--agent-id", plan.AgentID, "--status", status,
	)
}

func (r *Runner) maybeAttachRunningDispatchBroker(ctx context.Context, plan *upPlan) error {
	if plan == nil || plan.DispatchBrokerAddr != "" || inContainer() {
		return nil
	}
	if strings.TrimSpace(plan.ClusterID) == "" {
		return nil
	}
	stack, err := r.runningDispatchBrokerStack(ctx, plan.ClusterID)
	if err != nil {
		return err
	}
	capability, err := r.dockerCapture(
		ctx,
		"exec",
		stack.BrokerName,
		"/usr/local/bin/ward",
		"container",
		"dispatch-broker-capability",
		plan.AgentID,
	)
	if err != nil {
		return fmt.Errorf("attach to broker %s: mint peer capability: %w", stack.BrokerName, err)
	}
	applyDispatchBrokerAttachment(plan, stack, strings.TrimSpace(string(capability)))
	return nil
}

func applyDispatchBrokerAttachment(plan *upPlan, stack directorStack, capability string) {
	plan.ClusterID = stack.Project
	plan.DispatchBrokerAddr = dispatchBrokerServiceAddress
	plan.DispatchBrokerToken = capability
	plan.DispatchBrokerNetwork = stack.Project + "_default"
}

func genericPeerMessagingPrompt(agentID string) string {
	return fmt.Sprintf(
		"Peer collaboration is available through Ward. Your authenticated agent id is %s. "+
			"Use `ward agent message send --to <agent-id> <message>` to send and "+
			"`ward agent message receive --json` to read messages. You may launch a read-only peer with "+
			"`ward agent run --role <role> <work>`. The broker returns the new peer id. Composed roles select context only.",
		agentID,
	)
}

func (r *Runner) maybeForwardGenericAgent(ctx context.Context, c *cli.Command, mode containerMode, role, agentID, work string) (bool, error) { //nolint:gocyclo,cyclop
	addr := strings.TrimSpace(os.Getenv(envDispatchBrokerAddr))
	if addr == "" {
		return false, nil
	}
	if err := probeHostDispatchBroker(ctx, addr); err != nil {
		return true, err
	}
	argv := []string{"run", "--role", role}
	if agentID != "" {
		argv = append(argv, "--agent-id", agentID)
	}
	if repo := strings.TrimSpace(c.String("repo")); repo != "" {
		argv = append(argv, "--repo", repo)
	}
	if cluster := strings.TrimSpace(c.String("cluster")); cluster != "" {
		argv = append(argv, "--cluster", cluster)
	}
	argv = append(argv, "--harness", string(brokerDispatchHarness(c, mode)))
	argv = appendBrokerConfigFlags(argv, c)
	if image := strings.TrimSpace(c.String("image")); image != "" {
		argv = append(argv, "--image", image)
	}
	if tag := strings.TrimSpace(c.String("tag")); tag != "" {
		argv = append(argv, "--tag", tag)
	}
	if wardVersion := brokerWardVersion(c); wardVersion != "" {
		argv = append(argv, "--ward-version", wardVersion)
	}
	if bundle := strings.TrimSpace(c.String("context-bundle")); bundle != "" {
		if inContainer() {
			bundle = containerContextBundle
		}
		argv = append(argv, "--context-bundle", bundle)
	}
	if c.Bool("no-pull") {
		argv = append(argv, "--no-pull")
	}
	if c.Bool("print") {
		argv = append(argv, "--print")
	}
	argv = append(argv, work)
	req := dispatchBrokerRequest{
		RequestID: newDispatchBrokerRequestID(),
		Role:      role,
		AgentID:   agentID,
		Argv:      argv,
		Token:     strings.TrimSpace(os.Getenv(envDispatchBrokerToken)),
	}
	resp, err := sendDispatchBrokerLaunchAdmission(ctx, addr, req)
	if err != nil {
		return true, err
	}
	fmt.Fprintln(os.Stderr, dispatchBrokerForwardedLine(argv, resp.LogPath))
	if resp.AgentID != "" {
		writef(agentCommandWriter(c), "%s\n", resp.AgentID)
	}
	return true, nil
}
