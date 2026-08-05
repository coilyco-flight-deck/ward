---
doc_goal: Capture the human-feedback gate in one small page so the PR workflow docs stay under the size cap.
---
# ward agent human feedback

Ward admits tracker records by authenticated author and fixed record kind. A
marker, matching free-text username, or shared credential is never enough.

* Deployment supplies exact collaborator logins through
  `WARD_TRUSTED_COLLABORATORS` and one automation login through
  `WARD_AUTOMATION_ACTOR` to the broker launch.
* Missing or overlapping identities fail closed.
* The broker verifies its authenticated Forgejo login matches the configured
  automation actor before it accepts an agent-role tracker mutation.
* Only the configured automation actor can mint Ward machine state, and only
  the built-in `WARD-WORKFLOW:` family plus Ward's fixed hidden markers count.
* A trusted collaborator's prose is trusted human input. Her marker-shaped
  comment remains human input and cannot mint machine state.
* Plain automation-actor prose is input, not machine state or acknowledgement.
* External prose remains ordinary input. External marker forgery and missing
  authors are invalid.
* Former custom ignore-author and automation-marker settings are unsupported.

The gate uses comment threads plus available issue and pull-request update
timestamps. It blocks close, reopen, merge, and terminal consumption when newer
input lacks a later trusted machine acknowledgement.

## Approving external input

External issue or pull-request text is absent from model prompts until a
trusted collaborator approves an exact snapshot. The same rule covers linked
issues and selected external comments.

1. Generate the read-only plan. Repeat `--comment-id` for each external comment
   that should enter the prompt.

   ```console
   ward agent approval-plan coilyco-flight-deck/ward#1586 --comment-id 123
   ```

2. Post the plan's `intent_body` as an exact Forgejo comment under a configured
   trusted collaborator login. The intent contains the target kind, target ref,
   canonical SHA-256 hash, and sorted selected comment IDs.

3. From the read-only director surface, ask the broker to verify and seal it.

   ```console
   ward agent issue approve coilyco-flight-deck/ward#1586 --intent-comment-id 456
   ```

Use `--pr` on both commands for a pull request. Planning never writes. Approval
is unavailable without the director's master broker capability.

The broker re-reads the target and complete comment thread, verifies the intent
author, reconstructs the canonical JSON, compares the hash and selected
objects, verifies its own automation identity, then posts one complete
`WARD-APPROVAL: v1` record. Snapshots are capped at 256 KiB and never truncated.

An edited target, edited or missing selected comment, actor-policy change,
missing author or timestamp, or later non-machine comment invalidates the
snapshot before triage, prompts, dispatch, QA, merge, or recovery. A new plan
and intent are then required.

## Credential boundary

Engineer and QA launches receive `WARD_FORGEJO_GIT_TOKEN`, a deployment-owned
Git-only credential that must differ from `FORGEJO_TOKEN`. The broad token never
falls back into those launches. Their Forgejo reads and typed mutations cross a
role-bound broker capability. QA can post only QA records. An engineer can post
only the fixed workflow record kinds allowed for engineering and reaping, and
cannot mint approval records.

Ward currently fails closed for engineer and QA launches targeting another
forge because those forges do not yet have the same role-authenticated tracker
broker.

## See also

* [agent-pr-workflow.md](agent-pr-workflow.md) - the PR workflow verbs that use the gate.
* [broker.md](broker.md) - credential placement and typed mutation policy.
