output "workspace_id" {
  value = var.workspace_id
}

output "workflow_ids" {
  value = { for alias, workflow in tracecat_workflow.workflow : alias => workflow.id }
}

output "agent_preset_ids" {
  value = { for slug, preset in tracecat_agent_preset.preset : slug => preset.id }
}
