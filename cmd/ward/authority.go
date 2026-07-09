package main

import "strings"

// authority.go centralizes repo-scope forge policy.
// The namespace decides whether a bare owner/repo ref resolves to GitHub or Forgejo.

// repoAuthorityOverrides is the explicit repo-level opt-out/in map.
// Keep it sparse so special cases live here only when a repo must diverge.
var repoAuthorityOverrides = map[string]forge{}

// repoAuthority reports which forge owns a repo's issue/PR surface.
func repoAuthority(owner, repo string) forge {
	slug := strings.TrimSpace(owner) + "/" + strings.TrimSpace(repo)
	if f, ok := repoAuthorityOverrides[slug]; ok {
		return f
	}
	return ownerAuthority(owner)
}

// ownerAuthority reports the namespace default for an org/owner.
func ownerAuthority(owner string) forge {
	switch strings.TrimSpace(owner) {
	case "coilysiren":
		return forgeGitHub
	case "coilyco-flight-deck", "coilyco-bridge", "coilyco-gaming":
		return forgeForgejo
	default:
		return forgeForgejo
	}
}

// explicitForgejoIssueRef reports whether the operator spelled an explicit
// Forgejo URL or host-bearing ref. Bare owner/repo#N refs are not explicit.
func explicitForgejoIssueRef(s string) bool {
	s = strings.TrimSpace(s)
	return strings.Contains(s, forgejoCanonicalHost()) || strings.Contains(s, "://")
}
