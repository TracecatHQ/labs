package tracecat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func resourceJSON(endpoint string) *schema.Resource {
	return &schema.Resource{
		Description:   "A granular Tracecat REST resource represented by canonical JSON.",
		CreateContext: jsonCreate(endpoint),
		ReadContext:   jsonRead(endpoint),
		UpdateContext: jsonUpdate(endpoint),
		DeleteContext: jsonDelete(endpoint),
		Schema: map[string]*schema.Schema{
			"workspace_id": workspaceSchema(),
			"config_json": {
				Type:             schema.TypeString,
				Required:         true,
				DiffSuppressFunc: suppressEquivalentJSON,
			},
		},
	}
}

func resourceJSONByKey(endpoint, keyField, lookupPath string) *schema.Resource {
	resource := resourceJSON(endpoint)
	resource.CreateContext = jsonCreateByKey(endpoint, keyField, lookupPath)
	return resource
}

func decodeObject(raw string) (map[string]any, error) {
	var value map[string]any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil, fmt.Errorf("config_json must be a JSON object: %w", err)
	}
	return value, nil
}

func writeOnlyString(d *schema.ResourceData, name string) (string, bool, error) {
	value, diags := d.GetRawConfigAt(cty.GetAttrPath(name))
	if diags.HasError() {
		return "", false, fmt.Errorf("read write-only argument %s: %s", name, diags[0].Detail)
	}
	if !value.IsKnown() {
		return "", false, fmt.Errorf("write-only argument %s is unknown during apply", name)
	}
	if value.IsNull() {
		return "", false, nil
	}
	if !value.Type().Equals(cty.String) {
		return "", false, fmt.Errorf("write-only argument %s must be a string", name)
	}
	return value.AsString(), true, nil
}

func canonicalSubset(remote, desired map[string]any) map[string]any {
	out := make(map[string]any, len(desired))
	for key := range desired {
		if remoteValue, exists := remote[key]; exists {
			out[key] = remoteValue
		}
	}
	return out
}

func canonicalJSON(value any) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func suppressEquivalentJSON(_ string, old, new string, _ *schema.ResourceData) bool {
	var a, b any
	if json.Unmarshal([]byte(old), &a) != nil || json.Unmarshal([]byte(new), &b) != nil {
		return false
	}
	return canonicalJSON(a) == canonicalJSON(b)
}

func jsonCreate(endpoint string) schema.CreateContextFunc {
	return func(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
		c, err := client(meta)
		if err != nil {
			return diag.FromErr(err)
		}
		payload, err := decodeObject(d.Get("config_json").(string))
		if err != nil {
			return diag.FromErr(err)
		}
		var out map[string]any
		_, err = c.JSON(ctx, http.MethodPost, endpoint, d.Get("workspace_id").(string), payload, &out)
		if err != nil {
			return diag.FromErr(err)
		}
		id := responseID(out)
		if id == "" {
			return diag.Errorf("Tracecat %s create response omitted id", endpoint)
		}
		d.SetId(id)
		return jsonRead(endpoint)(ctx, d, meta)
	}
}

func jsonCreateByKey(endpoint, keyField, lookupPath string) schema.CreateContextFunc {
	return func(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
		c, err := client(meta)
		if err != nil {
			return diag.FromErr(err)
		}
		payload, err := decodeObject(d.Get("config_json").(string))
		if err != nil {
			return diag.FromErr(err)
		}
		key, ok := payload[keyField].(string)
		if !ok || key == "" {
			return diag.Errorf("config_json must include a non-empty %q for natural-key reconciliation", keyField)
		}

		workspaceID := d.Get("workspace_id").(string)
		lookup := endpoint + lookupPath + url.PathEscape(key)
		var existing map[string]any
		status, err := c.JSON(ctx, http.MethodGet, lookup, workspaceID, nil, &existing)
		if err == nil {
			id := responseID(existing)
			if id == "" {
				return diag.Errorf("Tracecat %s lookup by %s omitted id", endpoint, keyField)
			}
			d.SetId(id)
			if _, err := c.JSON(ctx, http.MethodPatch, endpoint+"/"+id, workspaceID, payload, nil); err != nil {
				return diag.FromErr(err)
			}
			return jsonRead(endpoint)(ctx, d, meta)
		}
		if status != http.StatusNotFound {
			return diag.FromErr(err)
		}

		var out map[string]any
		status, err = c.JSON(ctx, http.MethodPost, endpoint, workspaceID, payload, &out)
		if err != nil {
			// Another state may have created the shared resource after our lookup.
			// Resolve that race by key and reconcile the winner.
			if status != http.StatusConflict {
				return diag.FromErr(err)
			}
			if _, lookupErr := c.JSON(ctx, http.MethodGet, lookup, workspaceID, nil, &existing); lookupErr != nil {
				return diag.FromErr(err)
			}
			out = existing
		}
		id := responseID(out)
		if id == "" {
			return diag.Errorf("Tracecat %s create response omitted id", endpoint)
		}
		d.SetId(id)
		if status == http.StatusConflict {
			if _, err := c.JSON(ctx, http.MethodPatch, endpoint+"/"+id, workspaceID, payload, nil); err != nil {
				return diag.FromErr(err)
			}
		}
		return jsonRead(endpoint)(ctx, d, meta)
	}
}

func jsonRead(endpoint string) schema.ReadContextFunc {
	return func(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
		c, err := client(meta)
		if err != nil {
			return diag.FromErr(err)
		}
		var out map[string]any
		status, err := c.JSON(ctx, http.MethodGet, endpoint+"/"+d.Id(), d.Get("workspace_id").(string), nil, &out)
		if status == http.StatusNotFound {
			d.SetId("")
			return nil
		}
		if err != nil {
			return diag.FromErr(err)
		}
		desired, err := decodeObject(d.Get("config_json").(string))
		if err != nil {
			return diag.FromErr(err)
		}
		if err := d.Set("config_json", canonicalJSON(canonicalSubset(out, desired))); err != nil {
			return diag.FromErr(err)
		}
		return nil
	}
}

func jsonUpdate(endpoint string) schema.UpdateContextFunc {
	return func(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
		c, err := client(meta)
		if err != nil {
			return diag.FromErr(err)
		}
		payload, err := decodeObject(d.Get("config_json").(string))
		if err != nil {
			return diag.FromErr(err)
		}
		_, err = c.JSON(ctx, http.MethodPatch, endpoint+"/"+d.Id(), d.Get("workspace_id").(string), payload, nil)
		if err != nil {
			return diag.FromErr(err)
		}
		return jsonRead(endpoint)(ctx, d, meta)
	}
}

func jsonDelete(endpoint string) schema.DeleteContextFunc {
	return func(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
		c, err := client(meta)
		if err != nil {
			return diag.FromErr(err)
		}
		status, err := c.JSON(ctx, http.MethodDelete, endpoint+"/"+d.Id(), d.Get("workspace_id").(string), nil, nil)
		if err != nil && status != http.StatusNotFound {
			return diag.FromErr(err)
		}
		d.SetId("")
		return nil
	}
}
