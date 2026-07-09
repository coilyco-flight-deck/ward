---
doc_goal: Explain the temporary engineer-default bypass for the in-container review gate so readers know the skip is intentional, short-lived, and still leaves `ward agent review` callable for diagnostics.
---
# ward agent: temporary review default

Engineer dispatches currently skip the in-container review gate by default as a
temporary ward default pending brokered QA.

That keeps new launches moving while review launch regressions are repaired, but
the gate still exists, `--skip-review` and config skips still work, and
`ward agent review` remains callable directly for diagnostics.
