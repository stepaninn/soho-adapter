package soho

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

type inboxTestPage struct {
	chats   []ChatSummary
	next    string
	hasNext bool
	status  int
	cancel  bool
}

func TestListHomeworkChats(t *testing.T) {
	original := ChatSummary{ID: "b", UID: 1, StudentHomework: &HomeworkSummary{ID: "hw", MergedStatus: "OnReview"}}
	updated := original
	updated.StudentHomework = &HomeworkSummary{ID: "hw", MergedStatus: "ReviewedCorrect"}
	other := ChatSummary{ID: "a", UID: 1} // Same numeric UID must not cause deduplication.
	for _, tt := range []struct {
		name      string
		pages     []inboxTestPage
		after     string
		maxPages  int
		want      []ChatSummary
		wantErr   string
		wantCalls int
		canceled  bool
	}{
		{name: "multiple pages update duplicates in original order", pages: []inboxTestPage{
			{chats: []ChatSummary{original, other}, next: "A", hasNext: true},
			{chats: []ChatSummary{updated, {ID: "c"}}, next: "B", hasNext: true},
			{chats: []ChatSummary{{ID: "d"}}},
		}, maxPages: 3, want: []ChatSummary{updated, other, {ID: "c"}, {ID: "d"}}, wantCalls: 3},
		{name: "empty intermediate page still continues", pages: []inboxTestPage{
			{next: "A", hasNext: true}, {chats: []ChatSummary{other}},
		}, maxPages: 2, want: []ChatSummary{other}, wantCalls: 2},
		{name: "empty final page", pages: []inboxTestPage{{}}, maxPages: 1, want: []ChatSummary{}, wantCalls: 1},
		{name: "starts at supplied cursor", after: "start", pages: []inboxTestPage{
			{next: "A", hasNext: true}, {chats: []ChatSummary{other}},
		}, maxPages: 2, want: []ChatSummary{other}, wantCalls: 2},
		{name: "immediate repeat", pages: []inboxTestPage{
			{next: "A", hasNext: true}, {next: "A", hasNext: true},
		}, maxPages: 5, wantErr: "cursor repeated", wantCalls: 2},
		{name: "cursor cycle", pages: []inboxTestPage{
			{next: "A", hasNext: true}, {next: "B", hasNext: true}, {next: "A", hasNext: true},
		}, maxPages: 5, wantErr: "cursor repeated", wantCalls: 3},
		{name: "repeat initial cursor", after: "start", pages: []inboxTestPage{
			{next: "start", hasNext: true},
		}, maxPages: 5, wantErr: "cursor repeated", wantCalls: 1},
		{name: "page limit discards partial list", pages: []inboxTestPage{
			{chats: []ChatSummary{other}, next: "A", hasNext: true},
		}, maxPages: 1, wantErr: "exceeded maximum", wantCalls: 1},
		{name: "second page failure discards partial list", pages: []inboxTestPage{
			{chats: []ChatSummary{other}, next: "A", hasNext: true}, {status: 503},
		}, maxPages: 5, wantErr: "page 2", wantCalls: 2},
		{name: "missing continuation cursor", pages: []inboxTestPage{{hasNext: true}}, maxPages: 5, wantErr: "cursor is missing", wantCalls: 1},
		{name: "cancel between pages", pages: []inboxTestPage{
			{chats: []ChatSummary{other}, next: "A", hasNext: true, cancel: true},
		}, maxPages: 5, wantErr: "context canceled", wantCalls: 1, canceled: true},
		{name: "cancel before first page", maxPages: 5, wantErr: "context canceled", canceled: true},
		{name: "zero page limit", wantErr: "maximum page count must be positive"},
		{name: "negative page limit", maxPages: -1, wantErr: "maximum page count must be positive"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.canceled && len(tt.pages) == 0 {
				cancel()
			}
			calls := 0
			var firstVariables map[string]any
			c := inboxTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				index := calls
				calls++
				if index >= len(tt.pages) {
					t.Error("unexpected extra page request")
					w.WriteHeader(500)
					return
				}
				var input struct {
					Variables map[string]any `json:"variables"`
				}
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				var wantAfter any
				if index == 0 {
					if tt.after != "" {
						wantAfter = tt.after
					}
				} else {
					wantAfter = tt.pages[index-1].next
				}
				if got := input.Variables["after"]; got != wantAfter {
					t.Errorf("after = %v, want %v", got, wantAfter)
				}
				delete(input.Variables, "after")
				if index == 0 {
					firstVariables = input.Variables
				} else if !reflect.DeepEqual(input.Variables, firstVariables) {
					t.Error("filters or page size changed between requests")
				}
				page := tt.pages[index]
				if page.status != 0 {
					w.WriteHeader(page.status)
					return
				}
				edges := make([]map[string]any, 0, len(page.chats))
				for _, chat := range page.chats {
					edges = append(edges, map[string]any{"node": chat})
				}
				var cursor any
				if page.next != "" {
					cursor = page.next
				}
				err := json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"viewer": map[string]any{"chats": map[string]any{
					"pageInfo": map[string]any{"hasNextPage": page.hasNext, "endCursor": cursor}, "edges": edges,
				}}}})
				if err != nil {
					t.Error(err)
				}
				if page.cancel {
					cancel()
				}
			})
			options := ListHomeworkChatsOptions{OnlyAttention: true, OnlyMy: true, SortDesc: false, Limit: 30}
			if tt.after != "" {
				after := tt.after
				options.After = &after
			}
			originalOptions := options
			chats, err := c.ListHomeworkChats(ctx, options, tt.maxPages)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}
				if chats != nil {
					t.Errorf("failure returned partial chats: %#v", chats)
				}
				if tt.canceled && !errors.Is(err, context.Canceled) {
					t.Errorf("error does not wrap context.Canceled: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(chats, tt.want) {
					t.Errorf("chats = %#v, want %#v", chats, tt.want)
				}
			}
			if calls != tt.wantCalls {
				t.Errorf("requests = %d, want %d", calls, tt.wantCalls)
			}
			if !reflect.DeepEqual(options, originalOptions) || (options.After != nil && *options.After != tt.after) {
				t.Error("caller options were changed")
			}
		})
	}
}
