package scripts

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func mirrorScript(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed to locate the test file")
	}
	return filepath.Join(filepath.Dir(self), "check_workflow_mirror.py")
}

func TestWorkflowMirrorInSync(t *testing.T) {
	cmd := exec.Command("python3", mirrorScript(t))
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("check_workflow_mirror.py reported the mirror out of sync: %v\nstdout: %s\nstderr: %s\n"+
			"Run `make lint-workflows ARGS=--fix` to regenerate the Forgejo mirror.",
			err, out.String(), errb.String())
	}
}
