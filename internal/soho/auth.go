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

func (c *Client) organizationToken(
	ctx context.Context,
) (string, error) {
	var output tokenResponse

	err := c.doJSON(
		ctx,
		http.MethodPost,
		"/master/api/set-org",
		nil,
		&output,
	)
	if err != nil {
		return "", fmt.Errorf("set organization: %w", err)
	}

	if output.Token == "" {
		return "", fmt.Errorf("set organization has no token")
	}

	return output.Token, nil
}

func (c *Client) Authenticate(
	ctx context.Context,
	login string,
	password string,
) error {
	c.token = ""

	rootToken, err := c.Login(ctx, login, password)
	if err != nil {
		return err
	}

	c.token = rootToken

	orgToken, err := c.organizationToken(ctx)
	if err != nil {
		c.token = ""
		return err
	}

	c.token = orgToken

	return nil
}
