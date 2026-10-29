package soho

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

type MaterialReference struct {
	ChatID    string
	MessageID string
	Purpose   string
	URL       string
	State     string
}

type Attachment struct {
	MaterialReference
	Name         string          `json:"name"`
	Size         *int64          `json:"size"`
	FileID       *int64          `json:"fileId"`
	MIMEType     string          `json:"mimeType"`
	LastModified *int64          `json:"lastModified"`
	CustomInfo   json.RawMessage `json:"customInfo"`
}

type MessageMaterials struct {
	MessageID   string
	Role        string
	PlainText   string
	RawText     *string
	MetaType    string
	MetaPayload json.RawMessage
	CustomInfo  json.RawMessage
	Links       []MaterialReference
	Attachments []Attachment
	Warnings    []string
}

type HomeworkMaterials struct {
	Bundle   HomeworkBundle
	ChatURL  string
	Messages []MessageMaterials
	Warnings []string
}

func ChatURL(uid int64) string {
	if uid <= 0 {
		return ""
	}
	return fmt.Sprintf("https://master.soholms.com/inbox/%d", uid)
}

func NormalizeHomework(bundle HomeworkBundle) HomeworkMaterials {
	result := HomeworkMaterials{Bundle: bundle, ChatURL: ChatURL(bundle.ChatUID), Messages: make([]MessageMaterials, 0, len(bundle.Messages)), Warnings: append([]string{}, bundle.Warnings...)}
	for _, message := range bundle.Messages {
		materials := normalizeMessage(bundle.ChatID, message)
		result.Messages = append(result.Messages, materials)
		result.Warnings = append(result.Warnings, materials.Warnings...)
	}
	return result
}

func normalizeMessage(chatID string, message HomeworkMessage) MessageMaterials {
	result := MessageMaterials{MessageID: message.ID, Role: "unknown", RawText: message.Text, Links: []MaterialReference{}, Attachments: []Attachment{}, Warnings: []string{"Роль автора не установлена, материалы требуют ручного выбора"}}
	reference := func(rawURL, purpose string) MaterialReference {
		ref := MaterialReference{ChatID: chatID, MessageID: message.ID, Purpose: purpose, URL: rawURL, State: "discovered"}
		u, err := url.Parse(rawURL)
		if err != nil || !u.IsAbs() {
			result.Warnings = append(result.Warnings, "Относительный или некорректный URL сохранён без разрешения")
		}
		return ref
	}
	if message.Text != nil {
		if message.IsWysiwyg != nil && !*message.IsWysiwyg {
			result.PlainText = *message.Text
		} else {
			text, links, images, err := extractHTML(*message.Text)
			if err != nil {
				result.Warnings = append(result.Warnings, "Не удалось разобрать HTML")
			} else {
				result.PlainText = text
				for _, link := range links {
					result.Links = append(result.Links, reference(link, "external-link"))
				}
				for _, image := range images {
					result.Attachments = append(result.Attachments, Attachment{MaterialReference: reference(image, "inline-image")})
				}
			}
		}
	}
	if len(result.Links) > 0 {
		result.Warnings = append(result.Warnings, "Содержимое внешних ссылок не получено")
	}
	if len(message.ImageAnnotations) > 0 && string(message.ImageAnnotations) != "[]" && string(message.ImageAnnotations) != "null" {
		result.Warnings = append(result.Warnings, "Аннотации изображений сохранены в исходном сообщении без интерпретации")
	}
	if message.MetaInfo == nil {
		return result
	}
	var meta struct {
		Type    string          `json:"typ"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal([]byte(*message.MetaInfo), &meta); err != nil || meta.Type == "" {
		result.Warnings = append(result.Warnings, "Повреждённый metaInfo сохранён в исходном сообщении")
		return result
	}
	result.MetaType, result.MetaPayload = meta.Type, meta.Payload
	switch meta.Type {
	case "MultiFile":
		var payload struct {
			Files *[]struct {
				URL          string          `json:"url"`
				Name         string          `json:"name"`
				Size         *int64          `json:"size"`
				FileID       *int64          `json:"fileId"`
				MIMEType     string          `json:"mimeType"`
				LastModified *int64          `json:"lastModified"`
				CustomInfo   json.RawMessage `json:"customInfo"`
			} `json:"files"`
			CustomInfo json.RawMessage `json:"customInfo"`
		}
		if err := json.Unmarshal(meta.Payload, &payload); err != nil || payload.Files == nil {
			result.Warnings = append(result.Warnings, "Повреждённый MultiFile, список вложений неизвестен")
			return result
		}
		result.CustomInfo = payload.CustomInfo
		for _, file := range *payload.Files {
			result.Attachments = append(result.Attachments, Attachment{MaterialReference: reference(file.URL, "file"), Name: file.Name, Size: file.Size, FileID: file.FileID, MIMEType: file.MIMEType, LastModified: file.LastModified, CustomInfo: file.CustomInfo})
		}
	case "HomeworkStatus":
		if len(meta.Payload) == 0 || string(meta.Payload) == "null" {
			result.Warnings = append(result.Warnings, "Событие статуса не содержит payload")
		}
	default:
		result.Warnings = append(result.Warnings, "Неизвестный тип metaInfo сохранён без интерпретации")
	}
	return result
}

func extractHTML(source string) (string, []string, []string, error) {
	document, err := html.Parse(strings.NewReader(source))
	if err != nil {
		return "", nil, nil, err
	}
	var text strings.Builder
	var links, images []string
	newline := func() {
		if text.Len() > 0 && !strings.HasSuffix(text.String(), "\n") {
			text.WriteByte('\n')
		}
	}
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			switch node.Data {
			case "script", "style", "template", "head":
				return
			}
			switch node.Data {
			case "p", "div", "li", "br", "tr", "h1", "h2", "h3", "blockquote":
				newline()
			}
			for _, attribute := range node.Attr {
				if node.Data == "a" && attribute.Key == "href" {
					links = append(links, attribute.Val)
				}
				if node.Data == "img" && attribute.Key == "src" {
					images = append(images, attribute.Val)
				}
			}
		}
		if node.Type == html.TextNode {
			text.WriteString(node.Data)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
		if node.Type == html.ElementNode {
			switch node.Data {
			case "p", "div", "li", "tr", "h1", "h2", "h3", "blockquote":
				newline()
			}
		}
	}
	walk(document)
	return strings.TrimSpace(text.String()), links, images, nil
}
