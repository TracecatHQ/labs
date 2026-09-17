package tracecat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func TestCreateTableIndexes(t *testing.T) {
	t.Parallel()
	patched := false
	c, err := NewClient("http://tracecat.test/api/", "token")
	if err != nil {
		t.Fatal(err)
	}
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/tables/table-1":
			body := `{"columns":[{"id":"column-1","name":"case_id","is_index":false}]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		case r.Method == http.MethodPatch && r.URL.Path == "/api/tables/table-1/columns/column-1":
			patched = true
			return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})

	d := schema.TestResourceDataRaw(t, resourceTable().Schema, map[string]any{
		"workspace_id": "workspace-1",
		"name":         "case_templates",
		"columns_json": `[{"name":"case_id","type":"TEXT","is_index":true}]`,
	})
	d.SetId("table-1")
	desired := []map[string]any{{"name": "case_id", "type": "TEXT", "is_index": true}}
	if err := createTableIndexes(context.Background(), c, d, desired); err != nil {
		t.Fatal(err)
	}
	if !patched {
		t.Fatal("expected unique-index PATCH")
	}
}

func TestCreateTableIndexesTreatsExistingUniqueIndexAsSuccess(t *testing.T) {
	t.Parallel()
	getCount := 0
	c, err := NewClient("http://tracecat.test/api/", "token")
	if err != nil {
		t.Fatal(err)
	}
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		response := func(status int, body string) (*http.Response, error) {
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/tables/table-1":
			getCount++
			indexed := getCount > 1
			return response(http.StatusOK, fmt.Sprintf(`{"columns":[{"id":"column-1","name":"case_id","is_index":%t}]}`, indexed))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/tables/table-1/columns/column-1":
			return response(http.StatusBadRequest, `{"detail":"Table cannot have multiple unique indexes"}`)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})

	d := schema.TestResourceDataRaw(t, resourceTable().Schema, map[string]any{
		"workspace_id": "workspace-1",
		"name":         "case_templates",
		"columns_json": `[{"name":"case_id","type":"TEXT","is_index":true}]`,
	})
	d.SetId("table-1")
	desired := []map[string]any{{"name": "case_id", "type": "TEXT", "is_index": true}}
	if err := createTableIndexes(context.Background(), c, d, desired); err != nil {
		t.Fatal(err)
	}
}

func TestTableCreateAdoptsNameAndReconcilesColumns(t *testing.T) {
	t.Parallel()
	d := schema.TestResourceDataRaw(t, resourceTable().Schema, map[string]any{
		"workspace_id": "workspace-1",
		"name":         "case_templates",
		"columns_json": `[{"name":"case_id","type":"TEXT","is_index":true}]`,
	})

	var patched map[string]any
	c, err := NewClient("http://tracecat.test/api/", "token")
	if err != nil {
		t.Fatal(err)
	}
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		response := func(status int, body string) (*http.Response, error) {
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/tables":
			return response(http.StatusOK, `[{"id":"table-shared","name":"case_templates"}]`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/tables/table-shared":
			return response(http.StatusOK, `{"id":"table-shared","name":"case_templates","columns":[{"id":"column-1","name":"case_id","type":"TEXT","is_index":false}]}`)
		case r.Method == http.MethodPatch && r.URL.Path == "/api/tables/table-shared/columns/column-1":
			if err := json.NewDecoder(r.Body).Decode(&patched); err != nil {
				t.Fatal(err)
			}
			return response(http.StatusNoContent, "")
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})

	if diags := tableCreate(context.Background(), d, c); diags.HasError() {
		t.Fatal(diags)
	}
	if d.Id() != "table-shared" {
		t.Fatalf("id = %q", d.Id())
	}
	if patched["is_index"] != true {
		t.Fatalf("column patch = %#v", patched)
	}
}
