package soho

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDoGraphQL(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		responseBody string
		wantValue    int
		wantErr      bool
	}{
		{
			name:         "success",
			statusCode:   http.StatusOK,
			responseBody: `{"data":{"value":42}}`,
			wantValue:    42,
		},
		{
			name:         "HTTP error",
			statusCode:   http.StatusUnauthorized,
			responseBody: `{}`,
			wantErr:      true,
		},
		{
			name:         "GraphQL errors",
			statusCode:   http.StatusOK,
			responseBody: `{"errors":[{"message":"test error"}]}`,
			wantErr:      true,
		},
		{
			name:       "partial data with errors",
			statusCode: http.StatusOK,
			responseBody: `{
			"data":{"value":42},
			"errors":[{"message":"test error"}]
		}`,
			wantErr: true,
		},
		{
			name:         "null data",
			statusCode:   http.StatusOK,
			responseBody: `{"data":null}`,
			wantErr:      true,
		},
		{
			name:         "missing data",
			statusCode:   http.StatusOK,
			responseBody: `{}`,
			wantErr:      true,
		},
		{
			name:         "unexpected data type",
			statusCode:   http.StatusOK,
			responseBody: `{"data":{"value":"not a number"}}`,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodPost ||
						r.URL.Path != "/master/graphql" {
						t.Error("unexpected request")
					}

					if r.Header.Get("Authorization") != "Bearer test-token" {
						t.Error("missing authorization")
					}

					if r.Header.Get("x-procraft-org") != "42" {
						t.Error("missing organization context")
					}

					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(tt.statusCode)
					_, _ = io.WriteString(w, tt.responseBody)
				}),
			)
			defer server.Close()

			client, err := NewClient(server.URL)
			if err != nil {
				t.Fatal(err)
			}

			client.token = "test-token"
			client.orgID = "42"

			var result struct {
				Value int `json:"value"`
			}

			err = client.doGraphQL(
				context.Background(),
				"query TestQuery { value }",
				nil,
				&result,
			)

			if (err != nil) != tt.wantErr {
				t.Fatalf(
					"unexpected error: got %v, wantErr=%v",
					err,
					tt.wantErr,
				)
			}

			if tt.wantErr {
				return
			}

			if result.Value != tt.wantValue {
				t.Errorf(
					"unexpected value: got %d, want %d",
					result.Value,
					tt.wantValue,
				)
			}
		})
	}
}
