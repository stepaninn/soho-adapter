//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
)

func launchGUI() error         { return fmt.Errorf("Окно настройки доступно в Windows") }
func showError(message string) { fmt.Fprintln(os.Stderr, message) }
func hideWindow(*exec.Cmd)     {}
