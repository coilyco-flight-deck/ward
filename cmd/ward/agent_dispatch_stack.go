package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/config"
	"gopkg.in/yaml.v3"
)

const (
	directorStacksSubdir = "director-stacks"
	directorStackFile    = "compose.yaml"
	directorStackEnvFile = "launch.env"
	directorStackAssets  = "assets"
	directorStackMaxName = 55
)

type directorStack struct {
	Project     string
	Dir         string
	ComposePath string
	EnvPath     string
	AssetsDir   string
	BrokerName  string
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

func resolveDirectorStack(repo targetRepo, mode containerMode) (directorStack, error) {
	global, err := config.GlobalDir()
	if err != nil {
		return directorStack{}, err
	}
	project := directorStackProjectName(repo, mode)
	dir := filepath.Join(global, directorStacksSubdir, project)
	return directorStack{
		Project:     project,
		Dir:         dir,
		ComposePath: filepath.Join(dir, directorStackFile),
		EnvPath:     filepath.Join(dir, directorStackEnvFile),
		AssetsDir:   filepath.Join(dir, directorStackAssets),
		BrokerName:  project + "-broker",
	}, nil
}

func directorStackProjectName(repo targetRepo, mode containerMode) string {
	parts := []string{"ward", repo.Owner}
	if !strings.EqualFold(repo.Name, "ward") {
		parts = append(parts, repo.Name)
	}
	parts = append(parts, string(mode))
	project := config.SanitizeSlug(strings.Join(parts, "-"))
	if len(project) > directorStackMaxName {
		project = strings.TrimRight(project[:directorStackMaxName], "-")
	}
	return project
}

func prepareDirectorStackAssets(ctx context.Context, repo targetRepo, mode containerMode, wardSource, wardVersion string) (directorStack, error) {
	stack, err := resolveDirectorStack(repo, mode)
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

func renderDirectorStackCompose(plan upPlan, stack directorStack, envFile, globalDir string) ([]byte, error) {
	directorEnv := plan.wardEnv()
	delete(directorEnv, envDispatchBrokerToken)
	directorEnv[envDispatchBrokerAddr] = dispatchBrokerServiceAddress

	brokerEnv := plan.wardEnv()
	delete(brokerEnv, "WARD_READONLY")
	delete(brokerEnv, envDispatchBrokerToken)
	brokerEnv["WARD_CONTAINER_NAME"] = stack.BrokerName
	brokerEnv[envContainerService] = dispatchBrokerService
	brokerEnv[envDispatchBrokerListen] = dispatchBrokerServiceListen
	brokerEnv[envDispatchBrokerAddr] = dispatchBrokerProbeAddress
	brokerEnv[envDispatchBrokerRequester] = plan.Name
	brokerEnv[envDispatchBrokerID] = stack.Project
	brokerEnv[envPersistentDispatchBroker] = "1"
	brokerEnv["COILY_INVOKE_CWD"] = containerContextMount
	brokerEnv[envLaunchStagingDir] = filepath.ToSlash(filepath.Join("/root/.ward", "launch-staging"))

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
			volumes[mount.Source] = composeExternal{Name: mount.Source}
		}
	}
	doc := composeDocument{
		Services: map[string]composeService{
			"broker": {
				Image:         plan.Image,
				ContainerName: stack.BrokerName,
				Restart:       "unless-stopped",
				Entrypoint:    containerEntrypointPath,
				Environment:   brokerEnv,
				EnvFile:       []string{envFile},
				Volumes:       brokerMounts,
				Networks:      networks,
				Labels: map[string]string{
					"ward":      "true",
					labelRole:   "broker",
					labelRepo:   plan.Repo.slug(),
					labelDriver: string(plan.Mode),
				},
				MemLimit:     plan.MemoryLimit,
				MemswapLimit: plan.MemorySwap,
				OOMScoreAdj:  -250,
				Healthcheck: &composeHealth{
					Test:        []string{"CMD", "/usr/local/bin/ward", "container", "dispatch-broker-probe"},
					Interval:    "5s",
					Timeout:     "2s",
					Retries:     12,
					StartPeriod: "5s",
				},
			},
			"director": {
				Image:        plan.Image,
				Entrypoint:   containerEntrypointPath,
				Environment:  directorEnv,
				EnvFile:      []string{envFile},
				Volumes:      mounts,
				Networks:     networks,
				Labels:       labelsMap(plan.labels()),
				MemLimit:     plan.MemoryLimit,
				MemswapLimit: plan.MemorySwap,
				OOMScoreAdj:  -250,
				StdinOpen:    true,
				TTY:          plan.TTY,
			},
		},
		Volumes:  volumes,
		Networks: topNetworks,
	}
	return yaml.Marshal(doc)
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

func (r *Runner) runDirectorStack(ctx context.Context, plan upPlan, stack directorStack, envFile string) error {
	token, err := newDispatchBrokerToken()
	if err != nil {
		return fmt.Errorf("ward director stack: mint broker token: %w", err)
	}
	plan.DispatchBrokerAddr = dispatchBrokerServiceAddress
	plan.DispatchBrokerToken = token
	if err := appendLaunchEnvSecret(envFile, envDispatchBrokerToken, token); err != nil {
		return fmt.Errorf("ward director stack: write broker token: %w", err)
	}
	envBody, err := os.ReadFile(envFile) // #nosec G304 -- Ward-owned launch env path
	if err != nil {
		return fmt.Errorf("ward director stack: read launch environment: %w", err)
	}
	if err := os.WriteFile(stack.EnvPath, envBody, 0o600); err != nil {
		return fmt.Errorf("ward director stack: persist launch environment: %w", err)
	}
	globalDir, err := config.GlobalDir()
	if err != nil {
		return fmt.Errorf("ward director stack: resolve persistent state: %w", err)
	}
	body, err := renderDirectorStackCompose(plan, stack, stack.EnvPath, globalDir)
	if err != nil {
		return fmt.Errorf("ward director stack: render compose file: %w", err)
	}
	if err := os.WriteFile(stack.ComposePath, body, 0o600); err != nil {
		return fmt.Errorf("ward director stack: write compose file: %w", err)
	}
	if err := r.dockerExec(ctx, "compose", "version"); err != nil {
		return fmt.Errorf("ward director stack: Docker Compose plugin is required: %w", err)
	}
	upArgs, runArgs := directorStackComposeArgs(plan, stack)
	if err := r.dockerExec(ctx, upArgs...); err != nil {
		return fmt.Errorf("ward director stack: start broker: %w", err)
	}
	fmt.Fprintf(os.Stderr, "ward director stack: broker %s is healthy at %s (restart policy: unless-stopped)\n",
		stack.BrokerName, dispatchBrokerServiceAddress)
	if err := r.dockerExec(ctx, runArgs...); err != nil {
		return fmt.Errorf("ward director stack: director exited: %w", err)
	}
	fmt.Fprintf(os.Stderr, "ward director stack: director exited; broker %s remains supervised\n", stack.BrokerName)
	return nil
}

func directorStackComposeArgs(plan upPlan, stack directorStack) (up, run []string) {
	base := []string{"compose", "-p", stack.Project, "-f", stack.ComposePath}
	up = append(append([]string(nil), base...), "up", "-d", "--wait", "broker")
	run = append(append([]string(nil), base...), "run", "--rm", "--no-deps")
	if !plan.TTY {
		run = append(run, "-T")
	}
	run = append(run, "--name", plan.Name, "director")
	return up, run
}
