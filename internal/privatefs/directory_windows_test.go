package privatefs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestDirectoryWindowsACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private")
	if err := Directory(path); err != nil {
		t.Fatal(err)
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatal("directory inherits permissions")
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sddl := sd.String()
	if strings.Count(sddl, "(A;") != 2 || !strings.Contains(sddl, user.User.Sid.String()) || !strings.Contains(sddl, ";;;SY)") {
		t.Fatalf("unexpected ACL: %s", sddl)
	}
	file := filepath.Join(path, "material.pdf")
	if err := os.WriteFile(file, []byte("synthetic material"), 0600); err != nil {
		t.Fatal(err)
	}
	child, err := windows.GetNamedSecurityInfo(file, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	if sddl := child.String(); strings.Count(sddl, "(A;") != 2 || !strings.Contains(sddl, user.User.Sid.String()) || !strings.Contains(sddl, ";;;SY)") {
		t.Fatalf("material did not inherit private ACL: %s", sddl)
	}
}
