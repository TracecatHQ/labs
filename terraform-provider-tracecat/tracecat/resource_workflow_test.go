package tracecat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func TestWorkflowReadRefreshesPlatformFields(t *testing.T) {
	t.Parallel()

	const desiredYAML = "version: 1\ndefinition:\n  title: Candidate Run\n"
	d := schema.TestResourceDataRaw(t, resourceWorkflow().Schema, map[string]any{
		"workspace_id": "ws-1",
		"filename":     "candidate-run.yml",
		"yaml":         desiredYAML,
		"alias":        "candidate_run",
		"status":       "online",
	})
	d.SetId("wf-1")

	c, err := NewClient("http://tracecat.test/api/", "token")
	if err != nil {
		t.Fatal(err)
	}
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body string
		switch r.URL.Path {
		case "/api/workflows/wf-1":
			body = `{"id":"wf-1","alias":"candidate_run","title":"Candidate Run","status":"offline","version":2}`
		case "/api/workflows/wf-1/definition":
			body = `{"content":{"title":"Candidate Run"}}`
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})

	if diags := workflowRead(context.Background(), d, c); diags.HasError() {
		t.Fatal(diags)
	}
	if got := d.Get("yaml").(string); got != desiredYAML {
		t.Fatalf("yaml changed during refresh: %q", got)
	}
	if got := d.Get("version").(int); got != 2 {
		t.Fatalf("version = %d", got)
	}
	if got := d.Get("status").(string); got != "offline" {
		t.Fatalf("status = %q", got)
	}
	if got, want := d.Get("sha256").(string), workflowSourceFingerprint([]byte(desiredYAML)); got != want {
		t.Fatalf("sha256 = %q, want %q", got, want)
	}
}

func TestWorkflowCommittedDefinitionDriftPlansReplacement(t *testing.T) {
	t.Parallel()

	const desiredYAML = "version: 1\ndefinition:\n  title: Candidate Run\n  returns: baseline\n"
	desiredFingerprint := workflowSourceFingerprint([]byte(desiredYAML))
	d := schema.TestResourceDataRaw(t, resourceWorkflow().Schema, map[string]any{
		"workspace_id": "ws-1",
		"filename":     "candidate-run.yml",
		"yaml":         desiredYAML,
		"sha256":       desiredFingerprint,
		"alias":        "candidate_run",
		"status":       "online",
	})
	d.SetId("wf-1")

	c, err := NewClient("http://tracecat.test/api/", "token")
	if err != nil {
		t.Fatal(err)
	}
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		response := func(body string) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}
		switch r.URL.Path {
		case "/api/workflows/wf-1":
			return response(`{"id":"wf-1","alias":"candidate_run","title":"Candidate Run","status":"online","version":3}`)
		case "/api/workflows/wf-1/definition":
			return response(`{"content":{"title":"Candidate Run","returns":"edited in Tracecat"}}`)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	})

	if diags := workflowRead(context.Background(), d, c); diags.HasError() {
		t.Fatal(diags)
	}
	if got := d.Get("sha256").(string); got == desiredFingerprint {
		t.Fatalf("drift fingerprint remained equal to configured source hash %q", got)
	}
	want := workflowDriftFingerprint(map[string]any{"title": "Candidate Run", "returns": "edited in Tracecat"}, true)
	if got := d.Get("sha256").(string); got != want {
		t.Fatalf("sha256 = %q, want remote drift fingerprint %q", got, want)
	}
	config := terraform.NewResourceConfigRaw(map[string]any{
		"workspace_id": "ws-1",
		"filename":     "candidate-run.yml",
		"yaml":         desiredYAML,
		"sha256":       desiredFingerprint,
		"alias":        "candidate_run",
		"status":       "online",
	})
	diff, err := resourceWorkflow().Diff(context.Background(), d.State(), config, nil)
	if err != nil {
		t.Fatal(err)
	}
	change, ok := diff.Attributes["sha256"]
	if !ok || change.Old == change.New || change.New != desiredFingerprint || !change.RequiresNew {
		t.Fatalf("sha256 diff = %#v", change)
	}
}

func TestWorkflowSHA256CanBeConfiguredAsReplacementTrigger(t *testing.T) {
	t.Parallel()

	field := resourceWorkflow().Schema["sha256"]
	if !field.Optional || !field.Computed || !field.ForceNew {
		t.Fatalf("sha256 schema = Optional:%v Computed:%v ForceNew:%v", field.Optional, field.Computed, field.ForceNew)
	}
}

func TestWorkflowDefinitionMatchIgnoresPersistedActionOrder(t *testing.T) {
	t.Parallel()

	desired := map[string]any{
		"actions": []any{
			map[string]any{"ref": "first", "action": "core.first"},
			map[string]any{"ref": "second", "action": "core.second"},
		},
	}
	remote := map[string]any{
		"actions": []any{
			map[string]any{"ref": "second", "action": "core.second", "run_if": nil},
			map[string]any{"ref": "first", "action": "core.first", "run_if": nil},
		},
	}
	if !workflowValueMatches(desired, remote) {
		t.Fatal("persisted action ordering and server defaults must not create false drift")
	}
	remote["actions"].([]any)[0].(map[string]any)["action"] = "core.edited"
	if workflowValueMatches(desired, remote) {
		t.Fatal("an authored action change must still be detected")
	}
}

func TestWorkflowDeleteToleratesDeposedWorkflowAlreadyDeletedByCutover(t *testing.T) {
	t.Parallel()

	d := schema.TestResourceDataRaw(t, resourceWorkflow().Schema, map[string]any{
		"workspace_id": "ws-1",
		"filename":     "candidate-run.yml",
		"yaml":         "version: 1\ndefinition:\n  title: Candidate Run\n",
		"alias":        "candidate_run",
		"status":       "online",
	})
	d.SetId("wf-old")

	c, err := NewClient("http://tracecat.test/api/", "token")
	if err != nil {
		t.Fatal(err)
	}
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/workflows/wf-old" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader(`{"detail":"Resource not found"}`)),
			Header:     make(http.Header),
		}, nil
	})

	if diags := workflowDelete(context.Background(), d, c); diags.HasError() {
		t.Fatal(diags)
	}
	if d.Id() != "" {
		t.Fatalf("id = %q after deleting deposed resource", d.Id())
	}
}

func TestWorkflowYAMLChangesReplaceResource(t *testing.T) {
	t.Parallel()

	if !resourceWorkflow().Schema["yaml"].ForceNew {
		t.Fatal("workflow YAML must replace the resource so dependants receive the new workflow ID")
	}
}

func TestWorkflowCreateAdoptsMatchingDefinition(t *testing.T) {
	t.Parallel()

	const desiredYAML = `version: 1
definition:
  title: Candidate Run
  description: Current shared runner
  entrypoint:
    ref: start
    expects: {}
  actions: []
  returns: ok
`
	d := schema.TestResourceDataRaw(t, resourceWorkflow().Schema, map[string]any{
		"workspace_id": "ws-1",
		"filename":     "candidate-run.yml",
		"yaml":         desiredYAML,
		"alias":        "candidate_run",
		"status":       "online",
	})

	c, err := NewClient("http://tracecat.test/api/", "token")
	if err != nil {
		t.Fatal(err)
	}
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		response := func(status int, body string) (*http.Response, error) {
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/workflows":
			if got := r.URL.Query().Get("limit"); got != "0" {
				t.Fatalf("limit = %q", got)
			}
			return response(http.StatusOK, `{"items":[{"id":"wf-shared","alias":"candidate_run"}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/workflows/wf-shared/definition":
			return response(http.StatusOK, `{"content":{"title":"Candidate Run","description":"Current shared runner","entrypoint":{"ref":"start","expects":{}},"actions":[],"returns":"ok","config":{"timeout":60}}}`)
		case r.Method == http.MethodPatch && r.URL.Path == "/api/workflows/wf-shared":
			return response(http.StatusNoContent, "")
		case r.Method == http.MethodGet && r.URL.Path == "/api/workflows/wf-shared":
			return response(http.StatusOK, `{"id":"wf-shared","alias":"candidate_run","title":"Candidate Run","status":"online","version":3}`)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	})

	if diags := workflowCreate(context.Background(), d, c); diags.HasError() {
		t.Fatal(diags)
	}
	if d.Id() != "wf-shared" {
		t.Fatalf("id = %q", d.Id())
	}
}

func TestWorkflowCreateReplacesStaleDefinitionAndDeletesOld(t *testing.T) {
	t.Parallel()

	const desiredYAML = `version: 1
definition:
  title: Candidate Run
  entrypoint: {ref: start, expects: {}}
  actions: []
  returns: current
`
	d := schema.TestResourceDataRaw(t, resourceWorkflow().Schema, map[string]any{
		"workspace_id": "ws-1",
		"filename":     "candidate-run.yml",
		"yaml":         desiredYAML,
		"alias":        "candidate_run",
		"status":       "online",
	})

	archiveAlias := workflowCutoverAlias("candidate_run", "archived", "wf-old")
	var archived, deleted bool
	c, err := NewClient("http://tracecat.test/api/", "token")
	if err != nil {
		t.Fatal(err)
	}
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		response := func(status int, body string) (*http.Response, error) {
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}
		decodeBody := func() map[string]any {
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			return payload
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/workflows":
			return response(http.StatusOK, `{"items":[{"id":"wf-old","alias":"candidate_run"}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/workflows/wf-old/definition":
			return response(http.StatusOK, `{"content":{"title":"Candidate Run","entrypoint":{"ref":"start","expects":{}},"actions":[],"returns":"stale"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/workflows/wf-old":
			return response(http.StatusOK, `{"id":"wf-old","alias":"candidate_run","status":"online"}`)
		case r.Method == http.MethodPatch && r.URL.Path == "/api/workflows/wf-old":
			payload := decodeBody()
			if payload["alias"] != archiveAlias || payload["status"] != "offline" {
				t.Fatalf("archive payload = %#v", payload)
			}
			archived = true
			return response(http.StatusNoContent, "")
		case r.Method == http.MethodPost && r.URL.Path == "/api/workflows":
			if !archived {
				t.Fatal("uploaded replacement before archiving old workflow")
			}
			return response(http.StatusCreated, `{"id":"wf-new"}`)
		case r.Method == http.MethodPatch && r.URL.Path == "/api/workflows/wf-new":
			payload := decodeBody()
			if alias, exists := payload["alias"]; exists && alias != "candidate_run" {
				t.Fatalf("replacement alias = %#v", payload)
			}
			return response(http.StatusNoContent, "")
		case r.Method == http.MethodPost && r.URL.Path == "/api/workflows/wf-new/commit":
			return response(http.StatusOK, `{"status":"success"}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/workflows/wf-old":
			deleted = true
			return response(http.StatusNoContent, "")
		case r.Method == http.MethodGet && r.URL.Path == "/api/workflows/wf-new":
			return response(http.StatusOK, `{"id":"wf-new","alias":"candidate_run","title":"Candidate Run","status":"online","version":1}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/workflows/wf-new/definition":
			return response(http.StatusOK, `{"content":{"title":"Candidate Run","entrypoint":{"expects":{}},"actions":[],"returns":"current"}}`)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	})

	if diags := workflowCreate(context.Background(), d, c); diags.HasError() {
		t.Fatal(diags)
	}
	if d.Id() != "wf-new" {
		t.Fatalf("id = %q", d.Id())
	}
	if !archived {
		t.Fatal("expected stale workflow to be archived")
	}
	if !deleted {
		t.Fatal("expected stale workflow to be deleted after a successful cutover")
	}
}

func TestWorkflowCreateRollsBackFailedCutover(t *testing.T) {
	t.Parallel()

	const desiredYAML = `version: 1
definition:
  title: Candidate Run
  entrypoint: {ref: start, expects: {}}
  actions: []
  returns: current
`
	d := schema.TestResourceDataRaw(t, resourceWorkflow().Schema, map[string]any{
		"workspace_id": "ws-1",
		"filename":     "candidate-run.yml",
		"yaml":         desiredYAML,
		"alias":        "candidate_run",
		"status":       "online",
	})

	var restored, quarantined bool
	c, err := NewClient("http://tracecat.test/api/", "token")
	if err != nil {
		t.Fatal(err)
	}
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		response := func(status int, body string) (*http.Response, error) {
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}
		var payload map[string]any
		if r.Method == http.MethodPatch {
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/workflows":
			return response(http.StatusOK, `{"items":[{"id":"wf-old","alias":"candidate_run"}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/workflows/wf-old/definition":
			return response(http.StatusOK, `{"content":{"returns":"stale"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/workflows/wf-old":
			return response(http.StatusOK, `{"id":"wf-old","alias":"candidate_run","status":"online"}`)
		case r.Method == http.MethodPatch && r.URL.Path == "/api/workflows/wf-old":
			if payload["alias"] == "candidate_run" {
				restored = payload["status"] == "online"
			}
			return response(http.StatusNoContent, "")
		case r.Method == http.MethodPost && r.URL.Path == "/api/workflows":
			return response(http.StatusCreated, `{"id":"wf-new"}`)
		case r.Method == http.MethodPatch && r.URL.Path == "/api/workflows/wf-new":
			if payload["alias"] == workflowCutoverAlias("candidate_run", "failed", "wf-new") {
				quarantined = payload["status"] == "offline"
			}
			return response(http.StatusNoContent, "")
		case r.Method == http.MethodPost && r.URL.Path == "/api/workflows/wf-new/commit":
			return response(http.StatusOK, `{"status":"failure","message":"invalid"}`)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	})

	if diags := workflowCreate(context.Background(), d, c); !diags.HasError() {
		t.Fatal("expected failed replacement commit")
	}
	if !quarantined || !restored {
		t.Fatalf("rollback quarantined=%v restored=%v", quarantined, restored)
	}
}

func TestWorkflowValueMatchesAllowsRemoteDefaults(t *testing.T) {
	t.Parallel()
	desired := map[string]any{
		"actions": []any{map[string]any{"ref": "run", "args": map[string]any{"value": 1}}},
	}
	remote := map[string]any{
		"actions": []any{map[string]any{"ref": "run", "args": map[string]any{"value": float64(1)}, "retry_policy": map[string]any{"max_attempts": 1}}},
		"config":  map[string]any{"timeout": 60},
	}
	if !workflowValueMatches(desired, remote) {
		t.Fatal("server defaults should not make an authored definition stale")
	}
}

func TestWorkflowValueMatchesAllowsOmittedEntrypointRef(t *testing.T) {
	t.Parallel()
	desired := map[string]any{
		"entrypoint": map[string]any{"ref": "load_work", "expects": map[string]any{}},
		"actions":    []any{map[string]any{"ref": "run", "action": "core.transform.reshape"}},
		"config":     map[string]any{"timeout": 0},
	}
	remote := map[string]any{
		"entrypoint": map[string]any{"expects": map[string]any{}},
		"actions":    []any{map[string]any{"ref": "run", "action": "core.transform.reshape"}},
		"config":     map[string]any{"timeout": float64(0)},
	}
	if !workflowValueMatches(desired, remote) {
		t.Fatal("persisted workflows omit entrypoint.ref and normalize numeric values")
	}

	delete(remote["actions"].([]any)[0].(map[string]any), "ref")
	if workflowValueMatches(desired, remote) {
		t.Fatal("action refs remain semantically significant")
	}
}
