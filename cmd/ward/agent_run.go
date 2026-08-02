package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
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
	clusterID := strings.TrimSpace(c.String("cluster"))
	if clusterID != "" && !validClusterID(clusterID) {
		return fmt.Errorf("ward agent run: invalid --cluster %q", clusterID)
	}
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

func (r *Runner) maybeAttachRunningDispatchBroker(ctx context.Context, plan *upPlan) error {
	if plan == nil || plan.DispatchBrokerAddr != "" || inContainer() {
		return nil
	}
	if strings.TrimSpace(plan.ClusterID) == "" {
		return nil
	}
	stack, err := resolveDirectorStack(plan.ClusterID)
	if err != nil {
		return err
	}
	running, err := r.dockerCapture(ctx, "inspect", "--format", "{{.State.Running}}", stack.BrokerName)
	brokerIsRunning := err == nil && strings.TrimSpace(string(running)) == "true"
	if !brokerIsRunning {
		return nil
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
