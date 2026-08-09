package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/audit"
)

const maxForgejoEventBytes = 1 << 20

var (
	forgejoPullRefPattern = regexp.MustCompile(`^refs/pull/([1-9][0-9]*)/merge$`)
	fullObjectIDPattern   = regexp.MustCompile(`^(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64})$`)
)

type forgejoPREvent struct {
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Sender struct {
		Login string `json:"login"`
	} `json:"sender"`
	PullRequest *struct {
		Base struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"base"`
		Head struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"head"`
	} `json:"pull_request"`
}

type forgejoActionsMetadata struct {
	serverURL   string
	repository  string
	workspace   string
	eventName   string
	eventRef    string
	pullRequest string
	baseRef     string
	headRef     string
	headSHA     string
	eventPath   string
	workflow    string
	job         string
	actor       string
	runID       string
	runNumber   string
	runAttempt  string
}

func forgejoActionsPullRequestEnvPresent() bool {
	for _, pair := range [][2]string{{"CI", "true"}, {"GITHUB_ACTIONS", "true"}, {"FORGEJO_ACTIONS", "true"}, {"GITHUB_EVENT_NAME", "pull_request"}} {
		if strings.TrimSpace(os.Getenv(pair[0])) != pair[1] {
			return false
		}
	}
	return true
}

func validateForgejoActionsPR(repoRoot string) (*audit.CIContext, error) {
	metadata, err := readForgejoActionsMetadata()
	if err != nil {
		return nil, err
	}
	if err := validateForgejoOrigin(repoRoot, metadata.serverURL, metadata.repository); err != nil {
		return nil, err
	}
	if err := validateForgejoWorkspace(repoRoot, metadata.workspace); err != nil {
		return nil, err
	}
	actualHEAD, err := validateCurrentHEAD(repoRoot, metadata.headSHA)
	if err != nil {
		return nil, err
	}
	if err := validateForgejoPREvent(repoRoot, metadata); err != nil {
		return nil, err
	}
	return metadata.auditContext(actualHEAD), nil
}

func readForgejoActionsMetadata() (*forgejoActionsMetadata, error) {
	if err := validateForgejoRunnerEnv(); err != nil {
		return nil, err
	}
	values, err := requiredCIEnvSet(
		"GITHUB_SERVER_URL", "GITHUB_REPOSITORY", "GITHUB_EVENT_NAME", "GITHUB_REF",
		"GITHUB_SHA", "GITHUB_BASE_REF", "GITHUB_HEAD_REF", "GITHUB_ACTOR",
		"GITHUB_WORKFLOW", "GITHUB_JOB", "GITHUB_RUN_ID", "GITHUB_RUN_NUMBER",
		"GITHUB_RUN_ATTEMPT", "GITHUB_EVENT_PATH", "GITHUB_WORKSPACE",
	)
	if err != nil {
		return nil, err
	}
	if strings.Count(values["GITHUB_REPOSITORY"], "/") != 1 || strings.ContainsAny(values["GITHUB_REPOSITORY"], " \t\r\n") {
		return nil, fmt.Errorf("GITHUB_REPOSITORY must be owner/repo")
	}
	if values["GITHUB_EVENT_NAME"] != "pull_request" {
		return nil, fmt.Errorf("GITHUB_EVENT_NAME %q is not pull_request", values["GITHUB_EVENT_NAME"])
	}
	refMatch := forgejoPullRefPattern.FindStringSubmatch(values["GITHUB_REF"])
	if refMatch == nil {
		return nil, fmt.Errorf("GITHUB_REF %q is not refs/pull/<number>/merge", values["GITHUB_REF"])
	}
	if !fullObjectIDPattern.MatchString(values["GITHUB_SHA"]) {
		return nil, fmt.Errorf("GITHUB_SHA is not a full Git object ID")
	}
	for _, name := range []string{"GITHUB_RUN_ID", "GITHUB_RUN_NUMBER", "GITHUB_RUN_ATTEMPT"} {
		if err := validatePositiveDecimal(name, values[name]); err != nil {
			return nil, err
		}
	}
	return &forgejoActionsMetadata{
		serverURL:   values["GITHUB_SERVER_URL"],
		repository:  values["GITHUB_REPOSITORY"],
		workspace:   values["GITHUB_WORKSPACE"],
		eventName:   values["GITHUB_EVENT_NAME"],
		eventRef:    values["GITHUB_REF"],
		pullRequest: refMatch[1],
		baseRef:     values["GITHUB_BASE_REF"],
		headRef:     values["GITHUB_HEAD_REF"],
		headSHA:     values["GITHUB_SHA"],
		eventPath:   values["GITHUB_EVENT_PATH"],
		workflow:    values["GITHUB_WORKFLOW"],
		job:         values["GITHUB_JOB"],
		actor:       values["GITHUB_ACTOR"],
		runID:       values["GITHUB_RUN_ID"],
		runNumber:   values["GITHUB_RUN_NUMBER"],
		runAttempt:  values["GITHUB_RUN_ATTEMPT"],
	}, nil
}

func validateForgejoRunnerEnv() error {
	for _, pair := range [][2]string{{"CI", "true"}, {"GITHUB_ACTIONS", "true"}, {"FORGEJO_ACTIONS", "true"}} {
		if got := strings.TrimSpace(os.Getenv(pair[0])); got != pair[1] {
			return fmt.Errorf("%s must equal %q", pair[0], pair[1])
		}
	}
	return nil
}

func validateCurrentHEAD(repoRoot, expectedSHA string) (string, error) {
	actualHEAD, err := runGitReadOnly(repoRoot, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return "", fmt.Errorf("resolve HEAD: %w", err)
	}
	actualHEAD = strings.TrimSpace(actualHEAD)
	if !strings.EqualFold(expectedSHA, actualHEAD) {
		return "", fmt.Errorf("GITHUB_SHA does not match HEAD")
	}
	return actualHEAD, nil
}

func validateForgejoPREvent(repoRoot string, metadata *forgejoActionsMetadata) error {
	event, err := readForgejoPREvent(metadata.eventPath)
	if err != nil {
		return err
	}
	if event.PullRequest == nil {
		return fmt.Errorf("GITHUB_EVENT_PATH has no pull_request object")
	}
	if event.Repository.FullName != metadata.repository {
		return fmt.Errorf("event repository does not match GITHUB_REPOSITORY")
	}
	if event.Sender.Login != metadata.actor {
		return fmt.Errorf("event sender does not match GITHUB_ACTOR")
	}
	if event.PullRequest.Base.Ref != metadata.baseRef || event.PullRequest.Head.Ref != metadata.headRef {
		return fmt.Errorf("event base/head refs do not match GITHUB_BASE_REF and GITHUB_HEAD_REF")
	}
	if !fullObjectIDPattern.MatchString(event.PullRequest.Base.SHA) || !fullObjectIDPattern.MatchString(event.PullRequest.Head.SHA) {
		return fmt.Errorf("event base/head SHAs must be full Git object IDs")
	}
	return validateCurrentMergeParents(repoRoot, event.PullRequest.Base.SHA, event.PullRequest.Head.SHA)
}

func validateCurrentMergeParents(repoRoot, baseSHA, headSHA string) error {
	parents, err := headCommitParents(repoRoot)
	if err != nil {
		return err
	}
	if len(parents) != 2 {
		return fmt.Errorf("HEAD has %d parents, want a two-parent pull-request merge", len(parents))
	}
	if !strings.EqualFold(parents[0], baseSHA) || !strings.EqualFold(parents[1], headSHA) {
		return fmt.Errorf("HEAD parents do not match event base/head SHAs")
	}
	return nil
}

func (metadata *forgejoActionsMetadata) auditContext(actualHEAD string) *audit.CIContext {
	return &audit.CIContext{
		Provider:    "forgejo-actions",
		ServerURL:   metadata.serverURL,
		Repository:  metadata.repository,
		EventName:   metadata.eventName,
		EventRef:    metadata.eventRef,
		PullRequest: metadata.pullRequest,
		BaseRef:     metadata.baseRef,
		HeadRef:     metadata.headRef,
		HeadSHA:     strings.ToLower(actualHEAD),
		Workflow:    metadata.workflow,
		Job:         metadata.job,
		Actor:       metadata.actor,
		RunID:       metadata.runID,
		RunNumber:   metadata.runNumber,
		RunAttempt:  metadata.runAttempt,
	}
}

func requiredCIEnv(name string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", fmt.Errorf("%s is missing", name)
	}
	return value, nil
}

func requiredCIEnvSet(names ...string) (map[string]string, error) {
	values := make(map[string]string, len(names))
	for _, name := range names {
		value, err := requiredCIEnv(name)
		if err != nil {
			return nil, err
		}
		values[name] = value
	}
	return values, nil
}

func validatePositiveDecimal(name, value string) error {
	parsed, parseErr := strconv.ParseUint(value, 10, 64)
	if parseErr != nil || parsed == 0 {
		return fmt.Errorf("%s must be a positive decimal identifier", name)
	}
	return nil
}

func validateForgejoOrigin(repoRoot, rawServerURL, repository string) error {
	server, err := url.Parse(rawServerURL)
	if err != nil || (server.Scheme != "http" && server.Scheme != "https") || server.Hostname() == "" {
		return fmt.Errorf("GITHUB_SERVER_URL must be an absolute HTTP(S) URL")
	}
	if server.User != nil || server.RawQuery != "" || server.Fragment != "" {
		return fmt.Errorf("GITHUB_SERVER_URL must not contain credentials, query, or fragment")
	}
	origin, err := runGitReadOnly(repoRoot, "remote", "get-url", "origin")
	if err != nil {
		return fmt.Errorf("read origin remote: %w", err)
	}
	originHost, originPath, err := splitGitRemote(strings.TrimSpace(origin))
	if err != nil {
		return err
	}
	if !strings.EqualFold(server.Hostname(), originHost) {
		return fmt.Errorf("origin host does not match GITHUB_SERVER_URL")
	}
	expectedPath := cleanRepoPath(path.Join(server.Path, repository))
	if originPath != expectedPath {
		return fmt.Errorf("origin repository does not match GITHUB_REPOSITORY")
	}
	return nil
}

func validateForgejoWorkspace(repoRoot, workspace string) error {
	repoInfo, err := os.Stat(repoRoot)
	if err != nil {
		return fmt.Errorf("stat repository root: %w", err)
	}
	workspaceInfo, err := os.Stat(workspace)
	if err != nil {
		return fmt.Errorf("stat GITHUB_WORKSPACE: %w", err)
	}
	if !repoInfo.IsDir() || !workspaceInfo.IsDir() || !os.SameFile(repoInfo, workspaceInfo) {
		return fmt.Errorf("GITHUB_WORKSPACE does not match the repository root")
	}
	return nil
}

func splitGitRemote(raw string) (string, string, error) {
	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Hostname() == "" {
			return "", "", fmt.Errorf("origin remote URL is invalid")
		}
		return parsed.Hostname(), cleanRepoPath(parsed.Path), nil
	}
	colon := strings.IndexByte(raw, ':')
	if colon <= 0 || strings.Contains(raw[:colon], "/") {
		return "", "", fmt.Errorf("origin remote must be a network URL")
	}
	host := raw[:colon]
	if at := strings.LastIndexByte(host, '@'); at >= 0 {
		host = host[at+1:]
	}
	if host == "" {
		return "", "", fmt.Errorf("origin remote host is empty")
	}
	return host, cleanRepoPath(raw[colon+1:]), nil
}

func cleanRepoPath(raw string) string {
	cleaned := strings.TrimPrefix(path.Clean("/"+strings.TrimSpace(raw)), "/")
	return strings.TrimSuffix(cleaned, ".git")
}

func readForgejoPREvent(eventPath string) (*forgejoPREvent, error) {
	if !filepath.IsAbs(eventPath) {
		return nil, fmt.Errorf("GITHUB_EVENT_PATH must be absolute")
	}
	info, err := os.Stat(eventPath)
	if err != nil {
		return nil, fmt.Errorf("stat GITHUB_EVENT_PATH: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > maxForgejoEventBytes {
		return nil, fmt.Errorf("GITHUB_EVENT_PATH must be a regular file no larger than %d bytes", maxForgejoEventBytes)
	}
	body, err := os.ReadFile(eventPath) // #nosec G304 -- explicit CI metadata file, size-capped above
	if err != nil {
		return nil, fmt.Errorf("read GITHUB_EVENT_PATH: %w", err)
	}
	var event forgejoPREvent
	if err := json.Unmarshal(body, &event); err != nil {
		return nil, fmt.Errorf("decode GITHUB_EVENT_PATH: %w", err)
	}
	return &event, nil
}

func headCommitParents(repoRoot string) ([]string, error) {
	commit, err := runGitReadOnly(repoRoot, "cat-file", "-p", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("inspect HEAD commit: %w", err)
	}
	var parents []string
	for _, line := range strings.Split(commit, "\n") {
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "parent ") {
			parents = append(parents, strings.TrimSpace(strings.TrimPrefix(line, "parent ")))
		}
	}
	return parents, nil
}
