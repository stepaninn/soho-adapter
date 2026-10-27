package soho

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLogin(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		response  string
		wantError bool
	}{
		{
			name:     "success",
			status:   http.StatusOK,
			response: `{"token":"test-token"}`,
		},
		{
			name:      "unauthorized",
			status:    http.StatusUnauthorized,
			wantError: true,
		},
		{
			name:      "invalid response",
			status:    http.StatusOK,
			response:  "<html>error</html>",
			wantError: true,
		},
		{
			name:      "empty response",
			status:    http.StatusOK,
			wantError: true,
		},
		{
			name:      "null token",
			status:    http.StatusOK,
			response:  `{"token":null}`,
			wantError: true,
		},
		{
			name:      "bad json",
			status:    http.StatusOK,
			response:  `"{token":"test-token"}{}`,
			wantError: true,
		},
		{
			name:   "long json",
			status: http.StatusOK,
			response: `{"token":"` +
				strings.Repeat("a", maxJSONResponseSize+1) +
				`"}`,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodPost ||
						r.URL.Path != "/api/login/password" {
						t.Errorf(
							"unexpected request: %s %s",
							r.Method,
							r.URL.Path,
						)
					}

					if r.Header.Get("Content-Type") != "application/json" {
						t.Error("missing JSON Content-Type")
					}

					var input loginRequest

					if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
						t.Error("invalid request JSON")
						w.WriteHeader(http.StatusBadRequest)
						return
					}

					if input.Login != "test-login" || input.Password != "test-password" {
						t.Error("unexpected login and password")
					}

					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(tt.status)
					_, _ = io.WriteString(w, tt.response)
				}),
			)
			defer server.Close()

			client, err := NewClient(server.URL)
			if err != nil {
				t.Fatal(err)
			}

			token, err := client.Login(
				context.Background(),
				"test-login",
				"test-password",
			)
			if (err != nil) != tt.wantError {
				t.Fatalf("Login error() = %v, wantError = %v", err, tt.wantError)
			}

			if !tt.wantError && token != "test-token" {
				t.Errorf("Login() token=%q, want=%q", token, "test-token")
			}
		})
	}
}

func TestAuthenticate(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		response  string
		wantError bool
	}{
		{
			name:     "success",
			status:   http.StatusOK,
			response: `{"token":"org-token"}`,
		},
		{
			name:      "unauthorized",
			status:    http.StatusUnauthorized,
			wantError: true,
		},
		{
			name:      "invalid response",
			status:    http.StatusOK,
			response:  "<html>error</html>",
			wantError: true,
		},
		{
			name:      "no token in response",
			status:    http.StatusOK,
			response:  `{}`,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var paths []string

			server := httptest.NewServer(
				http.HandlerFunc(
					func(w http.ResponseWriter, r *http.Request) {
						paths = append(paths, r.URL.Path)

						if r.Method != http.MethodPost {
							t.Errorf("unexpected method: %s", r.Method)
						}

						w.Header().Set("Content-Type", "application/json")

						switch r.URL.Path {
						case "/api/login/password":
							if r.Header.Get("Authorization") != "" {
								t.Error("login must not contain empty token")
							}

							_, _ = io.WriteString(w, `{"token":"root-token"}`)
						case "/master/api/set-org":
							if r.Header.Get("Authorization") != "Bearer root-token" {
								t.Error("set-org must use the root-token")
							}

							body, err := io.ReadAll(r.Body)
							if err != nil {
								t.Error("cannot read request body")
							}

							if len(body) != 0 {
								t.Error("set-org must have empty body")
							}

							w.WriteHeader(tt.status)
							_, _ = io.WriteString(w, tt.response)
						default:
							http.NotFound(w, r)
						}
					}),
			)
			defer server.Close()

			client, err := NewClient(server.URL)
			if err != nil {
				t.Fatal(err)
			}

			client.token = "old-token"

			err = client.Authenticate(
				context.Background(),
				"test-login",
				"test-password",
			)
			if tt.wantError {
				if err == nil {
					t.Fatal("expected authentication error")
				}

				if client.token != "" {
					t.Error("failed authentication must clear the token")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}

				if client.token != "org-token" {
					t.Error("organization token was not saved")
				}
			}
			if len(paths) != 2 ||
				paths[0] != "/api/login/password" ||
				paths[1] != "/master/api/set-org" {
				t.Error("unexpected authentication sequence")
			}
		})
	}
}
