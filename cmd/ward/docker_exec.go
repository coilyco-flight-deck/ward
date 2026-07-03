// docker_exec.go adds the one mutating leaf on ward's otherwise read-only
// `ward docker` surface: `exec`, gated to ward-managed (ward=true) containers
// only (ward#220). The read-only inspection verbs (ps/logs/inspect/...) are
// generated from the exec-dialect guardfile; exec cannot be a pure guardfile
// grant because "is this container ward-managed?" is a runtime label check, not
// an argv shape. It ships as a hand-written leaf grafted onto the docker group.
// See docs/ward-docker-exec.md.
package main

import (
	"context"
	"fmt"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"github.com/urfave/cli/v3"
)

// wardContainerLabelKey is the bare label key ward stamps on every container it
// manages; containerLabel ("ward=true") is the key=value run form. docs/container.md.
const wardContainerLabelKey = "ward"

// graftDockerExec appends the hand-written `exec` leaf onto the `docker` group the
// guardfile mount built. No-op when the group is absent or an exec leaf exists.
func graftDockerExec(root *cli.Command, r *Runner) {
	docker := subCommandNamed(root, "docker")
	if docker == nil {
		return // the read-only docker surface didn't mount; nothing to graft onto
	}
	if subCommandNamed(docker, "exec") != nil {
		return // already present; never duplicate
	}
	docker.Commands = append(docker.Commands, dockerExecCommand(r))
}

// dockerExecCommand is `ward docker exec [OPTIONS] CONTAINER COMMAND [ARG...]`: the
// same grammar as `docker exec`, but refused unless CONTAINER carries ward=true.
func dockerExecCommand(r *Runner) *cli.Command {
	return &cli.Command{
		Name:      "exec",
		Usage:     "run a command in a RUNNING ward-managed container (docker exec, gated to ward=true containers)",
		ArgsUsage: "[OPTIONS] CONTAINER COMMAND [ARG...]",
		Description: `exec is the single mutating verb on the otherwise read-only ` + "`ward docker`" + `
surface. It forwards to ` + "`docker exec`" + ` unchanged - the same OPTIONS (-it, -e,
-u, -w, ...), the same CONTAINER COMMAND [ARG...] grammar - but only after
confirming CONTAINER carries the ward=true label, i.e. it is a container ward
itself launched (` + "`ward agent`" + ` runs). Any other container is refused; use bare
` + "`docker exec`" + ` for those. Every call is audit-logged. See docs/ward-docker-exec.md
for what this bypasses relative to the read-only posture.`,
		// docker's own flags (-it, -e, ...) must pass straight through, not be parsed
		// as ward flags; we identify CONTAINER ourselves. Matches the execverb leaves.
		SkipFlagParsing: true,
		Action: func(ctx context.Context, c *cli.Command) error {
			return r.WrapVerb(verb.Spec{
				Name: "docker.exec",
				// The command after CONTAINER runs inside the container via docker's
				// execve, never a host shell, so the metacharacter gate only harms.
				SkipPolicy: true,
				Action:     func(ctx context.Context, cmd *cli.Command) error { return r.runDockerExec(ctx, cmd) },
			}, r.Audit)(ctx, c)
		},
	}
}

// runDockerExec gates `docker exec` on the target being a ward-managed container,
// then forwards the whole argv (stdio wired for interactive -it) to docker.
func (r *Runner) runDockerExec(ctx context.Context, c *cli.Command) error {
	args := c.Args().Slice()
	container, ok := dockerExecContainerArg(args)
	if !ok {
		return fmt.Errorf("ward docker exec: no container named; usage: ward docker exec [OPTIONS] CONTAINER COMMAND [ARG...]")
	}
	warded, err := r.isWardedContainer(ctx, container)
	if err != nil {
		return fmt.Errorf("ward docker exec: cannot verify container %q is ward-managed: %w", container, err)
	}
	if !warded {
		return fmt.Errorf("ward docker exec: refusing to exec into %q - not a ward-managed container "+
			"(no %s=true label). `ward docker exec` is restricted to containers ward itself launched "+
			"(`ward agent` runs); use bare `docker exec` for anything else", container, wardContainerLabelKey)
	}
	return r.dockerExec(ctx, append([]string{"exec"}, args...)...)
}

// isWardedContainer reports whether the named container carries ward=true. A docker
// error (no such container, daemon down) is returned so the caller refuses loudly.
func (r *Runner) isWardedContainer(ctx context.Context, container string) (bool, error) {
	out, err := r.dockerCapture(ctx, "inspect",
		"--format", `{{index .Config.Labels "`+wardContainerLabelKey+`"}}`, container)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) == "true", nil
}

// dockerExecValueLongFlags are the long `docker exec` options that consume the
// next token as their value; every other long option is a boolean switch.
var dockerExecValueLongFlags = map[string]bool{
	"--detach-keys": true,
	"--env":         true,
	"--env-file":    true,
	"--user":        true,
	"--workdir":     true,
}

// dockerExecValueShortFlags are the short `docker exec` options that consume a
// value (attached as the rest of the cluster, or the next token): -e, -u, -w.
var dockerExecValueShortFlags = map[byte]bool{'e': true, 'u': true, 'w': true}

// dockerExecContainerArg walks a `docker exec` argv the way docker's own parser
// would and returns the CONTAINER positional. ok is false when none is found.
func dockerExecContainerArg(args []string) (container string, ok bool) {
	for i := 0; i < len(args); i++ {
		tok := args[i]
		switch {
		case tok == "--":
			// Everything after `--` is positional; the container is next.
			if i+1 < len(args) {
				return args[i+1], true
			}
			return "", false
		case strings.HasPrefix(tok, "--"):
			if strings.IndexByte(tok, '=') >= 0 {
				continue // --flag=value: self-contained
			}
			if dockerExecValueLongFlags[tok] {
				i++ // the value is the next token
			}
		case strings.HasPrefix(tok, "-") && len(tok) > 1:
			if dockerExecShortClusterTakesNextValue(tok) {
				i++ // the value is the next token
			}
		default:
			return tok, true // a bare positional: this is CONTAINER
		}
	}
	return "", false
}

// dockerExecShortClusterTakesNextValue reports whether a short-flag cluster (e.g.
// "-it", "-e", "-ie") consumes the NEXT token as its value.
func dockerExecShortClusterTakesNextValue(tok string) bool {
	// True only when a value-taking short is the cluster's LAST char; an attached
	// value like "-eKEY=VAL" is self-contained, so false.
	cluster := tok[1:] // drop the leading '-'
	for j := 0; j < len(cluster); j++ {
		if dockerExecValueShortFlags[cluster[j]] {
			return j == len(cluster)-1
		}
	}
	return false
}
