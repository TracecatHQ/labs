output "workspace_id" { value = var.workspace_id }
output "table_ids" { value = module.runtime.table_ids }
output "shared_workflow_ids" { value = module.runtime.workflow_ids }
output "lab_workflow_ids" { value = { for lab, resources in module.lab : lab => resources.workflow_ids } }
output "agent_preset_ids" { value = { for lab, resources in module.lab : lab => resources.agent_preset_ids } }
