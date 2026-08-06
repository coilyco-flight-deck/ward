package ollamaprobe

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestDoctorTargets_NoneConfigured(t *testing.T) {
	got := DoctorTargets(func(string) string { return "" })
	if len(got) != 0 {
		t.Fatalf("want no targets with nothing set, got %+v", got)
	}
}

func TestDoctorTargets_OpencodeFromEnv(t *testing.T) {
	env := map[string]string{OpencodeEndpointEnv: "http://model-host:11434/v1"}
	got := DoctorTargets(func(k string) string { return env[k] })
	if len(got) != 1 || got[0].Harness != "opencode" || got[0].Endpoint != "http://model-host:11434/v1" {
		t.Fatalf("want one opencode target, got %+v", got)
	}
	if !strings.Contains(got[0].Source, OpencodeEndpointEnv) {
		t.Errorf("source should name the env var, got %q", got[0].Source)
	}
}

func TestDoctorTargets_GooseDecodesBase64(t *testing.T) {
	host := "http://model-host:11434"
	env := map[string]string{GooseHostEnv: base64.StdEncoding.EncodeToString([]byte(host))}
	got := DoctorTargets(func(k string) string { return env[k] })
	if len(got) != 1 || got[0].Harness != "goose" || got[0].Endpoint != host {
		t.Fatalf("want one decoded goose target, got %+v", got)
	}
}

func TestDoctorTargets_GooseBadBase64Skipped(t *testing.T) {
	env := map[string]string{GooseHostEnv: "!!!not base64!!!"}
	got := DoctorTargets(func(k string) string { return env[k] })
	if len(got) != 0 {
		t.Fatalf("undecodable goose host should be skipped, got %+v", got)
	}
}

func TestDoctorTargets_Both(t *testing.T) {
	env := map[string]string{
		OpencodeEndpointEnv: "http://localhost:11434/v1",
		GooseHostEnv:        base64.StdEncoding.EncodeToString([]byte("http://model-host:11434")),
	}
	got := DoctorTargets(func(k string) string { return env[k] })
	if len(got) != 2 {
		t.Fatalf("want opencode + goose targets, got %+v", got)
	}
}

func TestProbeTargets_NoneSkips(t *testing.T) {
	got := ProbeTargets(nil, func(string) (string, error) {
		t.Fatal("reacher must not be called with no targets")
		return "", nil
	})
	if len(got) != 1 || got[0].Severity != SevSkip {
		t.Fatalf("want one SKIP, got %+v", got)
	}
	if !strings.Contains(got[0].Detail, OpencodeEndpointEnv) {
		t.Errorf("SKIP detail should name the opt-in env var, got %q", got[0].Detail)
	}
}

func TestProbeTargets_ReachablePasses(t *testing.T) {
	targets := []Target{{Harness: "opencode", Endpoint: "http://x:11434/v1", Source: "$" + OpencodeEndpointEnv}}
	got := ProbeTargets(targets, func(string) (string, error) { return "x:11434", nil })
	if len(got) != 1 || got[0].Severity != SevPass {
		t.Fatalf("want PASS against a reachable endpoint, got %+v", got)
	}
	if !strings.Contains(got[0].Detail, "x:11434") {
		t.Errorf("PASS detail should name the dialed addr, got %q", got[0].Detail)
	}
}

func TestProbeTargets_UnreachableFails(t *testing.T) {
	targets := []Target{{Harness: "goose", Endpoint: "http://dead:11434", Source: "$" + GooseHostEnv}}
	got := ProbeTargets(targets, func(string) (string, error) {
		return "", errors.New("connection refused")
	})
	if len(got) != 1 || got[0].Severity != SevFail {
		t.Fatalf("want FAIL against an unreachable endpoint, got %+v", got)
	}
	for _, want := range []string{"goose", "http://dead:11434", "unreachable", skipEnv} {
		if !strings.Contains(got[0].Detail, want) {
			t.Errorf("FAIL detail %q missing %q", got[0].Detail, want)
		}
	}
}

func TestProbeTargets_MixedPassAndFail(t *testing.T) {
	targets := []Target{
		{Harness: "opencode", Endpoint: "http://up:11434/v1", Source: "$" + OpencodeEndpointEnv},
		{Harness: "goose", Endpoint: "http://down:11434", Source: "$" + GooseHostEnv},
	}
	got := ProbeTargets(targets, func(endpoint string) (string, error) {
		if strings.Contains(endpoint, "down") {
			return "", errors.New("refused")
		}
		return "up:11434", nil
	})
	if len(got) != 2 || got[0].Severity != SevPass || got[1].Severity != SevFail {
		t.Fatalf("want PASS then FAIL, got %+v", got)
	}
}
