#!/usr/bin/env python3
"""Lint (and optionally fix) hardcoded issue references in public-facing docs.

Why this exists (ward#446): a bare issue ref like `ward#123` or `#123` does not
render as a link on the GitHub mirror - GitHub only autolinks `#N` / `owner/repo#N`
inside issues, PRs, and commit messages, never inside a rendered Markdown file. So
every terse ref in a doc a stranger reads on github.com is a dead reference. The
launch rule (ward#446 / ward#443): every ref in a PUBLIC-facing surface must be a
real Markdown link to a full URL, so it resolves for a GitHub reader. Contributor
-facing surfaces (AGENTS.md, CONTRIBUTING.md, Go source) keep terse Forgejo refs.

This one script is both halves the issue asks for:
  * `--check` (default) is the linter - it exits non-zero listing every terse ref
    left in a public surface, and is wired into the pre-commit gate so the rule
    stays true.
  * `--fix` is the one-hard-scrub - it rewrites every terse ref into a full-URL
    Markdown link using the repo map below, skipping code blocks, inline code, and
    refs that are already inside a link.

Refs inside fenced code blocks / inline code are illustrations (command examples),
not clickable references, and are left untouched.

Scope: README.md + docs/*.md, minus CONTRIBUTOR_DOCS. That set was the deep design
docs hand-tuned to the documentation-layout 4000-char cap, once exempt because
expanding their terse refs into ~76-char full-URL links would have busted the cap.
ward#521 trimmed the provenance-ref noise from each (mostly trailing `(ward#NNN)`
citations that read as internal-wiki clutter per ward#444) so they now fit the cap
WITH their remaining refs linked, and CONTRIBUTOR_DOCS is empty. It stays defined so
a future oversized design doc can be parked here again if it needs the same runway.
"""

from __future__ import annotations

import argparse
import glob
import os
import re
import sys

# Repo root is the parent of this scripts/ dir, so the linter runs the same from
# anywhere (pre-commit passes absolute paths; a bare invocation globs from here).
REPO_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

FORGEJO = "https://forgejo.coilysiren.me"
GITHUB = "https://github.com"

# prefix (terse repo shorthand) -> canonical issue web base. ward/cli-guard are
# Forgejo; the agentic-os family, coily, and the taps are GitHub (ward-kdl is ward).
PREFIX_BASE = {
    "ward": f"{FORGEJO}/coilyco-flight-deck/ward",
    "ward-kdl": f"{FORGEJO}/coilyco-flight-deck/ward",
    "cli-guard": f"{FORGEJO}/coilyco-flight-deck/cli-guard",
    "agentic-os": f"{GITHUB}/coilysiren/agentic-os",
    "aos": f"{GITHUB}/coilysiren/agentic-os",
    "agentic-os-kai": f"{GITHUB}/coilysiren/agentic-os-kai",
    "coily": f"{GITHUB}/coilyco-bridge/coily",
    "homebrew-tap": f"{GITHUB}/coilysiren/homebrew-tap",
    "infrastructure": f"{GITHUB}/coilysiren/infrastructure",
    "infra": f"{GITHUB}/coilysiren/infrastructure",
}

# owner -> forge web base, for already-qualified `owner/repo#N` refs. coilyco-flight
# -deck is the Forgejo org; the rest are GitHub owners.
OWNER_BASE = {
    "coilyco-flight-deck": FORGEJO,
    "coilysiren": GITHUB,
    "coilyco-bridge": GITHUB,
    "coilyco-gaming": GITHUB,
}

# Public-facing surfaces a GitHub reader browses. Everything else (AGENTS.md, *.go,
# .agents/, scripts/) is contributor-facing and keeps terse refs (ward#443).
INCLUDE_GLOBS = ["README.md", "docs/**/*.md"]

# Never scanned: generated docs (would drift against their generator) and the PR
# template (its `Fixes #123` is a GitHub example that resolves in a real PR).
EXCLUDE_SUBSTR = ["docs/ward-kdl/", "docs/agent-roster.md", "docs/PULL_REQUEST_TEMPLATE.md"]

# Was the deep design docs hand-tuned to the 4000-char doc cap; ward#521 trimmed
# each to fit WITH linked refs, so it is empty (kept defined for reuse; see docstring).
CONTRIBUTOR_DOCS: set[str] = set()

# A terse ref: an optional `owner/` then a repo-ish word, then `#digits`. The
# leading boundary keeps us off URL paths (.../issues/12) and mid-word hits.
REF_RE = re.compile(
    r"(?<![\w/#.-])"  # not preceded by a word char, slash, hash, dot, or dash
    r"(?:(?P<owner>[A-Za-z0-9][\w.-]*)/)?"  # optional owner/
    r"(?P<repo>[A-Za-z][\w.-]*?)"  # repo shorthand
    r"#(?P<num>\d+)"  # #issuenumber
    r"(?![\w-])"  # not followed by more word chars
)
# A bare `#123` (no prefix) resolves to ward. URLs/links are masked first, so the
# lookbehind only fends off word chars, `#`, and link brackets (`#455/#437` is ok).
BARE_RE = re.compile(r"(?<![\w#\[])#(?P<num>\d+)(?![\w-])")


def iter_files():
    seen = set()
    for pat in INCLUDE_GLOBS:
        for path in glob.glob(os.path.join(REPO_ROOT, pat), recursive=True):
            rel = os.path.relpath(path, REPO_ROOT).replace(os.sep, "/")
            if rel in CONTRIBUTOR_DOCS or any(s in rel for s in EXCLUDE_SUBSTR):
                continue
            if rel not in seen and os.path.isfile(path):
                seen.add(rel)
                yield path, rel
    # deterministic order for stable output
    return


def mask_text(text: str) -> str:
    """Blank out inline code, Markdown links, and bare URLs across the whole file,
    preserving every character position (newlines kept, other masked chars -> space)
    so line/column offsets still line up with the original. Terse refs only survive
    in plain prose, which is exactly what we want to catch. Whole-text (not per-line)
    masking is deliberate: an inline code span like `ward agent\\n#98` wraps a line,
    and a per-line pass would mistake its `#98` tail for a prose ref."""
    out = list(text)

    def blank(m):
        for i in range(m.start(), m.end()):
            if out[i] != "\n":
                out[i] = " "

    # inline code spans `...` (may wrap lines: [^`] includes newline)
    for m in re.finditer(r"`[^`]*`", text):
        blank(m)
    cur = "".join(out)
    # markdown links [text](target) - masks both the ref-as-link-text and the URL
    for m in re.finditer(r"\[[^\]]*\]\([^)]*\)", cur):
        blank(m)
    cur = "".join(out)
    # bare/autolink URLs
    for m in re.finditer(r"<?https?://[^\s>)]+>?", cur):
        blank(m)
    return "".join(out)


def url_for(owner: str, repo: str) -> str | None:
    if owner:
        base = OWNER_BASE.get(owner)
        return f"{base}/{owner}/{repo}" if base else None
    return PREFIX_BASE.get(repo)


def find_violations(masked: str):
    """Yield (start, end, token, url_base_or_None) for each terse ref in masked."""
    hits = []
    for m in REF_RE.finditer(masked):
        owner = m.group("owner") or ""
        repo = m.group("repo")
        base = url_for(owner, repo)
        hits.append((m.start(), m.end(), m.group(0), base))
    # bare #N (skip spans already covered by a prefixed hit)
    covered = [(s, e) for s, e, _, _ in hits]
    for m in BARE_RE.finditer(masked):
        if any(s <= m.start() < e for s, e in covered):
            continue
        hits.append((m.start(), m.end(), m.group(0), PREFIX_BASE["ward"]))
    hits.sort()
    return hits


def link_for(token: str, base: str) -> str:
    num = token.rsplit("#", 1)[1]
    return f"[{token}]({base}/issues/{num})"


def process(path: str, fix: bool):
    with open(path, encoding="utf-8") as fh:
        lines = fh.readlines()

    # Blank fenced code blocks (``` / ~~~) line-by-line first, keeping newlines so
    # positions survive, then mask inline code / links / URLs across the whole file.
    fenced = False
    scrubbed = []
    for line in lines:
        if re.match(r"^\s*(```|~~~)", line):
            fenced = not fenced
            scrubbed.append("\n" if line.endswith("\n") else "")
            continue
        scrubbed.append((" " * (len(line) - 1) + "\n") if fenced and line.endswith("\n") else (" " * len(line) if fenced else line))
    masked_lines = mask_text("".join(scrubbed)).split("\n")

    violations = []  # (lineno, token, fixable)
    for idx, line in enumerate(lines):
        masked = masked_lines[idx] if idx < len(masked_lines) else ""
        hits = find_violations(masked)
        if not hits:
            continue
        if fix:
            newline = line
            for start, end, token, base in reversed(hits):
                if base is None:
                    continue  # unmappable: leave for the human, still reported
                newline = newline[:start] + link_for(token, base) + newline[end:]
            lines[idx] = newline
        for _, _, token, base in hits:
            violations.append((idx + 1, token, base is not None))
    if fix and violations:
        with open(path, "w", encoding="utf-8") as fh:
            fh.writelines(lines)
    return violations


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--fix", action="store_true", help="rewrite terse refs into full-URL links")
    ap.add_argument("paths", nargs="*", help="optional explicit files (pre-commit passes these)")
    args = ap.parse_args()

    if args.paths:
        targets = []
        for p in args.paths:
            rel = os.path.relpath(os.path.abspath(p), REPO_ROOT).replace(os.sep, "/")
            if rel in CONTRIBUTOR_DOCS or any(s in rel for s in EXCLUDE_SUBSTR):
                continue
            if rel == "README.md" or rel.startswith("docs/"):
                targets.append((os.path.abspath(p), rel))
    else:
        targets = sorted(iter_files(), key=lambda t: t[1])

    total = 0
    unmappable = 0
    for path, rel in targets:
        for lineno, token, fixable in process(path, args.fix):
            total += 1
            if not fixable:
                unmappable += 1
            if not args.fix or not fixable:
                tag = "" if fixable else "  <-- no URL map, fix by hand"
                print(f"{rel}:{lineno}: terse issue ref '{token}'{tag}")

    if args.fix:
        print(f"lint_issue_refs: rewrote refs in public docs ({total} refs seen, {unmappable} unmappable left).")
        return 1 if unmappable else 0

    if total:
        print(
            f"\nlint_issue_refs: {total} terse issue ref(s) in public-facing docs "
            f"will not resolve for a GitHub reader (ward#446).\n"
            f"Run `python scripts/lint_issue_refs.py --fix` to rewrite them as full-URL links.",
            file=sys.stderr,
        )
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
