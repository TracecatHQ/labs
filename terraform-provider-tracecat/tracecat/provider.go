package tracecat

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func Provider() *schema.Provider {
	p := &schema.Provider{
		Schema: map[string]*schema.Schema{
			"api_url": {
				Type:        schema.TypeString,
				Optional:    true,
				DefaultFunc: schema.EnvDefaultFunc("TRACECAT_API_URL", "http://localhost/api"),
			},
			"api_key": {
				Type:        schema.TypeString,
				Optional:    true,
				Sensitive:   true,
				DefaultFunc: schema.EnvDefaultFunc("TRACECAT_API_KEY", nil),
			},
		},
		ResourcesMap: map[string]*schema.Resource{
			"tracecat_workspace":                  resourceWorkspace(),
			"tracecat_workflow":                   resourceWorkflow(),
			"tracecat_agent_preset":               resourceJSONByKey("/agent/presets", "slug", "/by-slug/"),
			"tracecat_workflow_folder":            resourceFolder("/folders", false),
			"tracecat_agent_folder":               resourceFolder("/agent-folders", true),
			"tracecat_tag":                        resourceTag("/tags", false),
			"tracecat_agent_tag":                  resourceTag("/agent-tags", true),
			"tracecat_workflow_folder_membership": resourceFolderMembership("/workflows", "workflow_id"),
			"tracecat_agent_folder_membership":    resourceFolderMembership("/agent/presets", "preset_id"),
			"tracecat_workflow_tag":               resourceTagAssociation("/workflows", "workflow_id", false),
			"tracecat_agent_preset_tag":           resourceTagAssociation("/agent/presets", "preset_id", true),
			"tracecat_table":                      resourceTable(),
			"tracecat_table_row":                  resourceTableRow(),
			"tracecat_mcp_integration":            resourceMCPIntegration(),
			"tracecat_secret":                     resourceSecret(),
		},
	}
	p.ConfigureContextFunc = func(ctx context.Context, d *schema.ResourceData) (any, diag.Diagnostics) {
		client, err := NewClient(d.Get("api_url").(string), d.Get("api_key").(string))
		if err != nil {
			return nil, diag.FromErr(err)
		}
		return client, nil
	}
	return p
}

func client(meta any) (*Client, error) {
	c, ok := meta.(*Client)
	if !ok || c == nil {
		return nil, fmt.Errorf("Tracecat provider was not configured")
	}
	return c, nil
}

func workspaceSchema() *schema.Schema {
	return &schema.Schema{
		Type:     schema.TypeString,
		Required: true,
		ForceNew: true,
	}
}
