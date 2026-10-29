package soho

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeHomework(t *testing.T) {
	htmlText := `<p>Условие &amp; ответ</p><p><a href="https://example.test/task">Первый</a><br><a href="/relative">Второй</a><img src="https://files.test/a.png"><img src="relative.png"></p><script>hidden()</script>`
	plain := "строка 1\nстрока 2 < 3 & текст"
	no := false
	meta := `{"typ":"MultiFile","payload":{"files":[{"url":"https://files.test/a.pdf","name":"a.pdf","size":123,"fileId":7,"mimeType":"application/pdf","customInfo":"{\"extra\":true}"},{"url":"https://files.test/b.png","name":"b.png","mimeType":"image/png","customInfo":{"future":true}}],"customInfo":{"extra":42}}}`
	bundle := HomeworkBundle{ChatID: "chat", ChatUID: 42, Messages: []HomeworkMessage{
		{ID: "html", Text: &htmlText}, {ID: "plain", Text: &plain, IsWysiwyg: &no}, {ID: "files", MetaInfo: &meta},
	}}
	normalized := NormalizeHomework(bundle)
	if normalized.ChatURL != "https://master.soholms.com/inbox/42" || ChatURL(0) != "" {
		t.Fatal("wrong chat link")
	}
	first := normalized.Messages[0]
	if first.PlainText != "Условие & ответ\nПервый\nВторой" {
		t.Errorf("text = %q", first.PlainText)
	}
	if len(first.Links) != 2 || first.Links[0].URL != "https://example.test/task" || first.Links[1].URL != "/relative" || len(first.Attachments) != 2 {
		t.Fatal("links or images lost")
	}
	if strings.Contains(first.PlainText, "hidden") || first.RawText == nil || *first.RawText != htmlText {
		t.Fatal("raw or extracted text corrupted")
	}
	if normalized.Messages[1].PlainText != plain {
		t.Fatal("plain text changed")
	}
	files := normalized.Messages[2]
	if len(files.Attachments) != 2 || *files.Attachments[0].FileID != 7 || *files.Attachments[0].Size != 123 || string(files.Attachments[0].CustomInfo) != `"{\"extra\":true}"` || string(files.CustomInfo) != `{"extra":42}` {
		t.Fatal("MultiFile fields lost")
	}
	if string(files.Attachments[1].CustomInfo) != `{"future":true}` {
		t.Fatal("unknown file customInfo lost")
	}
	if files.Role != "unknown" || len(normalized.Warnings) == 0 {
		t.Fatal("missing role uncertainty")
	}
	for _, file := range files.Attachments {
		if file.ChatID != "chat" || file.MessageID != "files" || file.Purpose != "file" || file.State != "discovered" {
			t.Fatal("provenance lost")
		}
	}
	if !reflect.DeepEqual(normalized.Bundle, bundle) {
		t.Fatal("source messages changed")
	}
}

func TestNormalizeHomeworkMetadata(t *testing.T) {
	for _, tt := range []struct {
		name, meta, wantType string
		warning              bool
		count                int
	}{
		{"broken JSON", `{broken`, "", true, 0},
		{"null JSON", `null`, "", true, 0},
		{"unknown type", `{"typ":"Future","payload":{"keep":true}}`, "Future", true, 0},
		{"status event", `{"typ":"HomeworkStatus","payload":{"attempt":0,"grade":null}}`, "HomeworkStatus", false, 0},
		{"missing files", `{"typ":"MultiFile","payload":{}}`, "MultiFile", true, 0},
		{"null files", `{"typ":"MultiFile","payload":{"files":null}}`, "MultiFile", true, 0},
		{"empty files", `{"typ":"MultiFile","payload":{"files":[]}}`, "MultiFile", false, 0},
		{"bad files", `{"typ":"MultiFile","payload":{"files":"bad"}}`, "MultiFile", true, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			original := HomeworkMessage{ID: "msg", MetaInfo: &tt.meta}
			result := NormalizeHomework(HomeworkBundle{ChatID: "chat", Messages: []HomeworkMessage{original}})
			message := result.Messages[0]
			if message.MetaType != tt.wantType || len(message.Attachments) != tt.count {
				t.Fatal("unexpected metadata result")
			}
			if (len(message.Warnings) > 1) != tt.warning {
				t.Errorf("warnings: %v", message.Warnings)
			}
			if *result.Bundle.Messages[0].MetaInfo != tt.meta {
				t.Fatal("raw metadata lost")
			}
			if tt.wantType == "Future" && !json.Valid(message.MetaPayload) {
				t.Fatal("unknown payload lost")
			}
		})
	}
}
