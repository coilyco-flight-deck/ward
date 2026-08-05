package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	approvalSnapshotSchema = "ward.actor-approval/v1"
	approvalPlanSchema     = "ward.actor-approval-plan/v1"
	approvalSnapshotLimit  = 256 << 10

	approvalTargetIssue       = "issue"
	approvalTargetPullRequest = "pull-request"
)

// approvalTargetSnapshot seals exact target content, state, author, and PR head.
// UpdatedAt stays out because posting the intent can update the issue timestamp.
type approvalTargetSnapshot struct {
	Kind    string `json:"kind"`
	Ref     string `json:"ref"`
	State   string `json:"state"`
	Author  string `json:"author"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	HeadSHA string `json:"head_sha,omitempty"`
	HeadRef string `json:"head_ref,omitempty"`
	BaseRef string `json:"base_ref,omitempty"`
}

type approvalCommentSnapshot struct {
	ID        int    `json:"id"`
	Author    string `json:"author"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Body      string `json:"body"`
}

type approvalAuthoritySnapshot struct {
	TrustedCollaborators []string `json:"trusted_collaborators"`
	AutomationActor      string   `json:"automation_actor"`
}

type actorApprovalSnapshot struct {
	Schema    string                    `json:"schema"`
	Target    approvalTargetSnapshot    `json:"target"`
	Comments  []approvalCommentSnapshot `json:"comments"`
	Authority approvalAuthoritySnapshot `json:"authority"`
}

type actorApprovalPlan struct {
	Schema         string                `json:"schema"`
	SnapshotSHA256 string                `json:"snapshot_sha256"`
	IntentBody     string                `json:"intent_body"`
	Snapshot       actorApprovalSnapshot `json:"snapshot"`
}

type actorApprovalIntent struct {
	TargetKind  string
	TargetRef   string
	SnapshotSHA string
	CommentIDs  []int
}

type actorApprovalRecord struct {
	SnapshotSHA     string
	IntentCommentID int
	Approver        string
	Snapshot        actorApprovalSnapshot
}

type admittedActorContent struct {
	Target   approvalTargetSnapshot
	Comments []issueComment
	Approval *actorApprovalSnapshot
}

func newActorApprovalPlan(target approvalTargetSnapshot, comments []issueComment, selectedIDs []int, policy actorAuthorityPolicy) (actorApprovalPlan, error) {
	snapshot, err := newActorApprovalSnapshot(target, comments, selectedIDs, policy)
	if err != nil {
		return actorApprovalPlan{}, err
	}
	canonical, err := canonicalApprovalSnapshot(snapshot)
	if err != nil {
		return actorApprovalPlan{}, err
	}
	digest := approvalSnapshotDigest(canonical)
	commentIDs := approvalSnapshotCommentIDs(snapshot.Comments)
	return actorApprovalPlan{
		Schema:         approvalPlanSchema,
		SnapshotSHA256: digest,
		IntentBody:     renderActorApprovalIntent(actorApprovalIntent{TargetKind: target.Kind, TargetRef: target.Ref, SnapshotSHA: digest, CommentIDs: commentIDs}),
		Snapshot:       snapshot,
	}, nil
}

func newActorApprovalSnapshot(target approvalTargetSnapshot, comments []issueComment, selectedIDs []int, policy actorAuthorityPolicy) (actorApprovalSnapshot, error) {
	if err := validateApprovalTarget(target); err != nil {
		return actorApprovalSnapshot{}, err
	}
	authority, err := approvalAuthorityFromPolicy(policy)
	if err != nil {
		return actorApprovalSnapshot{}, err
	}
	ids, err := canonicalApprovalCommentIDs(selectedIDs)
	if err != nil {
		return actorApprovalSnapshot{}, err
	}
	selected, err := approvalSelectedComments(comments, ids, policy)
	if err != nil {
		return actorApprovalSnapshot{}, err
	}
	snapshot := actorApprovalSnapshot{
		Schema:    approvalSnapshotSchema,
		Target:    target,
		Comments:  selected,
		Authority: authority,
	}
	if _, err := canonicalApprovalSnapshot(snapshot); err != nil {
		return actorApprovalSnapshot{}, err
	}
	return snapshot, nil
}

func approvalSelectedComments(comments []issueComment, ids []int, policy actorAuthorityPolicy) ([]approvalCommentSnapshot, error) {
	byID := make(map[int][]issueComment, len(comments))
	for _, comment := range comments {
		if comment.ID > 0 {
			byID[comment.ID] = append(byID[comment.ID], comment)
		}
	}
	selected := make([]approvalCommentSnapshot, 0, len(ids))
	for _, id := range ids {
		matches := byID[id]
		if len(matches) != 1 {
			return nil, fmt.Errorf("approval snapshot: selected comment %d resolved %d times, want exactly once", id, len(matches))
		}
		comment := matches[0]
		admission := classifyActorCommentWithPolicy(comment, policy)
		if admission.Class == actorClassInvalid {
			return nil, fmt.Errorf("approval snapshot: selected comment %d is inadmissible: %s", id, admission.Reason)
		}
		if strings.TrimSpace(comment.User.Login) == "" {
			return nil, fmt.Errorf("approval snapshot: selected comment %d has no author", id)
		}
		if comment.CreatedAt.IsZero() || comment.UpdatedAt.IsZero() {
			return nil, fmt.Errorf("approval snapshot: selected comment %d has incomplete timestamps", id)
		}
		selected = append(selected, approvalCommentSnapshot{
			ID:        id,
			Author:    comment.User.Login,
			CreatedAt: canonicalApprovalTime(comment.CreatedAt),
			UpdatedAt: canonicalApprovalTime(comment.UpdatedAt),
			Body:      comment.Body,
		})
	}
	return selected, nil
}

func validateApprovalTarget(target approvalTargetSnapshot) error {
	if target.Kind != approvalTargetIssue && target.Kind != approvalTargetPullRequest {
		return fmt.Errorf("approval snapshot: target kind %q is unsupported", target.Kind)
	}
	ref, err := parseAgentIssueRef(target.Ref)
	if err != nil || ref.Tracker != trackerForgejo {
		return fmt.Errorf("approval snapshot: target ref %q must be a Forgejo issue ref", target.Ref)
	}
	if strings.TrimSpace(target.Author) == "" {
		return fmt.Errorf("approval snapshot: target author is missing")
	}
	if strings.TrimSpace(target.State) == "" {
		return fmt.Errorf("approval snapshot: target state is missing")
	}
	if target.Kind == approvalTargetPullRequest && strings.TrimSpace(target.HeadSHA) == "" {
		return fmt.Errorf("approval snapshot: pull request head SHA is missing")
	}
	return nil
}

func approvalAuthorityFromPolicy(policy actorAuthorityPolicy) (approvalAuthoritySnapshot, error) {
	if policy.Err != nil {
		return approvalAuthoritySnapshot{}, fmt.Errorf("approval snapshot: %w", policy.Err)
	}
	people := make([]string, 0, len(policy.TrustedCollaborators))
	for login := range policy.TrustedCollaborators {
		people = append(people, login)
	}
	sort.Strings(people)
	return approvalAuthoritySnapshot{TrustedCollaborators: people, AutomationActor: policy.AutomationActor}, nil
}

func canonicalApprovalCommentIDs(ids []int) ([]int, error) {
	out := append([]int(nil), ids...)
	sort.Ints(out)
	for i, id := range out {
		if id <= 0 {
			return nil, fmt.Errorf("approval snapshot: selected comment ID %d must be positive", id)
		}
		if i > 0 && out[i-1] == id {
			return nil, fmt.Errorf("approval snapshot: selected comment ID %d is duplicated", id)
		}
	}
	return out, nil
}

func canonicalApprovalSnapshot(snapshot actorApprovalSnapshot) ([]byte, error) {
	if snapshot.Schema != approvalSnapshotSchema {
		return nil, fmt.Errorf("approval snapshot: schema %q is unsupported", snapshot.Schema)
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("approval snapshot: encode canonical JSON: %w", err)
	}
	if len(data) > approvalSnapshotLimit {
		return nil, fmt.Errorf("approval snapshot: exact content is %d bytes, exceeds the %d-byte limit; narrow the selected comments or move the work into a smaller issue without truncating it", len(data), approvalSnapshotLimit)
	}
	return data, nil
}

func approvalSnapshotDigest(canonical []byte) string {
	digest := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func canonicalApprovalTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func renderActorApprovalIntent(intent actorApprovalIntent) string {
	ids := "none"
	if len(intent.CommentIDs) > 0 {
		parts := make([]string, 0, len(intent.CommentIDs))
		for _, id := range intent.CommentIDs {
			parts = append(parts, strconv.Itoa(id))
		}
		ids = strings.Join(parts, ",")
	}
	return strings.Join([]string{
		wardApprovalIntentMarker + " v1",
		"target: " + intent.TargetKind + " " + intent.TargetRef,
		"snapshot-sha256: " + intent.SnapshotSHA,
		"selected-comment-ids: " + ids,
	}, "\n")
}

func parseActorApprovalIntent(body string) (actorApprovalIntent, error) {
	lines := exactApprovalRecordLines(body)
	if len(lines) != 4 || lines[0] != wardApprovalIntentMarker+" v1" {
		return actorApprovalIntent{}, fmt.Errorf("approval intent: expected the exact v1 four-line record")
	}
	kindRef, ok := strings.CutPrefix(lines[1], "target: ")
	if !ok {
		return actorApprovalIntent{}, fmt.Errorf("approval intent: target line is missing")
	}
	kind, ref, ok := strings.Cut(kindRef, " ")
	if !ok {
		return actorApprovalIntent{}, fmt.Errorf("approval intent: target line is malformed")
	}
	digest, ok := strings.CutPrefix(lines[2], "snapshot-sha256: ")
	if !ok || !validApprovalDigest(digest) {
		return actorApprovalIntent{}, fmt.Errorf("approval intent: snapshot digest is malformed")
	}
	rawIDs, ok := strings.CutPrefix(lines[3], "selected-comment-ids: ")
	if !ok {
		return actorApprovalIntent{}, fmt.Errorf("approval intent: selected-comment-ids line is missing")
	}
	ids, err := parseApprovalCommentIDs(rawIDs)
	if err != nil {
		return actorApprovalIntent{}, err
	}
	intent := actorApprovalIntent{TargetKind: kind, TargetRef: ref, SnapshotSHA: digest, CommentIDs: ids}
	if renderActorApprovalIntent(intent) != strings.Join(lines, "\n") {
		return actorApprovalIntent{}, fmt.Errorf("approval intent: record is not canonical")
	}
	return intent, nil
}

func parseApprovalCommentIDs(raw string) ([]int, error) {
	if raw == "none" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	ids := make([]int, 0, len(parts))
	for _, part := range parts {
		id, err := strconv.Atoi(part)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("approval intent: selected comment ID %q is invalid", part)
		}
		ids = append(ids, id)
	}
	canonical, err := canonicalApprovalCommentIDs(ids)
	if err != nil {
		return nil, err
	}
	for i := range ids {
		if ids[i] != canonical[i] {
			return nil, fmt.Errorf("approval intent: selected comment IDs are not in canonical ascending order")
		}
	}
	return ids, nil
}

func validApprovalDigest(raw string) bool {
	if !strings.HasPrefix(raw, "sha256:") || len(raw) != len("sha256:")+sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(raw, "sha256:"))
	return err == nil
}

func renderActorApprovalRecord(intentCommentID int, approver string, snapshot actorApprovalSnapshot) (string, error) {
	if intentCommentID <= 0 {
		return "", fmt.Errorf("approval snapshot: intent comment ID must be positive")
	}
	if strings.TrimSpace(approver) == "" {
		return "", fmt.Errorf("approval snapshot: approver is missing")
	}
	canonical, err := canonicalApprovalSnapshot(snapshot)
	if err != nil {
		return "", err
	}
	return strings.Join([]string{
		wardApprovalSnapshotMarker + " v1",
		"snapshot-sha256: " + approvalSnapshotDigest(canonical),
		"intent-comment-id: " + strconv.Itoa(intentCommentID),
		"approver: " + approver,
		"snapshot-json: " + string(canonical),
	}, "\n"), nil
}

func parseActorApprovalRecord(body string) (actorApprovalRecord, error) {
	lines := exactApprovalRecordLines(body)
	if len(lines) != 5 || lines[0] != wardApprovalSnapshotMarker+" v1" {
		return actorApprovalRecord{}, fmt.Errorf("approval snapshot: expected the exact v1 five-line record")
	}
	digest, intentID, approver, err := parseActorApprovalRecordFields(lines)
	if err != nil {
		return actorApprovalRecord{}, err
	}
	snapshot, err := parseEmbeddedActorApproval(lines[4], digest)
	if err != nil {
		return actorApprovalRecord{}, err
	}
	return actorApprovalRecord{SnapshotSHA: digest, IntentCommentID: intentID, Approver: approver, Snapshot: snapshot}, nil
}

func parseActorApprovalRecordFields(lines []string) (string, int, string, error) {
	digest, ok := strings.CutPrefix(lines[1], "snapshot-sha256: ")
	if !ok || !validApprovalDigest(digest) {
		return "", 0, "", fmt.Errorf("approval snapshot: digest is malformed")
	}
	rawIntentID, ok := strings.CutPrefix(lines[2], "intent-comment-id: ")
	if !ok {
		return "", 0, "", fmt.Errorf("approval snapshot: intent-comment-id line is missing")
	}
	intentID, err := strconv.Atoi(rawIntentID)
	if err != nil || intentID <= 0 {
		return "", 0, "", fmt.Errorf("approval snapshot: intent comment ID is invalid")
	}
	approver, ok := strings.CutPrefix(lines[3], "approver: ")
	if !ok || strings.TrimSpace(approver) == "" || approver != strings.TrimSpace(approver) {
		return "", 0, "", fmt.Errorf("approval snapshot: approver is malformed")
	}
	return digest, intentID, approver, nil
}

func parseEmbeddedActorApproval(line, digest string) (actorApprovalSnapshot, error) {
	rawSnapshot, ok := strings.CutPrefix(line, "snapshot-json: ")
	if !ok || len(rawSnapshot) > approvalSnapshotLimit {
		return actorApprovalSnapshot{}, fmt.Errorf("approval snapshot: embedded JSON is missing or oversized")
	}
	var snapshot actorApprovalSnapshot
	if err := json.Unmarshal([]byte(rawSnapshot), &snapshot); err != nil {
		return actorApprovalSnapshot{}, fmt.Errorf("approval snapshot: decode embedded JSON: %w", err)
	}
	canonical, err := canonicalApprovalSnapshot(snapshot)
	if err != nil {
		return actorApprovalSnapshot{}, err
	}
	if string(canonical) != rawSnapshot {
		return actorApprovalSnapshot{}, fmt.Errorf("approval snapshot: embedded JSON is not canonical")
	}
	if approvalSnapshotDigest(canonical) != digest {
		return actorApprovalSnapshot{}, fmt.Errorf("approval snapshot: embedded JSON does not match its digest")
	}
	return snapshot, nil
}

func exactApprovalRecordLines(body string) []string {
	body = strings.TrimSuffix(body, "\n")
	if strings.Contains(body, "\r") {
		return nil
	}
	return strings.Split(body, "\n")
}

func actorApprovalFromIntent(target approvalTargetSnapshot, comments []issueComment, intentCommentID int, policy actorAuthorityPolicy) (actorApprovalRecord, error) {
	intentComment, err := exactIssueComment(comments, intentCommentID)
	if err != nil {
		return actorApprovalRecord{}, fmt.Errorf("approve intent: %w", err)
	}
	admission := classifyActorCommentWithPolicy(intentComment, policy)
	if admission.Class != actorClassTrustedHuman || admission.RecordKind != recordKindApprovalIntent {
		return actorApprovalRecord{}, fmt.Errorf("approve intent: comment %d is not an approval intent from a trusted collaborator", intentCommentID)
	}
	intent, err := parseActorApprovalIntent(intentComment.Body)
	if err != nil {
		return actorApprovalRecord{}, err
	}
	if intent.TargetKind != target.Kind || intent.TargetRef != target.Ref {
		return actorApprovalRecord{}, fmt.Errorf("approve intent: target %s %s does not match current target %s %s", intent.TargetKind, intent.TargetRef, target.Kind, target.Ref)
	}
	plan, err := newActorApprovalPlan(target, comments, intent.CommentIDs, policy)
	if err != nil {
		return actorApprovalRecord{}, err
	}
	if plan.SnapshotSHA256 != intent.SnapshotSHA {
		return actorApprovalRecord{}, fmt.Errorf("approve intent: current content hash %s does not match requested hash %s", plan.SnapshotSHA256, intent.SnapshotSHA)
	}
	for _, selected := range plan.Snapshot.Comments {
		comment, _ := exactIssueComment(comments, selected.ID)
		if !comment.CreatedAt.Before(intentComment.CreatedAt) {
			return actorApprovalRecord{}, fmt.Errorf("approve intent: selected comment %d is not older than intent comment %d", selected.ID, intentCommentID)
		}
	}
	return actorApprovalRecord{
		SnapshotSHA:     plan.SnapshotSHA256,
		IntentCommentID: intentCommentID,
		Approver:        intentComment.User.Login,
		Snapshot:        plan.Snapshot,
	}, nil
}

func exactIssueComment(comments []issueComment, id int) (issueComment, error) {
	var match issueComment
	count := 0
	for _, comment := range comments {
		if comment.ID == id {
			match = comment
			count++
		}
	}
	if count != 1 {
		return issueComment{}, fmt.Errorf("comment %d resolved %d times, want exactly once", id, count)
	}
	return match, nil
}

func validateCurrentActorApproval(target approvalTargetSnapshot, comments []issueComment, approvalComment issueComment, policy actorAuthorityPolicy) (actorApprovalSnapshot, error) {
	admission := classifyActorCommentWithPolicy(approvalComment, policy)
	if admission.Class != actorClassTrustedMachine || admission.RecordKind != recordKindApprovalSnapshot {
		return actorApprovalSnapshot{}, fmt.Errorf("approval snapshot: comment %d is not an approval record from the configured automation actor", approvalComment.ID)
	}
	record, err := parseActorApprovalRecord(approvalComment.Body)
	if err != nil {
		return actorApprovalSnapshot{}, err
	}
	if record.Snapshot.Target != target {
		return actorApprovalSnapshot{}, fmt.Errorf("approval snapshot: target content or state changed after approval")
	}
	intentComment, intent, err := validateActorApprovalIntent(record, target, comments, policy)
	if err != nil {
		return actorApprovalSnapshot{}, err
	}
	if err := validateActorApprovalContent(record, target, comments, intent, policy); err != nil {
		return actorApprovalSnapshot{}, err
	}
	if err := validateActorApprovalTimeline(intentComment, approvalComment, comments, policy); err != nil {
		return actorApprovalSnapshot{}, err
	}
	return record.Snapshot, nil
}

func validateActorApprovalIntent(record actorApprovalRecord, target approvalTargetSnapshot, comments []issueComment, policy actorAuthorityPolicy) (issueComment, actorApprovalIntent, error) {
	authority, err := approvalAuthorityFromPolicy(policy)
	if err != nil {
		return issueComment{}, actorApprovalIntent{}, err
	}
	if !equalApprovalAuthority(record.Snapshot.Authority, authority) {
		return issueComment{}, actorApprovalIntent{}, fmt.Errorf("approval snapshot: actor authority policy changed after approval")
	}
	intentComment, err := exactIssueComment(comments, record.IntentCommentID)
	if err != nil {
		return issueComment{}, actorApprovalIntent{}, err
	}
	intentAdmission := classifyActorCommentWithPolicy(intentComment, policy)
	if intentAdmission.Class != actorClassTrustedHuman || intentAdmission.RecordKind != recordKindApprovalIntent {
		return issueComment{}, actorApprovalIntent{}, fmt.Errorf("approval snapshot: intent comment %d is no longer attributable to a trusted collaborator", record.IntentCommentID)
	}
	if intentComment.User.Login != record.Approver {
		return issueComment{}, actorApprovalIntent{}, fmt.Errorf("approval snapshot: approver does not match intent author")
	}
	intent, err := parseActorApprovalIntent(intentComment.Body)
	if err != nil {
		return issueComment{}, actorApprovalIntent{}, err
	}
	if intent.TargetKind != target.Kind || intent.TargetRef != target.Ref || intent.SnapshotSHA != record.SnapshotSHA {
		return issueComment{}, actorApprovalIntent{}, fmt.Errorf("approval snapshot: intent does not match the recorded target and hash")
	}
	if !equalApprovalCommentIDs(intent.CommentIDs, approvalSnapshotCommentIDs(record.Snapshot.Comments)) {
		return issueComment{}, actorApprovalIntent{}, fmt.Errorf("approval snapshot: intent selected a different comment set")
	}
	return intentComment, intent, nil
}

func validateActorApprovalContent(record actorApprovalRecord, target approvalTargetSnapshot, comments []issueComment, intent actorApprovalIntent, policy actorAuthorityPolicy) error {
	current, err := newActorApprovalSnapshot(target, comments, intent.CommentIDs, policy)
	if err != nil {
		return err
	}
	currentJSON, err := canonicalApprovalSnapshot(current)
	if err != nil {
		return err
	}
	if approvalSnapshotDigest(currentJSON) != record.SnapshotSHA {
		return fmt.Errorf("approval snapshot: selected content changed after approval")
	}
	return nil
}

func validateActorApprovalTimeline(intentComment, approvalComment issueComment, comments []issueComment, policy actorAuthorityPolicy) error {
	if !intentComment.CreatedAt.Before(approvalComment.CreatedAt) {
		return fmt.Errorf("approval snapshot: intent is not older than the approval record")
	}
	for _, comment := range comments {
		if comment.ID == approvalComment.ID || !actorCommentAfter(comment, approvalComment) {
			continue
		}
		if classifyActorCommentWithPolicy(comment, policy).Class != actorClassTrustedMachine {
			return fmt.Errorf("approval snapshot: later non-machine comment %d invalidates approval", comment.ID)
		}
	}
	return nil
}

func actorCommentAfter(comment, boundary issueComment) bool {
	return comment.CreatedAt.After(boundary.CreatedAt) ||
		(comment.CreatedAt.Equal(boundary.CreatedAt) && comment.ID > boundary.ID)
}

func latestValidActorApproval(target approvalTargetSnapshot, comments []issueComment, policy actorAuthorityPolicy) (actorApprovalSnapshot, bool, error) {
	var latest *issueComment
	for i := range comments {
		comment := comments[i]
		admission := classifyActorCommentWithPolicy(comment, policy)
		if admission.Class != actorClassTrustedMachine || admission.RecordKind != recordKindApprovalSnapshot {
			continue
		}
		if latest == nil || actorCommentAfter(comment, *latest) {
			candidate := comment
			latest = &candidate
		}
	}
	if latest != nil {
		snapshot, err := validateCurrentActorApproval(target, comments, *latest, policy)
		return snapshot, err == nil, err
	}
	return actorApprovalSnapshot{}, false, nil
}

func admitActorContent(target approvalTargetSnapshot, comments []issueComment, policy actorAuthorityPolicy) (admittedActorContent, error) {
	if policy.Err != nil {
		return admittedActorContent{}, fmt.Errorf("actor admission: %w", policy.Err)
	}
	snapshot, approved, approvalErr := latestValidActorApproval(target, comments, policy)
	if err := validateActorAdmissionTarget(target, policy, approved, approvalErr); err != nil {
		return admittedActorContent{}, err
	}
	approvedIDs := approvalCommentIDSet(snapshot.Comments, approved)
	admittedComments := admittedActorComments(comments, approvedIDs, policy)
	result := admittedActorContent{Target: target, Comments: admittedComments}
	if approved {
		result.Approval = &snapshot
	}
	return result, nil
}

func validateActorAdmissionTarget(target approvalTargetSnapshot, policy actorAuthorityPolicy, approved bool, approvalErr error) error {
	targetDirect, err := actorIdentityDirect(target.Author, policy)
	if err != nil {
		return fmt.Errorf("actor admission: target: %w", err)
	}
	if targetDirect {
		return nil
	}
	if approvalErr != nil {
		return fmt.Errorf("actor admission: external target approval is invalid: %w", approvalErr)
	}
	if !approved {
		return fmt.Errorf("actor admission: target author @%s is external and no current immutable approval snapshot exists", target.Author)
	}
	return nil
}

func approvalCommentIDSet(comments []approvalCommentSnapshot, approved bool) map[int]struct{} {
	ids := map[int]struct{}{}
	if !approved {
		return ids
	}
	for _, comment := range comments {
		ids[comment.ID] = struct{}{}
	}
	return ids
}

func admittedActorComments(comments []issueComment, approvedIDs map[int]struct{}, policy actorAuthorityPolicy) []issueComment {
	admittedComments := make([]issueComment, 0, len(comments))
	for _, comment := range comments {
		admission := classifyActorCommentWithPolicy(comment, policy)
		switch admission.Class {
		case actorClassTrustedMachine:
			// Machine records are control-plane state, never task prose.
			continue
		case actorClassTrustedHuman:
			if admission.RecordKind == recordKindApprovalIntent {
				continue
			}
			admittedComments = append(admittedComments, comment)
		case actorClassOrdinaryInput:
			if admission.Direct {
				admittedComments = append(admittedComments, comment)
				continue
			}
			if _, ok := approvedIDs[comment.ID]; ok {
				admittedComments = append(admittedComments, comment)
			}
		case actorClassInvalid:
			continue
		}
	}
	return admittedComments
}

func actorIdentityDirect(login string, policy actorAuthorityPolicy) (bool, error) {
	login = normalizeActorLogin(login)
	if login == "" {
		return false, fmt.Errorf("author is missing")
	}
	if login == policy.AutomationActor {
		return true, nil
	}
	_, trusted := policy.TrustedCollaborators[login]
	return trusted, nil
}

func approvalSnapshotCommentIDs(comments []approvalCommentSnapshot) []int {
	ids := make([]int, 0, len(comments))
	for _, comment := range comments {
		ids = append(ids, comment.ID)
	}
	return ids
}

func equalApprovalCommentIDs(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalApprovalAuthority(a, b approvalAuthoritySnapshot) bool {
	if a.AutomationActor != b.AutomationActor || len(a.TrustedCollaborators) != len(b.TrustedCollaborators) {
		return false
	}
	for i := range a.TrustedCollaborators {
		if a.TrustedCollaborators[i] != b.TrustedCollaborators[i] {
			return false
		}
	}
	return true
}
