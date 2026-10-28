package soho

import "testing"

func TestOrganizationIDFromToken(t *testing.T) {
	token := "test.eyJvcmdJZCI6IjQyIn0.test" // {"orgID":"42"}

	id, err := organizationIDFromToken(token)
	if err != nil {
		t.Fatal(err)
	}

	if id != "42" {
		t.Errorf("unexpected organization ID: %s", id)
	}
}
