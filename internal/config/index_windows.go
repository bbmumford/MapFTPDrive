//go:build windows

package config

import "golang.org/x/sys/windows"

// driveIndexSDDL is a protected DACL: SYSTEM and Administrators full
// control, Users read. Protection stops the restrictive ACL of the
// containing directory from being inherited over it.
const driveIndexSDDL = "D:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;FR;;;BU)"

func applyDriveIndexSecurity(path string) error {
	descriptor, err := windows.SecurityDescriptorFromString(driveIndexSDDL)
	if err != nil {
		return err
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		dacl,
		nil,
	)
}
