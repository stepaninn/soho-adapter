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
	ID              string           `json:"id"`
	UID             int64            `json:"uid"`
	ChatType        string           `json:"chatType"`
	StudentHomework *HomeworkSummary `json:"studentHomework"`
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
