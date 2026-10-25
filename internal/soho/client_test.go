package soho

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientDo(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		wantError bool
	}{
		{
			name:   "success",
			status: http.StatusOK,
		},
		{
			name:      "unauthorized",
			status:    http.StatusUnauthorized,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(
				http.HandlerFunc(
					func(w http.ResponseWriter, r *http.Request) {
						if r.Method != http.MethodGet ||
							r.URL.Path != "/test" {
							t.Errorf(
								"unexpected request: %s %s",
								r.Method,
								r.URL.Path,
							)
						}

						if r.Header.Get("Accept") != "application/json" {
							t.Error("missing Accept header")
						}

						w.WriteHeader(tt.status)
						_, _ = io.WriteString(w, "ok")
					},
				),
			)
			defer server.Close()

			client, err := NewClient(server.URL)
			if err != nil {
				t.Fatal(err)
			}

			resp, err := client.do(
				context.Background(),
				http.MethodGet,
				"/test",
				nil,
			)

			if tt.wantError {
				if err == nil {
					if resp != nil {
						resp.Body.Close()
					}
					t.Fatal("expected HTTP error")
				}
				return
			}

			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}

			if string(body) != "ok" {
				t.Fatalf("unexpected body: %q", body)
			}
		})
	}
}
