//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestSecurePrivateFileAssertsCurrentUserOnlyDACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "launch.env")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := securePrivateFile(f); err != nil {
		_ = f.Close()
		t.Fatalf("securePrivateFile: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	tokenUser, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	want, err := windows.SecurityDescriptorFromString(
		"D:P(A;;FA;;;" + tokenUser.User.Sid.String() + ")",
	)
	if err != nil {
		t.Fatal(err)
	}
	got, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.String() != want.String() {
		t.Fatalf("env-file DACL = %v, want %s", got, want)
	}
}
