package soho

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// Вызываем HTTP-обработчик в памяти без TCP-порта
type inboxTransport func(*http.Request) (*http.Response, error)

func (f inboxTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func inboxTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	c, err := NewClient("http://soho.test")
	if err != nil {
		t.Fatal(err)
	}
	c.token, c.orgID = "test-token", "42"
	c.httpClient.Transport = inboxTransport(func(r *http.Request) (*http.Response, error) {
		if err := r.Context().Err(); err != nil {
			return nil, err
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Result(), nil
	})
	return c
}

func TestListHomeworkChatsPageRequest(t *testing.T) {
	cursor := "opaque-cursor"
	for _, tt := range []struct {
		name    string
		options ListHomeworkChatsOptions
		after   any
	}{
		{"first page", ListHomeworkChatsOptions{OnlyAttention: true, OnlyMy: true, Limit: 30}, nil},
		{"continuation with opposite flags", ListHomeworkChatsOptions{SortDesc: true, Limit: 7, After: &cursor}, cursor},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			c := inboxTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != "/master/graphql" || r.URL.RawQuery != "" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				for key, want := range map[string]string{"Authorization": "Bearer test-token", "x-procraft-org": "42", "Content-Type": "application/json", "Accept": "application/json"} {
					if got := r.Header.Get(key); got != want {
						t.Errorf("%s = %q, want %q", key, got, want)
					}
				}
				var request struct {
					Query     string         `json:"query"`
					Variables map[string]any `json:"variables"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				// Проверяем операцию и передачу переменных в запрос
				for _, fragment := range []string{"query ListHomeworkChats(", "group: $group", "onlyAttention: $onlyAttention", "onlyMy: $onlyMy", "sortDesc: $sortDesc", "first: $limit", "after: $after", "pageInfo { hasNextPage endCursor }", "studentHomework { id uid mergedStatus }"} {
					if !strings.Contains(request.Query, fragment) {
						t.Errorf("query missing %q", fragment)
					}
				}
				want := map[string]any{
					"search": nil, "group": "Education", "dateFrom": nil, "dateTo": nil,
					"homeworkStatuses": []any{}, "academicDisciplines": []any{},
					"onlyAttention": tt.options.OnlyAttention, "onlyMy": tt.options.OnlyMy,
					"minPriority": nil, "deadlineOverdue": false, "sortDesc": tt.options.SortDesc,
					"transports": []any{}, "academicHomeworkIds": []any{}, "limit": float64(tt.options.Limit),
					"chatMessageLabels": nil, "orgUnitUid": nil, "clientTagIds": nil, "after": tt.after,
				}
				if !reflect.DeepEqual(request.Variables, want) {
					t.Errorf("variables = %#v, want %#v", request.Variables, want)
				}
				_, _ = io.WriteString(w, `{"data":{"viewer":{"chats":{"pageInfo":{"hasNextPage":false,"endCursor":null},"edges":[]}}}}`)
			})
			page, err := c.ListHomeworkChatsPage(context.Background(), tt.options)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Errorf("requests = %d, want 1", calls)
			}
			if page.Chats == nil || len(page.Chats) != 0 {
				t.Errorf("expected non-nil empty chats, got %#v", page.Chats)
			}
		})
	}
}

func TestListHomeworkChatsPageResponse(t *testing.T) {
	for _, tt := range []struct {
		name, body, wantErr string
		status              int
	}{
		{name: "valid numeric IDs and nullable homework", body: `{"data":{"viewer":{"chats":{"pageInfo":{"hasNextPage":true,"endCursor":"next"},"edges":[{"node":{"id":"chat-b","uid":9007199254740993,"chatType":"Education","studentHomework":{"id":"hw-b","uid":123,"mergedStatus":"ReviewedIncorrect"}}},{"node":{"id":"chat-a","uid":124,"chatType":"Education","studentHomework":null}}]}}}}`},
		{name: "null viewer", body: `{"data":{"viewer":null}}`, wantErr: "connection is unavailable"},
		{name: "null chats", body: `{"data":{"viewer":{"chats":null}}}`, wantErr: "connection is unavailable"},
		{name: "null pageInfo", body: `{"data":{"viewer":{"chats":{"pageInfo":null,"edges":[]}}}}`, wantErr: "connection is unavailable"},
		{name: "null node", body: `{"data":{"viewer":{"chats":{"pageInfo":{"hasNextPage":false},"edges":[{"node":null}]}}}}`, wantErr: "no valid node"},
		{name: "missing node", body: `{"data":{"viewer":{"chats":{"pageInfo":{"hasNextPage":false},"edges":[{}]}}}}`, wantErr: "no valid node"},
		{name: "empty node ID", body: `{"data":{"viewer":{"chats":{"pageInfo":{"hasNextPage":false},"edges":[{"node":{"uid":1}}]}}}}`, wantErr: "no valid node"},
		{name: "null cursor", body: `{"data":{"viewer":{"chats":{"pageInfo":{"hasNextPage":true,"endCursor":null},"edges":[]}}}}`, wantErr: "cursor is missing"},
		{name: "missing cursor", body: `{"data":{"viewer":{"chats":{"pageInfo":{"hasNextPage":true},"edges":[]}}}}`, wantErr: "cursor is missing"},
		{name: "empty cursor", body: `{"data":{"viewer":{"chats":{"pageInfo":{"hasNextPage":true,"endCursor":""},"edges":[]}}}}`, wantErr: "cursor is missing"},
		{name: "HTTP error", status: 503, body: `{}`, wantErr: "HTTP 503"},
		{name: "GraphQL error with partial data", body: `{"data":{"viewer":{"chats":{"pageInfo":{"hasNextPage":false},"edges":[]}}},"errors":[{"message":"synthetic failure"}]}`, wantErr: "GraphQL error"},
		{name: "invalid JSON", body: `{`, wantErr: "decode JSON response"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := inboxTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if tt.status != 0 {
					w.WriteHeader(tt.status)
				}
				_, _ = io.WriteString(w, tt.body)
			})
			page, err := c.ListHomeworkChatsPage(context.Background(), ListHomeworkChatsOptions{Limit: 30})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}
				if !reflect.DeepEqual(page, ChatPage{}) {
					t.Errorf("error must return zero page, got %#v", page)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := []ChatSummary{
				{ID: "chat-b", UID: 9007199254740993, ChatType: "Education", StudentHomework: &HomeworkSummary{ID: "hw-b", UID: 123, MergedStatus: "ReviewedIncorrect"}},
				{ID: "chat-a", UID: 124, ChatType: "Education"},
			}
			if !reflect.DeepEqual(page.Chats, want) {
				t.Errorf("chats = %#v, want %#v", page.Chats, want)
			}
			if !page.PageInfo.HasNextPage || page.PageInfo.EndCursor == nil || *page.PageInfo.EndCursor != "next" {
				t.Errorf("unexpected pageInfo: %#v", page.PageInfo)
			}
		})
	}
}

func TestListHomeworkChatsPageInvalidLimit(t *testing.T) {
	for _, limit := range []int{0, -1} {
		c := inboxTestClient(t, func(http.ResponseWriter, *http.Request) { t.Error("invalid limit must not send a request") })
		if _, err := c.ListHomeworkChatsPage(context.Background(), ListHomeworkChatsOptions{Limit: limit}); err == nil {
			t.Errorf("limit %d: expected error", limit)
		}
	}
}

func TestListHomeworkChatsPageCanceledContext(t *testing.T) {
	c := inboxTestClient(t, func(http.ResponseWriter, *http.Request) { t.Error("canceled request reached handler") })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.ListHomeworkChatsPage(ctx, ListHomeworkChatsOptions{Limit: 30})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}
