package soho

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
)

const maxJSONResponseSize = 8 << 20

func (c *Client) doJSON(
	ctx context.Context,
	method string,
	path string,
	input any,
	output any,
) error {
	var body io.Reader

	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("encode JSON request: %w", err)
		}

		body = bytes.NewReader(data)
	}

	resp, err := c.do(ctx, method, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(
		io.LimitReader(resp.Body, maxJSONResponseSize+1),
	)
	if err != nil {
		return fmt.Errorf("read JSON response: %w", err)
	}

	if len(data) > maxJSONResponseSize {
		return fmt.Errorf("JSON response exceeds %d bytes", maxJSONResponseSize)
	}

	if err := json.Unmarshal(data, output); err != nil {
		return fmt.Errorf("decode JSON response: %w", err)
	}

	return nil
}
