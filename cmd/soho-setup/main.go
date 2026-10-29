package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/stepaninn/soho-adapter/internal/desktopsetup"
)

//go:embed setup.ps1
var setupScript []byte

type response struct {
	OK      bool
	Message string
	Paths   desktopsetup.Paths
}

func main() {
	if len(os.Args) == 1 {
		if err := launchGUI(); err != nil {
			showError(err.Error())
			os.Exit(1)
		}
		return
	}
	result := response{}
	paths, err := desktopsetup.UserPaths()
	if err == nil {
		result.Paths = paths
		switch os.Args[1] {
		case "--status":
		case "--install":
			var credentials desktopsetup.Credentials
			if json.NewDecoder(io.LimitReader(os.Stdin, 16<<10)).Decode(&credentials) != nil {
				err = fmt.Errorf("Не удалось прочитать настройки доступа")
				break
			}
			var self string
			self, err = os.Executable()
			if err == nil {
				err = desktopsetup.Install(filepath.Dir(self), paths, credentials)
			}
		case "--check":
			err = check(paths)
		default:
			err = fmt.Errorf("Неизвестное действие настройки")
		}
	}
	result.OK = err == nil
	if err != nil {
		result.Message = err.Error()
	} else {
		result.Message = "Готово"
	}
	json.NewEncoder(os.Stdout).Encode(result)
	if err != nil {
		os.Exit(1)
	}
}

func check(paths desktopsetup.Paths) error {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, filepath.Join(paths.Directory, "soho-mcp-check.exe"), "--server", filepath.Join(paths.Directory, "soho-adapter.exe"), "--env-file", filepath.Join(paths.Directory, ".env"), "--max-materials", "0")
	hideWindow(command)
	if err := command.Run(); err != nil {
		return fmt.Errorf("Проверка MCP не пройдена: проверьте установку, логин, пароль и доступ к интернету")
	}
	return nil
}
