package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stepaninn/soho-adapter/internal/soho"
)

func TestAuthorRoleSettings(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "roles.json")
	for _, source := range []string{`{"chat":{"profile":"student"}}`, `{"chat":{"profile":"teacher"}}`} {
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
		roles, err := readAuthorRoles(path)
		if err != nil || roles["chat"]["profile"] == "" {
			t.Fatal("confirmed role not loaded")
		}
	}
	for _, source := range []string{`{"chat":{"profile":"guess"}}`, `{"chat":{"":"teacher"}}`, `{"":{"profile":"student"}}`, `{broken`} {
		os.WriteFile(path, []byte(source), 0600)
		if _, err := readAuthorRoles(path); err == nil {
			t.Fatal("invalid roles accepted")
		}
	}
	os.WriteFile(path, []byte(`{"chat":{"profile":"student"}}`), 0600)
	envPath := filepath.Join(directory, ".env")
	os.WriteFile(envPath, []byte("SOHO_AUTHOR_ROLES_FILE=roles.json\nSOHO_RESOURCE_HOSTS=files.test\nSOHO_OUTPUT_DIR=private\n"), 0600)
	t.Setenv("SOHO_AUTHOR_ROLES_FILE", "roles.json")
	t.Setenv("SOHO_RESOURCE_HOSTS", "files.test")
	t.Setenv("SOHO_OUTPUT_DIR", "private")
	options, err := parseOptions([]string{"--mcp", "--env-file", envPath}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(options.Hosts, "files.test") || options.AuthorRoles["chat"]["profile"] != "student" || options.Out != filepath.Join(directory, "private") {
		t.Fatal("MCP settings not resolved against env file")
	}
}

func TestDefaultDownloadSettings(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("SOHO_RESOURCE_HOSTS", "")
	t.Setenv("SOHO_AUTHOR_ROLES_FILE", "")
	options, err := parseOptions([]string{"--chat-id", "chat", "--out", "private"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if options.Hosts != strings.Join(soho.DefaultResourceHosts(), ",") {
		t.Fatal("ordinary downloads require manual host configuration")
	}
	metadata, err := parseOptions([]string{"--chat-id", "chat", "--out", "private", "--metadata-only"}, io.Discard)
	if err != nil || metadata.Hosts != "" {
		t.Fatal("metadata-only enabled downloads")
	}
}
