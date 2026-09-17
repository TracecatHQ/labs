terraform {
  required_version = ">= 1.11.0"
  required_providers {
    tracecat = { source = "tracecathq/tracecat", version = "0.1.0" }
  }
}

provider "tracecat" {}

locals {
  labs = {
    "001" = "The Bigger Interview"
    "002" = "BOTSv3"
    "003" = "Firewall Mitigation"
    "004" = "Cyber Defense Benchmark"
    "005" = "SecRL"
    "006" = "CTI-REALM Sigma"
    "007" = "SEvenLLM MCQ"
  }
  workflow_tag_colors = {
    "run-first"     = "#16A34A"
    "modify"        = "#D97706"
    "run-second"    = "#2563EB"
    "internal"      = "#64748B"
    "agent-tool"    = "#0891B2"
    "scorer"        = "#DC2626"
    "judge-tool"    = "#9333EA"
    "utility"       = "#475569"
    "deterministic" = "#0F766E"
    "model"         = "#7C3AED"
    "hybrid"        = "#C2410C"
  }
  agent_tag_colors = {
    "agent-under-test" = "#16A34A"
    "judge"            = "#7C3AED"
  }
}

resource "tracecat_tag" "labs" {
  for_each     = local.workflow_tag_colors
  workspace_id = var.workspace_id
  name         = each.key
  color        = each.value
}

resource "tracecat_agent_tag" "labs" {
  for_each     = local.agent_tag_colors
  workspace_id = var.workspace_id
  name         = each.key
  color        = each.value
}

resource "tracecat_workflow_folder" "shared" {
  workspace_id = var.workspace_id
  name         = "Shared evaluation runtime"
}

resource "tracecat_workflow_folder" "lab" {
  for_each     = local.labs
  workspace_id = var.workspace_id
  name         = "Lab ${each.key} - ${each.value}"
}

resource "tracecat_workflow_folder" "agent_tools" {
  for_each     = local.labs
  workspace_id = var.workspace_id
  name         = "Agent tools"
  parent_path  = tracecat_workflow_folder.lab[each.key].path
}

resource "tracecat_workflow_folder" "scoring" {
  for_each     = local.labs
  workspace_id = var.workspace_id
  name         = "Scoring"
  parent_path  = tracecat_workflow_folder.lab[each.key].path
}

resource "tracecat_workflow_folder" "utilities" {
  for_each     = local.labs
  workspace_id = var.workspace_id
  name         = "Utilities"
  parent_path  = tracecat_workflow_folder.lab[each.key].path
}

resource "tracecat_agent_folder" "lab" {
  for_each     = local.labs
  workspace_id = var.workspace_id
  name         = "Lab ${each.key} - ${each.value}"
}

module "runtime" {
  source               = "../modules/runtime"
  workspace_id         = var.workspace_id
  workflow_folder_path = tracecat_workflow_folder.shared.path
  internal_tag_id      = tracecat_tag.labs["internal"].id
}

module "lab" {
  for_each               = local.labs
  source                 = "../modules/lab"
  lab_id                 = each.key
  workspace_id           = var.workspace_id
  config_dir             = "${path.root}/../../${each.key}/tracecat"
  case_template_table_id = module.runtime.table_ids["case_templates"]
  candidate_model        = var.candidate_model
  judge_model            = var.judge_model
  secret_values          = var.secret_values
  mcp_credentials        = var.mcp_credentials
  agent_folder_path      = tracecat_agent_folder.lab[each.key].path
  workflow_folder_paths = {
    root       = tracecat_workflow_folder.lab[each.key].path
    agent_tool = tracecat_workflow_folder.agent_tools[each.key].path
    scorer     = tracecat_workflow_folder.scoring[each.key].path
    judge_tool = tracecat_workflow_folder.scoring[each.key].path
    utility    = tracecat_workflow_folder.utilities[each.key].path
  }
  tag_ids       = { for name, tag in tracecat_tag.labs : name => tag.id }
  agent_tag_ids = { for name, tag in tracecat_agent_tag.labs : name => tag.id }
  depends_on    = [module.runtime]
}
