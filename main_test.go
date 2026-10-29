package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

type cliTransport func(*http.Request) (*http.Response, error)

func (f cliTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func setCLITransport(t *testing.T, transport cliTransport) {
	t.Helper()
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
}

func TestRunMissingCredentials(t *testing.T) {
	for _, tt := range []struct{ name, login, password string }{
		{"both missing", "", ""},
		{"login missing", "", "test-password"},
		{"password missing", "test-login", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SOHO_LOGIN", tt.login)
			t.Setenv("SOHO_PASSWORD", tt.password)
			setCLITransport(t, func(*http.Request) (*http.Response, error) {
				t.Error("missing credentials must be rejected before network access")
				return nil, fmt.Errorf("unexpected request")
			})
			var out bytes.Buffer
			if err := run(&out); err == nil || err.Error() != "set SOHO_LOGIN and SOHO_PASSWORD" {
				t.Fatalf("unexpected error: %v", err)
			}
			if out.Len() != 0 {
				t.Errorf("unexpected output: %q", out.String())
			}
		})
	}
}

func TestRunHomeworkPage(t *testing.T) {
	for _, tt := range []struct {
		name, response string
		wantErr        bool
		wantOutput     []string
	}{
		{name: "statuses and continuation", response: `{"data":{"viewer":{"chats":{"pageInfo":{"hasNextPage":true,"endCursor":"private-cursor"},"edges":[{"node":{"id":"private-chat","uid":1,"studentHomework":{"id":"private-homework","uid":2,"mergedStatus":"OnReview"}}},{"node":{"id":"other-chat","uid":3,"studentHomework":null}},{"node":{"id":"empty-status","uid":4,"studentHomework":{"id":"hw","uid":5,"mergedStatus":""}}}]}}}}`, wantOutput: []string{"Homework chats on this page: 3", "1. Status: OnReview", "2. Status: unavailable (no studentHomework)", "3. Status: unavailable (empty mergedStatus)", "only the first page was loaded"}},
		{name: "empty final page", response: `{"data":{"viewer":{"chats":{"pageInfo":{"hasNextPage":false,"endCursor":null},"edges":[]}}}}`, wantOutput: []string{"Homework chats on this page: 0"}},
		{name: "page error", response: `{"errors":[{"message":"synthetic failure"}]}`, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SOHO_LOGIN", "test-login")
			t.Setenv("SOHO_PASSWORD", "test-password")
			var steps []string
			setCLITransport(t, func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodPost || r.URL.Scheme != "https" || r.URL.Host != "api.soholms.com" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				if _, ok := r.Context().Deadline(); !ok {
					t.Error("request must have a deadline")
				}
				var body string
				switch r.URL.Path {
				case "/api/login/password":
					steps = append(steps, "login")
					body = `{"token":"root-token"}`
				case "/master/api/set-org":
					steps = append(steps, "set-org")
					body = `{"token":"test.eyJvcmdJZCI6IjQyIn0.test"}`
				case "/master/graphql":
					var input struct {
						Query     string         `json:"query"`
						Variables map[string]any `json:"variables"`
					}
					if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
						return nil, err
					}
					if strings.Contains(input.Query, "query CurrentOrganization") {
						steps = append(steps, "organization")
						body = `{"data":{"viewer":{"org":{"uid":42}}}}`
					} else if strings.Contains(input.Query, "query ListHomeworkChats(") {
						steps = append(steps, "page")
						for key, want := range map[string]any{"onlyAttention": true, "onlyMy": true, "sortDesc": false, "limit": float64(30), "after": nil} {
							if got, ok := input.Variables[key]; !ok || got != want {
								t.Errorf("%s = %v, want %v", key, got, want)
							}
						}
						body = tt.response
					} else {
						return nil, fmt.Errorf("unexpected query")
					}
				default:
					return nil, fmt.Errorf("unexpected endpoint")
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			var out bytes.Buffer
			err := run(&out)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.wantErr && !strings.Contains(err.Error(), "read homework chat page:") {
				t.Errorf("missing error context: %v", err)
			}
			if !reflect.DeepEqual(steps, []string{"login", "set-org", "organization", "page"}) {
				t.Errorf("unexpected request sequence: %v", steps)
			}
			for _, want := range tt.wantOutput {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output missing %q: %s", want, &out)
				}
			}
			for _, secret := range []string{"test-password", "root-token", "test.eyJvcmdJZCI6IjQyIn0.test", "private-cursor", "private-chat", "private-homework"} {
				if strings.Contains(out.String(), secret) {
					t.Errorf("output contains private value %q", secret)
				}
			}
			if (tt.name == "empty final page" || tt.wantErr) && strings.Contains(out.String(), "More chats") {
				t.Error("unexpected continuation notice")
			}
			if tt.wantErr && strings.Contains(out.String(), "Homework chats on this page:") {
				t.Error("failed page must not be printed as success")
			}
		})
	}
}
