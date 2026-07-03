package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ward#579: awsMountMissingWarning fires when the aws capability bound ~/.aws but the
// host carries no credentials there, so an empty-dir mount doesn't read as working SSM.
func TestAWSMountMissingWarning(t *testing.T) {
	// Capability off (no mount source): never warn, whatever the creds flag says.
	if msg, warn := awsMountMissingWarning("", false); warn || msg != "" {
		t.Errorf("no aws mount: want no warning, got %q", msg)
	}
	if _, warn := awsMountMissingWarning("", true); warn {
		t.Error("no aws mount: want no warning even when hasCreds is true")
	}

	// Mount on, host has creds: the promise holds - stay quiet.
	if msg, warn := awsMountMissingWarning("/home/kai/.aws", true); warn || msg != "" {
		t.Errorf("aws mount with creds: want no warning, got %q", msg)
	}

	// Mount on, host has NO creds: the exact ward#579 hole - warn loudly and name the path.
	msg, warn := awsMountMissingWarning("/home/kai/.aws", false)
	if !warn {
		t.Fatal("aws mount without creds: want a warning")
	}
	if !strings.Contains(msg, "/home/kai/.aws") {
		t.Errorf("warning should name the missing ~/.aws path; got: %s", msg)
	}
	if !strings.Contains(msg, "NoCredentials") {
		t.Errorf("warning should name the NoCredentials symptom; got: %s", msg)
	}
}

// ward#579: awsHomeHasCreds reads true only when a real config or credentials file
// sits in the dir - a missing dir, an empty dir, or a same-named subdir all read false.
func TestAWSHomeHasCreds(t *testing.T) {
	// Empty capability path is trivially credential-less.
	if awsHomeHasCreds("") {
		t.Error("empty awsHome should read false")
	}

	// A dir that does not exist (docker would mint it empty) reads false.
	missing := filepath.Join(t.TempDir(), "nope", ".aws")
	if awsHomeHasCreds(missing) {
		t.Error("a non-existent ~/.aws should read false")
	}

	// An existing but empty ~/.aws reads false - this is the ward#579 case.
	empty := t.TempDir()
	if awsHomeHasCreds(empty) {
		t.Error("an empty ~/.aws should read false")
	}

	// A `config` file alone is enough (SSO / credential_process live there).
	withConfig := t.TempDir()
	if err := os.WriteFile(filepath.Join(withConfig, "config"), []byte("[default]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !awsHomeHasCreds(withConfig) {
		t.Error("~/.aws with a config file should read true")
	}

	// A `credentials` file alone is enough too.
	withCreds := t.TempDir()
	if err := os.WriteFile(filepath.Join(withCreds, "credentials"), []byte("[default]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !awsHomeHasCreds(withCreds) {
		t.Error("~/.aws with a credentials file should read true")
	}

	// A directory named `config` is not a creds file - stay false.
	dirNamedConfig := t.TempDir()
	if err := os.Mkdir(filepath.Join(dirNamedConfig, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if awsHomeHasCreds(dirNamedConfig) {
		t.Error("a subdir named config is not a creds file; should read false")
	}
}
