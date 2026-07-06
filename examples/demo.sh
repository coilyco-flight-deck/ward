#!/bin/sh
# ward demo - one happy path plus three danger classes (ward#251, ward#250).
#
# The launch thesis is "the boundary is the product": the interesting thing ward
# does is not what it runs, it is what it refuses. So this demo spends one beat
# on capability and three on denial. Every refusal you see below is real ward
# output, reproduced live against examples/toy - no canned strings.
#
# Safe to run. The happy path runs the toy repo's real test verb. None of the
# danger beats executes anything destructive: the argv-injection is rejected by
# the gate before the command runs, the protected-binary beat only feeds a string
# to `ward hook pre-tool-use` (inspect-and-refuse, nothing runs), and the ops-verb
# beat is refused by policy before any endpoint is touched (no token needed).
#
# Honest about mechanism (see ../docs/enforcement-boundary.md). Beat 2 and Beat 4
# are hard gates: the compiled cli-guard pipeline every `ward exec` and `ward ops`
# verb runs through. Beat 3 is the Claude PreToolUse hook, a host-side, claude-only,
# fail-open hint - inside a `ward agent` container the hard edge is the container
# plus cli-guard. The ops-verb denial (Beat 4) is the one issue ward is really
# built for: an agent holding live credentials reaching for an out-of-policy
# operator verb, refused by the operator surface itself (ward#250).
#
# Usage: sh examples/demo.sh   (run from a clean, pushed checkout for the green
# happy path - on an unsynced branch the clean-tree gate refuses, which the demo
# narrates rather than hides).

set -u

# Resolve the toy repo next to this script, wherever the demo is invoked from.
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
TOY="$SCRIPT_DIR/toy"

if ! command -v ward >/dev/null 2>&1; then
	echo "ward is not on PATH - install it first (see ../README.md#install)." >&2
	exit 1
fi
if [ ! -f "$TOY/.ward/ward.yaml" ]; then
	echo "cannot find examples/toy next to this script ($TOY)." >&2
	exit 1
fi

banner() {
	printf '\n===============================================================\n'
	printf '  %s\n' "$1"
	printf '===============================================================\n'
}

say() { printf '  %s\n' "$1"; }

# Show a command, run it, report its exit code. Never aborts the demo on a
# non-zero exit - a refusal is the point, not a failure.
show() {
	printf '\n  $ %s\n' "$1"
	sh -c "$1"
	printf '  [exit %s]\n' "$?"
}

# Feed one bash command to the PreToolUse hook as the toy repo's cwd and show
# the block hint. The hook only inspects the string, it never runs it.
hook_deny() {
	_cmd=$1
	printf '\n  $ echo <%s> | ward hook pre-tool-use\n' "$_cmd"
	printf '{"tool_name":"Bash","tool_input":{"command":"%s"},"cwd":"%s"}' "$_cmd" "$TOY" \
		| ward hook pre-tool-use
	printf '  [exit %s]\n' "$?"
}

cd "$TOY" || exit 1

banner "ward demo - the boundary is the product"
say "Target: examples/toy, ward's minimal managed repo (a POSIX-sh greet CLI)."
say "One capability beat, then three denials. Every line below is live output."

# ----------------------------------------------------------------------------
banner "BEAT 1 - happy path: ward exec test"
# ----------------------------------------------------------------------------
say "ward exec test runs the toy repo's 'make test' through cli-guard. It"
say "validates every argv token, appends one JSONL audit row, and gates on a"
say "clean, synced tree so the row is reconstructable from git history."
show "ward exec test"
say ""
say "The audit row it just wrote (append-only, one per invocation):"
AUDIT_DIR="${HOME}/.ward/audit"
LATEST_AUDIT=$(ls -t "$AUDIT_DIR"/*.jsonl 2>/dev/null | head -1)
if [ -n "${LATEST_AUDIT:-}" ]; then
	printf '\n  $ tail -1 %s\n' "$LATEST_AUDIT"
	tail -1 "$LATEST_AUDIT" | sed 's/^/  /'
else
	say "(no audit row found - ward writes to ~/.ward/audit/<repo>.jsonl)"
fi
say ""
say "For the real end-to-end story ward runs on itself, the run references a"
say "Forgejo issue (fj), since ward is Forgejo-canonical. The public one-liner"
say "an adopter copies uses gh against their own GitHub repo - same gate, same"
say "audit row, either forge."

# ----------------------------------------------------------------------------
banner "BEAT 2 - danger class one: repo danger (the hard gate)"
# ----------------------------------------------------------------------------
say "A destructive argument smuggled into what looks like a benign test filter."
say "This is the class a coding crowd reads instantly: 'gh pr merge' style repo"
say "damage, here as shell injection into 'ward exec test'. cli-guard rejects"
say "any argv token carrying a shell metacharacter before the verb ever runs."
show "ward exec test -- -run 'Cleanup; rm -rf \$HOME'"
say ""
say "This is the hard boundary: the compiled cli-guard verb pipeline, not a"
say "hint. It holds for every harness in the ward agent flow. The clean-tree"
say "gate is the sibling refusal - ward exec test on a branch with no synced"
say "upstream is declined for the same reason (the run must reconstruct from"
say "history). Both bypass loudly: ward --audit-override-dirty stamps the row"
say "audit_override=true so even an emergency stays reconstructable."

# ----------------------------------------------------------------------------
banner "BEAT 3 - danger class two: infra danger (protected binary)"
# ----------------------------------------------------------------------------
say "The bounded-autonomy money shot: an agent reaching for infra it should"
say "never touch bare. The toy repo's security block names kubectl and aws as"
say "protected, so the PreToolUse hook refuses the direct call and routes it"
say "back through an audited wrapper."
hook_deny "kubectl delete deployment checkout-api"
hook_deny "aws s3 rm s3://prod-uploads --recursive"
say ""
say "Honest scope: this beat is the Claude PreToolUse hook, a host-side,"
say "claude-only, fail-open hint (see ../docs/enforcement-boundary.md). It is"
say "the friendly nudge, not the wall. Inside a ward agent container the hard"
say "edge is the container plus cli-guard, and permissions.deny is the hard"
say "host-side denial. Name the mechanism when you show the denial."

# ----------------------------------------------------------------------------
banner "BEAT 4 - danger class three: ops danger (the operator surface)"
# ----------------------------------------------------------------------------
say "The class ward exists for (ward#250): a headless devsecops agent holding"
say "live credentials reaches for an operator verb that is out of policy. This"
say "is the hard sibling of Beat 3 - not a fail-open hint on a bare binary, but"
say "the compiled ward-kdl operator surface refusing the verb itself. ward's"
say "policy does not expose pull-request reads, so the verb is denied by policy"
say "before any endpoint or token is touched:"
show "ward ops forgejo pr list coilyco-flight-deck ward"
say ""
say "That is a real, credential-free policy denial (exit 2), not an absence: the"
say "verb is present on the surface and answers 'denied by policy'. The truly"
say "dangerous mutations are withheld one step further - 'ward ops aws s3' offers"
say "only ls/cp/sync (no rm), and 'ward ops kubectl' has no delete - so the agent"
say "cannot reach them through the gate at all. Allowed ops verbs (ward ops"
say "forgejo issue get, ward ops kubectl get) run audited, exactly like Beat 1;"
say "the log carries both the allow and the deny. This is the boundary a"
say "filesystem-plus-git sandbox cannot draw, because the danger is ops-adjacent."

# ----------------------------------------------------------------------------
banner "The boundary is the product"
# ----------------------------------------------------------------------------
say "One verb ran, audited. Three danger classes refused, each by the mechanism"
say "that actually holds - repo danger and ops danger by the hard cli-guard"
say "surface, infra danger by the claude-only hint. That asymmetry - capability"
say "is cheap, denial is the point - is the whole pitch."
say ""
say "Deeper: ../docs/gate-demo.md, ../docs/exec-verb.md, ../docs/example-repo.md."
printf '\n'
