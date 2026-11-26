package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stepaninn/soho-adapter/internal/soho"
)

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMCPWorkflow(t *testing.T) {
	previous := http.DefaultTransport
	defer func() { http.DefaultTransport = previous }()
	requests := 0
	http.DefaultTransport = testTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		var body string
		switch r.URL.Path {
		case "/api/login/password", "/master/api/set-org":
			body = `{"token":"test.eyJvcmdJZCI6IjQyIn0.test"}`
		case "/master/graphql":
			var request struct {
				Query     string
				Variables map[string]any
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			if strings.Contains(request.Query, "mutation") {
				t.Error("mutation sent")
			}
			if strings.Contains(request.Query, "query ListHomeworkChats") {
				body = `{"data":{"viewer":{"chats":{"pageInfo":{"hasNextPage":false},"edges":[{"node":{"id":"chat","uid":42,"title":"Тест"}}]}}}}`
			} else {
				body = `{"data":{"chat":{"__typename":"Chat","id":"chat","uid":42,"title":"Тест","teachers":[{"user":{"id":"teacher"}}],"students":[],"studentHomework":{"academicHomework":{"id":"assignment"}},"messagesNew":{"pageInfo":{"hasPreviousPage":false,"hasNextPage":false},"edges":[{"node":{"id":"teacher-message","chatId":42,"userCreated":{"id":"teacher","uid":50,"name":"Преподаватель"},"messageType":"Text","isWysiwyg":false,"text":"Задание https://files.test/task"}}]}}}}`
			}
		default:
			t.Error("unexpected endpoint")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	client, err := soho.NewClient("https://api.test")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.Authenticate(ctx, "test-login", "test-password"); err != nil {
		t.Fatal(err)
	}
	service := soho.NewReviewService(client, nil, t.TempDir(), nil)
	server := New(service)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	session, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	connection, err := mcpClient.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	instructions := connection.InitializeResult().Instructions
	for _, section := range []string{"Сценарий: список", "Сценарий: проверка", "OnReview"} {
		if !strings.Contains(instructions, section) {
			t.Errorf("workflow instruction missing: %s", section)
		}
	}
	guide, err := connection.ReadResource(ctx, &mcp.ReadResourceParams{URI: workflowURI})
	if err != nil || len(guide.Contents) != 1 || guide.Contents[0].Text != instructions || guide.Contents[0].MIMEType != "text/markdown" {
		t.Fatal("workflow guide resource differs from initialization instructions")
	}

	tools, err := connection.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 3 {
		t.Fatalf("unexpected tools: %d", len(tools.Tools))
	}
	for _, tool := range tools.Tools {
		if tool.Name != "get_homework" && tool.Name != "list_homeworks" && tool.Name != "read_material" {
			t.Fatal("unexpected writable tool")
		}
	}
	listed, err := connection.CallTool(ctx, &mcp.CallToolParams{Name: "list_homeworks", Arguments: map[string]any{}})
	if err != nil || listed.IsError {
		t.Fatalf("list: %v %+v", err, listed)
	}
	encoded, _ := json.Marshal(listed.StructuredContent)
	if !strings.Contains(string(encoded), "https://master.soholms.com/inbox/42") {
		t.Fatal("chat link missing")
	}
	result, err := connection.CallTool(ctx, &mcp.CallToolParams{Name: "get_homework", Arguments: map[string]any{"chat_id": "chat"}})
	if err != nil || result.IsError {
		t.Fatalf("get: %v %+v", err, result)
	}
	encoded, err = json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var review soho.ReviewPackage
	if err := json.Unmarshal(encoded, &review); err != nil {
		t.Fatal(err)
	}
	if len(review.Groups["teacher"]) != 1 || len(review.UnreadMaterialIDs) != 1 || review.MaterialsAvailable || review.Messages[0].Author.ID != "teacher" || review.Messages[0].RoleSource != "chat-participant-profile-id" {
		t.Fatal("roles or missing link omitted")
	}
	before := requests
	for _, id := range []string{"https://arbitrary.test/", review.UnreadMaterialIDs[0]} {
		result, err := connection.CallTool(ctx, &mcp.CallToolParams{Name: "read_material", Arguments: map[string]any{"material_id": id}})
		if err != nil || !result.IsError {
			t.Fatalf("expected tool error: %v %+v", err, result)
		}
	}
	if requests != before {
		t.Fatal("invalid/unconfigured resource caused network access")
	}
	invalid, err := connection.CallTool(ctx, &mcp.CallToolParams{Name: "get_homework", Arguments: map[string]any{"chat_id": "chat", "message_limit": 9999}})
	if err != nil || !invalid.IsError || requests != before {
		t.Fatal("invalid message limit reached API")
	}
}

func TestMCPMaterialContent(t *testing.T) {
	for _, mime := range []string{"image/png", "application/pdf"} {
		path := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(path, []byte("synthetic bytes"), 0600); err != nil {
			t.Fatal(err)
		}
		response, _, err := materialResponse("material-id", soho.DownloadResult{Path: path, MIMEType: mime, Attachment: soho.Attachment{MaterialReference: soho.MaterialReference{Role: "teacher", MessageID: "source-message", Author: &soho.MessageAuthor{ID: "profile"}}}})
		if err != nil || len(response.Content) != 2 {
			t.Fatalf("response: %v %v", response, err)
		}
		content := response.Content
		if !strings.Contains(content[0].(*mcp.TextContent).Text, "source-message") || !strings.Contains(content[0].(*mcp.TextContent).Text, "teacher") {
			t.Fatal("binary material source missing from text fallback")
		}
		if mime == "image/png" {
			if image, ok := content[1].(*mcp.ImageContent); !ok || len(image.Data) == 0 {
				t.Fatal("image bytes missing")
			}
		} else {
			resource, ok := content[1].(*mcp.EmbeddedResource)
			if !ok || len(resource.Resource.Blob) == 0 || resource.Resource.URI != "soho-material://material-id" {
				t.Fatal("PDF bytes missing")
			}
		}
	}
}

func TestMCPLargeMaterialReturnsLocalPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "attachment.pdf")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	const size = 18 << 20
	if err := file.Truncate(size); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	response, output, err := materialResponse("material-id", soho.DownloadResult{
		Path: path, Bytes: size, MIMEType: "application/pdf", State: "downloaded",
		Attachment: soho.Attachment{MaterialReference: soho.MaterialReference{Role: "student", MessageID: "source-message"}},
	})
	if err != nil || len(response.Content) != 1 {
		t.Fatalf("expected metadata without binary content, got %v, %v", response, err)
	}
	var decoded materialResult
	if err := json.Unmarshal([]byte(response.Content[0].(*mcp.TextContent).Text), &decoded); err != nil {
		t.Fatal(err)
	}
	for _, result := range []materialResult{output.(materialResult), decoded} {
		if result.Delivery != "local_file" || result.Path != path || result.Bytes != size || result.State != "downloaded" || result.Attachment.MessageID != "source-message" || result.ReadingInstructions == "" {
			t.Fatalf("incomplete local file response: %+v", result)
		}
	}
}
