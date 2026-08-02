package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDirectorStackComposeSeparatesBrokerLifecycle(t *testing.T) {
	t.Setenv(envHostGOOS, "windows")
	plan := upPlan{
		Image:               "example.invalid/dev-base:test",
		Name:                "director-codex-ab45",
		Role:                roleDirector,
		ConfigRole:          roleDirector,
		Repo:                targetRepo{Owner: "coilyco-flight-deck", Name: "ward"},
		Mode:                modeCodex,
		ForgejoBase:         forgejoBaseURL,
		ReadOnly:            true,
		TSSidecar:           true,
		DispatchBrokerToken: "must-not-enter-compose",
		Mounts: []mountSpec{
			{Source: `X:\projects\coilyco-flight-deck\ward`, Target: containerContextMount, ReadOnly: true},
			{Source: containerGitcacheVol, Target: containerGitcacheMnt, Volume: true},
			dockerSockMount(),
		},
	}
	stack := directorStack{
		Project:    "codex-ab45",
		BrokerName: "codex-ab45-broker",
	}
	body, err := renderDirectorStackCompose(plan, stack, `X:\tmp\broker.env`, `X:\tmp\director.env`, `X:\home\.ward`)
	if err != nil {
		t.Fatalf("renderDirectorStackCompose: %v", err)
	}
	text := string(body)
	for _, want := range []string{
		"broker:",
		"director:",
		"container_name: director-codex-ab45",
		"restart: unless-stopped",
		"WARD_CONTAINER_SERVICE: dispatch-broker",
		"WARD_DISPATCH_BROKER_LISTEN: 0.0.0.0:7420",
		"WARD_DISPATCH_BROKER_ADDR: broker:7420",
		"WARD_CLUSTER_ID: codex-ab45",
		"ward.cluster: codex-ab45",
		"WARD_HOST_GOOS: windows",
		"source: X:\\projects\\coilyco-flight-deck\\ward",
		"target: /root/.ward",
		"ward-tailnet",
		"dispatch-broker-probe",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("compose output missing %q\n%s", want, text)
		}
	}
	if strings.Contains(text, plan.DispatchBrokerToken) {
		t.Fatal("Compose output contains the broker token")
	}
	if !strings.Contains(text, `- X:\tmp\broker.env`) || !strings.Contains(text, `- X:\tmp\director.env`) {
		t.Fatalf("Compose output does not carry separate service env files:\n%s", text)
	}
	if strings.Contains(text, "network_mode: host") {
		t.Fatal("Compose director cannot use host networking with broker service DNS")
	}
}

func TestNormalizeDirectorStackNetworkPreservesComposeDNS(t *testing.T) {
	plan := upPlan{HostNet: true}
	normalizeDirectorStackNetwork(&plan)
	if plan.HostNet || !plan.TSSidecar {
		t.Fatalf("normalized plan = HostNet %t TSSidecar %t, want false/true", plan.HostNet, plan.TSSidecar)
	}
}

func TestLaunchHostGOOSUsesInjectedHostIdentity(t *testing.T) {
	t.Setenv(envHostGOOS, "darwin")
	if got := launchHostGOOS(); got != "darwin" {
		t.Fatalf("launchHostGOOS = %q, want darwin", got)
	}
}

func TestDirectorStackCredsCarryEveryHarnessIntoBroker(t *testing.T) {
	t.Setenv(claudeCredsEnvKey, "claude-credential")
	t.Setenv(codexAuthEnvKey, "codex-credential")
	t.Setenv(gooseOllamaHostEnvKey, "goose-endpoint")

	creds := (&Runner{}).resolveDirectorStackCreds(t.Context(), &upPlan{}, modeCodex)
	wants := map[string]string{
		claudeCredsEnvKey:     "claude-credential",
		codexAuthEnvKey:       "codex-credential",
		gooseOllamaHostEnvKey: "goose-endpoint",
	}
	seen := make(map[string]int)
	for _, line := range creds {
		if want, ok := wants[line.Key]; ok {
			seen[line.Key]++
			if line.Value != want {
				t.Errorf("%s = %q, want %q", line.Key, line.Value, want)
			}
		}
	}
	for key := range wants {
		if seen[key] != 1 {
			t.Errorf("%s count = %d, want 1 in %+v", key, seen[key], creds)
		}
	}
}

func TestDirectorExitCommandLeavesBrokerSupervised(t *testing.T) {
	stack := directorStack{Project: "codex-ab45", ComposePath: "/state/compose.yaml"}
	commands := directorStackComposeArgs(stack)
	if got := strings.Join(commands.BrokerUp, " "); got != "compose -p codex-ab45 -f /state/compose.yaml up -d --wait broker" {
		t.Fatalf("broker up args = %v", commands.BrokerUp)
	}
	if got := strings.Join(commands.DirectorUp, " "); got != "compose -p codex-ab45 -f /state/compose.yaml up -d --no-deps director" {
		t.Fatalf("director up args = %v", commands.DirectorUp)
	}
	if got := strings.Join(commands.DirectorAttach, " "); got != "compose -p codex-ab45 -f /state/compose.yaml attach director" {
		t.Fatalf("director attach args = %v", commands.DirectorAttach)
	}
	remove := strings.Join(commands.DirectorRemove, " ")
	if remove != "compose -p codex-ab45 -f /state/compose.yaml rm -f -s director" ||
		strings.Contains(remove, "down") || strings.Contains(remove, "broker") {
		t.Fatalf("director removal couples broker cleanup: %v", commands.DirectorRemove)
	}
	for _, argv := range [][]string{commands.DirectorUp, commands.DirectorAttach, commands.DirectorRemove} {
		if strings.Contains(strings.Join(argv, " "), " run ") {
			t.Fatalf("director lifecycle still uses a Compose one-off: %v", argv)
		}
	}
}

func TestResolveDirectorStackUsesStableClusterKey(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	first, err := resolveDirectorStack("codex-ab45")
	if err != nil {
		t.Fatalf("resolveDirectorStack first: %v", err)
	}
	second, err := resolveDirectorStack("codex-ab45")
	if err != nil {
		t.Fatalf("resolveDirectorStack second: %v", err)
	}
	if first != second {
		t.Fatalf("director stack changed: %#v != %#v", first, second)
	}
	if first.Project != "codex-ab45" {
		t.Fatalf("director stack project = %q", first.Project)
	}
	if filepath.Base(first.ComposePath) != directorStackFile ||
		filepath.Base(first.EnvPath) != directorStackEnvFile ||
		filepath.Base(first.AssetsDir) != directorStackAssets {
		t.Fatalf("unexpected persistent paths: %#v", first)
	}
}

func TestPolicyBoundaryDirectorStackTeardownRemovesCredentialState(t *testing.T) {
	dir := t.TempDir()
	stack := directorStack{
		EnvPath:         filepath.Join(dir, directorStackEnvFile),
		DirectorEnvPath: filepath.Join(dir, directorAgentEnvFile),
	}
	for _, path := range []string{stack.EnvPath, stack.DirectorEnvPath} {
		if err := os.WriteFile(path, []byte("synthetic-secret"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cleanupDirectorStackEnvFiles(stack)
	for _, path := range []string{stack.EnvPath, stack.DirectorEnvPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("stack credential state still exists at %s: %v", path, err)
		}
	}
}

func TestMintClusterIDIsHarnessScopedAndDistinct(t *testing.T) {
	original := clusterIDSuffix
	t.Cleanup(func() { clusterIDSuffix = original })
	suffixes := []string{"ab45", "cd67"}
	clusterIDSuffix = func() string {
		value := suffixes[0]
		suffixes = suffixes[1:]
		return value
	}
	first := mintClusterID(modeCodex)
	second := mintClusterID(modeCodex)
	if first != "codex-ab45" || second != "codex-cd67" || first == second {
		t.Fatalf("cluster ids = %q, %q", first, second)
	}
	if strings.Contains(first, "ward-") || strings.Contains(first, "coilyco-flight-deck") {
		t.Fatalf("cluster id encodes product or repository: %q", first)
	}
}

func TestResolveDirectorStackRejectsNonCanonicalClusterID(t *testing.T) {
	setTestHome(t, t.TempDir())
	for _, id := range []string{"ward-ab45", "codex-ward-ab45", "codex-ab12", "codex-AB45"} {
		if _, err := resolveDirectorStack(id); err == nil {
			t.Fatalf("resolveDirectorStack(%q) accepted a non-canonical id", id)
		}
	}
}

func TestClassifyDispatchReconcileNeverBlindlyDuplicates(t *testing.T) {
	journal := dispatchRequestJournal{RequestID: strings.Repeat("a", 32)}
	tests := []struct {
		name       string
		phase      string
		containers []dispatchContainerState
		resume     string
		outcome    string
		wantErr    bool
	}{
		{name: "accepted before create replays", phase: dispatchPhaseAccepted},
		{name: "creating with no container replays", phase: dispatchPhaseCreating},
		{name: "created container resumes same id", phase: dispatchPhaseCreated, containers: []dispatchContainerState{{ID: "one", State: "created"}}, resume: "one"},
		{name: "starting running adopts", phase: dispatchPhaseStarting, containers: []dispatchContainerState{{ID: "one", State: "running"}}, outcome: dispatchOutcomeLaunched},
		{name: "visible exited terminalizes", phase: dispatchPhaseVisible, containers: []dispatchContainerState{{ID: "one", State: "exited"}}, outcome: dispatchOutcomeFailed, wantErr: true},
		{name: "starting without container is ambiguous", phase: dispatchPhaseStarting, outcome: dispatchOutcomeInterrupted, wantErr: true},
		{name: "multiple matching containers blocks readiness", phase: dispatchPhaseCreating, containers: []dispatchContainerState{{ID: "one"}, {ID: "two"}}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			journal.Phase = tc.phase
			got := classifyDispatchReconcile(journal, tc.containers)
			if got.ResumeContainer != tc.resume || got.TerminalOutcome != tc.outcome || (got.Err != nil) != tc.wantErr {
				t.Fatalf("decision = %#v, want resume=%q outcome=%q err=%t", got, tc.resume, tc.outcome, tc.wantErr)
			}
		})
	}
}

func TestDispatchArtifactPathIsStablePerRequest(t *testing.T) {
	req := dispatchBrokerRequest{
		Requester: "director-codex-ab45",
		Role:      roleEngineer,
		Argv:      []string{"engineer", "coilyco-flight-deck/ward#1562"},
	}
	first := newDispatchArtifactPaths(req, time.Unix(1, 0), strings.Repeat("b", 32))
	second := newDispatchArtifactPaths(req, time.Unix(2, 0), strings.Repeat("b", 32))
	if first.Dir != second.Dir || first.ConsolePath != second.ConsolePath {
		t.Fatalf("same request id produced different artifacts: %s != %s", first.Dir, second.Dir)
	}
}

func TestDispatchFingerprintExcludesTokenButIncludesLaunchShape(t *testing.T) {
	base := dispatchBrokerRequest{
		RequestID: strings.Repeat("c", 32),
		Role:      roleEngineer,
		Argv:      []string{"engineer", "coilyco-flight-deck/ward#1562"},
		Token:     "first-secret",
	}
	first, err := dispatchRequestFingerprint(base)
	if err != nil {
		t.Fatal(err)
	}
	base.Token = "second-secret"
	second, err := dispatchRequestFingerprint(base)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("transport token changed the semantic request fingerprint")
	}
	base.Argv = append(base.Argv, "--no-pull")
	third, err := dispatchRequestFingerprint(base)
	if err != nil {
		t.Fatal(err)
	}
	if first == third {
		t.Fatal("launch arguments did not change the semantic request fingerprint")
	}
}

func TestReservationRequestMarkerMakesRecoveryIdempotent(t *testing.T) {
	requestID := strings.Repeat("e", 32)
	comments := []issueComment{{Body: "visible\n" + reservationRequestMarker(requestID)}}
	if !reservationRequestAlreadyHeld(comments, requestID) {
		t.Fatal("same dispatch request did not reacquire its remote reservation")
	}
	if reservationRequestAlreadyHeld(comments, strings.Repeat("f", 32)) {
		t.Fatal("different dispatch request reused a foreign reservation")
	}
}

func TestAcceptedRequestIDLaunchesExactlyOnce(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	t.Setenv(envDispatchJournalDir, filepath.Join(home, "journals"))

	original := dispatchBrokerLaunch
	t.Cleanup(func() { dispatchBrokerLaunch = original })
	launched := make(chan struct{}, 2)
	release := make(chan struct{})
	finished := make(chan struct{})
	dispatchBrokerLaunch = func(_ context.Context, _ dispatchBrokerRequest) error {
		launched <- struct{}{}
		<-release
		close(finished)
		return nil
	}

	req := dispatchBrokerRequest{
		RequestID: strings.Repeat("d", 32),
		Role:      roleQA,
		Argv:      []string{"qa", "coilyco-flight-deck/ward#1562", "--harness", "codex"},
	}
	firstPath, err := (&Runner{}).startHostDispatchBrokerRequest(t.Context(), req)
	if err != nil {
		t.Fatalf("first accepted request: %v", err)
	}
	select {
	case <-launched:
	case <-time.After(2 * time.Second):
		t.Fatal("accepted request never launched")
	}
	secondPath, err := (&Runner{}).startHostDispatchBrokerRequest(t.Context(), req)
	if err != nil {
		t.Fatalf("duplicate accepted request: %v", err)
	}
	if firstPath != secondPath {
		t.Fatalf("duplicate request returned different artifacts: %q != %q", firstPath, secondPath)
	}
	select {
	case <-launched:
		t.Fatal("duplicate request launched a second worker")
	default:
	}
	close(release)
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("accepted launch did not finish")
	}
}
