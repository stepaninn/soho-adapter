package soho

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testDownloader(t *testing.T, transport inboxTransport) *Downloader {
	t.Helper()
	d, err := NewDownloader(DownloadOptions{AllowedHosts: []string{"files.test"}, MaxBytes: 1024, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	d.client.Transport = transport
	return d
}

func TestDownloadAttachment(t *testing.T) {
	for _, tt := range []struct {
		name, mime string
		body       []byte
	}{
		{"PDF", "application/pdf", []byte("%PDF-1.7\nsynthetic test\n%%EOF")},
		{"PNG", "image/png", append([]byte{137, 80, 78, 71, 13, 10, 26, 10}, bytes.Repeat([]byte{0}, 32)...)},
		{"JPEG", "image/jpeg", append([]byte{255, 216, 255, 224}, bytes.Repeat([]byte{0}, 32)...)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d := testDownloader(t, func(r *http.Request) (*http.Response, error) {
				if r.Method != "GET" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("x-procraft-org") != "" {
					t.Error("unsafe attachment request")
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{tt.mime}}, ContentLength: -1, Body: io.NopCloser(bytes.NewReader(tt.body))}, nil
			})
			directory := filepath.Join(t.TempDir(), "private")
			size := int64(len(tt.body))
			fileID := int64(7)
			attachment := Attachment{MaterialReference: MaterialReference{ChatID: "chat", MessageID: "message", URL: "https://files.test/file?secret=hidden", Purpose: "file"}, Name: "file", Size: &size, FileID: &fileID, MIMEType: tt.mime}
			result, err := d.DownloadAttachment(context.Background(), attachment, directory)
			if err != nil {
				t.Fatal(err)
			}
			body, err := os.ReadFile(result.Path)
			if err != nil || !bytes.Equal(body, tt.body) || result.State != "downloaded" || result.Bytes != size || result.MIMEType != tt.mime || result.Attachment.MessageID != "message" {
				t.Fatal("download or provenance corrupted")
			}
			info, _ := os.Stat(result.Path)
			if info.Mode().Perm() != 0600 {
				t.Fatal("file is not private")
			}
			second, err := d.DownloadAttachment(context.Background(), attachment, directory)
			if err != nil || second.Path == result.Path {
				t.Fatal("repeated download overwrote an existing file")
			}
			entries, _ := os.ReadDir(directory)
			if len(entries) != 2 {
				t.Fatal("temporary files left behind")
			}
		})
	}
}

func TestDownloadAttachmentFailures(t *testing.T) {
	for _, tt := range []struct {
		name, url, filename, metadataMIME, contentType, purpose string
		body                                                    string
		status                                                  int
		size                                                    *int64
		cancel                                                  bool
	}{
		{name: "bad signature", body: "<html>error</html>"},
		{name: "oversize without Content-Length", body: strings.Repeat("a", 1025)},
		{name: "HTTP error", status: 503}, {name: "partial response", status: 206}, {name: "redirect", status: 302},
		{name: "cancel", cancel: true},
		{name: "unsafe filename", filename: "../outside.pdf"},
		{name: "windows filename", filename: `..\outside.pdf`},
		{name: "untrusted host", url: "https://other.test/file?secret=hidden"},
		{name: "local address", url: "https://127.0.0.1/file"},
		{name: "http scheme", url: "http://files.test/file"},
		{name: "URL userinfo", url: "https://user:password@files.test/file"},
		{name: "unsupported MIME", metadataMIME: "text/plain"},
		{name: "MIME mismatch", body: "%PDF-1.7\n", metadataMIME: "image/png"},
		{name: "header mismatch", body: "%PDF-1.7\n", contentType: "text/html"},
		{name: "inline PDF", body: "%PDF-1.7\n", purpose: "inline-image"},
		{name: "external href", body: "%PDF-1.7\n", purpose: "external-link"},
		{name: "size mismatch", body: "%PDF-1.7\n", size: func() *int64 { n := int64(99); return &n }()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			d := testDownloader(t, func(r *http.Request) (*http.Response, error) {
				calls++
				status := tt.status
				if status == 0 {
					status = 200
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{tt.contentType}, "Location": []string{"https://other.test/?secret=hidden"}}, ContentLength: -1, Body: io.NopCloser(strings.NewReader(tt.body))}, nil
			})
			directory := filepath.Join(t.TempDir(), "private")
			raw := tt.url
			if raw == "" {
				raw = "https://files.test/file?secret=hidden"
			}
			purpose := tt.purpose
			if purpose == "" {
				purpose = "file"
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancel {
				cancel()
			}
			result, err := d.DownloadAttachment(ctx, Attachment{MaterialReference: MaterialReference{URL: raw, Purpose: purpose}, Name: tt.filename, MIMEType: tt.metadataMIME, Size: tt.size}, directory)
			if err == nil || result.State == "downloaded" || result.Path != "" {
				t.Fatal("expected failed download")
			}
			if strings.Contains(err.Error(), "hidden") || strings.Contains(err.Error(), raw) {
				t.Fatal("error leaks URL")
			}
			if tt.cancel && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost")
			}
			entries, _ := os.ReadDir(directory)
			if len(entries) != 0 {
				t.Fatal("failed download left files")
			}
			if calls > 1 {
				t.Fatal("redirect followed")
			}
		})
	}
}

func TestDownloadAddressPolicy(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.1.2.3", "169.254.169.254", "100.64.0.1", "::1", "fc00::1", "::ffff:127.0.0.1", "2002:7f00:1::"} {
		if publicAddress(netip.MustParseAddr(ip)) {
			t.Errorf("restricted address allowed: %s", ip)
		}
	}
	if !publicAddress(netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("public address rejected")
	}
	for _, options := range []DownloadOptions{{}, {AllowedHosts: []string{"*"}, MaxBytes: 1, Timeout: time.Second}, {AllowedHosts: []string{"127.0.0.1"}, MaxBytes: 1, Timeout: time.Second}} {
		if _, err := NewDownloader(options); err == nil {
			t.Fatal("bad options accepted")
		}
	}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := PrivateDirectory(directory); err == nil {
		t.Fatal("public directory accepted")
	}
}

func TestClientRejectsRedirect(t *testing.T) {
	c, _ := NewClient("https://api.test")
	c.token = "private-token"
	calls := 0
	c.httpClient.Transport = inboxTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://other.test/"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	if _, err := c.do(context.Background(), "POST", "/master/graphql", nil); err == nil {
		t.Fatal("redirect accepted")
	}
	if calls != 1 {
		t.Fatal("API token may have crossed redirect")
	}
}

type interruptedBody struct{ cancel context.CancelFunc }

func (b interruptedBody) Read(buffer []byte) (int, error) {
	b.cancel()
	return copy(buffer, []byte("%PDF-1.7")), context.Canceled
}
func (b interruptedBody) Close() error { return nil }

func TestDownloadCancellationDuringRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := testDownloader(t, func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), ContentLength: -1, Body: interruptedBody{cancel: cancel}}, nil
	})
	directory := filepath.Join(t.TempDir(), "private")
	_, err := d.DownloadAttachment(ctx, Attachment{MaterialReference: MaterialReference{URL: "https://files.test/file", Purpose: "file"}}, directory)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error: %v", err)
	}
	entries, _ := os.ReadDir(directory)
	if len(entries) != 0 {
		t.Fatal("canceled download left temporary file")
	}
}
