package soho

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed queries/get_homework.graphql
var getHomeworkBundleQuery string

type HomeworkChatPeer struct {
	TypeName string           `json:"__typename"`
	Student  *HomeworkStudent `json:"student"`
}

type HomeworkStudent struct {
	TypeName string  `json:"__typename"`
	Name     *string `json:"name"`
}

type StudentHomework struct {
	DeadlineAt         *string `json:"deadlineAt"`
	ID                 string  `json:"id"`
	UID                int64   `json:"uid"`
	AcademicHomeworkID int64   `json:"academicHomeworkId"`
	ProgressStatus     string  `json:"progressStatus"`
	MergedStatus       string  `json:"mergedStatus"`
	Attempt            int     `json:"attempt"`
	GradingScale       string  `json:"gradingScale"`
	// Сохраняем null и формат оценки без предположений о шкале
	Grade                      json.RawMessage   `json:"grade"`
	Scores                     json.RawMessage   `json:"scores"`
	GradeOrScoresMaxAchievable json.RawMessage   `json:"gradeOrScoresMaxAchievable"`
	AcademicHomework           *AcademicHomework `json:"academicHomework"`
}

type AcademicHomework struct {
	ID            string                 `json:"id"`
	Kind          string                 `json:"kind"`
	DetailsSimple *SimpleHomeworkDetails `json:"detailsSimple"`
}

type SimpleHomeworkDetails struct {
	IsGradable *bool `json:"isGradable"`
	IsInline   *bool `json:"isInline"`
}

type MessageAuthor struct {
	ID  string `json:"id"`
	UID int64  `json:"uid"`
}

type HomeworkMessage struct {
	ID          string         `json:"id"`
	UID         int64          `json:"uid"`
	ChatID      *int64         `json:"chatId"`
	MessageType string         `json:"messageType"`
	Text        *string        `json:"text"`
	MetaInfo    *string        `json:"metaInfo"`
	IsWysiwyg   *bool          `json:"isWysiwyg"`
	TimeCreated string         `json:"timeCreated"`
	UserCreated *MessageAuthor `json:"userCreated"`
	// Ненулевой формат связей пока не подтверждён
	QuoteMessageID   json.RawMessage `json:"quoteMessageId"`
	ThreadMessageID  json.RawMessage `json:"threadMessageId"`
	Visibility       string          `json:"visibility"`
	Status           string          `json:"status"`
	ImageAnnotations json.RawMessage `json:"imageAnnotations"`
	Raw              json.RawMessage `json:"-"`
}

type MessagePageInfo struct {
	HasPreviousPage *bool   `json:"hasPreviousPage"`
	HasNextPage     *bool   `json:"hasNextPage"`
	StartCursor     *string `json:"startCursor"`
	EndCursor       *string `json:"endCursor"`
}

type HomeworkBundle struct {
	Title           *string
	LastUpdated     *string
	ChatPeer        *HomeworkChatPeer
	ChatID          string
	ChatUID         int64
	StudentHomework *StudentHomework
	Messages        []HomeworkMessage
	PageInfo        *MessagePageInfo
	Complete        bool
	Warnings        []string
	// Исходный контекст сохраняет поля, которых пока нет в модели
	RawChat json.RawMessage
}

func (c *Client) GetHomeworkBundle(ctx context.Context, chatID string, limit int) (HomeworkBundle, error) {
	if chatID == "" || limit <= 0 {
		return HomeworkBundle{}, fmt.Errorf("chat ID must not be empty and message limit must be positive")
	}
	var data struct {
		Chat json.RawMessage `json:"chat"`
	}
	if err := c.doGraphQL(ctx, getHomeworkBundleQuery, map[string]any{"id": chatID, "limit": limit}, &data); err != nil {
		return HomeworkBundle{}, fmt.Errorf("get homework bundle: %w", err)
	}
	if len(data.Chat) == 0 || string(data.Chat) == "null" {
		return HomeworkBundle{}, fmt.Errorf("homework chat is unavailable")
	}
	var chat struct {
		Title           *string           `json:"title"`
		LastUpdated     *string           `json:"lastUpdated"`
		ChatPeer        *HomeworkChatPeer `json:"chatPeer"`
		TypeName        string            `json:"__typename"`
		ID              string            `json:"id"`
		UID             int64             `json:"uid"`
		StudentHomework *StudentHomework  `json:"studentHomework"`
		Messages        *struct {
			PageInfo *MessagePageInfo `json:"pageInfo"`
			Edges    *[]struct {
				Node json.RawMessage `json:"node"`
			} `json:"edges"`
		} `json:"messagesNew"`
	}
	if err := json.Unmarshal(data.Chat, &chat); err != nil {
		return HomeworkBundle{}, fmt.Errorf("decode homework chat: %w", err)
	}
	if chat.TypeName != "Chat" || chat.ID != chatID || chat.UID <= 0 {
		return HomeworkBundle{}, fmt.Errorf("response contains no matching Chat")
	}
	bundle := HomeworkBundle{
		ChatID: chat.ID, ChatUID: chat.UID, StudentHomework: chat.StudentHomework,
		Title: chat.Title, LastUpdated: chat.LastUpdated, ChatPeer: chat.ChatPeer,
		Messages: make([]HomeworkMessage, 0), Warnings: make([]string, 0), RawChat: data.Chat,
	}
	if chat.StudentHomework == nil {
		bundle.Warnings = append(bundle.Warnings, "Контекст домашней работы недоступен")
	} else if chat.StudentHomework.AcademicHomework == nil {
		bundle.Warnings = append(bundle.Warnings, "Контекст исходного задания недоступен")
	}
	if chat.Messages == nil {
		bundle.Warnings = append(bundle.Warnings, "Список сообщений недоступен")
		return bundle, nil
	}
	bundle.PageInfo = chat.Messages.PageInfo
	if bundle.PageInfo == nil || bundle.PageInfo.HasPreviousPage == nil || bundle.PageInfo.HasNextPage == nil {
		bundle.Warnings = append(bundle.Warnings, "Полнота истории сообщений неизвестна")
	} else if *bundle.PageInfo.HasPreviousPage || *bundle.PageInfo.HasNextPage {
		bundle.Warnings = append(bundle.Warnings, "История сообщений усечена, запрос продолжения пока не установлен")
	}
	if chat.Messages.Edges == nil {
		bundle.Warnings = append(bundle.Warnings, "Содержимое списка сообщений недоступно")
		return bundle, nil
	}
	for i, edge := range *chat.Messages.Edges {
		if len(edge.Node) == 0 || string(edge.Node) == "null" {
			return HomeworkBundle{}, fmt.Errorf("message %d contains no node", i+1)
		}
		var message HomeworkMessage
		if err := json.Unmarshal(edge.Node, &message); err != nil {
			return HomeworkBundle{}, fmt.Errorf("decode message %d: %w", i+1, err)
		}
		if message.ID == "" || (message.ChatID != nil && *message.ChatID != chat.UID) {
			return HomeworkBundle{}, fmt.Errorf("message %d has no ID or belongs to another chat", i+1)
		}
		message.Raw = edge.Node
		bundle.Messages = append(bundle.Messages, message)
	}
	bundle.Complete = len(bundle.Warnings) == 0
	return bundle, nil
}
