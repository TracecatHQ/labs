terraform {
  required_version = ">= 1.11.0"
  required_providers {
    tracecat = {
      source  = "tracecathq/tracecat"
      version = "0.1.0"
    }
  }
}

provider "tracecat" {}

variable "workspace_id" { type = string }

variable "candidate_model" {
  type = object({ provider = string, name = string })
  default = {
    provider = "openai"
    name     = "gpt-5.6-terra"
  }
}

variable "judge_model" {
  type = object({ provider = string, name = string })
  default = {
    provider = "openai"
    name     = "gpt-5.6-sol"
  }
}

module "lab" {
  source          = "../../terraform/modules/lab"
  lab_id          = "002"
  workspace_id    = var.workspace_id
  config_dir      = "${path.module}/../tracecat"
  candidate_model = var.candidate_model
  judge_model     = var.judge_model
}

output "workspace_id" { value = module.lab.workspace_id }
output "workflow_ids" { value = module.lab.workflow_ids }
output "table_ids" { value = module.lab.table_ids }
