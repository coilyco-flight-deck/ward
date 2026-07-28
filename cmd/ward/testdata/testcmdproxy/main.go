package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	proxyPath, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	scriptPath := strings.TrimSuffix(proxyPath, filepath.Ext(proxyPath))
	env := append(os.Environ(), "MSYS2_ARG_CONV_EXCL=*", "WARD_TEST_COMMAND_SCRIPT="+scriptPath)
	var command strings.Builder
	command.WriteString(`exec "$WARD_TEST_COMMAND_SCRIPT"`)
	for i, arg := range os.Args[1:] {
		name := fmt.Sprintf("WARD_TEST_COMMAND_ARG_%d", i)
		env = append(env, name+"="+arg)
		fmt.Fprintf(&command, ` "$%s"`, name)
	}
	cmd := exec.Command("sh", "-c", command.String())
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
