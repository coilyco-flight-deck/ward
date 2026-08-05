package main

import (
	"fmt"
	"os"
	"strings"
)

const (
	envTrustedCollaborators = "WARD_TRUSTED_COLLABORATORS"
	envAutomationActor      = "WARD_AUTOMATION_ACTOR"

	actorClassTrustedHuman   = "trusted-human"
	actorClassTrustedMachine = "trusted-machine"
	actorClassOrdinaryInput  = "ordinary-input"
	actorClassInvalid        = "invalid"

	recordKindApprovalIntent     = "approval-intent"
	recordKindApprovalSnapshot   = "approval-snapshot"
	recordKindDispatch           = "dispatch"
	recordKindOutcome            = "outcome"
	recordKindPreflight          = "preflight"
	recordKindQA                 = "qa"
	recordKindReservation        = "reservation"
	recordKindReservationRelease = "reservation-release"
	recordKindReview             = "review"
	recordKindRoute              = "route"
	recordKindSignature          = "signature"
	recordKindTriage             = "triage"

	wardApprovalIntentMarker   = "WARD-APPROVAL-INTENT:"
	wardApprovalSnapshotMarker = "WARD-APPROVAL:"
)

type actorAuthorityPolicy struct {
	TrustedCollaborators map[string]struct{}
	AutomationActor      string
	Err                  error
}

type actorAdmission struct {
	Class      string
	RecordKind string
	Direct     bool
	Reason     string
}

func loadActorAuthorityPolicy() actorAuthorityPolicy {
	return actorAuthorityPolicyFromInputs(
		os.Getenv(envTrustedCollaborators),
		os.Getenv(envAutomationActor),
	)
}

func actorAuthorityPolicyFromInputs(collaborators, automationActor string) actorAuthorityPolicy {
	policy := actorAuthorityPolicy{
		TrustedCollaborators: map[string]struct{}{},
		AutomationActor:      normalizeActorLogin(automationActor),
	}
	for _, raw := range strings.FieldsFunc(collaborators, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	}) {
		if login := normalizeActorLogin(raw); login != "" {
			policy.TrustedCollaborators[login] = struct{}{}
		}
	}
	switch {
	case len(policy.TrustedCollaborators) == 0:
		policy.Err = fmt.Errorf("actor authority policy has no trusted collaborators")
	case policy.AutomationActor == "":
		policy.Err = fmt.Errorf("actor authority policy has no automation actor")
	default:
		if _, ambiguous := policy.TrustedCollaborators[policy.AutomationActor]; ambiguous {
			policy.Err = fmt.Errorf("actor authority policy assigns %q as both collaborator and automation actor", policy.AutomationActor)
		}
	}
	return policy
}

func normalizeActorLogin(login string) string {
	return strings.ToLower(strings.TrimSpace(login))
}

func classifyActorComment(c issueComment) actorAdmission {
	return classifyActorCommentWithPolicy(c, loadActorAuthorityPolicy())
}

func classifyActorCommentWithPolicy(c issueComment, policy actorAuthorityPolicy) actorAdmission {
	if policy.Err != nil {
		return actorAdmission{Class: actorClassInvalid, Reason: policy.Err.Error()}
	}
	login := normalizeActorLogin(c.User.Login)
	if login == "" {
		return actorAdmission{Class: actorClassInvalid, Reason: "comment author is missing"}
	}
	recordKind, recognized, markerAttempt := fixedWardRecordKind(c.Body)
	_, trustedHuman := policy.TrustedCollaborators[login]
	trustedMachine := login == policy.AutomationActor
	if recognized {
		return classifyFixedActorRecord(recordKind, trustedHuman, trustedMachine)
	}
	if markerAttempt {
		return actorAdmission{Class: actorClassInvalid, Reason: "comment uses an unrecognized Ward marker"}
	}
	if trustedHuman {
		return actorAdmission{Class: actorClassTrustedHuman, Direct: true}
	}
	if trustedMachine {
		return actorAdmission{Class: actorClassOrdinaryInput, Direct: true, Reason: "automation actor prose is not machine state"}
	}
	return actorAdmission{Class: actorClassOrdinaryInput, Reason: "comment requires an immutable approval snapshot"}
}

func classifyFixedActorRecord(recordKind string, trustedHuman, trustedMachine bool) actorAdmission {
	if recordKind == recordKindApprovalIntent {
		if trustedHuman {
			return actorAdmission{Class: actorClassTrustedHuman, RecordKind: recordKind, Direct: true}
		}
		return actorAdmission{Class: actorClassInvalid, Reason: "approval intent author is not a trusted collaborator"}
	}
	if trustedMachine {
		return actorAdmission{Class: actorClassTrustedMachine, RecordKind: recordKind, Direct: true}
	}
	if trustedHuman {
		return actorAdmission{Class: actorClassTrustedHuman, Direct: true, Reason: "trusted collaborator cannot mint machine state"}
	}
	return actorAdmission{Class: actorClassInvalid, Reason: "machine record author is not the configured automation actor"}
}

func trustedMachineComment(c issueComment, allowedKinds ...string) bool {
	admission := classifyActorComment(c)
	if admission.Class != actorClassTrustedMachine {
		return false
	}
	if len(allowedKinds) == 0 {
		return true
	}
	kinds := fixedWardRecordKinds(c.Body)
	for _, kind := range allowedKinds {
		if _, ok := kinds[kind]; ok {
			return true
		}
	}
	return false
}

func fixedWardRecordKinds(body string) map[string]struct{} {
	kinds := map[string]struct{}{}
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		switch {
		case line == agentReservationReleaseMarker:
			kinds[recordKindReservationRelease] = struct{}{}
			continue
		case line == agentReservationMarker:
			kinds[recordKindReservation] = struct{}{}
			continue
		case line == agentNeedsRedispatchMarker, strings.HasPrefix(line, "<!-- ward-dispatch-request:"):
			kinds[recordKindDispatch] = struct{}{}
			continue
		case line == preflightNoGoMarker:
			kinds[recordKindPreflight] = struct{}{}
			continue
		case line == agentSignatureMarker:
			kinds[recordKindSignature] = struct{}{}
			continue
		}
		visible := backlogCommentLine(line)
		upper := strings.ToUpper(visible)
		switch {
		case strings.HasPrefix(upper, wardApprovalIntentMarker):
			kinds[recordKindApprovalIntent] = struct{}{}
		case strings.HasPrefix(upper, wardApprovalSnapshotMarker):
			kinds[recordKindApprovalSnapshot] = struct{}{}
		default:
			if header, ok := parseWorkflowCommentHeaderLine(line); ok {
				kinds[workflowRecordKind(header.Variant)] = struct{}{}
			}
		}
		break
	}
	return kinds
}

func fixedWardRecordKind(body string) (kind string, recognized, markerAttempt bool) {
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		return fixedWardRecordKindLine(line)
	}
	return "", false, false
}

func fixedWardRecordKindLine(line string) (kind string, recognized, markerAttempt bool) {
	if kind, ok := fixedHiddenRecordKind(line); ok {
		return kind, true, true
	}
	visible := backlogCommentLine(line)
	upper := strings.ToUpper(visible)
	if strings.HasPrefix(upper, wardApprovalIntentMarker) {
		return recordKindApprovalIntent, true, true
	}
	if strings.HasPrefix(upper, wardApprovalSnapshotMarker) {
		return recordKindApprovalSnapshot, true, true
	}
	if header, ok := parseWorkflowCommentHeaderLine(line); ok {
		return workflowRecordKind(header.Variant), true, true
	}
	lower := strings.ToLower(line)
	markerAttempt = strings.HasPrefix(upper, "WARD-") || strings.HasPrefix(upper, "WARDED_") || strings.HasPrefix(lower, "<!-- ward-")
	return "", false, markerAttempt
}

func fixedHiddenRecordKind(line string) (string, bool) {
	switch {
	case line == agentReservationReleaseMarker:
		return recordKindReservationRelease, true
	case line == agentReservationMarker:
		return recordKindReservation, true
	case line == agentNeedsRedispatchMarker, strings.HasPrefix(line, "<!-- ward-dispatch-request:"):
		return recordKindDispatch, true
	case line == preflightNoGoMarker:
		return recordKindPreflight, true
	case line == agentSignatureMarker:
		return recordKindSignature, true
	default:
		return "", false
	}
}

func workflowRecordKind(variant string) string {
	variant = canonicalWorkflowCommentVariant(variant)
	switch {
	case strings.HasPrefix(variant, "qa-"):
		return recordKindQA
	case strings.HasPrefix(variant, "review-"):
		return recordKindReview
	case strings.HasPrefix(variant, "reservation-"):
		if variant == "reservation-released" {
			return recordKindReservationRelease
		}
		return recordKindReservation
	case strings.HasPrefix(variant, "dispatch-"):
		return recordKindDispatch
	case strings.HasPrefix(variant, "pre-flight-"):
		return recordKindPreflight
	case variant == "routed" || variant == "route-unclear":
		return recordKindRoute
	case variant == "triage":
		return recordKindTriage
	default:
		return recordKindOutcome
	}
}
