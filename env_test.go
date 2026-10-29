package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadEnvFile(t *testing.T) {
	for _, tt := range []struct {
		name, content string
		want          map[string]string
		wantErr       bool
	}{
		{name: "literal values", content: "# Настройки\r\n\n SOHO_LOGIN = user@example.test \r\nSOHO_PASSWORD=' p#a=$HOME\\q=42 '\r\nOTHER=\"literal value\"\n", want: map[string]string{"SOHO_LOGIN": "user@example.test", "SOHO_PASSWORD": " p#a=$HOME\\q=42 ", "OTHER": "literal value"}},
		{name: "empty values", content: "SOHO_LOGIN=\nSOHO_PASSWORD=\"\"\n", want: map[string]string{"SOHO_LOGIN": "", "SOHO_PASSWORD": ""}},
		{name: "missing equals", content: "private-password", wantErr: true},
		{name: "invalid key", content: "1BAD=private-password", wantErr: true},
		{name: "unclosed quote", content: "SOHO_PASSWORD='private-password", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".env")
			if err := os.WriteFile(path, []byte(tt.content), 0600); err != nil {
				t.Fatal(err)
			}
			values, err := readEnvFile(path)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if strings.Contains(err.Error(), "private-password") {
					t.Error("error leaks secret")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(values) != len(tt.want) {
				t.Fatalf("unexpected number of settings: %d", len(values))
			}
			for key, want := range tt.want {
				if values[key] != want {
					t.Errorf("unexpected value for %s", key)
				}
			}
		})
	}
}

func TestCredentialsFromEnvFile(t *testing.T) {
	// Изолируем тест от реальных настроек окружения
	for _, key := range []string{"SOHO_LOGIN", "SOHO_PASSWORD"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), ".env")
	if _, _, err := credentialsFromEnvFile(path); err == nil {
		t.Fatal("missing file and environment must fail")
	}
	if err := os.WriteFile(path, []byte("SOHO_LOGIN=file-login\nSOHO_PASSWORD=file-password\n"), 0600); err != nil {
		t.Fatal(err)
	}
	login, password, err := credentialsFromEnvFile(path)
	if err != nil || login != "file-login" || password != "file-password" {
		t.Fatal("file credentials not loaded")
	}
	t.Setenv("SOHO_LOGIN", "env-login")
	login, password, err = credentialsFromEnvFile(path)
	if err != nil || login != "env-login" || password != "file-password" {
		t.Fatal("environment priority not respected")
	}
	t.Setenv("SOHO_PASSWORD", "")
	if _, _, err := credentialsFromEnvFile(path); err == nil {
		t.Fatal("explicit empty environment value must override file")
	}
	t.Setenv("SOHO_PASSWORD", "env-password")
	if _, _, err := credentialsFromEnvFile(filepath.Join(t.TempDir(), "absent")); err != nil {
		t.Fatal(err)
	}
}
