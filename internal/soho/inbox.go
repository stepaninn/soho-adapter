package soho

import (
	"context"
	_ "embed"
	"fmt"
)

//go:embed queries/list_homeworks.graphql
var listHomeworkChatsQuery string

type ListHomeworkChatsOptions struct {
	OnlyAttention bool
	OnlyMy        bool
	SortDesc      bool
	Limit         int
	After         *string
}

type HomeworkSummary struct {
	ID           string `json:"id"`
	UID          int64  `json:"uid"`
	MergedStatus string `json:"mergedStatus"`
}

type ChatSummary struct {
	Title           *string           `json:"title"`
	LastUpdated     *string           `json:"lastUpdated"`
	ChatPeer        *HomeworkChatPeer `json:"chatPeer"`
	ID              string            `json:"id"`
	UID             int64             `json:"uid"`
	ChatType        string            `json:"chatType"`
	StudentHomework *HomeworkSummary  `json:"studentHomework"`
}

type PageInfo struct {
	HasNextPage bool    `json:"hasNextPage"`
	EndCursor   *string `json:"endCursor"`
}

type ChatPage struct {
	Chats    []ChatSummary
	PageInfo PageInfo
}

type chatEdge struct {
	Node *ChatSummary `json:"node"`
}

type chatConnection struct {
	PageInfo *PageInfo  `json:"pageInfo"`
	Edges    []chatEdge `json:"edges"`
}

func (c *Client) ListHomeworkChatsPage(
	ctx context.Context,
	options ListHomeworkChatsOptions,
) (ChatPage, error) {
	if options.Limit <= 0 {
		return ChatPage{}, fmt.Errorf(
			"page limit must be positive",
		)
	}

	variables := map[string]any{
		"search":              nil,
		"group":               "Education",
		"dateFrom":            nil,
		"dateTo":              nil,
		"homeworkStatuses":    []string{},
		"academicDisciplines": []int64{},
		"onlyAttention":       options.OnlyAttention,
		"onlyMy":              options.OnlyMy,
		"minPriority":         nil,
		"deadlineOverdue":     false,
		"sortDesc":            options.SortDesc,
		"transports":          []any{},
		"academicHomeworkIds": []int64{},
		"limit":               options.Limit,
		"chatMessageLabels":   nil,
		"orgUnitUid":          nil,
		"clientTagIds":        nil,
		"after":               options.After,
	}

	var data struct {
		Viewer *struct {
			Chats *chatConnection `json:"chats"`
		} `json:"viewer"`
	}

	err := c.doGraphQL(
		ctx,
		listHomeworkChatsQuery,
		variables,
		&data,
	)
	if err != nil {
		return ChatPage{}, fmt.Errorf(
			"list homework chats: %w",
			err,
		)
	}

	if data.Viewer == nil ||
		data.Viewer.Chats == nil ||
		data.Viewer.Chats.PageInfo == nil {
		return ChatPage{}, fmt.Errorf(
			"chat connection is unavailable",
		)
	}

	connection := data.Viewer.Chats

	page := ChatPage{
		Chats: make(
			[]ChatSummary,
			0,
			len(connection.Edges),
		),
		PageInfo: *connection.PageInfo,
	}

	if page.PageInfo.HasNextPage &&
		(page.PageInfo.EndCursor == nil ||
			*page.PageInfo.EndCursor == "") {
		return ChatPage{}, fmt.Errorf(
			"next page exists but its cursor is missing",
		)
	}

	for _, edge := range connection.Edges {
		if edge.Node == nil || edge.Node.ID == "" {
			return ChatPage{}, fmt.Errorf(
				"chat edge contains no valid node",
			)
		}

		page.Chats = append(page.Chats, *edge.Node)
	}

	return page, nil
}

func (c *Client) ListHomeworkChats(
	ctx context.Context,
	options ListHomeworkChatsOptions,
	maxPages int,
) ([]ChatSummary, error) {
	if maxPages <= 0 {
		return nil, fmt.Errorf("maximum page count must be positive")
	}

	chats := make([]ChatSummary, 0)
	positions := make(map[string]int)
	seenCursors := make(map[string]bool)
	if options.After != nil {
		// Копируем курсор вызывающего кода
		after := *options.After
		options.After = &after
		seenCursors[after] = true
	}

	for pageNumber := 1; ; pageNumber++ {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("list homework chats page %d: %w", pageNumber, err)
		}
		page, err := c.ListHomeworkChatsPage(ctx, options)
		if err != nil {
			return nil, fmt.Errorf("list homework chats page %d: %w", pageNumber, err)
		}
		for _, chat := range page.Chats {
			if position, exists := positions[chat.ID]; exists {
				chats[position] = chat
			} else {
				positions[chat.ID] = len(chats)
				chats = append(chats, chat)
			}
		}
		if !page.PageInfo.HasNextPage {
			return chats, nil
		}

		// Наличие курсора уже проверено при чтении страницы
		next := *page.PageInfo.EndCursor
		if seenCursors[next] {
			return nil, fmt.Errorf("homework chat pagination cursor repeated after page %d", pageNumber)
		}
		seenCursors[next] = true
		if pageNumber == maxPages {
			return nil, fmt.Errorf("homework chat pagination exceeded maximum of %d pages", maxPages)
		}
		options.After = &next
	}
}
