package grading

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadTerraformOutputsWithoutJQ(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable")
	}
	directory := t.TempDir()
	terraform := filepath.Join(directory, "terraform")
	script := `#!/bin/sh
printf '%s' '{"workspace_id":{"value":"workspace"},"table_ids":{"value":{"evaluation_runs":"runs","evaluation_trials":"trials","evaluation_evidence":"evidence","evaluation_scores":"scores","evaluation_metrics":"metrics","scoring_attempts":"attempts"}}}'
`
	if err := os.WriteFile(terraform, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	outputs, err := LoadTerraformOutputs(context.Background(), "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if outputs.WorkspaceID != "workspace" || outputs.TableIDs["evaluation_scores"] != "scores" {
		t.Fatalf("outputs = %#v", outputs)
	}
}

func TestFetchRowsPaginatesAndAuthenticates(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("authorization = %q", request.Header.Get("Authorization"))
		}
		if request.URL.Query().Get("limit") != "200" {
			t.Errorf("limit = %q", request.URL.Query().Get("limit"))
		}
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Query().Get("cursor") == "" {
			_, _ = response.Write([]byte(`{"items":[{"id":1}],"has_more":true,"next_cursor":"next"}`))
			return
		}
		_, _ = response.Write([]byte(`{"items":[{"id":2}],"has_more":false}`))
	}))
	defer server.Close()
	client, err := NewAPIClient(server.URL, "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	rows, err := client.FetchRows(context.Background(), "workspace", "table")
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || len(rows) != 2 {
		t.Fatalf("requests = %d, rows = %d", requests, len(rows))
	}
}

func TestFetchExecutionRejectsMalformedID(t *testing.T) {
	client, err := NewAPIClient("http://example.test", "secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.FetchExecution(context.Background(), "workspace", "invalid"); err == nil {
		t.Fatal("expected malformed execution ID error")
	}
}

func TestValidateIdentifiersRejectsPathTraversal(t *testing.T) {
	for _, test := range []struct{ lab, runID string }{
		{"../", "candidate/exec"},
		{"001", "../exec"},
		{"001", "candidate/../../escape"},
	} {
		if err := ValidateIdentifiers(test.lab, test.runID); err == nil {
			t.Fatalf("ValidateIdentifiers(%q, %q) succeeded", test.lab, test.runID)
		}
	}
}

func TestFetchRowsRejectsMalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`not-json`))
	}))
	defer server.Close()
	client, err := NewAPIClient(server.URL, "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.FetchRows(context.Background(), "workspace", "table"); err == nil {
		t.Fatal("expected malformed JSON error")
	}
}

func TestDecodeRowsIdentifiesBadRow(t *testing.T) {
	_, err := decodeRows[Execution]([]json.RawMessage{json.RawMessage(`{"id":"ok"}`), json.RawMessage(`{`)})
	if err == nil {
		t.Fatal("expected row decode error")
	}
}

func TestNumericColumnsAcceptNumbersAndQuotedDecimals(t *testing.T) {
	for _, input := range []string{`12.5`, `"12.5"`} {
		var value Number
		if err := json.Unmarshal([]byte(input), &value); err != nil {
			t.Fatalf("unmarshal %s: %v", input, err)
		}
		if value != 12.5 {
			t.Fatalf("unmarshal %s = %v", input, value)
		}
	}
}

func TestCollectInputsLoadsNormalizedTables(t *testing.T) {
	inputs := fixtureInputs()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var value any
		switch r.URL.Path {
		case "/workspaces/workspace/tables/runs/rows":
			value = map[string]any{"items": inputs.RunRows}
		case "/workspaces/workspace/tables/trials/rows":
			value = map[string]any{"items": inputs.Trials}
		case "/workspaces/workspace/tables/evidence/rows":
			value = map[string]any{"items": inputs.Evidence}
		case "/workspaces/workspace/tables/scores/rows":
			value = map[string]any{"items": inputs.Scores}
		case "/workspaces/workspace/tables/metrics/rows":
			value = map[string]any{"items": inputs.Metrics}
		case "/workspaces/workspace/tables/attempts/rows":
			value = map[string]any{"items": inputs.Attempts}
		case "/workspaces/workspace/workflows/candidate/executions/exec":
			value = inputs.CandidateExecution
		case "/workspaces/workspace/workflows/judge/executions/exec":
			value = inputs.ScoringExecutions[0]
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(value)
	}))
	defer server.Close()
	client, err := NewAPIClient(server.URL, "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	collected, err := CollectInputs(context.Background(), client, TerraformOutputs{WorkspaceID: "workspace", TableIDs: map[string]string{"evaluation_runs": "runs", "evaluation_trials": "trials", "evaluation_evidence": "evidence", "evaluation_scores": "scores", "evaluation_metrics": "metrics", "scoring_attempts": "attempts"}}, "999", fixtureRunID)
	if err != nil {
		t.Fatal(err)
	}
	report, err := NewReport(collected)
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary().Trials.Count != 2 {
		t.Fatalf("trial count = %d", report.Summary().Trials.Count)
	}
}
