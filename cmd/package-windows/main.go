package main

import (
	"archive/zip"
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Ошибка сборки:", err)
		os.Exit(1)
	}
}

func run() error {
	arch := flag.String("arch", "amd64", "Архитектура Windows: amd64 или arm64")
	out := flag.String("out", "dist", "Каталог архива")
	flag.Parse()
	if flag.NArg() != 0 || (*arch != "amd64" && *arch != "arm64") {
		return fmt.Errorf("выберите amd64 или arm64")
	}
	guide, err := os.ReadFile("docs/windows-first-run.txt")
	if err != nil {
		return fmt.Errorf("запускайте сборку из корня репозитория")
	}
	temporary, err := os.MkdirTemp("", "soho-windows-build-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	for _, target := range []struct{ name, pkg string }{
		{"soho-adapter.exe", "."}, {"soho-mcp-check.exe", "./cmd/mcp-check"}, {"SOHO-Setup.exe", "./cmd/soho-setup"},
	} {
		args := []string{"build", "-trimpath", "-buildvcs=false", "-o", filepath.Join(temporary, target.name)}
		if target.name == "SOHO-Setup.exe" {
			args = append(args, "-ldflags=-H=windowsgui")
		}
		args = append(args, target.pkg)
		command := exec.Command("go", args...)
		command.Env = append(os.Environ(), "GOOS=windows", "GOARCH="+*arch, "CGO_ENABLED=0")
		command.Stdout, command.Stderr = os.Stdout, os.Stderr
		if err := command.Run(); err != nil {
			return fmt.Errorf("сборка %s не завершена: %w", target.name, err)
		}
	}
	if err := os.MkdirAll(*out, 0755); err != nil {
		return err
	}
	archivePath := filepath.Join(*out, "soho-windows-"+*arch+".zip")
	file, err := os.CreateTemp(*out, ".soho-archive-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	writer := zip.NewWriter(file)
	for _, name := range []string{"soho-adapter.exe", "soho-mcp-check.exe", "SOHO-Setup.exe", "Первый запуск.txt"} {
		var body []byte
		if name == "Первый запуск.txt" {
			body = append([]byte{0xef, 0xbb, 0xbf}, guide...)
		} else {
			body, err = os.ReadFile(filepath.Join(temporary, name))
			if err != nil {
				return err
			}
		}
		entry, err := writer.CreateHeader(&zip.FileHeader{Name: "SOHO Review/" + name, Method: zip.Deflate})
		if err != nil {
			return err
		}
		if _, err := entry.Write(body); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), archivePath); err != nil {
		return err
	}
	archive, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer archive.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, archive); err != nil {
		return err
	}
	if err := os.WriteFile(archivePath+".sha256", []byte(fmt.Sprintf("%x  %s\n", hash.Sum(nil), filepath.Base(archivePath))), 0644); err != nil {
		return err
	}
	fmt.Println("Готово:", archivePath)
	return nil
}
