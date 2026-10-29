package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

func credentialsFromEnvFile(path string) (string, string, error) {
	values, err := readEnvFile(path)
	if err != nil {
		return "", "", err
	}
	login, password := values["SOHO_LOGIN"], values["SOHO_PASSWORD"]
	// Окружение имеет приоритет, включая явно заданное пустое значение
	if value, exists := os.LookupEnv("SOHO_LOGIN"); exists {
		login = value
	}
	if value, exists := os.LookupEnv("SOHO_PASSWORD"); exists {
		password = value
	}
	if login == "" || password == "" {
		return "", "", fmt.Errorf("set SOHO_LOGIN and SOHO_PASSWORD")
	}
	return login, password, nil
}

// readEnvFile читает KEY=value без подстановок и выполнения команд
func readEnvFile(path string) (map[string]string, error) {
	values := make(map[string]string)
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return values, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open .env: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !found || !validEnvKey(key) {
			return nil, fmt.Errorf("invalid .env assignment at line %d", lineNumber)
		}
		if strings.HasPrefix(value, "\"") || strings.HasPrefix(value, "'") {
			quote := value[0]
			if len(value) < 2 || value[len(value)-1] != quote {
				return nil, fmt.Errorf("invalid .env quotes at line %d", lineNumber)
			}
			value = value[1 : len(value)-1]
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		// Текст ошибки сканера не должен содержать строку с секретом
		return nil, fmt.Errorf("cannot read .env")
	}
	return values, nil
}

func validEnvKey(key string) bool {
	if key == "" {
		return false
	}
	for i, ch := range key {
		if ch == '_' || ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || i > 0 && ch >= '0' && ch <= '9' {
			continue
		}
		return false
	}
	return true
}
