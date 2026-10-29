package soho

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
	token      string
	orgID      string
}

func NewClient(rawBaseURL string) (*Client, error) {
	u, err := url.Parse(rawBaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}

	if (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf(
			"base URL must contain http or https and a host",
		)
	}

	if (u.Path != "" && u.Path != "/") ||
		u.RawQuery != "" ||
		u.Fragment != "" ||
		u.User != nil {
		return nil, fmt.Errorf(
			"base URL must contain only scheme and host",
		)
	}

	return &Client{
		baseURL: strings.TrimRight(u.String(), "/"),
		httpClient: &http.Client{
			Timeout:       30 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

func (c *Client) do(
	ctx context.Context,
	method string,
	path string,
	body io.Reader,
) (*http.Response, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		method,
		c.baseURL+path,
		body,
	)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")

	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	if path == "/master/graphql" && c.orgID != "" {
		req.Header.Set("x-procraft-org", c.orgID)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()

		return nil, fmt.Errorf(
			"SOHO API returned HTTP %d",
			resp.StatusCode,
		)
	}

	return resp, nil
}
