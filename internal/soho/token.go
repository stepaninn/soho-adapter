package soho

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func organizationIDFromToken(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("unexpected token format")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode token payload: %w", err)
	}

	var claims struct {
		OrgID string `json:"orgID"`
	}

	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("decode token claim: %w", err)
	}

	id, err := strconv.ParseInt(claims.OrgID, 10, 64)
	if err != nil || id <= 0 {
		return "", fmt.Errorf("token contains no valid organization ID")
	}

	return strconv.FormatInt(id, 10), nil
}
