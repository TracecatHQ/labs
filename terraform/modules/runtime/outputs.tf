output "table_ids" {
  value = { for name, table in tracecat_table.platform : name => table.id }
}

output "workflow_ids" {
  value = { for alias, workflow in tracecat_workflow.shared : alias => workflow.id }
}
