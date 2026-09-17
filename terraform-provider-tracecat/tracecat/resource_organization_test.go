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

func testResponse(status int, body string) (*http.Response, error) {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
}

func TestFolderCreateAdoptsNaturalKey(t *testing.T) {
	t.Parallel()
	d := schema.TestResourceDataRaw(t, resourceFolder("/folders", false).Schema, map[string]any{
		"workspace_id": "workspace-1", "name": "Scoring", "parent_path": "/Lab 005 - SecRL",
	})
	c, _ := NewClient("http://tracecat.test/api/", "token")
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/folders" && r.URL.Query().Get("parent_path") == "/Lab 005 - SecRL":
			return testResponse(http.StatusOK, `[{"id":"folder-1","name":"Scoring","path":"/Lab 005 - SecRL/Scoring"}]`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/folders/folder-1":
			return testResponse(http.StatusOK, `{"id":"folder-1","name":"Scoring","path":"/Lab 005 - SecRL/Scoring"}`)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	})
	if diags := folderCreate("/folders", false)(context.Background(), d, c); diags.HasError() {
		t.Fatal(diags)
	}
	if d.Id() != "folder-1" || d.Get("path") != "/Lab 005 - SecRL/Scoring" {
		t.Fatalf("unexpected folder state: id=%q path=%q", d.Id(), d.Get("path"))
	}
}

func TestFolderMembershipMovesReadsAndRemoves(t *testing.T) {
	t.Parallel()
	d := schema.TestResourceDataRaw(t, resourceFolderMembership("/workflows", "workflow_id").Schema, map[string]any{
		"workspace_id": "workspace-1", "workflow_id": "workflow-1", "folder_path": "/Lab 007 - SEvenLLM MCQ/Scoring",
	})
	moveCount := 0
	c, _ := NewClient("http://tracecat.test/api/", "token")
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/workflows/workflow-1/move":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			moveCount++
			if moveCount == 1 && body["folder_path"] != "/Lab 007 - SEvenLLM MCQ/Scoring" {
				t.Fatalf("move body = %#v", body)
			}
			if moveCount == 2 && body["folder_path"] != nil {
				t.Fatalf("delete move body = %#v", body)
			}
			return testResponse(http.StatusNoContent, "")
		case r.Method == http.MethodGet && r.URL.Path == "/api/workflows/workflow-1":
			return testResponse(http.StatusOK, `{"id":"workflow-1","folder_id":"folder-1"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/folders" && r.URL.Query().Get("parent_path") == "/Lab 007 - SEvenLLM MCQ":
			return testResponse(http.StatusOK, `[{"id":"folder-1","name":"Scoring","path":"/Lab 007 - SEvenLLM MCQ/Scoring"}]`)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	})
	if diags := folderMembershipUpsert("/workflows", "workflow_id")(context.Background(), d, c); diags.HasError() {
		t.Fatal(diags)
	}
	if diags := folderMembershipDelete("/workflows", "workflow_id")(context.Background(), d, c); diags.HasError() {
		t.Fatal(diags)
	}
	if moveCount != 2 || d.Id() != "" {
		t.Fatalf("moveCount=%d id=%q", moveCount, d.Id())
	}
}

func TestTagAssociationLifecycle(t *testing.T) {
	t.Parallel()
	d := schema.TestResourceDataRaw(t, resourceTagAssociation("/agent/presets", "preset_id", true).Schema, map[string]any{
		"workspace_id": "workspace-1", "preset_id": "preset-1", "tag_id": "tag-1",
	})
	c, _ := NewClient("http://tracecat.test/api/", "token")
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/agent/presets/preset-1/tags":
			return testResponse(http.StatusCreated, `{"id":"tag-1"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/agent/presets/preset-1/tags":
			return testResponse(http.StatusOK, `{"items":[{"id":"tag-1","name":"judge"}]}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/agent/presets/preset-1/tags/tag-1":
			return testResponse(http.StatusNoContent, "")
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	})
	if diags := tagAssociationCreate("/agent/presets", "preset_id", true)(context.Background(), d, c); diags.HasError() {
		t.Fatal(diags)
	}
	if diags := tagAssociationRead("/agent/presets", "preset_id", true)(context.Background(), d, c); diags.HasError() || d.Id() == "" {
		t.Fatalf("read failed: %v", diags)
	}
	if diags := tagAssociationDelete("/agent/presets", "preset_id")(context.Background(), d, c); diags.HasError() {
		t.Fatal(diags)
	}
}

func TestAgentTagCreateAdoptsNaturalKey(t *testing.T) {
	t.Parallel()
	d := schema.TestResourceDataRaw(t, resourceTag("/agent-tags", true).Schema, map[string]any{
		"workspace_id": "workspace-1", "name": "judge", "color": "#7C3AED",
	})
	c, _ := NewClient("http://tracecat.test/api/", "token")
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/agent-tags" && r.URL.Query().Get("limit") == "100":
			return testResponse(http.StatusOK, `{"items":[{"id":"agent-tag-1","name":"judge","ref":"judge","color":"#7C3AED"}]}`)
		case r.Method == http.MethodPatch && r.URL.Path == "/api/agent-tags/agent-tag-1":
			return testResponse(http.StatusOK, `{"id":"agent-tag-1","name":"judge","ref":"judge","color":"#7C3AED"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/agent-tags/agent-tag-1":
			return testResponse(http.StatusOK, `{"id":"agent-tag-1","name":"judge","ref":"judge","color":"#7C3AED"}`)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	})
	if diags := tagCreate("/agent-tags", true)(context.Background(), d, c); diags.HasError() {
		t.Fatal(diags)
	}
	if d.Id() != "agent-tag-1" {
		t.Fatalf("id=%q", d.Id())
	}
}
