variable "lab_id" { type = string }
variable "workspace_id" { type = string }
variable "config_dir" { type = string }
variable "case_template_table_id" { type = string }
variable "workflow_folder_paths" { type = map(string) }
variable "agent_folder_path" { type = string }
variable "tag_ids" { type = map(string) }
variable "agent_tag_ids" { type = map(string) }
variable "candidate_model" { type = object({ provider = string, name = string, catalog_id = optional(string) }) }
variable "judge_model" { type = object({ provider = string, name = string, catalog_id = optional(string) }) }
variable "secret_values" {
  type      = map(map(string))
  sensitive = true
  ephemeral = true
  default   = {}
}
variable "mcp_credentials" {
  type      = map(map(string))
  sensitive = true
  ephemeral = true
  default   = {}
}
