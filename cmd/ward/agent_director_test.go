package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

func TestDirectorCommandRetiresAutonomousBurndown(t *testing.T) {
	cmd := agentDirectorCommand()
	if !strings.Contains(cmd.Description, "Ward does not poll, rank, choose, or redispatch work") {
		t.Fatalf("director description does not name the retained boundary:\n%s", cmd.Description)
	}
	names := map[string]bool{}
	for _, flag := range cmd.Flags {
		for _, name := range flag.Names() {
			names[name] = true
		}
	}
	for _, removed := range []string{"burndown", "drain", "triage", "no-triage", "poll-interval", "max-cycles", "max-parallel", "engineer-harness", "dry-run", "override-reservation", verificationFixtureFlagName} {
		if names[removed] {
			t.Errorf("retired director flag --%s is still public", removed)
		}
	}
	for _, retained := range []string{"repo", "org", "with-repo", "limit", "harness", "print"} {
		if !names[retained] {
			t.Errorf("retained director flag --%s is missing", retained)
		}
	}
}

func TestDirectorSurfaceArgv(t *testing.T) {
	cfg := directorConfig{
		mode:          modeCodex,
		print:         true,
		image:         "example.invalid/dev-base",
		tag:           "test",
		wardVersion:   "v1.2.3",
		versionSource: wardVersionSourceExplicit,
		contextBundle: "/tmp/context",
		wardSource:    "/tmp/ward",
		noPull:        true,
		withRepo:      []string{"a/extra", " ", "b/extra"},
	}
	want := []string{
		directorSurfaceVerb, "--repo", "a/main", "--harness", string(modeCodex),
		"--image", "example.invalid/dev-base", "--tag", "test", "--ward-version", "v1.2.3",
		"--ward-source", "/tmp/ward", "--context-bundle", "/tmp/context", "--no-pull", "--print",
		"--with-repo", "a/extra", "--with-repo", "b/extra",
	}
	if got := directorSurfaceArgv("a/main", cfg); !reflect.DeepEqual(got, want) {
		t.Fatalf("directorSurfaceArgv() = %#v, want %#v", got, want)
	}
}

func TestValidateDirectorIssueTargetOnlyRequiresOpen(t *testing.T) {
	ref := agentIssueRef{Owner: "a", Repo: "b", Number: 7}
	interactive := &Issue{Number: 7, State: "open", Labels: []string{"interactive"}}
	if err := validateDirectorIssueTarget("ward agent director", ref, interactive); err != nil {
		t.Fatalf("read-only director rejected an open interactive issue: %v", err)
	}
	closed := &Issue{Number: 7, State: "closed"}
	if err := validateDirectorIssueTarget("ward agent director", ref, closed); err == nil || !strings.Contains(err.Error(), "not open") {
		t.Fatalf("closed issue error = %v, want not-open refusal", err)
	}
}

func TestDirectorScopeHelpers(t *testing.T) {
	if got, want := parseScopeRepos(" a/b, c/d,a/b ", ""), []string{"a/b", "c/d"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("parseScopeRepos() = %v, want %v", got, want)
	}
	orgs, repos := partitionScopeEntries([]string{"alpha", "a/b", "alpha", "c/d"})
	if !reflect.DeepEqual(orgs, []string{"alpha"}) || !reflect.DeepEqual(repos, []string{"a/b", "c/d"}) {
		t.Fatalf("partitionScopeEntries() = orgs %v repos %v", orgs, repos)
	}
	if got, want := mergeScopeRepos([]string{"a/b", "c/d"}, []string{"c/d", "e/f"}), []string{"a/b", "c/d", "e/f"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeScopeRepos() = %v, want %v", got, want)
	}
	briefs := []repoBrief{{Name: "live"}, {Name: "archived", Archived: true}, {Name: "empty", Empty: true}}
	if got, want := orgReposToSlugs("a", briefs), []string{"a/live"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("orgReposToSlugs() = %v, want %v", got, want)
	}
}

func directorFlagNames(flags []cli.Flag) []string {
	var names []string
	for _, flag := range flags {
		names = append(names, flag.Names()...)
	}
	return names
}

func containsArg(argv []string, want string) bool {
	for _, arg := range argv {
		if arg == want {
			return true
		}
	}
	return false
}

func argFollowedBy(argv []string, flag, value string) bool {
	for i := 0; i < len(argv)-1; i++ {
		if argv[i] == flag && argv[i+1] == value {
			return true
		}
	}
	return false
}
