package soho

import (
	"context"
	"fmt"
	"net/http"
)

type loginRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

type tokenResponse struct {
	Token string `json:"token"`
}

func (c *Client) Login(
	ctx context.Context,
	login string,
	password string,
) (string, error) {
	if login == "" || password == "" {
		return "", fmt.Errorf("login and password must not be empty")
	}

	input := loginRequest{
		Login:    login,
		Password: password,
	}

	var output tokenResponse

	err := c.doJSON(
		ctx,
		http.MethodPost,
		"/api/login/password",
		input,
		&output,
	)
	if err != nil {
		return "", fmt.Errorf("login: %w", err)
	}

	if output.Token == "" {
		return "", fmt.Errorf("login response contains no token")
	}

	return output.Token, nil
}
