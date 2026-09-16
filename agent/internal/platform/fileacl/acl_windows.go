//go:build windows

package fileacl

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
)

func Supported() bool { return true }

// Protect restricts a machine-owned path to LocalSystem, Administrators, and
// the user performing installation. The protected DACL intentionally stops
// inheriting permissive ACLs from ProgramData.
func Protect(path string) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil || user == nil || user.User.Sid == nil {
		return errors.New("resolve installer SID failed")
	}
	sddl := fmt.Sprintf("D:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;FA;;;%s)", user.User.Sid.String())
	descriptor, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return errors.New("build protected ACL failed")
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return errors.New("read protected ACL failed")
	}
	information := windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, information, nil, nil, dacl, nil); err != nil {
		return errors.New("apply protected ACL failed")
	}
	return nil
}
