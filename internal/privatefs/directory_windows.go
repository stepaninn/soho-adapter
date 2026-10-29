package privatefs

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func Directory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return fmt.Errorf("cannot create private output directory")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("private output path must be a directory")
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fmt.Errorf("cannot identify Windows user")
	}
	// Закрываем наследование и даём доступ пользователю и системе
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + user.User.Sid.String() + ")(A;OICI;FA;;;SY)")
	if err != nil {
		return fmt.Errorf("cannot prepare private directory permissions")
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return fmt.Errorf("cannot prepare private directory ACL")
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		return fmt.Errorf("cannot protect directory with Windows ACL")
	}
	return nil
}
