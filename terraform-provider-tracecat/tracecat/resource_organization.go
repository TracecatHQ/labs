package tracecat

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func resourceFolder(endpoint string, paginated bool) *schema.Resource {
	return &schema.Resource{
		CreateContext: folderCreate(endpoint, paginated),
		ReadContext:   folderRead(endpoint),
		UpdateContext: folderUpdate(endpoint),
		DeleteContext: folderDelete(endpoint),
		Schema: map[string]*schema.Schema{
			"workspace_id": workspaceSchema(),
			"name":         {Type: schema.TypeString, Required: true},
			"parent_path":  {Type: schema.TypeString, Optional: true, Default: "/", ForceNew: true},
			"path":         {Type: schema.TypeString, Computed: true},
		},
	}
}

func folderCreate(endpoint string, paginated bool) schema.CreateContextFunc {
	return func(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
		c, err := client(meta)
		if err != nil {
			return diag.FromErr(err)
		}
		workspaceID := d.Get("workspace_id").(string)
		name := d.Get("name").(string)
		parent := normalizeFolderPath(d.Get("parent_path").(string))
		folders, err := listFolders(ctx, c, endpoint, workspaceID, parent, paginated)
		if err != nil {
			return diag.FromErr(err)
		}
		for _, folder := range folders {
			if folder["name"] == name {
				if id := responseID(folder); id != "" {
					d.SetId(id)
					return folderRead(endpoint)(ctx, d, meta)
				}
			}
		}
		var out map[string]any
		status, err := c.JSON(ctx, http.MethodPost, endpoint, workspaceID, map[string]any{"name": name, "parent_path": parent}, &out)
		if err != nil {
			if status != http.StatusConflict {
				return diag.FromErr(err)
			}
			folders, err = listFolders(ctx, c, endpoint, workspaceID, parent, paginated)
			if err != nil {
				return diag.FromErr(err)
			}
			for _, folder := range folders {
				if folder["name"] == name {
					out = folder
					break
				}
			}
		}
		id := responseID(out)
		if id == "" {
			return diag.Errorf("Tracecat %s create response omitted id", endpoint)
		}
		d.SetId(id)
		return folderRead(endpoint)(ctx, d, meta)
	}
}

func listFolders(ctx context.Context, c *Client, endpoint, workspaceID, parent string, paginated bool) ([]map[string]any, error) {
	path := endpoint + "?parent_path=" + url.QueryEscape(parent)
	if paginated {
		path += "&limit=100"
		var page struct {
			Items []map[string]any `json:"items"`
		}
		_, err := c.JSON(ctx, http.MethodGet, path, workspaceID, nil, &page)
		return page.Items, err
	}
	var folders []map[string]any
	_, err := c.JSON(ctx, http.MethodGet, path, workspaceID, nil, &folders)
	return folders, err
}

func folderRead(endpoint string) schema.ReadContextFunc {
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
		for _, key := range []string{"name", "path"} {
			if value, ok := out[key]; ok {
				if err := d.Set(key, value); err != nil {
					return diag.FromErr(err)
				}
			}
		}
		return nil
	}
}

func folderUpdate(endpoint string) schema.UpdateContextFunc {
	return func(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
		c, err := client(meta)
		if err != nil {
			return diag.FromErr(err)
		}
		if d.HasChange("name") {
			if _, err := c.JSON(ctx, http.MethodPatch, endpoint+"/"+d.Id(), d.Get("workspace_id").(string), map[string]any{"name": d.Get("name")}, nil); err != nil {
				return diag.FromErr(err)
			}
		}
		return folderRead(endpoint)(ctx, d, meta)
	}
}

func folderDelete(endpoint string) schema.DeleteContextFunc {
	return func(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
		c, err := client(meta)
		if err != nil {
			return diag.FromErr(err)
		}
		status, err := c.JSON(ctx, http.MethodDelete, endpoint+"/"+d.Id(), d.Get("workspace_id").(string), map[string]any{"recursive": false}, nil)
		if err != nil && status != http.StatusNotFound {
			return diag.FromErr(err)
		}
		d.SetId("")
		return nil
	}
}

func normalizeFolderPath(path string) string {
	if path == "" || path == "/" {
		return "/"
	}
	return "/" + strings.Trim(path, "/")
}

func resourceTag(endpoint string, paginated bool) *schema.Resource {
	return &schema.Resource{
		CreateContext: tagCreate(endpoint, paginated), ReadContext: tagRead(endpoint), UpdateContext: tagUpdate(endpoint), DeleteContext: tagDelete(endpoint),
		Schema: map[string]*schema.Schema{
			"workspace_id": workspaceSchema(),
			"name":         {Type: schema.TypeString, Required: true},
			"color":        {Type: schema.TypeString, Optional: true},
			"ref":          {Type: schema.TypeString, Computed: true},
		},
	}
}

func tagCreate(endpoint string, paginated bool) schema.CreateContextFunc {
	return func(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
		c, err := client(meta)
		if err != nil {
			return diag.FromErr(err)
		}
		workspaceID := d.Get("workspace_id").(string)
		var tags []map[string]any
		if paginated {
			var page struct {
				Items []map[string]any `json:"items"`
			}
			if _, err := c.JSON(ctx, http.MethodGet, endpoint+"?limit=100", workspaceID, nil, &page); err != nil {
				return diag.FromErr(err)
			}
			tags = page.Items
		} else if _, err := c.JSON(ctx, http.MethodGet, endpoint, workspaceID, nil, &tags); err != nil {
			return diag.FromErr(err)
		}
		for _, tag := range tags {
			if tag["name"] == d.Get("name") {
				d.SetId(responseID(tag))
				return tagUpdate(endpoint)(ctx, d, meta)
			}
		}
		var out map[string]any
		if _, err := c.JSON(ctx, http.MethodPost, endpoint, workspaceID, map[string]any{"name": d.Get("name"), "color": emptyToNil(d.Get("color").(string))}, &out); err != nil {
			return diag.FromErr(err)
		}
		if id := responseID(out); id != "" {
			d.SetId(id)
		} else {
			return diag.Errorf("Tracecat tag create response omitted id")
		}
		return tagRead(endpoint)(ctx, d, meta)
	}
}

func tagRead(endpoint string) schema.ReadContextFunc {
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
		for _, key := range []string{"name", "color", "ref"} {
			if value, ok := out[key]; ok {
				if err := d.Set(key, value); err != nil {
					return diag.FromErr(err)
				}
			}
		}
		return nil
	}
}

func tagUpdate(endpoint string) schema.UpdateContextFunc {
	return func(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
		c, err := client(meta)
		if err != nil {
			return diag.FromErr(err)
		}
		if _, err := c.JSON(ctx, http.MethodPatch, endpoint+"/"+d.Id(), d.Get("workspace_id").(string), map[string]any{"name": d.Get("name"), "color": emptyToNil(d.Get("color").(string))}, nil); err != nil {
			return diag.FromErr(err)
		}
		return tagRead(endpoint)(ctx, d, meta)
	}
}

func tagDelete(endpoint string) schema.DeleteContextFunc {
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

func emptyToNil(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func resourceFolderMembership(endpoint, idField string) *schema.Resource {
	return &schema.Resource{
		CreateContext: folderMembershipUpsert(endpoint, idField),
		ReadContext:   folderMembershipRead(endpoint, idField),
		UpdateContext: func(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
			return folderMembershipUpsert(endpoint, idField)(ctx, d, meta)
		},
		DeleteContext: folderMembershipDelete(endpoint, idField),
		Schema: map[string]*schema.Schema{
			"workspace_id": workspaceSchema(),
			idField:        {Type: schema.TypeString, Required: true, ForceNew: true},
			"folder_path":  {Type: schema.TypeString, Required: true},
		},
	}
}

func folderMembershipUpsert(endpoint, idField string) schema.CreateContextFunc {
	return func(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
		c, err := client(meta)
		if err != nil {
			return diag.FromErr(err)
		}
		id := d.Get(idField).(string)
		if _, err := c.JSON(ctx, http.MethodPost, endpoint+"/"+id+"/move", d.Get("workspace_id").(string), map[string]any{"folder_path": normalizeFolderPath(d.Get("folder_path").(string))}, nil); err != nil {
			return diag.FromErr(err)
		}
		d.SetId(id)
		return folderMembershipRead(endpoint, idField)(ctx, d, meta)
	}
}

func folderMembershipRead(endpoint, idField string) schema.ReadContextFunc {
	return func(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
		c, err := client(meta)
		if err != nil {
			return diag.FromErr(err)
		}
		id := d.Get(idField).(string)
		var out map[string]any
		status, err := c.JSON(ctx, http.MethodGet, endpoint+"/"+id, d.Get("workspace_id").(string), nil, &out)
		if status == http.StatusNotFound {
			d.SetId("")
			return nil
		}
		if err != nil {
			return diag.FromErr(err)
		}
		desiredPath := normalizeFolderPath(d.Get("folder_path").(string))
		desiredFolderID := ""
		if desiredPath != "/" {
			folderEndpoint, paginated := "/folders", false
			if strings.HasPrefix(endpoint, "/agent/") {
				folderEndpoint, paginated = "/agent-folders", true
			}
			folderID, err := findFolderByPath(ctx, c, folderEndpoint, d.Get("workspace_id").(string), desiredPath, paginated)
			if err != nil {
				return diag.FromErr(err)
			}
			if folderID == "" {
				d.SetId("")
				return nil
			}
			desiredFolderID = folderID
		}
		actualFolderID, _ := out["folder_id"].(string)
		if actualFolderID != desiredFolderID {
			d.SetId("")
		}
		return nil
	}
}

func findFolderByPath(ctx context.Context, c *Client, endpoint, workspaceID, path string, paginated bool) (string, error) {
	path = normalizeFolderPath(path)
	lastSlash := strings.LastIndex(path, "/")
	parent := path[:lastSlash]
	if parent == "" {
		parent = "/"
	}
	name := path[lastSlash+1:]
	folders, err := listFolders(ctx, c, endpoint, workspaceID, parent, paginated)
	if err != nil {
		return "", err
	}
	for _, folder := range folders {
		if folder["name"] == name && normalizeFolderPath(fmt.Sprint(folder["path"])) == path {
			return responseID(folder), nil
		}
	}
	return "", nil
}

func folderMembershipDelete(endpoint, idField string) schema.DeleteContextFunc {
	return func(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
		c, err := client(meta)
		if err != nil {
			return diag.FromErr(err)
		}
		status, err := c.JSON(ctx, http.MethodPost, endpoint+"/"+d.Get(idField).(string)+"/move", d.Get("workspace_id").(string), map[string]any{"folder_path": nil}, nil)
		if err != nil && status != http.StatusNotFound {
			return diag.FromErr(err)
		}
		d.SetId("")
		return nil
	}
}

func resourceTagAssociation(endpoint, idField string, paginated bool) *schema.Resource {
	return &schema.Resource{
		CreateContext: tagAssociationCreate(endpoint, idField, paginated), ReadContext: tagAssociationRead(endpoint, idField, paginated), DeleteContext: tagAssociationDelete(endpoint, idField),
		Schema: map[string]*schema.Schema{
			"workspace_id": workspaceSchema(),
			idField:        {Type: schema.TypeString, Required: true, ForceNew: true},
			"tag_id":       {Type: schema.TypeString, Required: true, ForceNew: true},
		},
	}
}

func tagAssociationCreate(endpoint, idField string, paginated bool) schema.CreateContextFunc {
	return func(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
		c, err := client(meta)
		if err != nil {
			return diag.FromErr(err)
		}
		target, tag := d.Get(idField).(string), d.Get("tag_id").(string)
		status, err := c.JSON(ctx, http.MethodPost, endpoint+"/"+target+"/tags", d.Get("workspace_id").(string), map[string]any{"tag_id": tag}, nil)
		if err != nil && status != http.StatusConflict {
			return diag.FromErr(err)
		}
		d.SetId(fmt.Sprintf("%s:%s", target, tag))
		return tagAssociationRead(endpoint, idField, paginated)(ctx, d, meta)
	}
}

func tagAssociationRead(endpoint, idField string, paginated bool) schema.ReadContextFunc {
	return func(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
		c, err := client(meta)
		if err != nil {
			return diag.FromErr(err)
		}
		target, wanted := d.Get(idField).(string), d.Get("tag_id").(string)
		var tags []map[string]any
		if paginated {
			var page struct {
				Items []map[string]any `json:"items"`
			}
			status, err := c.JSON(ctx, http.MethodGet, endpoint+"/"+target+"/tags?limit=100", d.Get("workspace_id").(string), nil, &page)
			if status == http.StatusNotFound {
				d.SetId("")
				return nil
			}
			if err != nil {
				return diag.FromErr(err)
			}
			tags = page.Items
		} else {
			status, err := c.JSON(ctx, http.MethodGet, endpoint+"/"+target+"/tags", d.Get("workspace_id").(string), nil, &tags)
			if status == http.StatusNotFound {
				d.SetId("")
				return nil
			}
			if err != nil {
				return diag.FromErr(err)
			}
		}
		for _, tag := range tags {
			if responseID(tag) == wanted {
				return nil
			}
		}
		d.SetId("")
		return nil
	}
}

func tagAssociationDelete(endpoint, idField string) schema.DeleteContextFunc {
	return func(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
		c, err := client(meta)
		if err != nil {
			return diag.FromErr(err)
		}
		path := fmt.Sprintf("%s/%s/tags/%s", endpoint, d.Get(idField).(string), d.Get("tag_id").(string))
		status, err := c.JSON(ctx, http.MethodDelete, path, d.Get("workspace_id").(string), nil, nil)
		if err != nil && status != http.StatusNotFound {
			return diag.FromErr(err)
		}
		d.SetId("")
		return nil
	}
}
