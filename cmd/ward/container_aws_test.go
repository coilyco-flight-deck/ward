package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ward#586: parseAWSExportCreds accepts the export-credentials JSON and requires the
// access-key pair; a missing pair is a failure so the launch falls back to the mount.
func TestParseAWSExportCreds(t *testing.T) {
	// A full session credential (SSO / assumed-role): all four fields present.
	full := `{"Version":1,"AccessKeyId":"AKIA","SecretAccessKey":"secret","SessionToken":"tok","Expiration":"2026-07-03T20:00:00Z"}`
	c, err := parseAWSExportCreds([]byte(full))
	if err != nil {
		t.Fatalf("full creds: unexpected error %v", err)
	}
	if c.AccessKeyID != "AKIA" || c.SecretAccessKey != "secret" || c.SessionToken != "tok" || c.Expiration != "2026-07-03T20:00:00Z" {
		t.Errorf("full creds parsed wrong: %+v", c)
	}

	// Static IAM-user keys: no session token, no expiration - still valid.
	static := `{"Version":1,"AccessKeyId":"AKIA","SecretAccessKey":"secret"}`
	if c, err := parseAWSExportCreds([]byte(static)); err != nil || c.SessionToken != "" || c.Expiration != "" {
		t.Errorf("static creds: want valid no-token/no-expiry creds, got %+v err %v", c, err)
	}

	// Missing the access key pair reads as a failure (fall back to the mount).
	if _, err := parseAWSExportCreds([]byte(`{"Version":1,"SessionToken":"tok"}`)); err == nil {
		t.Error("creds with no access key should error")
	}
	// Not JSON at all (e.g. a v1 CLI's usage error on stdout) is a failure too.
	if _, err := parseAWSExportCreds([]byte("usage: aws [options]")); err == nil {
		t.Error("non-JSON output should error")
	}
}

// ward#586: envLines injects the access-key pair always, the session token and region
// only when present, so static keys and a region-less host don't ship empty env vars.
func TestAWSExportedCredsEnvLines(t *testing.T) {
	toMap := func(c awsExportedCreds, region string) map[string]string {
		m := map[string]string{}
		for _, l := range c.envLines(region) {
			m[l.Key] = l.Value
		}
		return m
	}

	// Full session cred + region: all five keys.
	full := awsExportedCreds{AccessKeyID: "AKIA", SecretAccessKey: "s", SessionToken: "tok"}
	m := toMap(full, "us-east-1")
	for k, want := range map[string]string{
		awsAccessKeyEnv:     "AKIA",
		awsSecretKeyEnv:     "s",
		awsSessionTokenEnv:  "tok",
		awsDefaultRegionEnv: "us-east-1",
		awsRegionEnv:        "us-east-1",
	} {
		if m[k] != want {
			t.Errorf("env %s = %q, want %q", k, m[k], want)
		}
	}

	// Static keys, no region: only the access-key pair, no empty token/region lines.
	static := awsExportedCreds{AccessKeyID: "AKIA", SecretAccessKey: "s"}
	m = toMap(static, "")
	if _, ok := m[awsSessionTokenEnv]; ok {
		t.Error("static keys should inject no AWS_SESSION_TOKEN")
	}
	if _, ok := m[awsDefaultRegionEnv]; ok {
		t.Error("region-less host should inject no AWS_DEFAULT_REGION")
	}
	if m[awsAccessKeyEnv] != "AKIA" || m[awsSecretKeyEnv] != "s" {
		t.Errorf("static keys missing the access-key pair: %+v", m)
	}
}

// ward#586: awsExpiryNote names the expiry so a run outliving it is diagnosable; static
// keys say so, and a parseable RFC3339 instant carries a from-now delta.
func TestAWSExpiryNote(t *testing.T) {
	if note := awsExpiryNote("", time.Now()); !strings.Contains(note, "static keys") {
		t.Errorf("empty expiration should mention static keys; got %q", note)
	}
	now := time.Date(2026, 7, 3, 19, 0, 0, 0, time.UTC)
	note := awsExpiryNote("2026-07-03T20:00:00Z", now)
	if !strings.Contains(note, "2026-07-03T20:00:00Z") {
		t.Errorf("note should carry the expiration instant; got %q", note)
	}
	if !strings.Contains(note, "1h0m0s from now") {
		t.Errorf("note should carry the from-now delta; got %q", note)
	}
	// An unparseable expiration still names the instant, just without a delta.
	if note := awsExpiryNote("later", now); !strings.Contains(note, "later") || strings.Contains(note, "from now") {
		t.Errorf("unparseable expiration should name it without a delta; got %q", note)
	}
}

// ward#586: dropAWSMount removes only the ~/.aws bind and clears AWSHome, so a successful
// export supersedes the mount and the #579 empty-mount warning stays silent.
func TestDropAWSMount(t *testing.T) {
	plan := upPlan{
		AWSHome: "/home/kai/.aws",
		Mounts: []mountSpec{
			{Source: "/cwd", Target: containerContextMount, ReadOnly: true},
			{Source: "/home/kai/.aws", Target: containerAWSMount, ReadOnly: true},
			{Source: containerGitcacheVol, Target: containerGitcacheMnt, Volume: true},
		},
	}
	plan.dropAWSMount()
	if plan.AWSHome != "" {
		t.Errorf("dropAWSMount should clear AWSHome, got %q", plan.AWSHome)
	}
	for _, m := range plan.Mounts {
		if m.Target == containerAWSMount {
			t.Errorf("dropAWSMount left the ~/.aws bind: %+v", m)
		}
	}
	if len(plan.Mounts) != 2 {
		t.Errorf("dropAWSMount should keep the other two mounts, got %d", len(plan.Mounts))
	}
}

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
