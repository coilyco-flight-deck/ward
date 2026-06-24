package main

import (
	"errors"
	"reflect"
	"testing"
)

func TestSplitDriveArgs(t *testing.T) {
	cases := []struct {
		name        string
		args        []string
		wantWard    []string
		wantHarness string
		wantHargs   []string
		wantErr     bool
	}{
		{
			name:        "ward flag equals form then harness then harness flags",
			args:        []string{"--policy=strict", "gptme", "--non-interactive", "deploy"},
			wantWard:    []string{"--policy=strict"},
			wantHarness: "gptme",
			wantHargs:   []string{"--non-interactive", "deploy"},
		},
		{
			name:        "ward flag space form skips its value",
			args:        []string{"--policy", "strict", "gptme", "--foo"},
			wantWard:    []string{"--policy", "strict"},
			wantHarness: "gptme",
			wantHargs:   []string{"--foo"},
		},
		{
			name:        "explicit -- after harness is stripped once",
			args:        []string{"gptme", "--", "--non-interactive", "deploy"},
			wantHarness: "gptme",
			wantHargs:   []string{"--non-interactive", "deploy"},
		},
		{
			name:        "second -- after harness is preserved as a harness arg",
			args:        []string{"gptme", "--", "--", "deploy"},
			wantHarness: "gptme",
			wantHargs:   []string{"--", "deploy"},
		},
		{
			name:        "leading -- before harness names the next token",
			args:        []string{"--", "gptme", "--foo"},
			wantHarness: "gptme",
			wantHargs:   []string{"--foo"},
		},
		{
			name:        "bare harness with only a positional arg",
			args:        []string{"gptme", "deploy the thing"},
			wantHarness: "gptme",
			wantHargs:   []string{"deploy the thing"},
		},
		{
			name:        "harness with no args",
			args:        []string{"gptme"},
			wantHarness: "gptme",
			wantHargs:   nil,
		},
		{
			name:        "non-value ward bool flag then harness",
			args:        []string{"--print", "gptme", "--foo"},
			wantWard:    []string{"--print"},
			wantHarness: "gptme",
			wantHargs:   []string{"--foo"},
		},
		{
			name:    "no harness, only ward flags",
			args:    []string{"--policy=strict"},
			wantErr: true,
		},
		{
			name:    "empty args",
			args:    []string{},
			wantErr: true,
		},
		{
			name:    "dangling -- with nothing after",
			args:    []string{"--policy=strict", "--"},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inv, err := splitDriveArgs(tc.args)
			if tc.wantErr {
				if !errors.Is(err, errNoHarness) {
					t.Fatalf("splitDriveArgs(%v) err = %v, want errNoHarness", tc.args, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("splitDriveArgs(%v) unexpected err = %v", tc.args, err)
			}
			if !reflect.DeepEqual(nilIfEmpty(inv.WardArgs), nilIfEmpty(tc.wantWard)) {
				t.Errorf("WardArgs = %#v, want %#v", inv.WardArgs, tc.wantWard)
			}
			if inv.Harness != tc.wantHarness {
				t.Errorf("Harness = %q, want %q", inv.Harness, tc.wantHarness)
			}
			if !reflect.DeepEqual(nilIfEmpty(inv.HarnessArgs), nilIfEmpty(tc.wantHargs)) {
				t.Errorf("HarnessArgs = %#v, want %#v", inv.HarnessArgs, tc.wantHargs)
			}
		})
	}
}

// nilIfEmpty normalizes an empty slice to nil so DeepEqual treats a nil and an
// empty slice as equal (the splitter mixes the two, and the difference is moot).
func nilIfEmpty(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
}

func TestParseDriveWardFlags(t *testing.T) {
	cases := []struct {
		name       string
		wardArgs   []string
		wantPolicy string
		wantHelp   bool
		wantErr    bool
	}{
		{name: "empty", wardArgs: nil},
		{name: "policy equals", wardArgs: []string{"--policy=strict"}, wantPolicy: "strict"},
		{name: "policy space", wardArgs: []string{"--policy", "strict"}, wantPolicy: "strict"},
		{name: "help long", wardArgs: []string{"--help"}, wantHelp: true},
		{name: "help short", wardArgs: []string{"-h"}, wantHelp: true},
		{name: "policy missing value", wardArgs: []string{"--policy"}, wantErr: true},
		{name: "unknown ward flag", wardArgs: []string{"--bogus"}, wantErr: true},
		{name: "harness flag leaked before harness", wardArgs: []string{"--non-interactive"}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := parseDriveWardFlags(tc.wardArgs)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseDriveWardFlags(%v) err = nil, want error", tc.wardArgs)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseDriveWardFlags(%v) unexpected err = %v", tc.wardArgs, err)
			}
			if f.Policy != tc.wantPolicy {
				t.Errorf("Policy = %q, want %q", f.Policy, tc.wantPolicy)
			}
			if f.Help != tc.wantHelp {
				t.Errorf("Help = %v, want %v", f.Help, tc.wantHelp)
			}
		})
	}
}

func TestRenderArgv(t *testing.T) {
	cases := []struct {
		argv []string
		want string
	}{
		{[]string{"gptme", "--foo"}, "gptme --foo"},
		{[]string{"gptme", "deploy the thing"}, `gptme "deploy the thing"`},
		{[]string{"gptme", ""}, `gptme ""`},
	}
	for _, tc := range cases {
		if got := renderArgv(tc.argv); got != tc.want {
			t.Errorf("renderArgv(%#v) = %q, want %q", tc.argv, got, tc.want)
		}
	}
}

func TestWantsDriveHelp(t *testing.T) {
	cases := []struct {
		raw  []string
		want bool
	}{
		{nil, true},
		{[]string{"--help"}, true},
		{[]string{"-h"}, true},
		{[]string{"--policy=strict"}, false},
		{[]string{"--policy"}, false},
	}
	for _, tc := range cases {
		if got := wantsDriveHelp(tc.raw); got != tc.want {
			t.Errorf("wantsDriveHelp(%#v) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

// TestWardedPublicFaceShim documents the argv0 rewrite contract: invoking the
// binary as `warded` is `ward drive` with the same trailing args.
func TestWardedPublicFaceShim(t *testing.T) {
	if wardedPublicFace != "warded" {
		t.Fatalf("wardedPublicFace = %q, want warded", wardedPublicFace)
	}
}
