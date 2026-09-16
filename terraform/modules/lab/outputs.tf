output "workspace_id" {
  value = var.workspace_id
}

output "workflow_ids" {
  value = { for alias, workflow in tracecat_workflow.workflow : alias => workflow.id }
}

output "table_ids" {
  value = { for name, table in tracecat_table.platform : name => table.id }
}
