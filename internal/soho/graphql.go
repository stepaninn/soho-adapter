package soho

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type graphqlRequest struct {
	Query     string `json:"query"`
	Variables any    `json:"variables"`
}

type graphqlError struct {
	Message string `json:"message"`
}

type graphqlResponse struct {
	Data   json.RawMessage `json:"data"` // один запрос возвращает разные данные
	Errors []graphqlError  `json:"errors"`
}

func (c *Client) doGraphQL(
	ctx context.Context,
	query string,
	variables any,
	output any,
) error {
	if c.token == "" || c.orgID == "" {
		return fmt.Errorf("client is not authenticated for an organization")
	}

	input := graphqlRequest{
		Query:     query,
		Variables: variables,
	}

	var response graphqlResponse

	err := c.doJSON(
		ctx,
		http.MethodPost,
		"/master/graphql",
		input,
		&response,
	)
	if err != nil {
		return fmt.Errorf("GraphQL request: %w", err)
	}

	if len(response.Errors) > 0 {
		return fmt.Errorf(
			"GraphQL error: %s",
			response.Errors[0].Message,
		)
	}

	if len(response.Data) == 0 ||
		string(response.Data) == "null" {
		return fmt.Errorf("GraphQL response contains no data")
	}

	if err := json.Unmarshal(response.Data, output); err != nil {
		return fmt.Errorf("decode GraphQL data: %w", err)
	}

	return nil
}
