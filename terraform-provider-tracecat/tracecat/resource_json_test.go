package tracecat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func TestJSONCreateByKeyAdoptsAndUpdates(t *testing.T) {
	t.Parallel()
	resource := resourceJSONByKey("/agent/presets", "slug", "/by-slug/")
	d := schema.TestResourceDataRaw(t, resource.Schema, map[string]any{
		"workspace_id": "workspace-1",
		"config_json":  `{"slug":"candidate","name":"Candidate","model_provider":"openai","model_name":"new-model"}`,
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
		case r.Method == http.MethodGet && r.URL.Path == "/api/agent/presets/by-slug/candidate":
			return response(http.StatusOK, `{"id":"preset-shared","slug":"candidate","name":"Candidate","model_provider":"openai","model_name":"old-model"}`)
		case r.Method == http.MethodPatch && r.URL.Path == "/api/agent/presets/preset-shared":
			if err := json.NewDecoder(r.Body).Decode(&patched); err != nil {
				t.Fatal(err)
			}
			return response(http.StatusOK, `{"id":"preset-shared"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/agent/presets/preset-shared":
			return response(http.StatusOK, `{"id":"preset-shared","slug":"candidate","name":"Candidate","model_provider":"openai","model_name":"new-model"}`)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})

	if diags := resource.CreateContext(context.Background(), d, c); diags.HasError() {
		t.Fatal(diags)
	}
	if d.Id() != "preset-shared" {
		t.Fatalf("id = %q", d.Id())
	}
	if patched["model_name"] != "new-model" {
		t.Fatalf("preset patch = %#v", patched)
	}
}
