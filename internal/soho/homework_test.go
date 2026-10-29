package soho

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

const homeworkChatFixture = `{
 "__typename":"Chat","id":"chat-test","uid":42,
 "studentHomework":{
  "id":"hw-test","uid":43,"academicHomeworkId":44,
  "progressStatus":"ReviewedCorrect","mergedStatus":"ReviewedCorrect","attempt":0,
  "gradingScale":"Hundred","grade":75.5,"scores":null,"gradeOrScoresMaxAchievable":100,
  "academicHomework":{"id":"academic-test","kind":"Simple","detailsSimple":{"isGradable":true,"isInline":false}}
 },
 "messagesNew":{
  "pageInfo":{"hasPreviousPage":false,"hasNextPage":false,"startCursor":"start","endCursor":"end"},
  "edges":[
   {"node":{"id":"message-b","uid":51,"chatId":42,"messageType":"Text","text":"<p>Текст</p>","metaInfo":null,"isWysiwyg":true,"timeCreated":"2026-01-01T10:00:00Z","userCreated":{"id":"author","uid":71},"quoteMessageId":null,"threadMessageId":null,"visibility":"All","status":"Sent","imageAnnotations":[]}},
   {"node":{"id":"message-a","uid":52,"chatId":42,"messageType":"FutureType","text":null,"metaInfo":"{\"typ\":\"FutureMeta\"}","isWysiwyg":null,"timeCreated":"2026-01-01T11:00:00Z","userCreated":null,"quoteMessageId":"opaque-quote","threadMessageId":51,"imageAnnotations":[],"futureField":{"keep":true}}}
  ]
 }
}`

func TestGetHomeworkBundle(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		name := "Hundred"
		body := homeworkChatFixture
		if disabled {
			name = "Disabled"
			body = strings.ReplaceAll(body, `"gradingScale":"Hundred","grade":75.5,"scores":null,"gradeOrScoresMaxAchievable":100`, `"gradingScale":"Disabled","grade":null,"scores":null,"gradeOrScoresMaxAchievable":null`)
			body = strings.ReplaceAll(body, `"isGradable":true`, `"isGradable":false`)
		}
		t.Run(name, func(t *testing.T) {
			calls := 0
			c := inboxTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != "/master/graphql" {
					t.Error("unexpected endpoint")
				}
				if r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("x-procraft-org") != "42" {
					t.Error("missing session headers")
				}
				var input graphqlRequest
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				if !reflect.DeepEqual(input.Variables, map[string]any{"id": "chat-test", "limit": float64(20)}) {
					t.Errorf("unexpected variables: %#v", input.Variables)
				}
				for _, fragment := range []string{"query GetHomeworkBundle(", "chat: node(id: $id)", "__typename", "... on Chat", "messagesNew(last: $limit)", "metaInfo", "quoteMessageId", "imageAnnotations"} {
					if !strings.Contains(input.Query, fragment) {
						t.Errorf("query missing %q", fragment)
					}
				}
				_, _ = io.WriteString(w, `{"data":{"chat":`+body+`}}`)
			})
			bundle, err := c.GetHomeworkBundle(context.Background(), "chat-test", 20)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 || !bundle.Complete || len(bundle.Warnings) != 0 {
				t.Fatalf("unexpected completeness: %+v", bundle)
			}
			hw := bundle.StudentHomework
			if bundle.ChatID != "chat-test" || bundle.ChatUID != 42 || hw == nil || hw.UID != 43 || hw.AcademicHomeworkID != 44 || hw.Attempt != 0 || hw.GradingScale != name {
				t.Fatal("homework context not preserved")
			}
			if hw.AcademicHomework == nil || hw.AcademicHomework.ID != "academic-test" || hw.AcademicHomework.DetailsSimple == nil {
				t.Fatal("academic homework not preserved")
			}
			gradable := hw.AcademicHomework.DetailsSimple.IsGradable
			if gradable == nil || *gradable == disabled {
				t.Fatal("isGradable not preserved")
			}
			wantGrade, wantMax := "75.5", "100"
			if disabled {
				wantGrade, wantMax = "null", "null"
			}
			if string(hw.Grade) != wantGrade || string(hw.Scores) != "null" || string(hw.GradeOrScoresMaxAchievable) != wantMax {
				t.Fatal("grades not preserved")
			}
			if len(bundle.Messages) != 2 || bundle.Messages[0].ID != "message-b" || bundle.Messages[1].ID != "message-a" {
				t.Fatal("message order not preserved")
			}
			first, second := bundle.Messages[0], bundle.Messages[1]
			if first.Text == nil || *first.Text != "<p>Текст</p>" || first.MetaInfo != nil || first.UserCreated == nil || first.UserCreated.UID != 71 || first.TimeCreated != "2026-01-01T10:00:00Z" || first.Status != "Sent" {
				t.Fatal("message fields not preserved")
			}
			if second.MessageType != "FutureType" || second.Text != nil || second.IsWysiwyg != nil || second.UserCreated != nil || second.MetaInfo == nil || *second.MetaInfo != `{"typ":"FutureMeta"}` {
				t.Fatal("unknown message or null fields not preserved")
			}
			if string(second.QuoteMessageID) != `"opaque-quote"` || string(second.ThreadMessageID) != "51" || !strings.Contains(string(second.Raw), `"futureField"`) {
				t.Fatal("raw fields not preserved")
			}
			if !json.Valid(bundle.RawChat) || bundle.PageInfo == nil || bundle.PageInfo.StartCursor == nil || *bundle.PageInfo.StartCursor != "start" {
				t.Fatal("raw context or pagination not preserved")
			}
		})
	}
}

func TestGetHomeworkBundleIncompleteAndInvalid(t *testing.T) {
	for _, tt := range []struct {
		name, old, replacement, wantErr string
		incomplete                      bool
	}{
		{name: "null chat", old: homeworkChatFixture, replacement: `null`, wantErr: "unavailable"},
		{name: "wrong type", old: `"__typename":"Chat"`, replacement: `"__typename":"User"`, wantErr: "matching Chat"},
		{name: "wrong chat", old: `"id":"chat-test"`, replacement: `"id":"other-chat"`, wantErr: "matching Chat"},
		{name: "foreign message", old: `"chatId":42`, replacement: `"chatId":99`, wantErr: "another chat"},
		{name: "missing message ID", old: `"id":"message-b"`, replacement: `"id":""`, wantErr: "no ID"},
		{name: "null node", old: `{"node":{"id":"message-b"`, replacement: `{"node":null},{"node":{"id":"message-b"`, wantErr: "no node"},
		{name: "older messages exist", old: `"hasPreviousPage":false`, replacement: `"hasPreviousPage":true`, incomplete: true},
		{name: "newer messages exist", old: `"hasNextPage":false`, replacement: `"hasNextPage":true`, incomplete: true},
		{name: "missing pagination flag", old: `"hasPreviousPage":false,`, replacement: ``, incomplete: true},
		{name: "null pagination", old: `{"hasPreviousPage":false,"hasNextPage":false,"startCursor":"start","endCursor":"end"}`, replacement: `null`, incomplete: true},
		{name: "null message connection", old: `"messagesNew":{`, replacement: `"messagesNew":null,"unused":{`, incomplete: true},
		{name: "null edges", old: `"edges":[`, replacement: `"edges":null,"unused":[`, incomplete: true},
		{name: "empty messages", old: `"edges":[`, replacement: `"edges":[],"unused":[`},
		{name: "missing chatId allowed", old: `"chatId":42,`, replacement: ``},
		{name: "null homework context", old: `"studentHomework":{`, replacement: `"studentHomework":null,"unused":{`, incomplete: true},
		{name: "null academic homework", old: `"academicHomework":{`, replacement: `"academicHomework":null,"unused":{`, incomplete: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := strings.ReplaceAll(homeworkChatFixture, tt.old, tt.replacement)
			c := inboxTestClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `{"data":{"chat":`+body+`}}`) })
			bundle, err := c.GetHomeworkBundle(context.Background(), "chat-test", 20)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}
				if !reflect.DeepEqual(bundle, HomeworkBundle{}) {
					t.Error("invalid response returned a bundle")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if bundle.Complete == tt.incomplete || (len(bundle.Warnings) > 0) != tt.incomplete {
				t.Fatalf("unexpected completeness: %v, %v", bundle.Complete, bundle.Warnings)
			}
			if tt.name == "empty messages" && (bundle.Messages == nil || len(bundle.Messages) != 0) {
				t.Error("expected empty messages")
			}
			if strings.Contains(tt.name, "messages exist") && len(bundle.Messages) != 2 {
				t.Error("truncated history lost messages")
			}
		})
	}
}

func TestGetHomeworkBundleErrors(t *testing.T) {
	for _, tt := range []struct {
		name, chatID, body string
		limit, status      int
		canceled           bool
	}{
		{name: "empty ID", limit: 20}, {name: "zero limit", chatID: "chat-test"}, {name: "negative limit", chatID: "chat-test", limit: -1},
		{name: "HTTP", chatID: "chat-test", limit: 20, status: 503},
		{name: "GraphQL partial error", chatID: "chat-test", limit: 20, body: `{"data":{"chat":` + homeworkChatFixture + `},"errors":[{"message":"synthetic failure"}]}`},
		{name: "missing chat", chatID: "chat-test", limit: 20, body: `{"data":{}}`},
		{name: "cancel", chatID: "chat-test", limit: 20, canceled: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			c := inboxTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if tt.status != 0 {
					w.WriteHeader(tt.status)
				}
				_, _ = io.WriteString(w, tt.body)
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.canceled {
				cancel()
			}
			bundle, err := c.GetHomeworkBundle(ctx, tt.chatID, tt.limit)
			if err == nil || !reflect.DeepEqual(bundle, HomeworkBundle{}) {
				t.Fatal("expected error and zero bundle")
			}
			if tt.canceled && !errors.Is(err, context.Canceled) {
				t.Errorf("error does not wrap cancellation: %v", err)
			}
			if (tt.chatID == "" || tt.limit <= 0 || tt.canceled) && calls != 0 {
				t.Error("invalid input reached handler")
			}
		})
	}
}
