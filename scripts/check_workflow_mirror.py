#!/usr/bin/env python3
"""Lint and optionally fix the Forgejo <-> GitHub test-workflow mirror."""

from __future__ import annotations

import argparse
import difflib
import glob
import os
import re
import sys

REPO_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
WORKFLOW_GLOBS = [".github/workflows/*.yml", ".forgejo/workflows/*.yml"]
RUNNER = {"github": "ubuntu-latest", "forgejo": "docker"}
MIRRORED = [
    {
        "name": "test",
        "github": ".github/workflows/test.yml",
        "forgejo": ".forgejo/workflows/test.yml",
    },
]
FORGEJO_ONLY = [
    ".forgejo/workflows/release.yml",
    ".forgejo/workflows/promote.yml",
]
GITHUB_ONLY: list[str] = []
RUNS_ON_RE = re.compile(r"^(?P<pre>\s*runs-on:\s*)(?P<val>\S+)(?P<post>\s*)$")


def read(rel: str) -> str:
    with open(os.path.join(REPO_ROOT, rel), encoding="utf-8") as fh:
        return fh.read()


def runs_on_values(text: str) -> list[str]:
    return [m.group("val") for line in text.splitlines() if (m := RUNS_ON_RE.match(line))]


def normalize_runs_on(text: str) -> str:
    out = []
    for line in text.splitlines(keepends=True):
        m = RUNS_ON_RE.match(line.rstrip("\n"))
        if m:
            nl = "\n" if line.endswith("\n") else ""
            out.append(m.group("pre").rstrip() + " <RUNNER>" + nl)
        else:
            out.append(line)
    return "".join(out)


def rewrite_runs_on(text: str, value: str) -> str:
    out = []
    for line in text.splitlines(keepends=True):
        m = RUNS_ON_RE.match(line.rstrip("\n"))
        if m:
            nl = "\n" if line.endswith("\n") else ""
            out.append(m.group("pre") + value + nl)
        else:
            out.append(line)
    return "".join(out)


def check_classification(errors: list[str]) -> None:
    classified = set(GITHUB_ONLY) | set(FORGEJO_ONLY)
    for pair in MIRRORED:
        classified.add(pair["github"])
        classified.add(pair["forgejo"])
    for pat in WORKFLOW_GLOBS:
        for path in sorted(glob.glob(os.path.join(REPO_ROOT, pat))):
            rel = os.path.relpath(path, REPO_ROOT).replace(os.sep, "/")
            if rel not in classified:
                errors.append(
                    f"{rel}: workflow is not classified. Add it to MIRRORED, "
                    f"FORGEJO_ONLY, or GITHUB_ONLY in scripts/check_workflow_mirror.py."
                )
    for rel in sorted(classified):
        if not os.path.isfile(os.path.join(REPO_ROOT, rel)):
            errors.append(f"{rel}: listed in the manifest but does not exist on disk.")


def check_runner(rel: str, forge: str, errors: list[str]) -> None:
    want = RUNNER[forge]
    for val in runs_on_values(read(rel)):
        if val != want:
            errors.append(
                f"{rel}: runs-on is '{val}', but the {forge} side must use '{want}'."
            )


def check_pair(pair: dict, errors: list[str]) -> None:
    gh, fj = pair["github"], pair["forgejo"]
    for rel in (gh, fj):
        if not os.path.isfile(os.path.join(REPO_ROOT, rel)):
            errors.append(f"{rel}: mirrored '{pair['name']}' workflow is missing.")
            return
    check_runner(gh, "github", errors)
    check_runner(fj, "forgejo", errors)
    gh_norm = normalize_runs_on(read(gh))
    fj_norm = normalize_runs_on(read(fj))
    if gh_norm != fj_norm:
        diff = "".join(
            difflib.unified_diff(
                gh_norm.splitlines(keepends=True),
                fj_norm.splitlines(keepends=True),
                fromfile=f"{gh} (normalized)",
                tofile=f"{fj} (normalized)",
            )
        )
        errors.append(
            f"mirrored '{pair['name']}' pair diverges (beyond runs-on):\n{diff}"
            f"Run `make lint-workflows ARGS=--fix` to regenerate the Forgejo mirror "
            f"from {gh}."
        )


def fix_pair(pair: dict) -> bool:
    gh, fj = pair["github"], pair["forgejo"]
    if not os.path.isfile(os.path.join(REPO_ROOT, gh)):
        print(f"check_workflow_mirror: source {gh} missing; cannot fix.", file=sys.stderr)
        return False
    desired = rewrite_runs_on(read(gh), RUNNER["forgejo"])
    fj_path = os.path.join(REPO_ROOT, fj)
    current = read(fj) if os.path.isfile(fj_path) else None
    if current == desired:
        return False
    with open(fj_path, "w", encoding="utf-8") as fh:
        fh.write(desired)
    print(f"check_workflow_mirror: wrote {fj} from {gh} (runs-on -> {RUNNER['forgejo']}).")
    return True


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--fix", action="store_true", help="regenerate mirrored Forgejo files from their GitHub source")
    ap.add_argument("paths", nargs="*", help="ignored; accepted for pre-commit compatibility")
    args = ap.parse_args()

    if args.fix:
        changed = False
        for pair in MIRRORED:
            changed |= fix_pair(pair)
        if not changed:
            print("check_workflow_mirror: mirror already in sync; nothing to fix.")
        errors: list[str] = []
        check_classification(errors)
        for pair in MIRRORED:
            check_pair(pair, errors)
        for e in errors:
            print(e, file=sys.stderr)
        return 1 if errors else 0

    errors = []
    check_classification(errors)
    for pair in MIRRORED:
        check_pair(pair, errors)
    if errors:
        for e in errors:
            print(e, file=sys.stderr)
        print(
            f"\ncheck_workflow_mirror: {len(errors)} problem(s) in the Forgejo<->GitHub workflow mirror (ward#214).",
            file=sys.stderr,
        )
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
