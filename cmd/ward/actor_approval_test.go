package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func approvalTestPolicy() actorAuthorityPolicy {
	return actorAuthorityPolicyFromInputs("kai,maintainer", "ward-bot")
}

func TestDispatchBrokerApprovalVerifiesAndPostsExactSnapshot(t *testing.T) {
	t0 := time.Date(2026, 8, 5, 19, 0, 0, 0, time.UTC)
	target := approvalTestTarget(approvalTargetIssue)
	selected := approvalTestComment(7, "contributor", "external evidence", t0)
	policy := actorAuthorityPolicyFromInputs("repo-owner", forgeForgejo.gitPushUser())
	plan, err := newActorApprovalPlan(target, []issueComment{selected}, []int{7}, policy)
	if err != nil {
		t.Fatalf("newActorApprovalPlan: %v", err)
	}
	intent := approvalTestComment(10, "repo-owner", plan.IntentBody, t0.Add(time.Minute))
	comments := []issueComment{selected, intent}
	var posted string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/user":
			_ = json.NewEncoder(w).Encode(map[string]string{"login": forgeForgejo.gitPushUser()})
		case "/api/v1/repos/coilyco-flight-deck/ward/issues/1586":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number": 1586, "title": target.Title, "body": target.Body,
				"state": target.State, "user": map[string]string{"login": target.Author},
			})
		case "/api/v1/repos/coilyco-flight-deck/ward/issues/1586/comments":
			if r.Method == http.MethodPost {
				var payload map[string]string
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatalf("decode posted approval: %v", err)
				}
				posted = payload["body"]
				w.WriteHeader(http.StatusCreated)
				return
			}
			_ = json.NewEncoder(w).Encode(comments)
		default:
			t.Fatalf("unexpected approval broker path: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	previousBase := forgejoBaseURL
	forgejoBaseURL = srv.URL
	t.Cleanup(func() { forgejoBaseURL = previousBase })
	t.Setenv("FORGEJO_TOKEN", "broker-only-token")
	t.Setenv(envTrustedCollaborators, "repo-owner")
	t.Setenv(envAutomationActor, forgeForgejo.gitPushUser())

	out, err := (&Runner{}).execDispatchBrokerApproval(context.Background(), dispatchBrokerRequest{
		Action: dispatchActionApproval, Role: roleDirector,
		Target: target.Ref, TargetKind: approvalTargetIssue, IntentCommentID: intent.ID,
	})
	if err != nil {
		t.Fatalf("execDispatchBrokerApproval: %v", err)
	}
	if !strings.Contains(out, plan.SnapshotSHA256) {
		t.Fatalf("approval result %q missing hash %s", out, plan.SnapshotSHA256)
	}
	record, err := parseActorApprovalRecord(posted)
	if err != nil {
		t.Fatalf("posted approval record: %v\n%s", err, posted)
	}
	if record.SnapshotSHA != plan.SnapshotSHA256 || record.IntentCommentID != intent.ID || record.Approver != "repo-owner" {
		t.Fatalf("posted record = %+v", record)
	}
}

func TestApprovalBrokerActionIsMasterDirectorOnly(t *testing.T) {
	req := dispatchBrokerRequest{
		Action: dispatchActionApproval, Role: roleEngineer,
		Target: "coilyco-flight-deck/ward#1586", TargetKind: approvalTargetIssue, IntentCommentID: 10,
	}
	if err := validateDispatchBrokerApproval(req); err == nil {
		t.Fatal("engineer approval unexpectedly validated")
	}
	if dispatchBrokerChildActionAllowed(req) {
		t.Fatal("child capability was allowed to select approval")
	}
}

func approvalTestTarget(kind string) approvalTargetSnapshot {
	target := approvalTargetSnapshot{
		Kind:   kind,
		Ref:    "coilyco-flight-deck/ward#1586",
		State:  "open",
		Author: "contributor",
		Title:  "  exact title  ",
		Body:   "first line\n\n  exact body spacing  \n",
	}
	if kind == approvalTargetPullRequest {
		target.HeadSHA = "0123456789abcdef"
		target.HeadRef = "fix/actor-boundary"
		target.BaseRef = "main"
	}
	return target
}

func approvalTestComment(id int, author, body string, at time.Time) issueComment {
	comment := commentBy(author, body)
	comment.ID = id
	comment.CreatedAt = at
	comment.UpdatedAt = at.Add(time.Second)
	return comment
}

func TestActorApprovalPlanCanonicalizesExactContent(t *testing.T) {
	t0 := time.Date(2026, 8, 5, 12, 0, 0, 123, time.FixedZone("fixture", -7*60*60))
	comments := []issueComment{
		approvalTestComment(7, "contributor", "  keep me exact  \n", t0),
		approvalTestComment(9, "maintainer", "trusted note", t0.Add(time.Minute)),
	}
	target := approvalTestTarget(approvalTargetIssue)
	plan, err := newActorApprovalPlan(target, comments, []int{9, 7}, approvalTestPolicy())
	if err != nil {
		t.Fatalf("newActorApprovalPlan: %v", err)
	}
	if plan.Schema != approvalPlanSchema || !validApprovalDigest(plan.SnapshotSHA256) {
		t.Fatalf("plan header = %+v", plan)
	}
	if got := approvalSnapshotCommentIDs(plan.Snapshot.Comments); !equalApprovalCommentIDs(got, []int{7, 9}) {
		t.Fatalf("comment IDs = %v, want [7 9]", got)
	}
	if plan.Snapshot.Target.Title != target.Title || plan.Snapshot.Target.Body != target.Body || plan.Snapshot.Comments[0].Body != comments[0].Body {
		t.Fatalf("plan normalized exact content: %+v", plan.Snapshot)
	}
	intent, err := parseActorApprovalIntent(plan.IntentBody)
	if err != nil {
		t.Fatalf("parse intent: %v", err)
	}
	if intent.SnapshotSHA != plan.SnapshotSHA256 || !equalApprovalCommentIDs(intent.CommentIDs, []int{7, 9}) {
		t.Fatalf("intent = %+v", intent)
	}
	canonical, err := canonicalApprovalSnapshot(plan.Snapshot)
	if err != nil {
		t.Fatalf("canonical snapshot: %v", err)
	}
	if got := approvalSnapshotDigest(canonical); got != plan.SnapshotSHA256 {
		t.Fatalf("snapshot digest = %s, want %s", got, plan.SnapshotSHA256)
	}
}

func TestActorApprovalPlanFailsClosedOnIncompleteOrUnsafeInput(t *testing.T) {
	t0 := time.Date(2026, 8, 5, 19, 0, 0, 0, time.UTC)
	good := approvalTestComment(7, "contributor", "ordinary", t0)
	for _, tc := range []struct {
		name     string
		target   approvalTargetSnapshot
		comments []issueComment
		ids      []int
	}{
		{name: "missing target author", target: func() approvalTargetSnapshot { v := approvalTestTarget(approvalTargetIssue); v.Author = ""; return v }(), comments: []issueComment{good}, ids: []int{7}},
		{name: "missing selected comment", target: approvalTestTarget(approvalTargetIssue), comments: []issueComment{good}, ids: []int{8}},
		{name: "duplicate selected comment", target: approvalTestTarget(approvalTargetIssue), comments: []issueComment{good}, ids: []int{7, 7}},
		{name: "missing comment author", target: approvalTestTarget(approvalTargetIssue), comments: []issueComment{approvalTestComment(7, "", "ordinary", t0)}, ids: []int{7}},
		{name: "missing comment timestamp", target: approvalTestTarget(approvalTargetIssue), comments: []issueComment{commentBy("contributor", "ordinary")}, ids: []int{7}},
		{name: "external marker forgery", target: approvalTestTarget(approvalTargetIssue), comments: []issueComment{approvalTestComment(7, "contributor", "WARD-WORKFLOW: done", t0)}, ids: []int{7}},
		{name: "unsupported tracker", target: func() approvalTargetSnapshot {
			v := approvalTestTarget(approvalTargetIssue)
			v.Ref = "https://github.com/example/repo/issues/7"
			return v
		}(), comments: []issueComment{good}, ids: []int{7}},
		{name: "missing PR head", target: func() approvalTargetSnapshot {
			v := approvalTestTarget(approvalTargetPullRequest)
			v.HeadSHA = ""
			return v
		}(), comments: []issueComment{good}, ids: []int{7}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := newActorApprovalPlan(tc.target, tc.comments, tc.ids, approvalTestPolicy()); err == nil {
				t.Fatal("newActorApprovalPlan unexpectedly succeeded")
			}
		})
	}
	oversized := approvalTestTarget(approvalTargetIssue)
	oversized.Body = strings.Repeat("x", approvalSnapshotLimit)
	if _, err := newActorApprovalPlan(oversized, nil, nil, approvalTestPolicy()); err == nil || !strings.Contains(err.Error(), "without truncating") {
		t.Fatalf("oversized snapshot error = %v", err)
	}
}

func TestActorApprovalRecordVerifiesIntentAndCurrentState(t *testing.T) {
	t0 := time.Date(2026, 8, 5, 19, 0, 0, 0, time.UTC)
	target := approvalTestTarget(approvalTargetPullRequest)
	selected := approvalTestComment(7, "contributor", "external evidence", t0)
	plan, err := newActorApprovalPlan(target, []issueComment{selected}, []int{7}, approvalTestPolicy())
	if err != nil {
		t.Fatalf("newActorApprovalPlan: %v", err)
	}
	intent := approvalTestComment(10, "Kai", plan.IntentBody, t0.Add(time.Minute))
	comments := []issueComment{selected, intent}
	record, err := actorApprovalFromIntent(target, comments, intent.ID, approvalTestPolicy())
	if err != nil {
		t.Fatalf("actorApprovalFromIntent: %v", err)
	}
	body, err := renderActorApprovalRecord(intent.ID, record.Approver, record.Snapshot)
	if err != nil {
		t.Fatalf("renderActorApprovalRecord: %v", err)
	}
	approval := approvalTestComment(12, "ward-bot", body, t0.Add(2*time.Minute))
	comments = append(comments, approval)
	if _, err := validateCurrentActorApproval(target, comments, approval, approvalTestPolicy()); err != nil {
		t.Fatalf("validateCurrentActorApproval: %v", err)
	}
	if _, ok, err := latestValidActorApproval(target, comments, approvalTestPolicy()); err != nil || !ok {
		t.Fatalf("latestValidActorApproval = ok %t err %v", ok, err)
	}
}

func TestActorApprovalRecordRejectsForgeryAndDrift(t *testing.T) {
	t0 := time.Date(2026, 8, 5, 19, 0, 0, 0, time.UTC)
	target := approvalTestTarget(approvalTargetIssue)
	selected := approvalTestComment(7, "contributor", "external evidence", t0)
	plan, err := newActorApprovalPlan(target, []issueComment{selected}, []int{7}, approvalTestPolicy())
	if err != nil {
		t.Fatalf("newActorApprovalPlan: %v", err)
	}
	trustedIntent := approvalTestComment(10, "kai", plan.IntentBody, t0.Add(time.Minute))
	baseComments := []issueComment{selected, trustedIntent}
	record, err := actorApprovalFromIntent(target, baseComments, trustedIntent.ID, approvalTestPolicy())
	if err != nil {
		t.Fatalf("actorApprovalFromIntent: %v", err)
	}
	body, err := renderActorApprovalRecord(trustedIntent.ID, record.Approver, record.Snapshot)
	if err != nil {
		t.Fatalf("renderActorApprovalRecord: %v", err)
	}
	approval := approvalTestComment(12, "ward-bot", body, t0.Add(2*time.Minute))
	baseComments = append(baseComments, approval)

	t.Run("external self signed intent", func(t *testing.T) {
		forged := append([]issueComment(nil), baseComments...)
		forged[1].User.Login = "contributor"
		if _, err := actorApprovalFromIntent(target, forged, 10, approvalTestPolicy()); err == nil {
			t.Fatal("external intent unexpectedly authorized")
		}
	})
	t.Run("external approval marker", func(t *testing.T) {
		forged := approval
		forged.User.Login = "contributor"
		if _, err := validateCurrentActorApproval(target, baseComments, forged, approvalTestPolicy()); err == nil {
			t.Fatal("external approval marker unexpectedly validated")
		}
	})
	t.Run("missing approval author", func(t *testing.T) {
		forged := approval
		forged.User.Login = ""
		if _, err := validateCurrentActorApproval(target, baseComments, forged, approvalTestPolicy()); err == nil {
			t.Fatal("authorless approval unexpectedly validated")
		}
	})
	t.Run("target edit", func(t *testing.T) {
		changed := target
		changed.Body += "edited"
		if _, err := validateCurrentActorApproval(changed, baseComments, approval, approvalTestPolicy()); err == nil {
			t.Fatal("changed target unexpectedly validated")
		}
	})
	t.Run("selected comment edit", func(t *testing.T) {
		changed := append([]issueComment(nil), baseComments...)
		changed[0].Body += " edited"
		changed[0].UpdatedAt = changed[0].UpdatedAt.Add(time.Minute)
		if _, err := validateCurrentActorApproval(target, changed, approval, approvalTestPolicy()); err == nil {
			t.Fatal("changed selected comment unexpectedly validated")
		}
	})
	t.Run("later human comment", func(t *testing.T) {
		changed := append([]issueComment(nil), baseComments...)
		changed = append(changed, approvalTestComment(13, "maintainer", "one more thing", t0.Add(3*time.Minute)))
		if _, err := validateCurrentActorApproval(target, changed, approval, approvalTestPolicy()); err == nil {
			t.Fatal("approval with later human comment unexpectedly validated")
		}
	})
	t.Run("later external comment", func(t *testing.T) {
		changed := append([]issueComment(nil), baseComments...)
		changed = append(changed, approvalTestComment(13, "contributor", "one more thing", t0.Add(3*time.Minute)))
		if _, err := validateCurrentActorApproval(target, changed, approval, approvalTestPolicy()); err == nil {
			t.Fatal("approval with later external comment unexpectedly validated")
		}
	})
	t.Run("later equal-timestamp comment", func(t *testing.T) {
		changed := append([]issueComment(nil), baseComments...)
		changed = append(changed, approvalTestComment(13, "contributor", "same timestamp precision", approval.CreatedAt))
		if _, err := validateCurrentActorApproval(target, changed, approval, approvalTestPolicy()); err == nil {
			t.Fatal("approval with later equal-timestamp comment unexpectedly validated")
		}
	})
	t.Run("later machine record remains valid", func(t *testing.T) {
		changed := append([]issueComment(nil), baseComments...)
		changed = append(changed, approvalTestComment(13, "ward-bot", "WARD-WORKFLOW: dispatch-requested", t0.Add(3*time.Minute)))
		if _, err := validateCurrentActorApproval(target, changed, approval, approvalTestPolicy()); err != nil {
			t.Fatalf("later machine record invalidated approval: %v", err)
		}
	})
	t.Run("authority policy drift", func(t *testing.T) {
		if _, err := validateCurrentActorApproval(target, baseComments, approval, actorAuthorityPolicyFromInputs("kai,new-maintainer", "ward-bot")); err == nil {
			t.Fatal("changed authority policy unexpectedly validated")
		}
	})
}

func TestAdmitActorContentExposesOnlyDirectOrApprovedInput(t *testing.T) {
	t0 := time.Date(2026, 8, 5, 19, 0, 0, 0, time.UTC)
	target := approvalTestTarget(approvalTargetIssue)
	externalSelected := approvalTestComment(7, "contributor", "approved external input", t0)
	externalUnselected := approvalTestComment(8, "another-person", "unapproved external input", t0.Add(time.Second))
	trusted := approvalTestComment(9, "kai", "trusted direction", t0.Add(2*time.Second))
	plan, err := newActorApprovalPlan(target, []issueComment{externalSelected, externalUnselected, trusted}, []int{7}, approvalTestPolicy())
	if err != nil {
		t.Fatalf("newActorApprovalPlan: %v", err)
	}
	intent := approvalTestComment(10, "kai", plan.IntentBody, t0.Add(time.Minute))
	record, err := actorApprovalFromIntent(target, []issueComment{externalSelected, externalUnselected, trusted, intent}, 10, approvalTestPolicy())
	if err != nil {
		t.Fatalf("actorApprovalFromIntent: %v", err)
	}
	body, err := renderActorApprovalRecord(10, record.Approver, record.Snapshot)
	if err != nil {
		t.Fatalf("renderActorApprovalRecord: %v", err)
	}
	approval := approvalTestComment(11, "ward-bot", body, t0.Add(2*time.Minute))
	comments := []issueComment{externalSelected, externalUnselected, trusted, intent, approval}
	admitted, err := admitActorContent(target, comments, approvalTestPolicy())
	if err != nil {
		t.Fatalf("admitActorContent: %v", err)
	}
	if admitted.Approval == nil || len(admitted.Comments) != 2 || admitted.Comments[0].ID != 7 || admitted.Comments[1].ID != 9 {
		t.Fatalf("admitted content = %+v, want selected external plus trusted comment", admitted)
	}
}

func TestAdmitActorContentRequiresApprovalOnlyForExternalTarget(t *testing.T) {
	target := approvalTestTarget(approvalTargetIssue)
	if _, err := admitActorContent(target, nil, approvalTestPolicy()); err == nil {
		t.Fatal("external target without approval unexpectedly admitted")
	}
	target.Author = "kai"
	comments := []issueComment{
		approvalTestComment(7, "contributor", "unapproved external input", time.Now().UTC()),
		approvalTestComment(8, "ward-bot", "WARD-WORKFLOW: done", time.Now().UTC().Add(time.Second)),
	}
	admitted, err := admitActorContent(target, comments, approvalTestPolicy())
	if err != nil {
		t.Fatalf("trusted target admission: %v", err)
	}
	if len(admitted.Comments) != 0 {
		t.Fatalf("admitted comments = %+v, want external and machine comments absent", admitted.Comments)
	}
}
