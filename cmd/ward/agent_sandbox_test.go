package main

import (
	"strings"
	"testing"
)

// `sandbox` is a top-level agent surface alongside work/headless/task/reply/ask:
// the unguided interactive session (ward#292).
func TestAgentHasSandboxSurface(t *testing.T) {
	surfaces := map[string]bool{}
	for _, c := range agentCommand().Commands {
		surfaces[c.Name] = true
	}
	if !surfaces["sandbox"] {
		t.Errorf("ward agent missing %q surface; got %v", "sandbox", surfaces)
	}
}

// A sandbox plan is the bare interactive bring-up: empty AgentArgs (no seed),
// neither WARD_ASK nor WARD_HEADLESS, attached with a TTY. The entrypoint then
// launches a plain agent REPL, so the docker argv ends at the image.
func TestSandboxPlanBareInteractive(t *testing.T) {
	p := sampleUpPlan()
	p.AgentArgs = nil
	p.Interactive = true
	p.TTY = true
	env := p.wardEnv()
	if _, ok := env["WARD_ASK"]; ok {
		t.Error("sandbox plan must not set WARD_ASK (it is interactive, not one-shot)")
	}
	if _, ok := env["WARD_HEADLESS"]; ok {
		t.Error("sandbox plan must not set WARD_HEADLESS (it is attached, not detached)")
	}
	argv := dockerCreateArgv(p, "")
	joined := strings.Join(argv, " ")
	if strings.Contains(joined, " -d ") || strings.HasSuffix(joined, " -d") {
		t.Errorf("sandbox plan must not detach (-d)\n got: %s", joined)
	}
	if !strings.Contains(joined, "-it") {
		t.Errorf("attached sandbox plan with a TTY should pass -it\n got: %s", joined)
	}
	// No seed: the argv must end at the image, with no trailing agent args.
	if argv[len(argv)-1] != p.Image {
		t.Errorf("sandbox argv should end at the image (no seed)\n got: %s", joined)
	}
}
