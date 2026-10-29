package desktopsetup

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const configStart = "# >>> SOHO LMS >>>"
const configEnd = "# <<< SOHO LMS <<<"

func UpdateConfig(original []byte, binary, envFile string) ([]byte, error) {
	original = bytes.TrimPrefix(original, []byte{0xef, 0xbb, 0xbf})
	var before map[string]any
	if err := toml.Unmarshal(original, &before); err != nil {
		return nil, fmt.Errorf("Конфигурация ChatGPT содержит ошибку TOML, изменения не внесены")
	}
	remaining := string(original)
	start, end := strings.Index(remaining, configStart), strings.Index(remaining, configEnd)
	if start >= 0 || end >= 0 {
		if start < 0 || end < start || strings.Count(remaining, configStart) != 1 || strings.Count(remaining, configEnd) != 1 {
			return nil, fmt.Errorf("Блок SOHO в конфигурации повреждён, изменения не внесены")
		}
		remaining = remaining[:start] + remaining[end+len(configEnd):]
	}
	var unmanaged map[string]any
	if err := toml.Unmarshal([]byte(remaining), &unmanaged); err != nil {
		return nil, fmt.Errorf("Не удалось безопасно обновить блок SOHO")
	}
	if servers, ok := unmanaged["mcp_servers"].(map[string]any); ok && servers["soho"] != nil {
		return nil, fmt.Errorf("Подключение soho уже настроено вручную: удалите его в Settings → MCP servers и повторите установку")
	}
	block, err := toml.Marshal(map[string]any{"mcp_servers": map[string]any{"soho": map[string]any{
		"command": binary, "args": []string{"--mcp", "--env-file", envFile},
		"startup_timeout_sec": 90, "tool_timeout_sec": 90,
	}}})
	if err != nil {
		return nil, fmt.Errorf("Не удалось подготовить подключение MCP")
	}
	updated := []byte(strings.TrimRight(remaining, "\r\n") + "\n\n" + configStart + "\n" + string(block) + configEnd + "\n")
	var after map[string]any
	if err := toml.Unmarshal(updated, &after); err != nil {
		return nil, fmt.Errorf("Формат существующей конфигурации не позволяет добавить SOHO автоматически")
	}
	// Сравниваем остальные настройки, включая строки с похожими маркерами
	removeSOHO := func(value map[string]any) {
		if servers, ok := value["mcp_servers"].(map[string]any); ok {
			delete(servers, "soho")
			if len(servers) == 0 {
				delete(value, "mcp_servers")
			}
		}
	}
	removeSOHO(before)
	removeSOHO(after)
	if len(before) != len(after) || len(before) != 0 && !reflect.DeepEqual(before, after) {
		return nil, fmt.Errorf("Обновление затрагивает другие настройки, изменения не внесены")
	}
	return updated, nil
}
