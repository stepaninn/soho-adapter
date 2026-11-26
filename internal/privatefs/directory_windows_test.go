package privatefs

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"

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
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		t.Fatal(err)
	}
	assertPrivateACL(t, sd, user.User.Sid, system, windows.OBJECT_INHERIT_ACE|windows.CONTAINER_INHERIT_ACE)

	file := filepath.Join(path, "material.pdf")
	if err := os.WriteFile(file, []byte("synthetic material"), 0600); err != nil {
		t.Fatal(err)
	}
	child, err := windows.GetNamedSecurityInfo(file, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	assertPrivateACL(t, child, user.User.Sid, system, windows.INHERITED_ACE)
}

func assertPrivateACL(t *testing.T, sd *windows.SECURITY_DESCRIPTOR, user, system *windows.SID, flags uint8) {
	t.Helper()
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil || dacl.AceCount != 2 {
		t.Fatalf("expected two DACL entries: %s (%v)", sd, err)
	}

	// SDDL may display well-known aliases such as LA in place of the numeric SID.
	want := map[string]int{user.String(): 1}
	want[system.String()]++
	const fileAllAccess = windows.STANDARD_RIGHTS_REQUIRED | windows.SYNCHRONIZE | 0x1ff
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			t.Fatal(err)
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceFlags != flags || ace.Mask != fileAllAccess {
			t.Fatalf("unexpected DACL entry %d: %s", i, sd)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart)).String()
		if want[sid] == 0 {
			t.Fatalf("unexpected trustee in DACL entry %d: %s", i, sd)
		}
		want[sid]--
	}
}
