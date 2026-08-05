package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/config"
	"gopkg.in/yaml.v3"
)

const (
	directorStacksSubdir = "clusters"
	directorStackFile    = "compose.yaml"
	directorStackEnvFile = "launch.env"
	directorAgentEnvFile = "director.env"
	directorStackAssets  = "assets"
)

var clusterIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*-[abcdefghjkmpqrstuvwxyz]{2}[456789]{2}$`)
var clusterIDSuffix = dictatableID

type directorStack struct {
	Project         string
	Dir             string
	ComposePath     string
	EnvPath         string
	DirectorEnvPath string
	AssetsDir       string
	BrokerName      string
}

type composeDocument struct {
	Services map[string]composeService  `yaml:"services"`
	Volumes  map[string]composeExternal `yaml:"volumes,omitempty"`
	Networks map[string]composeExternal `yaml:"networks,omitempty"`
}

type composeService struct {
	Image         string            `yaml:"image"`
	ContainerName string            `yaml:"container_name,omitempty"`
	Restart       string            `yaml:"restart,omitempty"`
	Entrypoint    string            `yaml:"entrypoint"`
	Environment   map[string]string `yaml:"environment"`
	EnvFile       []string          `yaml:"env_file,omitempty"`
	Volumes       []composeMount    `yaml:"volumes,omitempty"`
	Networks      []string          `yaml:"networks,omitempty"`
	Labels        map[string]string `yaml:"labels,omitempty"`
	MemLimit      string            `yaml:"mem_limit,omitempty"`
	MemswapLimit  string            `yaml:"memswap_limit,omitempty"`
	OOMScoreAdj   int               `yaml:"oom_score_adj,omitempty"`
	StdinOpen     bool              `yaml:"stdin_open,omitempty"`
	TTY           bool              `yaml:"tty,omitempty"`
	Healthcheck   *composeHealth    `yaml:"healthcheck,omitempty"`
}

type composeMount struct {
	Type     string `yaml:"type"`
	Source   string `yaml:"source"`
	Target   string `yaml:"target"`
	ReadOnly bool   `yaml:"read_only,omitempty"`
}

type composeExternal struct {
	External bool   `yaml:"external,omitempty"`
	Name     string `yaml:"name"`
}

type composeHealth struct {
	Test        []string `yaml:"test"`
	Interval    string   `yaml:"interval"`
	Timeout     string   `yaml:"timeout"`
	Retries     int      `yaml:"retries"`
	StartPeriod string   `yaml:"start_period"`
}

func resolveDirectorStack(clusterID string) (directorStack, error) {
	clusterID = strings.TrimSpace(clusterID)
	if !validClusterID(clusterID) {
		return directorStack{}, fmt.Errorf("ward collaboration cluster: invalid cluster id %q", clusterID)
	}
	global, err := config.GlobalDir()
	if err != nil {
		return directorStack{}, err
	}
	dir := filepath.Join(global, directorStacksSubdir, clusterID)
	return directorStack{
		Project:         clusterID,
		Dir:             dir,
		ComposePath:     filepath.Join(dir, directorStackFile),
		EnvPath:         filepath.Join(dir, directorStackEnvFile),
		DirectorEnvPath: filepath.Join(dir, directorAgentEnvFile),
		AssetsDir:       filepath.Join(dir, directorStackAssets),
		BrokerName:      clusterID + "-broker",
	}, nil
}

func validClusterID(clusterID string) bool {
	if !clusterIDPattern.MatchString(clusterID) || len(clusterID) < 6 {
		return false
	}
	prefix := strings.TrimSuffix(clusterID, clusterID[len(clusterID)-5:])
	_, err := parseMode(prefix)
	return err == nil
}

func mintClusterID(mode containerMode) string {
	return string(mode) + "-" + clusterIDSuffix()
}

func prepareDirectorStackAssets(ctx context.Context, clusterID, wardSource, wardVersion string) (directorStack, error) {
	stack, err := resolveDirectorStack(clusterID)
	if err != nil {
		return directorStack{}, fmt.Errorf("ward director stack: resolve state dir: %w", err)
	}
	if err := os.MkdirAll(stack.Dir, 0o700); err != nil {
		return directorStack{}, fmt.Errorf("ward director stack: create state dir: %w", err)
	}
	if err := writeContainerAssetsAt(ctx, stack.AssetsDir, wardSource, wardVersion); err != nil {
		return directorStack{}, err
	}
	return stack, nil
}

// normalizeDirectorStackNetwork moves a Linux host-network director onto the project
// and ward-tailnet networks because Compose service DNS and network_mode=host conflict.
func normalizeDirectorStackNetwork(plan *upPlan) {
	if plan != nil && plan.HostNet {
		plan.HostNet = false
		plan.TSSidecar = true
	}
}

func renderDirectorStackCompose(plan upPlan, stack directorStack, brokerEnvFile, directorEnvFile, globalDir string) ([]byte, error) {
	directorEnv := plan.wardEnv()
	delete(directorEnv, envDispatchBrokerToken)
	directorEnv[envDispatchBrokerAddr] = dispatchBrokerServiceAddress
	directorEnv[envClusterID] = stack.Project

	brokerEnv := plan.wardEnv()
	delete(brokerEnv, "WARD_READONLY")
	delete(brokerEnv, envDispatchBrokerToken)
	brokerEnv["WARD_CONTAINER_NAME"] = stack.BrokerName
	brokerEnv[envContainerService] = dispatchBrokerService
	brokerEnv[envDispatchBrokerListen] = dispatchBrokerServiceListen
	brokerEnv[envDispatchBrokerAddr] = dispatchBrokerProbeAddress
	brokerEnv[envDispatchBrokerRequester] = plan.Name
	brokerEnv[envDispatchBrokerID] = stack.Project
	brokerEnv[envClusterID] = stack.Project
	brokerEnv[envPersistentDispatchBroker] = "1"
	brokerEnv["COILY_INVOKE_CWD"] = containerContextMount
	brokerEnv[envInternalLaunchStagingDir] = filepath.ToSlash(filepath.Join("/root/.ward", "launch-staging"))

	mounts := composeMounts(plan.Mounts)
	brokerMounts := append([]composeMount(nil), mounts...)
	brokerMounts = append(brokerMounts, composeMount{
		Type:   "bind",
		Source: globalDir,
		Target: "/root/.ward",
	})
	networks := []string{"default"}
	topNetworks := map[string]composeExternal{}
	if plan.TSSidecar {
		networks = append(networks, tailnetNetwork())
		topNetworks[tailnetNetwork()] = composeExternal{External: true, Name: tailnetNetwork()}
	}

	volumes := map[string]composeExternal{}
	for _, mount := range plan.Mounts {
		if mount.Volume {
			// Ward owns shared named-volume lifecycle outside each Compose project.
			volumes[mount.Source] = composeExternal{External: true, Name: mount.Source}
		}
	}
	doc := composeDocument{
		Services: map[string]composeService{
			"broker": brokerComposeService(plan, stack, brokerEnvFile, brokerEnv, brokerMounts, networks),
			"director": {
				Image:         plan.Image,
				ContainerName: plan.Name,
				Entrypoint:    containerEntrypointPath,
				Environment:   directorEnv,
				EnvFile:       []string{directorEnvFile},
				Volumes:       mounts,
				Networks:      networks,
				Labels:        labelsMap(plan.labels()),
				MemLimit:      plan.MemoryLimit,
				MemswapLimit:  plan.MemorySwap,
				OOMScoreAdj:   -250,
				StdinOpen:     true,
				TTY:           plan.TTY,
			},
		},
		Volumes:  volumes,
		Networks: topNetworks,
	}
	return yaml.Marshal(doc)
}

func renderBrokerOnlyStackCompose(plan upPlan, stack directorStack, brokerEnvFile, globalDir string) ([]byte, error) {
	body, err := renderDirectorStackCompose(plan, stack, brokerEnvFile, "", globalDir)
	if err != nil {
		return nil, err
	}
	var doc composeDocument
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("ward collaboration cluster: decode rendered Compose plan: %w", err)
	}
	delete(doc.Services, "director")
	return yaml.Marshal(doc)
}

func brokerComposeService(plan upPlan, stack directorStack, brokerEnvFile string, brokerEnv map[string]string, mounts []composeMount, networks []string) composeService {
	labels := map[string]string{
		"ward":       "true",
		labelRole:    "broker",
		labelCluster: stack.Project,
		labelDriver:  string(plan.Mode),
	}
	if plan.Repo.Owner != "" && plan.Repo.Name != "" {
		labels[labelRepo] = plan.Repo.slug()
	}
	return composeService{
		Image:         plan.Image,
		ContainerName: stack.BrokerName,
		Restart:       "unless-stopped",
		Entrypoint:    containerEntrypointPath,
		Environment:   brokerEnv,
		EnvFile:       []string{brokerEnvFile},
		Volumes:       mounts,
		Networks:      networks,
		Labels:        labels,
		MemLimit:      plan.MemoryLimit,
		MemswapLimit:  plan.MemorySwap,
		OOMScoreAdj:   -250,
		Healthcheck: &composeHealth{
			Test:        []string{"CMD", "/usr/local/bin/ward", "container", "dispatch-broker-probe"},
			Interval:    "5s",
			Timeout:     "2s",
			Retries:     12,
			StartPeriod: "5s",
		},
	}
}

func composeMounts(mounts []mountSpec) []composeMount {
	out := make([]composeMount, 0, len(mounts))
	for _, mount := range mounts {
		kind := "bind"
		if mount.Volume {
			kind = "volume"
		}
		out = append(out, composeMount{
			Type:     kind,
			Source:   mount.Source,
			Target:   mount.Target,
			ReadOnly: mount.ReadOnly,
		})
	}
	return out
}

func (r *Runner) ensureComposeExternalVolumes(ctx context.Context, mounts []mountSpec) error {
	seen := map[string]bool{}
	for _, mount := range mounts {
		if !mount.Volume || seen[mount.Source] {
			continue
		}
		seen[mount.Source] = true
		if err := r.runDockerSilenced(ctx, true, "volume", "create", mount.Source); err != nil {
			return fmt.Errorf("create external volume %s: %w", mount.Source, err)
		}
	}
	return nil
}

func labelsMap(labels []string) map[string]string {
	out := make(map[string]string, len(labels))
	for _, label := range labels {
		key, value, ok := strings.Cut(label, "=")
		if ok {
			out[key] = value
		}
	}
	return out
}

func appendLaunchEnvSecret(path, key, value string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304 -- Ward-owned launch env path
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return writeEnvLine(f, func() {}, key, value, "broker secret")
}

func (r *Runner) runDirectorStack(ctx context.Context, plan upPlan, stack directorStack, brokerEnvFile, directorEnvFile string) error { //nolint:gocyclo,cyclop
	token, err := newDispatchBrokerToken()
	if err != nil {
		return fmt.Errorf("ward director stack: mint broker token: %w", err)
	}
	plan.DispatchBrokerAddr = dispatchBrokerServiceAddress
	plan.DispatchBrokerToken = token
	if err := appendLaunchEnvSecret(brokerEnvFile, envDispatchBrokerToken, token); err != nil {
		return fmt.Errorf("ward director stack: write broker token: %w", err)
	}
	if err := appendLaunchEnvSecret(directorEnvFile, envDispatchBrokerToken, token); err != nil {
		return fmt.Errorf("ward director stack: write director broker capability: %w", err)
	}
	envBody, err := os.ReadFile(brokerEnvFile) // #nosec G304 -- Ward-owned launch env path
	if err != nil {
		return fmt.Errorf("ward director stack: read launch environment: %w", err)
	}
	if err := writePrivateFile(stack.EnvPath, envBody); err != nil {
		return fmt.Errorf("ward director stack: persist launch environment: %w", err)
	}
	defer cleanupDirectorStackEnvFiles(stack)
	directorEnvBody, err := os.ReadFile(directorEnvFile) // #nosec G304 -- Ward-owned launch env path
	if err != nil {
		return fmt.Errorf("ward director stack: read director environment: %w", err)
	}
	if err := writePrivateFile(stack.DirectorEnvPath, directorEnvBody); err != nil {
		return fmt.Errorf("ward director stack: persist director environment: %w", err)
	}
	globalDir, err := config.GlobalDir()
	if err != nil {
		return fmt.Errorf("ward director stack: resolve persistent state: %w", err)
	}
	body, err := renderDirectorStackCompose(plan, stack, stack.EnvPath, stack.DirectorEnvPath, globalDir)
	if err != nil {
		return fmt.Errorf("ward director stack: render compose file: %w", err)
	}
	if err := os.WriteFile(stack.ComposePath, body, 0o600); err != nil {
		return fmt.Errorf("ward director stack: write compose file: %w", err)
	}
	if err := r.dockerExec(ctx, "compose", "version"); err != nil {
		return fmt.Errorf("ward director stack: Docker Compose plugin is required: %w", err)
	}
	if err := r.ensureComposeExternalVolumes(ctx, plan.Mounts); err != nil {
		return fmt.Errorf("ward director stack: prepare Compose volumes: %w", err)
	}
	commands := directorStackComposeArgs(stack)
	if err := r.dockerExec(ctx, commands.BrokerUp...); err != nil {
		return fmt.Errorf("ward director stack: start broker: %w", err)
	}
	fmt.Fprintf(os.Stderr, "ward director stack: broker %s is healthy at %s (restart policy: unless-stopped)\n",
		stack.BrokerName, dispatchBrokerServiceAddress)
	if err := r.runAttachedDirectorStack(ctx, commands); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "ward director stack: director exited; broker %s remains supervised\n", stack.BrokerName)
	return nil
}

func cleanupDirectorStackEnvFiles(stack directorStack) {
	_ = os.Remove(stack.EnvPath)
	_ = os.Remove(stack.DirectorEnvPath)
}

func (r *Runner) runAttachedDirectorStack(ctx context.Context, commands directorStackCommands) error {
	if err := r.dockerExec(ctx, commands.DirectorUp...); err != nil {
		if cleanupErr := r.removeDirectorStackService(ctx, commands.DirectorRemove); cleanupErr != nil {
			fmt.Fprintf(os.Stderr, "ward director stack: warning: remove director service after start failure: %v\n", cleanupErr)
		}
		return fmt.Errorf("ward director stack: start director: %w", err)
	}
	attachErr := r.dockerExec(ctx, commands.DirectorAttach...)
	cleanupErr := r.removeDirectorStackService(ctx, commands.DirectorRemove)
	if attachErr != nil {
		if cleanupErr != nil {
			fmt.Fprintf(os.Stderr, "ward director stack: warning: remove director service after attach failure: %v\n", cleanupErr)
		}
		return fmt.Errorf("ward director stack: director exited: %w", attachErr)
	}
	if cleanupErr != nil {
		return fmt.Errorf("ward director stack: remove director service: %w", cleanupErr)
	}
	return nil
}

func (r *Runner) removeDirectorStackService(ctx context.Context, argv []string) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	return r.dockerExec(cleanupCtx, argv...)
}

type directorStackCommands struct {
	BrokerUp       []string
	DirectorUp     []string
	DirectorAttach []string
	DirectorRemove []string
}

func directorStackComposeArgs(stack directorStack) directorStackCommands {
	base := []string{"compose", "-p", stack.Project, "-f", stack.ComposePath}
	withBase := func(args ...string) []string {
		return append(append([]string(nil), base...), args...)
	}
	return directorStackCommands{
		BrokerUp:       withBase("up", "-d", "--wait", "broker"),
		DirectorUp:     withBase("up", "-d", "--no-deps", "director"),
		DirectorAttach: withBase("attach", "director"),
		DirectorRemove: withBase("rm", "-f", "-s", "director"),
	}
}
