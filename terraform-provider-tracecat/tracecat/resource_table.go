package tracecat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// Tracecat initializes a workspace's table schema during table creation. The
// initialization is not concurrency-safe, so serialize creates within a
// provider process.
var tableCreateMu sync.Mutex

func resourceTable() *schema.Resource {
	return &schema.Resource{
		CreateContext: tableCreate,
		ReadContext:   tableRead,
		UpdateContext: tableUpdate,
		DeleteContext: tableDelete,
		Schema: map[string]*schema.Schema{
			"workspace_id": workspaceSchema(),
			"name":         {Type: schema.TypeString, Required: true},
			"columns_json": {Type: schema.TypeString, Required: true, DiffSuppressFunc: suppressEquivalentJSON},
		},
	}
}

func tableCreate(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	c, err := client(meta)
	if err != nil {
		return diag.FromErr(err)
	}
	var columns []map[string]any
	if err := json.Unmarshal([]byte(d.Get("columns_json").(string)), &columns); err != nil {
		return diag.Errorf("columns_json must be an array: %v", err)
	}
	createColumns := make([]map[string]any, 0, len(columns))
	for _, column := range columns {
		created := make(map[string]any, len(column))
		for key, value := range column {
			if key != "is_index" {
				created[key] = value
			}
		}
		createColumns = append(createColumns, created)
	}
	workspaceID := d.Get("workspace_id").(string)
	tableCreateMu.Lock()
	existingID, err := findTableByName(ctx, c, workspaceID, d.Get("name").(string))
	if err == nil && existingID == "" {
		status, createErr := c.JSON(ctx, http.MethodPost, "/tables", workspaceID, map[string]any{"name": d.Get("name"), "columns": createColumns}, nil)
		if createErr != nil && status != http.StatusConflict {
			err = createErr
		} else {
			for attempt := 0; attempt < 100; attempt++ {
				existingID, err = findTableByName(ctx, c, workspaceID, d.Get("name").(string))
				if err != nil || existingID != "" {
					break
				}
				time.Sleep(200 * time.Millisecond)
			}
		}
	}
	tableCreateMu.Unlock()
	if err != nil {
		return diag.FromErr(err)
	}
	d.SetId(existingID)
	if d.Id() == "" {
		return diag.Errorf("created table %q but could not resolve its id", d.Get("name"))
	}
	if err := reconcileTableColumns(ctx, c, d, columns); err != nil {
		return diag.FromErr(err)
	}
	if err := createTableIndexes(ctx, c, d, columns); err != nil {
		return diag.FromErr(err)
	}
	return tableRead(ctx, d, meta)
}

func findTableByName(ctx context.Context, c *Client, workspaceID, name string) (string, error) {
	var tables []map[string]any
	if _, err := c.JSON(ctx, http.MethodGet, "/tables", workspaceID, nil, &tables); err != nil {
		return "", err
	}
	for _, table := range tables {
		if table["name"] == name {
			return responseID(table), nil
		}
	}
	return "", nil
}

func reconcileTableColumns(ctx context.Context, c *Client, d *schema.ResourceData, desired []map[string]any) error {
	workspaceID := d.Get("workspace_id").(string)
	var table map[string]any
	if _, err := c.JSON(ctx, http.MethodGet, "/tables/"+d.Id(), workspaceID, nil, &table); err != nil {
		return err
	}
	columns, _ := table["columns"].([]any)
	byName := make(map[string]map[string]any, len(columns))
	for _, raw := range columns {
		if column, ok := raw.(map[string]any); ok {
			if name, ok := column["name"].(string); ok {
				byName[name] = column
			}
		}
	}

	for _, wanted := range desired {
		name, _ := wanted["name"].(string)
		if name == "" {
			return fmt.Errorf("table %q has a column without a name", d.Get("name"))
		}
		column := byName[name]
		if column == nil {
			createPayload := make(map[string]any, len(wanted))
			for key, value := range wanted {
				if key != "is_index" {
					createPayload[key] = value
				}
			}
			status, addErr := c.JSON(ctx, http.MethodPost, "/tables/"+d.Id()+"/columns", workspaceID, createPayload, nil)
			if addErr != nil && status != http.StatusConflict {
				return fmt.Errorf("add column %q to table %q: %w", name, d.Get("name"), addErr)
			}
			for attempt := 0; attempt < 50 && column == nil; attempt++ {
				var refreshed map[string]any
				if _, err := c.JSON(ctx, http.MethodGet, "/tables/"+d.Id(), workspaceID, nil, &refreshed); err != nil {
					return err
				}
				refreshedColumns, ok := refreshed["columns"].([]any)
				if !ok {
					return fmt.Errorf("table %q response omitted columns", d.Get("name"))
				}
				for _, raw := range refreshedColumns {
					candidate, ok := raw.(map[string]any)
					if ok && candidate["name"] == name {
						column = candidate
						break
					}
				}
				if column == nil {
					time.Sleep(100 * time.Millisecond)
				}
			}
			if column == nil {
				return fmt.Errorf("added column %q to table %q but could not resolve its id", name, d.Get("name"))
			}
		}

		patch := make(map[string]any)
		for key, value := range wanted {
			if key == "is_index" {
				continue
			}
			if remote, exists := column[key]; !exists || canonicalJSON(remote) != canonicalJSON(value) {
				patch[key] = value
			}
		}
		if len(patch) == 0 {
			continue
		}
		columnID := responseID(column)
		if columnID == "" {
			return fmt.Errorf("table %q column %q omitted id", d.Get("name"), name)
		}
		if _, err := c.JSON(ctx, http.MethodPatch, "/tables/"+d.Id()+"/columns/"+columnID, workspaceID, patch, nil); err != nil {
			return fmt.Errorf("reconcile column %q in table %q: %w", name, d.Get("name"), err)
		}
	}
	return nil
}

func createTableIndexes(ctx context.Context, c *Client, d *schema.ResourceData, desired []map[string]any) error {
	var table map[string]any
	if _, err := c.JSON(ctx, http.MethodGet, "/tables/"+d.Id(), d.Get("workspace_id").(string), nil, &table); err != nil {
		return err
	}
	columns, _ := table["columns"].([]any)
	for _, wanted := range desired {
		indexed, _ := wanted["is_index"].(bool)
		if !indexed {
			continue
		}
		name, _ := wanted["name"].(string)
		columnID := ""
		for _, raw := range columns {
			column, ok := raw.(map[string]any)
			if ok && column["name"] == name {
				columnID = responseID(column)
				break
			}
		}
		if columnID == "" {
			return fmt.Errorf("created table %q but could not resolve indexed column %q", d.Get("name"), name)
		}
		if _, err := c.JSON(ctx, http.MethodPatch, "/tables/"+d.Id()+"/columns/"+columnID, d.Get("workspace_id").(string), map[string]any{"is_index": true}, nil); err != nil {
			// Tracecat can create the PostgreSQL index before its API read model
			// returns an idempotency error. Accept it only when the requested
			// column is actually indexed; the conflicting relation may belong to
			// a renamed legacy table in the same PostgreSQL schema.
			if strings.Contains(err.Error(), "already exists") ||
				strings.Contains(err.Error(), "cannot have multiple unique indexes") {
				confirmed, confirmErr := tableColumnIsIndexed(ctx, c, d.Get("workspace_id").(string), d.Id(), name)
				if confirmErr == nil && confirmed {
					continue
				}
			}
			return err
		}
	}
	return nil
}

func tableColumnIsIndexed(ctx context.Context, c *Client, workspaceID, tableID, columnName string) (bool, error) {
	var table map[string]any
	if _, err := c.JSON(ctx, http.MethodGet, "/tables/"+tableID, workspaceID, nil, &table); err != nil {
		return false, err
	}
	columns, _ := table["columns"].([]any)
	for _, raw := range columns {
		column, ok := raw.(map[string]any)
		if !ok || column["name"] != columnName {
			continue
		}
		indexed, _ := column["is_index"].(bool)
		return indexed, nil
	}
	return false, nil
}

func tableRead(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	c, err := client(meta)
	if err != nil {
		return diag.FromErr(err)
	}
	var out map[string]any
	status, err := c.JSON(ctx, http.MethodGet, "/tables/"+d.Id(), d.Get("workspace_id").(string), nil, &out)
	if status == http.StatusNotFound {
		d.SetId("")
		return nil
	}
	if err != nil {
		return diag.FromErr(err)
	}
	if name, ok := out["name"].(string); ok {
		_ = d.Set("name", name)
	}
	if columns, ok := out["columns"].([]any); ok {
		var desired []map[string]any
		if err := json.Unmarshal([]byte(d.Get("columns_json").(string)), &desired); err != nil {
			return diag.FromErr(err)
		}
		var normalized []map[string]any
		for _, wanted := range desired {
			for _, raw := range columns {
				column, ok := raw.(map[string]any)
				if !ok || column["name"] != wanted["name"] {
					continue
				}
				normalized = append(normalized, canonicalSubset(column, wanted))
				break
			}
		}
		_ = d.Set("columns_json", canonicalJSON(normalized))
	}
	return nil
}

func tableUpdate(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	c, err := client(meta)
	if err != nil {
		return diag.FromErr(err)
	}
	if d.HasChange("name") {
		_, err = c.JSON(ctx, http.MethodPatch, "/tables/"+d.Id(), d.Get("workspace_id").(string), map[string]any{"name": d.Get("name")}, nil)
		if err != nil {
			return diag.FromErr(err)
		}
	}
	var columns []map[string]any
	if err := json.Unmarshal([]byte(d.Get("columns_json").(string)), &columns); err != nil {
		return diag.Errorf("columns_json must be an array: %v", err)
	}
	if err := reconcileTableColumns(ctx, c, d, columns); err != nil {
		return diag.FromErr(err)
	}
	if err := createTableIndexes(ctx, c, d, columns); err != nil {
		return diag.FromErr(err)
	}
	return tableRead(ctx, d, meta)
}

func tableDelete(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	return jsonDelete("/tables")(ctx, d, meta)
}
