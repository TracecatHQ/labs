package grading

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type TerraformOutputs struct {
	WorkspaceID string
	TableIDs    map[string]string
}

func ValidateIdentifiers(lab, runID string) error {
	if len(lab) != 3 || lab[0] < '0' || lab[0] > '9' || lab[1] < '0' || lab[1] > '9' || lab[2] < '0' || lab[2] > '9' {
		return invalid("lab must be a zero-padded three-digit ID")
	}
	_, _, err := parseExecutionID(runID)
	return err
}

type terraformOutput struct {
	Value json.RawMessage `json:"value"`
}

func LoadTerraformOutputs(ctx context.Context, root, lab string) (TerraformOutputs, error) {
	terraformDir := filepath.Join(root, lab, "terraform")
	command := exec.CommandContext(ctx, "terraform", "-chdir="+terraformDir, "output", "-json")
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return TerraformOutputs{}, fmt.Errorf("load Terraform outputs: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var envelope map[string]terraformOutput
	if err := json.Unmarshal(output, &envelope); err != nil {
		return TerraformOutputs{}, fmt.Errorf("decode Terraform outputs: %w", err)
	}
	var result TerraformOutputs
	workspace, ok := envelope["workspace_id"]
	if !ok || json.Unmarshal(workspace.Value, &result.WorkspaceID) != nil || result.WorkspaceID == "" {
		return TerraformOutputs{}, fmt.Errorf("Terraform output workspace_id is missing or invalid")
	}
	tables, ok := envelope["table_ids"]
	if !ok || json.Unmarshal(tables.Value, &result.TableIDs) != nil {
		return TerraformOutputs{}, fmt.Errorf("Terraform output table_ids is missing or invalid")
	}
	for _, name := range []string{"evaluation_runs", "evaluation_scores"} {
		if result.TableIDs[name] == "" {
			return TerraformOutputs{}, fmt.Errorf("Terraform table output %s is missing", name)
		}
	}
	return result, nil
}

type APIClient struct {
	baseURL    *url.URL
	apiKey     string
	httpClient *http.Client
}

func NewAPIClient(rawURL, apiKey string, httpClient *http.Client) (*APIClient, error) {
	baseURL, err := url.Parse(rawURL)
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, fmt.Errorf("TRACECAT_API_URL is missing or invalid")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("TRACECAT_API_KEY is not set")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &APIClient{baseURL: baseURL, apiKey: apiKey, httpClient: httpClient}, nil
}

type rowsPage struct {
	Items      []json.RawMessage `json:"items"`
	HasMore    bool              `json:"has_more"`
	NextCursor string            `json:"next_cursor"`
}

func (c *APIClient) FetchRows(ctx context.Context, workspaceID, tableID string) ([]json.RawMessage, error) {
	var rows []json.RawMessage
	cursor := ""
	for {
		endpoint := c.endpoint("workspaces", workspaceID, "tables", tableID, "rows")
		query := endpoint.Query()
		query.Set("limit", "200")
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		endpoint.RawQuery = query.Encode()
		var page rowsPage
		if err := c.getJSON(ctx, endpoint, &page); err != nil {
			return nil, err
		}
		rows = append(rows, page.Items...)
		if !page.HasMore {
			return rows, nil
		}
		if page.NextCursor == "" || page.NextCursor == cursor {
			return nil, fmt.Errorf("Tracecat returned an invalid pagination cursor")
		}
		cursor = page.NextCursor
	}
}

func (c *APIClient) FetchExecution(ctx context.Context, workspaceID, executionID string) (Execution, error) {
	workflowID, nativeID, err := parseExecutionID(executionID)
	if err != nil {
		return Execution{}, err
	}
	endpoint := c.endpoint("workspaces", workspaceID, "workflows", workflowID, "executions", nativeID)
	var execution Execution
	if err := c.getJSON(ctx, endpoint, &execution); err != nil {
		return Execution{}, err
	}
	return execution, nil
}

func parseExecutionID(executionID string) (string, string, error) {
	workflowID, nativeID, ok := strings.Cut(executionID, "/")
	unsafe := func(value string) bool {
		return value == "" || value == "." || value == ".." || strings.ContainsAny(value, `/\\`)
	}
	if !ok || unsafe(workflowID) || unsafe(nativeID) {
		return "", "", invalid("RUN_ID must be a Tracecat workflow execution ID")
	}
	return workflowID, nativeID, nil
}

func (c *APIClient) endpoint(parts ...string) *url.URL {
	copyURL := *c.baseURL
	path := strings.TrimSuffix(copyURL.EscapedPath(), "/")
	for _, part := range parts {
		path += "/" + url.PathEscape(part)
	}
	copyURL.RawPath = path
	decoded, err := url.PathUnescape(path)
	if err == nil {
		copyURL.Path = decoded
	}
	return &copyURL
}

func (c *APIClient) getJSON(ctx context.Context, endpoint *url.URL, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("Tracecat request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("Tracecat returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	decoder := json.NewDecoder(response.Body)
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode Tracecat response: %w", err)
	}
	return nil
}

func CollectInputs(ctx context.Context, client *APIClient, outputs TerraformOutputs, labID, runID string) (Inputs, error) {
	candidateExecution, err := client.FetchExecution(ctx, outputs.WorkspaceID, runID)
	if err != nil {
		return Inputs{}, fmt.Errorf("load Candidate execution: %w", err)
	}
	if _, err := validateExecution(candidateExecution, runID, "Candidate"); err != nil {
		return Inputs{}, err
	}
	runMessages, err := client.FetchRows(ctx, outputs.WorkspaceID, outputs.TableIDs["evaluation_runs"])
	if err != nil {
		return Inputs{}, fmt.Errorf("load Evaluation Runs: %w", err)
	}
	scoreMessages, err := client.FetchRows(ctx, outputs.WorkspaceID, outputs.TableIDs["evaluation_scores"])
	if err != nil {
		return Inputs{}, fmt.Errorf("load evaluation scores: %w", err)
	}
	var detailMessages []json.RawMessage
	if tableID := outputs.TableIDs["evaluation_trial_details"]; tableID != "" {
		detailMessages, err = client.FetchRows(ctx, outputs.WorkspaceID, tableID)
		if err != nil {
			return Inputs{}, fmt.Errorf("load trial details: %w", err)
		}
	}
	runRows, err := decodeRows[EvaluationRunRow](runMessages)
	if err != nil {
		return Inputs{}, fmt.Errorf("decode Evaluation Runs: %w", err)
	}
	scores, err := decodeRows[ScoreRow](scoreMessages)
	if err != nil {
		return Inputs{}, fmt.Errorf("decode evaluation scores: %w", err)
	}
	details, err := decodeRows[TrialDetail](detailMessages)
	if err != nil {
		return Inputs{}, fmt.Errorf("decode trial details: %w", err)
	}
	judgeExecutionID := ""
	for _, score := range scores {
		if score.EvaluationRunID == runID {
			judgeExecutionID = score.JudgeRunExecutionID
			break
		}
	}
	if judgeExecutionID == "" {
		return Inputs{}, invalid("no Judge scores found; run: just judge %s RUN_ID=%s", labID, runID)
	}
	judgeExecution, err := client.FetchExecution(ctx, outputs.WorkspaceID, judgeExecutionID)
	if err != nil {
		return Inputs{}, fmt.Errorf("load Judge execution: %w", err)
	}
	return Inputs{
		LabID: labID, RunID: runID, RunRows: runRows, Scores: scores, Details: details,
		CandidateExecution: candidateExecution, JudgeExecution: judgeExecution,
	}, nil
}

func decodeRows[T any](messages []json.RawMessage) ([]T, error) {
	rows := make([]T, 0, len(messages))
	for index, message := range messages {
		var row T
		if err := json.Unmarshal(message, &row); err != nil {
			return nil, fmt.Errorf("row %d: %w", index+1, err)
		}
		rows = append(rows, row)
	}
	return rows, nil
}
