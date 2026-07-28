package main

import (
	"context"
	"fmt"
	"runtime"
	"strings"
)

func dockerReadyArgv() []string {
	return []string{"version", "--format", "{{.Server.Version}}"}
}

func (r *Runner) checkDockerReady(ctx context.Context) error {
	out, err := r.dockerCapture(ctx, dockerReadyArgv()...)
	if err != nil {
		return fmt.Errorf("%s", dockerInitPrompt(err))
	}
	if strings.TrimSpace(string(out)) == "" {
		return fmt.Errorf("%s", dockerInitPrompt(fmt.Errorf("docker returned an empty server version")))
	}
	return nil
}

func dockerInitPrompt(cause error) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Docker is not ready for warded container launches: %v.\n", cause)
	b.WriteString("Initialize or start Docker, wait until the daemon is running, then rerun the warded command.\n")
	switch runtime.GOOS {
	case "darwin":
		b.WriteString("On macOS: open Docker Desktop and wait for it to report that Docker is running.\n")
	case "linux":
		b.WriteString("On Linux: start the Docker daemon, for example `sudo systemctl start docker`, or start your rootless Docker service.\n")
	case "windows":
		b.WriteString("On Windows: start Docker Desktop and wait for it to report that Docker is running.\n")
	default:
		b.WriteString("Start the Docker daemon for this host before launching an agent container.\n")
	}
	b.WriteString("Readiness probe: `docker version --format '{{.Server.Version}}'`.")
	return b.String()
}
