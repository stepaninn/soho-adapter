package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"

	"github.com/stepaninn/soho-adapter/internal/privatefs"
	"golang.org/x/sys/windows"
)

func hideWindow(command *exec.Cmd) { command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }

func launchGUI() error {
	systemDirectory, err := windows.GetSystemDirectory()
	if err != nil {
		return fmt.Errorf("Не удалось найти системный каталог Windows: %w", err)
	}
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("Не удалось найти программу настройки")
	}
	directory, err := os.MkdirTemp("", "soho-setup-")
	if err != nil {
		return fmt.Errorf("Не удалось создать временный каталог")
	}
	defer os.RemoveAll(directory)
	if err := privatefs.Directory(directory); err != nil {
		return err
	}
	script := filepath.Join(directory, "setup.ps1")
	// Windows PowerShell читает русский текст из UTF-8 с BOM
	if err := os.WriteFile(script, append([]byte{0xef, 0xbb, 0xbf}, setupScript...), 0600); err != nil {
		return fmt.Errorf("Не удалось подготовить окно настройки")
	}
	powershell := filepath.Join(systemDirectory, "WindowsPowerShell", "v1.0", "powershell.exe")
	command := exec.Command(powershell, "-NoLogo", "-NoProfile", "-NonInteractive", "-STA", "-ExecutionPolicy", "Bypass", "-File", script, "-SetupExe", self)
	// CREATE_NO_WINDOW suppresses the console without setting SW_HIDE on the form.
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	if err := command.Run(); err != nil {
		return fmt.Errorf("Не удалось открыть окно настройки через Windows PowerShell: %w", err)
	}
	return nil
}

func showError(message string) {
	text, _ := syscall.UTF16PtrFromString(message)
	title, _ := syscall.UTF16PtrFromString("Настройка SOHO")
	syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), 0x10)
}
