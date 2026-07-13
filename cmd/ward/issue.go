package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/issueref"
)

// The issue types lived in cli-guard/cli/dispatch, removed as legacy. cli-guard
// parses a *reference* (pkg/issueref) - the issue and its forge are ward's domain.

// Platform tags which forge an issue ref resolves against. Empty means the ref
// was shortform, so the caller picks the forge.
type Platform string

const (
	PlatformGitHub  Platform = "github"
	PlatformForgejo Platform = "forgejo"
)

// IssueRef is the parsed shape of an issue reference.
type IssueRef struct {
	Owner    string
	Repo     string
	Number   int
	Platform Platform
}

func (i IssueRef) String() string {
	return fmt.Sprintf("%s/%s#%d", i.Owner, i.Repo, i.Number)
}

// Issue is the platform-neutral fetch result. GitHub and Forgejo share the same
// JSON field names, so one struct covers both.
type Issue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	State  string `json:"state"`
	URL    string `json:"html_url"`
	// Labels holds the issue's label names, populated by each fetcher (label
	// JSON is objects, not strings, so json:"-"). Drives the ceiling gate.
	Labels []string `json:"-"`
}

// githubIssueRefRE matches github.com issue URLs and compact refs, tolerating scheme,
// www, .git, and trailing query/fragment. issueref.Parse does not know GitHub.
var githubIssueRefRE = regexp.MustCompile(
	`(?i)^(?:https?://)?(?:www\.)?github\.com/([A-Za-z0-9._-]+)/([A-Za-z0-9._-]+?)(?:\.git)?(?:/issues/(\d+)|#(\d+))(?:[/?#].*)?$`,
)

// ParseIssueRef resolves every supported reference form. GitHub is matched first
// (issueref.Parse would never see it), then cli-guard normalizes the rest identically.
func ParseIssueRef(baseURL, s string) (IssueRef, error) {
	s = strings.TrimSpace(s)
	if m := githubIssueRefRE.FindStringSubmatch(s); m != nil {
		num := m[3]
		if num == "" {
			num = m[4]
		}
		return buildIssueRef(m[1], m[2], num, PlatformGitHub, s)
	}
	ref, err := issueref.Parse(s, baseURL)
	if err == nil {
		return IssueRef{
			Owner:    ref.Owner,
			Repo:     ref.Repo,
			Number:   ref.Number,
			Platform: platformOf(s, baseURL),
		}, nil
	}
	// A scheme-less Forgejo URL (forge.host/owner/repo/issues/N) is still a
	// Forgejo ref; issueref.Parse only matches it once it carries a scheme.
	if baseURL != "" && !strings.Contains(s, "://") {
		if ref, retryErr := issueref.Parse("https://"+s, baseURL); retryErr == nil {
			return IssueRef{
				Owner:    ref.Owner,
				Repo:     ref.Repo,
				Number:   ref.Number,
				Platform: PlatformForgejo,
			}, nil
		}
	}
	return IssueRef{}, err
}

// platformOf tags a ref that issueref.Parse accepted. Parse does not report which
// form matched, so the Forgejo host separates a URL ref from untagged owner/repo#N.
func platformOf(s, baseURL string) Platform {
	if baseURL == "" {
		return ""
	}
	host := strings.TrimRight(strings.TrimPrefix(strings.TrimPrefix(baseURL, "https://"), "http://"), "/")
	if host == "" {
		return ""
	}
	if strings.Contains(strings.ToLower(s), strings.ToLower(host)+"/") {
		return PlatformForgejo
	}
	return ""
}

func buildIssueRef(owner, repo, num string, platform Platform, s string) (IssueRef, error) {
	n, err := strconv.Atoi(num)
	if err != nil {
		return IssueRef{}, fmt.Errorf("parse issue number in %q: %w", s, err)
	}
	if n <= 0 {
		return IssueRef{}, fmt.Errorf("issue number must be positive: %q", s)
	}
	return IssueRef{Owner: owner, Repo: repo, Number: n, Platform: platform}, nil
}
