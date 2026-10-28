package soho

import (
	"context"
	"fmt"
	"strconv"
)

type Organization struct {
	UID int64 `json:"uid"`
}

const currentOrganizationQuery = `
query CurrentOrganization {
	viewer {
		org {
			uid
		}
	}
}
`

func (c *Client) CurrentOrganization(
	ctx context.Context,
) (*Organization, error) {
	var data struct {
		Viewer struct {
			Org *Organization `json:"org"`
		} `json:"viewer"`
	}

	err := c.doGraphQL(
		ctx,
		currentOrganizationQuery,
		nil,
		&data,
	)
	if err != nil {
		return nil, err
	}

	if data.Viewer.Org == nil || data.Viewer.Org.UID <= 0 {
		return nil, fmt.Errorf("current organization is unavailable")
	}

	if strconv.FormatInt(data.Viewer.Org.UID, 10) != c.orgID {
		return nil, fmt.Errorf("response org does not match the session")
	}

	return data.Viewer.Org, nil
}
