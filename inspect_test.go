package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stepaninn/soho-adapter/internal/soho"
)

func TestParseOptions(t *testing.T) {
	for _, args := range [][]string{{"--chat-number=-1"}, {"--out=private"}, {"--chat-id=id", "--chat-number=1"}, {"--chat-id=id", "--allow-file-host=files.test"}, {"--message-limit=0"}, {"--max-file-bytes=0"}, {"unexpected"}} {
		if _, err := parseOptions(args, io.Discard); err == nil {
			t.Errorf("invalid arguments accepted: %v", args)
		}
	}
	options, err := parseOptions([]string{"--chat-number=1", "--out=" + filepath.Join(t.TempDir(), "private")}, io.Discard)
	if err != nil || options.ChatNumber != 1 || options.MessageLimit != 20 {
		t.Fatalf("options: %+v, error: %v", options, err)
	}
}

func TestInspectHomeworkManifest(t *testing.T) {
	setCLITransport(t, func(r *http.Request) (*http.Response, error) {
		body := `{"data":{"chat":{"__typename":"Chat","id":"chat-test","uid":42,"studentHomework":{"academicHomework":{"id":"academic"}},"messagesNew":{"pageInfo":{"hasPreviousPage":false,"hasNextPage":false},"edges":[{"node":{"id":"message","chatId":42,"messageType":"Text","isWysiwyg":true,"text":"<p>Пример</p><img src=\"https://files.test/a.png?secret=hidden\">"}}]}}}}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	// Готовим сессию через искусственные ответы авторизации
	c, err := soho.NewClient("https://api.test")
	if err != nil {
		t.Fatal(err)
	}
	previous := http.DefaultTransport
	http.DefaultTransport = cliTransport(func(r *http.Request) (*http.Response, error) {
		body := `{"token":"test.eyJvcmdJZCI6IjQyIn0.test"}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	if err := c.Authenticate(context.Background(), "test-login", "test-password"); err != nil {
		t.Fatal(err)
	}
	http.DefaultTransport = previous
	directory := filepath.Join(t.TempDir(), "private")
	if err := soho.PrivateDirectory(directory); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := inspectHomework(context.Background(), &out, c, inspectOptions{ChatID: "chat-test", MessageLimit: 20, Out: directory}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "https://master.soholms.com/inbox/42") || !strings.Contains(out.String(), "not-requested") {
		t.Fatalf("output: %s", &out)
	}
	if strings.Contains(out.String(), "hidden") {
		t.Fatal("signed query leaked to output")
	}
	entries, _ := os.ReadDir(directory)
	if len(entries) != 1 {
		t.Fatalf("expected manifest only, found %d files", len(entries))
	}
	path := filepath.Join(directory, entries[0].Name())
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("manifest is not private")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Materials soho.HomeworkMaterials
		Downloads []soho.DownloadResult
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Materials.ChatURL != "https://master.soholms.com/inbox/42" || manifest.Materials.Messages[0].PlainText != "Пример" || len(manifest.Downloads) != 1 || manifest.Downloads[0].State != "not-requested" {
		t.Fatal("manifest lost materials or download state")
	}
}

func TestRunSelectedChat(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("SOHO_LOGIN", "test-login")
	t.Setenv("SOHO_PASSWORD", "test-password")
	calls := 0
	setCLITransport(t, func(r *http.Request) (*http.Response, error) {
		calls++
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
			if strings.Contains(request.Query, "query CurrentOrganization") {
				body = `{"data":{"viewer":{"org":{"uid":42}}}}`
			} else if strings.Contains(request.Query, "query ListHomeworkChats(") {
				body = `{"data":{"viewer":{"chats":{"pageInfo":{"hasNextPage":false},"edges":[{"node":{"id":"chosen-opaque-id","uid":99}}]}}}}`
			} else if strings.Contains(request.Query, "query GetHomeworkBundle(") {
				if request.Variables["id"] != "chosen-opaque-id" {
					t.Error("selected numeric UID instead of opaque GraphQL ID")
				}
				body = `{"data":{"chat":{"__typename":"Chat","id":"chosen-opaque-id","uid":99,"studentHomework":{"academicHomework":{"id":"academic"}},"messagesNew":{"pageInfo":{"hasPreviousPage":false,"hasNextPage":false},"edges":[]}}}}`
			} else {
				t.Fatal("unexpected GraphQL operation")
			}
		default:
			t.Fatal("unexpected endpoint")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	var out bytes.Buffer
	if err := runConfigured(&out, inspectOptions{ChatNumber: 1, MessageLimit: 20}); err != nil {
		t.Fatal(err)
	}
	if calls != 5 || !strings.Contains(out.String(), "https://master.soholms.com/inbox/99") || !strings.Contains(out.String(), "History complete: true") {
		t.Fatalf("unexpected flow: %d requests, %s", calls, &out)
	}
}
