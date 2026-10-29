package desktopsetup

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/stepaninn/soho-adapter/internal/privatefs"
	reviewskill "github.com/stepaninn/soho-adapter/skills/soho-homework-review"
)

type Paths struct {
	Directory string
	Skill     string
	Config    string
	Materials string
}

func UserPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil || os.Getenv("LOCALAPPDATA") == "" {
		return Paths{}, fmt.Errorf("Не удалось определить каталоги пользователя Windows")
	}
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	directory := filepath.Join(os.Getenv("LOCALAPPDATA"), "SOHO Review")
	return Paths{Directory: directory, Skill: filepath.Join(home, ".agents", "skills", "soho-homework-review", "SKILL.md"),
		Config: filepath.Join(codexHome, "config.toml"), Materials: filepath.Join(directory, "materials")}, nil
}

type Credentials struct {
	Login    string
	Password string
}

func Install(bundle string, paths Paths, credentials Credentials) error {
	if credentials.Login == "" != (credentials.Password == "") || strings.ContainsAny(credentials.Login+credentials.Password, "\r\n\x00") {
		return fmt.Errorf("Заполните логин и пароль вместе, без переводов строк")
	}
	configuration, err := os.ReadFile(paths.Config)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("Не удалось прочитать настройки ChatGPT")
	}
	updated, err := UpdateConfig(configuration, filepath.Join(paths.Directory, "soho-adapter.exe"), filepath.Join(paths.Directory, ".env"))
	if err != nil {
		return err
	}
	// Проверяем комплект до изменения файлов пользователя
	payload := map[string][]byte{}
	for _, name := range []string{"soho-adapter.exe", "soho-mcp-check.exe", "SOHO-Setup.exe"} {
		body, err := os.ReadFile(filepath.Join(bundle, name))
		if err != nil || len(body) < 2 || !bytes.HasPrefix(body, []byte("MZ")) {
			return fmt.Errorf("Комплект неполный: распакуйте весь архив и запустите SOHO-Setup.exe")
		}
		payload[name] = body
	}
	envFile := filepath.Join(paths.Directory, ".env")
	if credentials.Login == "" {
		if _, err := os.Stat(envFile); err != nil {
			return fmt.Errorf("При первой установке введите логин и пароль SOHO")
		}
	}
	for _, directory := range []string{paths.Directory, paths.Materials, filepath.Dir(paths.Skill)} {
		if err := privatefs.Directory(directory); err != nil {
			return fmt.Errorf("Не удалось защитить каталог установки или материалов")
		}
	}
	for name, body := range payload {
		if err := replaceFile(filepath.Join(paths.Directory, name), body); err != nil {
			return fmt.Errorf("Не удалось обновить программу: закройте ChatGPT и повторите установку")
		}
	}
	if credentials.Login != "" {
		// Внешние кавычки сохраняют пробелы, содержимое не исполняется
		previous, err := os.ReadFile(envFile)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("Не удалось прочитать настройки доступа")
		}
		if errors.Is(err, os.ErrNotExist) {
			previous = []byte("SOHO_OUTPUT_DIR=materials\n")
		}
		body := updateCredentials(previous, credentials)
		if err := replaceFile(envFile, body); err != nil {
			return fmt.Errorf("Не удалось сохранить настройки доступа")
		}
	}
	if _, err := os.Lstat(paths.Skill); errors.Is(err, os.ErrNotExist) {
		if err := replaceFile(paths.Skill, reviewskill.Content); err != nil {
			return fmt.Errorf("Не удалось установить навык")
		}
	} else if err != nil {
		return fmt.Errorf("Не удалось проверить существующий навык")
	} else {
		// Пользовательская методика сохраняется и не заменяется шаблоном
		if err := backup(paths.Skill); err != nil {
			return fmt.Errorf("Не удалось сохранить резервную копию навыка")
		}
	}
	if err := os.MkdirAll(filepath.Dir(paths.Config), 0700); err != nil {
		return fmt.Errorf("Не удалось создать каталог настроек ChatGPT")
	}
	if err := replaceFile(paths.Config, updated); err != nil {
		return fmt.Errorf("Не удалось сохранить подключение MCP")
	}
	return nil
}

func updateCredentials(previous []byte, credentials Credentials) []byte {
	values := map[string]string{"SOHO_LOGIN": credentials.Login, "SOHO_PASSWORD": credentials.Password}
	var lines []string
	for _, line := range strings.Split(strings.TrimRight(string(previous), "\r\n"), "\n") {
		key, _, found := strings.Cut(line, "=")
		if value, ok := values[strings.TrimSpace(key)]; found && ok {
			line = strings.TrimSpace(key) + "=\"" + value + "\""
		}
		lines = append(lines, line)
	}
	for _, key := range []string{"SOHO_LOGIN", "SOHO_PASSWORD"} {
		found := false
		for _, line := range lines {
			candidate, _, _ := strings.Cut(line, "=")
			found = found || strings.TrimSpace(candidate) == key
		}
		if !found {
			lines = append(lines, key+"=\""+values[key]+"\"")
		}
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func backup(path string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path+".backup-"+rand.Text(), body, 0600)
}

func replaceFile(path string, body []byte) error {
	previous, err := os.ReadFile(path)
	if err == nil {
		if bytes.Equal(previous, body) {
			return nil
		}
		if err := backup(path); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".soho-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err := file.Write(body); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
