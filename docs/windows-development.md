---
doc_goal: Define Ward's native Windows test prerequisites, isolation, platform exceptions, and cross-compile lane.
---
# Windows development

Ward's full unit-test lane runs on native Windows:

```powershell
ward exec test
```

There is no Windows-only baseline, `-short` mode, or build tag. A failure in
that command is a test failure to investigate, not expected platform noise.
Linux CI remains the authoritative merge gate.

## Requirements

The repository test lane needs Go, GNU Make, and Git for Windows' `sh.exe` on
`PATH`. Ward's command-heavy tests write POSIX shell fixtures because the
commands they model run in Linux containers. On Windows, the test harness gives
each fixture an `.exe` proxy and disables MSYS argument conversion so native
argv reaches the fixture unchanged.

Test setup also redirects `HOME`, `USERPROFILE`, `LocalAppData`, and cache state
to temporary directories. Host Ward and agent configuration must not influence
the result. Container and transcript paths use POSIX semantics; host filesystem
paths continue to use native Windows semantics. Tests for Unix-only signals,
filesystem modes, and release shell scripts skip explicitly.

## Cross-compile check

From Linux, use:

```console
ward exec test-windows-compile
```

This compiles every test package for `windows/amd64` and catches build-tag,
platform-API, filename-case, and embedded-asset compile regressions. It does not
run the Windows binaries and does not replace the native lane.

## umbra workspace

[`make workspace`](workspace.md) can resolve Ward imports from a sibling
umbra checkout, but `ward exec test` still tests Ward's package tree only.
Run umbra's own repository test verb in that checkout; its test policy and
platform exceptions belong there.
