//go:build windows

package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// securePrivateFile sets and verifies a protected current-user-only Windows
// DACL before the caller writes credentials (docs/container-staging.md).
func securePrivateFile(f *os.File) error {
	tokenUser, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fmt.Errorf("resolve current Windows user: %w", err)
	}
	sddl := "D:P(A;;FA;;;" + tokenUser.User.Sid.String() + ")"
	want, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return fmt.Errorf("build private Windows DACL: %w", err)
	}
	wantDACL, _, err := want.DACL()
	if err != nil {
		return fmt.Errorf("read private Windows DACL: %w", err)
	}
	if err := windows.SetNamedSecurityInfo(
		f.Name(),
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		wantDACL,
		nil,
	); err != nil {
		return fmt.Errorf("set private Windows DACL: %w", err)
	}

	got, err := windows.GetNamedSecurityInfo(
		f.Name(),
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return fmt.Errorf("verify private Windows DACL: %w", err)
	}
	if got == nil || got.String() != want.String() {
		gotSDDL := "<nil>"
		if got != nil {
			gotSDDL = got.String()
		}
		return fmt.Errorf("verify private Windows DACL: got %q, want %q", gotSDDL, want.String())
	}
	control, _, err := got.Control()
	if err != nil {
		return fmt.Errorf("verify protected Windows DACL: %w", err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		return fmt.Errorf("verify protected Windows DACL: inheritance remains enabled")
	}
	return nil
}
