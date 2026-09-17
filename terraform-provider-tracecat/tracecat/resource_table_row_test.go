package tracecat

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func TestResolveTableRowIDFollowsCursorPages(t *testing.T) {
	t.Parallel()
	c, err := NewClient("http://tracecat.test/api/", "token")
	if err != nil {
		t.Fatal(err)
	}
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		response := func(body string) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}
		if r.Method != http.MethodGet || r.URL.Path != "/api/tables/table-1/rows" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		if r.URL.Query().Get("cursor") == "" {
			return response(`{"items":[{"id":"row-1","template_key":"005:first"}],"next_cursor":"page-2","has_more":true}`)
		}
		if r.URL.Query().Get("cursor") != "page-2" {
			t.Fatalf("cursor = %q", r.URL.Query().Get("cursor"))
		}
		return response(`{"items":[{"id":"row-target","template_key":"005:target"}],"has_more":false}`)
	})

	d := schema.TestResourceDataRaw(t, resourceTableRow().Schema, map[string]any{
		"workspace_id":    "workspace-1",
		"table_id":        "table-1",
		"data_json":       `{"template_key":"005:target"}`,
		"identity_column": "template_key",
		"identity_value":  "005:target",
	})
	if err := resolveTableRowID(context.Background(), c, d); err != nil {
		t.Fatal(err)
	}
	if d.Id() != "row-target" {
		t.Fatalf("id = %q", d.Id())
	}
}
