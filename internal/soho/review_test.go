package soho

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewReadsPublicLinksAndChildMaterials(t *testing.T) {
	fixture := strings.Replace(homeworkChatFixture, `"text":"<p>Текст</p>"`, `"text":"<a href=\"https://board.test/task\">Доска</a>"`, 1)
	c := inboxTestClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"data":{"chat":`+fixture+`}}`) })
	calls := 0
	d := testDownloader(t, func(r *http.Request) (*http.Response, error) {
		calls++
		for _, header := range []string{"Authorization", "Cookie", "x-procraft-org", "Referer"} {
			if r.Header.Get(header) != "" {
				t.Fatalf("credentials sent to material host: %s", header)
			}
		}
		body, mime := []byte(`<html><body><p>Вычислите производную</p><img src="https://images.test/figure.png"><a href="https://docs.test/task.pdf">Условие</a></body></html>`), "text/html"
		switch r.URL.Hostname() {
		case "board.test":
		case "images.test":
			body, mime = append([]byte{137, 80, 78, 71, 13, 10, 26, 10}, bytes.Repeat([]byte{0}, 32)...), "image/png"
		case "docs.test":
			body, mime = []byte("%PDF-1.7\nsynthetic"), "application/pdf"
		default:
			t.Fatal("unexpected material host")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{mime}}, ContentLength: -1, Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
	})
	service := NewReviewService(c, d, t.TempDir(), map[string]map[string]string{"chat-test": {"author": "teacher"}})
	review, err := service.Get(context.Background(), "chat-test", 20)
	if err != nil || len(review.Resources) != 1 {
		t.Fatalf("get: %v", err)
	}
	page, err := service.Read(context.Background(), review.Resources[0].ID)
	if err != nil || !strings.Contains(page.Text, "производную") || len(page.Images) != 1 || len(page.Links) != 1 {
		t.Fatalf("public page unavailable: %v", err)
	}
	for _, ref := range []MaterialReference{page.Images[0].MaterialReference, page.Links[0]} {
		result, err := service.Read(context.Background(), ref.ID)
		if err != nil || result.Bytes == 0 || result.Attachment.MessageID != review.Resources[0].MessageID || result.Attachment.Role != "teacher" {
			t.Fatalf("child material unavailable or source lost: %v", err)
		}
	}
	if _, err := service.Read(context.Background(), review.Resources[0].ID); err != nil || calls != 3 {
		t.Fatal("cached page fetched again")
	}
	if _, err := service.Read(context.Background(), "https://arbitrary.test/task"); err == nil || calls != 3 {
		t.Fatal("unregistered URL fetched")
	}
}

func TestReviewPackageCompleteness(t *testing.T) {
	fixture := strings.Replace(homeworkChatFixture, `"text":"<p>Текст</p>"`, `"text":"<a href=\"https://files.test/task\">Задание</a>"`, 1)
	// Убираем неизвестный тип из второго сообщения, чтобы проверить полноту отдельно
	fixture = strings.Replace(fixture, `"{\"typ\":\"FutureMeta\"}"`, `null`, 1)
	c := inboxTestClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"data":{"chat":`+fixture+`}}`) })
	d := testDownloader(t, func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, ContentLength: -1, Body: io.NopCloser(strings.NewReader(`<html><body><p>Задание</p><img src="/figure.png"></body></html>`))}, nil
	})
	service := NewReviewService(c, d, filepath.Join(t.TempDir(), "private"), map[string]map[string]string{"chat-test": {"author": "teacher"}})
	ctx := context.Background()
	review, err := service.Get(ctx, "chat-test", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(review.Resources) != 1 || len(review.UnreadMaterialIDs) != 1 || review.MaterialsAvailable || len(review.Groups["teacher"]) != 1 {
		t.Fatalf("incomplete materials not exposed: %+v", review)
	}
	if _, err := service.Read(ctx, "https://arbitrary.test/"); err == nil {
		t.Fatal("arbitrary URL accepted as material ID")
	}
	result, err := service.Read(ctx, review.Resources[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text == "" || !strings.Contains(result.Path, "teacher") {
		t.Fatal("link content or author directory lost")
	}
	after, err := service.Get(ctx, "chat-test", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Resources) != 2 || len(after.UnreadMaterialIDs) != 1 || after.MaterialsAvailable {
		t.Fatal("unread image on linked page omitted")
	}
}

func TestReviewKeepsFileMetadataAndFailures(t *testing.T) {
	body := `{"data":{"chat":{"__typename":"Chat","id":"chat","uid":42,"studentHomework":{"academicHomework":{"id":"assignment"}},"messagesNew":{"pageInfo":{"hasPreviousPage":false,"hasNextPage":false},"edges":[{"node":{"id":"message","userCreated":{"id":"teacher"},"metaInfo":"{\"typ\":\"MultiFile\",\"payload\":{\"files\":[{\"url\":\"https://files.test/a.pdf\",\"size\":999,\"mimeType\":\"application/pdf\"}]}}"}}]}}}}`
	c := inboxTestClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) })
	d := testDownloader(t, func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/pdf"}}, ContentLength: -1, Body: io.NopCloser(strings.NewReader("%PDF-1.7\n"))}, nil
	})
	service := NewReviewService(c, d, filepath.Join(t.TempDir(), "private"), map[string]map[string]string{"chat": {"teacher": "teacher"}})
	review, err := service.Get(context.Background(), "chat", 20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Read(context.Background(), review.Resources[0].ID); err == nil {
		t.Fatal("MultiFile size metadata lost")
	}
	after, err := service.Get(context.Background(), "chat", 20)
	if err != nil {
		t.Fatal(err)
	}
	if after.MaterialsAvailable || after.Resources[0].State != "failed" || after.Resources[0].FetchError == "" || after.Messages[0].Attachments[0].State != "failed" {
		t.Fatal("failed file disguised as available")
	}
}
