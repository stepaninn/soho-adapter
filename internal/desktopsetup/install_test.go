package desktopsetup

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestUpdateConfigPreservesOtherSettings(t *testing.T) {
	original := []byte("# Личные настройки\r\nmodel = 'example'\r\n[mcp_servers.other]\r\ncommand = 'other.exe'\r\n")
	binary, envFile := `C:\Users\Брат\SOHO Review\soho-adapter.exe`, `C:\Users\Брат\SOHO Review\.env`
	updated, err := UpdateConfig(original, binary, envFile)
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Model   string `toml:"model"`
		Servers map[string]struct {
			Command string
			Args    []string
		} `toml:"mcp_servers"`
	}
	if err := toml.Unmarshal(updated, &config); err != nil || config.Model != "example" || config.Servers["other"].Command != "other.exe" || config.Servers["soho"].Command != binary || config.Servers["soho"].Args[2] != envFile || !bytes.Contains(updated, []byte("# Личные настройки")) {
		t.Fatalf("config corrupted: %v", err)
	}
	again, err := UpdateConfig(updated, binary, envFile)
	if err != nil || !bytes.Equal(updated, again) {
		t.Fatal("reinstall duplicates or changes configuration")
	}
	changed, err := UpdateConfig(updated, `D:\SOHO\soho-adapter.exe`, envFile)
	if err != nil || bytes.Count(changed, []byte(configStart)) != 1 || bytes.Contains(changed, []byte(binary)) {
		t.Fatal("managed configuration not updated")
	}
}

func TestUpdateConfigRejectsConflicts(t *testing.T) {
	for _, original := range []string{
		`invalid = [`,
		"[mcp_servers.soho]\ncommand = 'manual.exe'\n",
		configStart + "\nmodel = 'example'\n",
		"# <<< SOHO LMS <<<\n# >>> SOHO LMS >>>\n",
		"note = '''\n" + configStart + "\nkeep this text\n" + configEnd + "\n'''\n",
	} {
		if _, err := UpdateConfig([]byte(original), "server.exe", "settings.env"); err == nil {
			t.Fatal("conflicting or damaged config accepted")
		}
	}
}

func installerFixture(t *testing.T) (string, Paths) {
	t.Helper()
	root := t.TempDir()
	bundle := filepath.Join(root, "Комплект с пробелами")
	if err := os.Mkdir(bundle, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"soho-adapter.exe", "soho-mcp-check.exe", "SOHO-Setup.exe"} {
		if err := os.WriteFile(filepath.Join(bundle, name), []byte("MZsynthetic "+name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	paths := Paths{Directory: filepath.Join(root, "Приложение"), Skill: filepath.Join(root, "skills", "soho", "SKILL.md"), Config: filepath.Join(root, "codex", "config.toml"), Materials: filepath.Join(root, "Приложение", "materials")}
	return bundle, paths
}

func TestInstallPreservesUserFiles(t *testing.T) {
	bundle, paths := installerFixture(t)
	if err := Install(bundle, paths, Credentials{Login: "test-login", Password: ` test"$password' `}); err != nil {
		t.Fatal(err)
	}
	customSkill := []byte("Мои требования к проверке\n")
	if err := os.WriteFile(paths.Skill, customSkill, 0600); err != nil {
		t.Fatal(err)
	}
	envFile := filepath.Join(paths.Directory, ".env")
	originalEnv, _ := os.ReadFile(envFile)
	originalEnv = append(originalEnv, []byte("SOHO_RESOURCE_HOSTS=files.test\n")...)
	if err := os.WriteFile(envFile, originalEnv, 0600); err != nil {
		t.Fatal(err)
	}
	if err := Install(bundle, paths, Credentials{}); err != nil {
		t.Fatal(err)
	}
	gotSkill, _ := os.ReadFile(paths.Skill)
	gotEnv, _ := os.ReadFile(envFile)
	backups, _ := filepath.Glob(paths.Skill + ".backup-*")
	if !bytes.Equal(gotSkill, customSkill) || !bytes.Equal(gotEnv, originalEnv) || len(backups) != 1 {
		t.Fatal("user skill or credentials overwritten, or backup missing")
	}
	if err := Install(bundle, paths, Credentials{Login: "new-login", Password: "new-password"}); err != nil {
		t.Fatal(err)
	}
	gotEnv, _ = os.ReadFile(envFile)
	if !bytes.Contains(gotEnv, []byte("SOHO_RESOURCE_HOSTS=files.test")) || !bytes.Contains(gotEnv, []byte(`SOHO_LOGIN="new-login"`)) || bytes.Contains(gotEnv, []byte("test-login")) {
		t.Fatal("credential update damaged custom settings")
	}
}

func TestInstallPreflight(t *testing.T) {
	for _, mode := range []string{"credentials", "empty credentials", "missing password", "missing payload", "config"} {
		t.Run(mode, func(t *testing.T) {
			bundle, paths := installerFixture(t)
			credentials := Credentials{Login: "test-login", Password: "sensitive-value"}
			switch mode {
			case "credentials":
				credentials.Login += "\nBAD=value"
			case "empty credentials":
				credentials = Credentials{}
			case "missing password":
				credentials.Password = ""
			case "missing payload":
				os.Remove(filepath.Join(bundle, "soho-adapter.exe"))
			case "config":
				os.MkdirAll(filepath.Dir(paths.Config), 0700)
				os.WriteFile(paths.Config, []byte("broken = ["), 0600)
			}
			err := Install(bundle, paths, credentials)
			if err == nil || strings.Contains(err.Error(), "sensitive-value") {
				t.Fatal("invalid install accepted or secret exposed")
			}
			if _, err := os.Stat(paths.Directory); !os.IsNotExist(err) {
				t.Fatal("preflight failure changed install directory")
			}
		})
	}
}
