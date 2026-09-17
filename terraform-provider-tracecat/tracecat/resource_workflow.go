package tracecat

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"gopkg.in/yaml.v3"
)

func resourceWorkflow() *schema.Resource {
	return &schema.Resource{
		CreateContext: workflowCreate,
		ReadContext:   workflowRead,
		UpdateContext: workflowUpdate,
		DeleteContext: workflowDelete,
		Schema: map[string]*schema.Schema{
			"workspace_id": workspaceSchema(),
			"filename":     {Type: schema.TypeString, Required: true, ForceNew: true},
			"yaml":         {Type: schema.TypeString, Required: true, Sensitive: true, ForceNew: true},
			"sha256": {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
				Description: "SHA-256 of the configured workflow YAML. Configure this value to detect committed definition drift and replace the workflow with the configured YAML on apply; use lifecycle.create_before_destroy so dependent memberships and tags move to the replacement in the same apply.",
			},
			"alias":   {Type: schema.TypeString, Required: true},
			"title":   {Type: schema.TypeString, Computed: true},
			"status":  {Type: schema.TypeString, Optional: true, Default: "online"},
			"version": {Type: schema.TypeInt, Computed: true},
		},
	}
}

func workflowCreate(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	c, err := client(meta)
	if err != nil {
		return diag.FromErr(err)
	}
	raw := []byte(d.Get("yaml").(string))
	id, err := ensureWorkflowDefinition(ctx, c, d, raw)
	if err != nil {
		return diag.FromErr(err)
	}
	d.SetId(id)
	return workflowRead(ctx, d, meta)
}

func ensureWorkflowDefinition(ctx context.Context, c *Client, d *schema.ResourceData, raw []byte) (string, error) {
	workspaceID := d.Get("workspace_id").(string)
	desiredAlias := d.Get("alias").(string)
	desiredDefinition, err := workflowDefinitionFromYAML(raw)
	if err != nil {
		return "", err
	}

	id, err := findWorkflowByAlias(ctx, c, workspaceID, desiredAlias)
	if err != nil {
		return "", err
	}
	if id == "" && d.Id() != "" {
		var current map[string]any
		status, getErr := c.JSON(ctx, http.MethodGet, "/workflows/"+d.Id(), workspaceID, nil, &current)
		if getErr != nil && status != http.StatusNotFound {
			return "", getErr
		}
		if getErr == nil {
			id = d.Id()
		}
	}

	if id == "" {
		id, err = uploadAndFinalizeWorkflow(ctx, c, d, raw)
		if err != nil {
			return "", err
		}
	} else {
		matches, err := remoteWorkflowDefinitionMatches(ctx, c, workspaceID, id, desiredDefinition)
		if err != nil {
			return "", err
		}
		if matches {
			if _, err := c.JSON(ctx, http.MethodPatch, "/workflows/"+id, workspaceID, map[string]any{
				"alias":  desiredAlias,
				"status": d.Get("status"),
			}, nil); err != nil {
				return "", err
			}
		} else {
			id, err = replaceWorkflowDefinition(ctx, c, d, id, raw)
			if err != nil {
				return "", err
			}
		}
	}

	if id == "" {
		return "", fmt.Errorf("Tracecat workflow create response omitted id")
	}
	if err := d.Set("sha256", workflowSourceFingerprint(raw)); err != nil {
		return "", err
	}
	return id, nil
}

func workflowDefinitionFromYAML(raw []byte) (map[string]any, error) {
	var source map[string]any
	if err := yaml.Unmarshal(raw, &source); err != nil {
		return nil, fmt.Errorf("decode workflow YAML: %w", err)
	}
	definition, ok := source["definition"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("workflow YAML must contain a definition object")
	}
	return definition, nil
}

func remoteWorkflowDefinitionMatches(ctx context.Context, c *Client, workspaceID, workflowID string, desired map[string]any) (bool, error) {
	definition, found, err := remoteWorkflowDefinition(ctx, c, workspaceID, workflowID)
	if err != nil || !found {
		return false, err
	}
	return workflowValueMatches(desired, definition), nil
}

func remoteWorkflowDefinition(ctx context.Context, c *Client, workspaceID, workflowID string) (map[string]any, bool, error) {
	var definition struct {
		Content map[string]any `json:"content"`
	}
	status, err := c.JSON(ctx, http.MethodGet, "/workflows/"+workflowID+"/definition", workspaceID, nil, &definition)
	if status == http.StatusNotFound {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return definition.Content, true, nil
}

func workflowSourceFingerprint(raw []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

func workflowDriftFingerprint(definition map[string]any, found bool) string {
	if !found {
		return "remote:missing"
	}
	return workflowSourceFingerprint([]byte("remote:" + canonicalJSON(definition)))
}

// Tracecat may materialize server defaults in a persisted definition. Compare
// every authored field and list element while allowing extra remote map fields.
func workflowValueMatches(desired, remote any) bool {
	return workflowValueMatchesAt(desired, remote, nil)
}

func workflowValueMatchesAt(desired, remote any, path []string) bool {
	switch wanted := desired.(type) {
	case map[string]any:
		actual, ok := remote.(map[string]any)
		if !ok {
			return false
		}
		for key, value := range wanted {
			remoteValue, exists := actual[key]
			// Tracecat uses entrypoint.ref while importing a workflow, then omits
			// it from the persisted definition returned by the public API.
			if !exists && len(path) == 1 && path[0] == "entrypoint" && key == "ref" {
				continue
			}
			if !exists || !workflowValueMatchesAt(value, remoteValue, append(path, key)) {
				return false
			}
		}
		return true
	case []any:
		actual, ok := remote.([]any)
		if !ok || len(wanted) != len(actual) {
			return false
		}
		// Tracecat persists actions in an implementation-defined order. Action
		// refs are unique, so compare this list as a ref-keyed collection while
		// retaining order-sensitive comparisons for every other authored list.
		if len(path) == 1 && path[0] == "actions" {
			actualByRef := make(map[string]any, len(actual))
			for _, value := range actual {
				action, ok := value.(map[string]any)
				if !ok {
					return false
				}
				ref, ok := action["ref"].(string)
				if !ok || ref == "" {
					return false
				}
				actualByRef[ref] = value
			}
			for _, value := range wanted {
				action, ok := value.(map[string]any)
				if !ok {
					return false
				}
				ref, ok := action["ref"].(string)
				if !ok || !workflowValueMatchesAt(value, actualByRef[ref], path) {
					return false
				}
			}
			return true
		}
		for index := range wanted {
			if !workflowValueMatchesAt(wanted[index], actual[index], path) {
				return false
			}
		}
		return true
	default:
		return canonicalJSON(desired) == canonicalJSON(remote)
	}
}

func uploadAndFinalizeWorkflow(ctx context.Context, c *Client, d *schema.ResourceData, raw []byte) (string, error) {
	workspaceID := d.Get("workspace_id").(string)
	var out map[string]any
	if err := c.UploadWorkflow(ctx, workspaceID, filepath.Base(d.Get("filename").(string)), raw, &out); err != nil {
		return "", err
	}
	id := responseID(out)
	if id == "" {
		return "", fmt.Errorf("Tracecat workflow create response omitted id")
	}
	if _, err := c.JSON(ctx, http.MethodPatch, "/workflows/"+id, workspaceID, map[string]any{"alias": d.Get("alias")}, nil); err != nil {
		return id, err
	}
	var commit map[string]any
	if _, err := c.JSON(ctx, http.MethodPost, "/workflows/"+id+"/commit", workspaceID, map[string]any{}, &commit); err != nil {
		return id, err
	}
	if commit["status"] != "success" {
		return id, fmt.Errorf("Tracecat rejected workflow %q: %v", d.Get("alias"), commit)
	}
	if _, err := c.JSON(ctx, http.MethodPatch, "/workflows/"+id, workspaceID, map[string]any{"status": d.Get("status")}, nil); err != nil {
		return id, err
	}
	return id, nil
}

func replaceWorkflowDefinition(ctx context.Context, c *Client, d *schema.ResourceData, oldID string, raw []byte) (string, error) {
	workspaceID := d.Get("workspace_id").(string)
	desiredAlias := d.Get("alias").(string)
	var oldWorkflow map[string]any
	if _, err := c.JSON(ctx, http.MethodGet, "/workflows/"+oldID, workspaceID, nil, &oldWorkflow); err != nil {
		return "", err
	}
	originalAlias, _ := oldWorkflow["alias"].(string)
	originalStatus, _ := oldWorkflow["status"].(string)
	if originalStatus == "" {
		originalStatus = "offline"
	}
	archiveAlias := workflowCutoverAlias(desiredAlias, "archived", oldID)
	if _, err := c.JSON(ctx, http.MethodPatch, "/workflows/"+oldID, workspaceID, map[string]any{
		"alias":  archiveAlias,
		"status": "offline",
	}, nil); err != nil {
		return "", fmt.Errorf("archive stale workflow %s: %w", oldID, err)
	}

	newID, err := uploadAndFinalizeWorkflow(ctx, c, d, raw)
	if err == nil {
		status, deleteErr := c.JSON(ctx, http.MethodDelete, "/workflows/"+oldID, workspaceID, nil, nil)
		if deleteErr == nil || status == http.StatusNotFound {
			return newID, nil
		}
		err = fmt.Errorf("delete stale workflow %s after cutover: %w", oldID, deleteErr)
	}
	rollbackErr := rollbackWorkflowCutover(ctx, c, workspaceID, oldID, originalAlias, originalStatus, newID, desiredAlias)
	if rollbackErr != nil {
		return "", fmt.Errorf("replace workflow %s: %w; rollback also failed: %v", oldID, err, rollbackErr)
	}
	return "", fmt.Errorf("replace workflow %s: %w; restored original alias", oldID, err)
}

func rollbackWorkflowCutover(ctx context.Context, c *Client, workspaceID, oldID, originalAlias, originalStatus, newID, desiredAlias string) error {
	var quarantineErr error
	if newID != "" {
		_, quarantineErr = c.JSON(ctx, http.MethodPatch, "/workflows/"+newID, workspaceID, map[string]any{
			"alias":  workflowCutoverAlias(desiredAlias, "failed", newID),
			"status": "offline",
		}, nil)
	}
	_, restoreErr := c.JSON(ctx, http.MethodPatch, "/workflows/"+oldID, workspaceID, map[string]any{
		"alias":  originalAlias,
		"status": originalStatus,
	}, nil)
	if quarantineErr != nil && restoreErr != nil {
		return fmt.Errorf("quarantine replacement: %v; restore original: %w", quarantineErr, restoreErr)
	}
	if quarantineErr != nil {
		return fmt.Errorf("quarantine replacement: %w", quarantineErr)
	}
	return restoreErr
}

func workflowCutoverAlias(alias, state, workflowID string) string {
	digest := sha256.Sum256([]byte(workflowID))
	const suffixBudget = 24
	if len(alias) > 100-suffixBudget {
		alias = alias[:100-suffixBudget]
	}
	return fmt.Sprintf("%s__%s_%x", alias, state, digest[:6])
}

func findWorkflowByAlias(ctx context.Context, c *Client, workspaceID, alias string) (string, error) {
	var page struct {
		Items []map[string]any `json:"items"`
	}
	if _, err := c.JSON(ctx, http.MethodGet, "/workflows?limit=0", workspaceID, nil, &page); err != nil {
		return "", err
	}
	for _, workflow := range page.Items {
		if workflow["alias"] == alias {
			return responseID(workflow), nil
		}
	}
	return "", nil
}

func workflowRead(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	c, err := client(meta)
	if err != nil {
		return diag.FromErr(err)
	}
	var out map[string]any
	status, err := c.JSON(ctx, http.MethodGet, "/workflows/"+d.Id(), d.Get("workspace_id").(string), nil, &out)
	if status == http.StatusNotFound {
		d.SetId("")
		return nil
	}
	if err != nil {
		return diag.FromErr(err)
	}
	if value, ok := out["alias"].(string); ok {
		_ = d.Set("alias", value)
	}
	if value, ok := out["title"].(string); ok {
		_ = d.Set("title", value)
	}
	if value, ok := out["status"].(string); ok {
		_ = d.Set("status", value)
	}
	if value, ok := out["version"].(float64); ok {
		_ = d.Set("version", int(value))
	}
	desiredYAML := []byte(d.Get("yaml").(string))
	desiredDefinition, err := workflowDefinitionFromYAML(desiredYAML)
	if err != nil {
		return diag.FromErr(err)
	}
	remoteDefinition, found, err := remoteWorkflowDefinition(ctx, c, d.Get("workspace_id").(string), d.Id())
	if err != nil {
		return diag.FromErr(err)
	}
	fingerprint := workflowSourceFingerprint(desiredYAML)
	if !found || !workflowValueMatches(desiredDefinition, remoteDefinition) {
		fingerprint = workflowDriftFingerprint(remoteDefinition, found)
	}
	if err := d.Set("sha256", fingerprint); err != nil {
		return diag.FromErr(err)
	}
	return nil
}

func workflowUpdate(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	c, err := client(meta)
	if err != nil {
		return diag.FromErr(err)
	}
	if d.HasChange("yaml") || d.HasChange("alias") || d.HasChange("filename") {
		id, err := ensureWorkflowDefinition(ctx, c, d, []byte(d.Get("yaml").(string)))
		if err != nil {
			return diag.FromErr(err)
		}
		d.SetId(id)
	} else if d.HasChange("status") {
		if _, err := c.JSON(ctx, http.MethodPatch, "/workflows/"+d.Id(), d.Get("workspace_id").(string), map[string]any{"status": d.Get("status")}, nil); err != nil {
			return diag.FromErr(err)
		}
	}
	return workflowRead(ctx, d, meta)
}

func workflowDelete(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	return jsonDelete("/workflows")(ctx, d, meta)
}
