package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stepaninn/soho-adapter/internal/soho"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "MCP check: FAIL:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("mcp-check", flag.ContinueOnError)
	flags.SetOutput(out)
	envFile := flags.String("env-file", ".env", "Путь к настройкам SOHO")
	chatID := flags.String("chat-id", "", "Проверить выбранный чат, иначе первый из очереди")
	maxMaterials := flags.Int("max-materials", 2, "Максимум вложений для чтения, 0..5")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 || *maxMaterials < 0 || *maxMaterials > 5 {
		return fmt.Errorf("invalid check options")
	}
	absoluteEnv, err := filepath.Abs(*envFile)
	if err != nil {
		return fmt.Errorf("cannot resolve settings path")
	}
	temporary, err := os.MkdirTemp("", "soho-mcp-check-")
	if err != nil {
		return fmt.Errorf("cannot create build directory")
	}
	defer os.RemoveAll(temporary)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	binary := filepath.Join(temporary, "soho-adapter")
	build := exec.CommandContext(ctx, "go", "build", "-buildvcs=false", "-o", binary, ".")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		return fmt.Errorf("server build failed")
	}
	fmt.Fprintln(out, "Server build: OK")
	command := exec.Command(binary, "--mcp", "--env-file", absoluteEnv)
	var stderr lockedBuffer
	command.Stderr = &stderr
	client := mcp.NewClient(&mcp.Implementation{Name: "soho-mcp-check", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		message := "server initialization failed"
		diagnostic := stderr.String()
		switch {
		case strings.Contains(diagnostic, "operation not permitted"):
			message = "network unavailable in sandbox"
		case strings.Contains(diagnostic, "set SOHO_LOGIN and SOHO_PASSWORD"):
			message = "SOHO_LOGIN or SOHO_PASSWORD missing"
		case strings.Contains(diagnostic, "HTTP 401"):
			message = "SOHO returned HTTP 401"
		case strings.Contains(diagnostic, "HTTP 403"):
			message = "SOHO returned HTTP 403"
		case strings.Contains(diagnostic, "author roles"):
			message = "author roles settings invalid"
		case strings.Contains(diagnostic, "prepare session"):
			message = "SOHO authentication failed"
		case strings.Contains(diagnostic, "read organization"):
			message = "organization verification failed"
		}
		return errors.New(message)
	}
	defer session.Close()
	fmt.Fprintln(out, "STDIO initialize: OK")
	instructions := session.InitializeResult().Instructions
	if !strings.Contains(instructions, "Сценарий: список") || !strings.Contains(instructions, "Сценарий: проверка") {
		return fmt.Errorf("workflow guide missing from MCP initialization")
	}
	guide, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "soho://guide/workflow"})
	if err != nil || len(guide.Contents) != 1 || guide.Contents[0].Text != instructions {
		return fmt.Errorf("workflow guide resource unavailable")
	}
	fmt.Fprintln(out, "Workflow guide: OK")
	tools, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		return fmt.Errorf("tools/list failed")
	}
	names := []string{}
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	if strings.Join(names, ",") != "get_homework,list_homeworks,read_material" {
		return fmt.Errorf("unexpected tool set")
	}
	fmt.Fprintln(out, "tools/list: OK (get_homework, list_homeworks, read_material)")
	listed, err := call(ctx, session, "list_homeworks", map[string]any{"limit": 3})
	if err != nil {
		return err
	}
	var page struct {
		Chats    []struct{ ID string }
		PageInfo soho.PageInfo
	}
	if err := decode(listed, &page); err != nil {
		return err
	}
	fmt.Fprintf(out, "list_homeworks: OK (%d chats)\n", len(page.Chats))
	selected := *chatID
	if selected == "" {
		if len(page.Chats) == 0 {
			fmt.Fprintln(out, "Queue empty: homework and material checks skipped")
			return nil
		}
		selected = page.Chats[0].ID
	}
	received, err := call(ctx, session, "get_homework", map[string]any{"chat_id": selected})
	if err != nil {
		return err
	}
	var review soho.ReviewPackage
	if err := decode(received, &review); err != nil {
		return err
	}
	fmt.Fprintf(out, "get_homework: OK (%d messages; teacher=%d, student=%d, unknown=%d; history_complete=%t)\n", len(review.Messages), len(review.Groups["teacher"]), len(review.Groups["student"]), len(review.Groups["unknown"]), review.HistoryComplete)
	read := 0
	for _, resource := range review.Resources {
		if read >= *maxMaterials {
			break
		}
		if resource.Purpose != "file" && resource.Purpose != "inline-image" {
			continue
		}
		response, err := call(ctx, session, "read_material", map[string]any{"material_id": resource.ID})
		if err != nil {
			return err
		}
		var downloaded soho.DownloadResult
		if err := decode(response, &downloaded); err != nil {
			return err
		}
		binaryReceived := false
		for _, content := range response.Content {
			switch value := content.(type) {
			case *mcp.ImageContent:
				binaryReceived = binaryReceived || int64(len(value.Data)) == downloaded.Bytes
			case *mcp.EmbeddedResource:
				binaryReceived = binaryReceived || (value.Resource != nil && int64(len(value.Resource.Blob)) == downloaded.Bytes)
			}
		}
		if downloaded.State != "downloaded" || !binaryReceived || downloaded.Bytes <= 0 || downloaded.Attachment.Role != resource.Role {
			return fmt.Errorf("read_material returned inconsistent data")
		}
		fmt.Fprintf(out, "read_material: OK (%s; role=%s; %d bytes; binary received)\n", downloaded.MIMEType, downloaded.Attachment.Role, downloaded.Bytes)
		read++
	}
	if read == 0 {
		fmt.Fprintln(out, "No attachment read: no supported candidates selected")
	}
	fmt.Fprintln(out, "MCP check: PASS (external links and model interpretation not checked)")
	return nil
}

func call(ctx context.Context, session *mcp.ClientSession, name string, input any) (*mcp.CallToolResult, error) {
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: input})
	if err != nil {
		return nil, fmt.Errorf("%s protocol request failed", name)
	}
	if result.IsError {
		// Ошибки схемы полезны для диагностики, исходные данные работ не печатаем
		for _, content := range result.Content {
			if text, ok := content.(*mcp.TextContent); ok && strings.HasPrefix(text.Text, "get homework bundle: GraphQL error: Cannot query field") {
				return nil, fmt.Errorf("%s: %s", name, text.Text)
			}
		}
		return nil, fmt.Errorf("%s tool returned an error", name)
	}
	return result, nil
}

func decode(result *mcp.CallToolResult, destination any) error {
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		return fmt.Errorf("cannot encode structured result")
	}
	if err := json.Unmarshal(encoded, destination); err != nil {
		return fmt.Errorf("cannot decode structured result")
	}
	return nil
}

type lockedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (buffer *lockedBuffer) Write(value []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.Write(value)
}
func (buffer *lockedBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.String()
}
