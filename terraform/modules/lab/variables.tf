variable "lab_id" {
  type = string
}

variable "workspace_id" {
  type        = string
  description = "Existing Tracecat workspace in which to provision the lab."
}

variable "config_dir" {
  type = string
}

variable "candidate_model" {
  type = object({ provider = string, name = string, catalog_id = optional(string) })
  default = {
    provider = "openai"
    name     = "gpt-5.6-terra"
  }
}

variable "judge_model" {
  type = object({ provider = string, name = string, catalog_id = optional(string) })
  default = {
    provider = "openai"
    name     = "gpt-5.6-sol"
  }
}

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
