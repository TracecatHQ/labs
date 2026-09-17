terraform {
  required_version = ">= 1.11.0"
  required_providers {
    tracecat = { source = "tracecathq/tracecat", version = "0.1.0" }
  }
}

locals {
  manifest     = jsondecode(file("${var.config_dir}/tracecat.json"))
  profile      = jsondecode(file("${var.config_dir}/../evals/rubric.json"))
  case_records = [for line in split("\n", trimspace(file("${var.config_dir}/../evals/cases.ndjson"))) : jsondecode(line) if trimspace(line) != ""]
  cases        = { for record in local.case_records : record.case_id => record }

  scorer_output_type = {
    type     = "object", additionalProperties = false
    required = ["evaluation_error", "criteria", "metrics"]
    properties = {
      evaluation_error       = { type = ["string", "null"] }
      judge_session_id       = { type = ["string", "null"] }
      scorer_latency_seconds = { type = ["number", "null"] }
      criteria = {
        type = "array", minItems = length(local.profile.criteria), maxItems = length(local.profile.criteria)
        items = {
          type     = "object", additionalProperties = false
          required = ["criterion_id", "value", "reason", "evidence_refs"]
          properties = {
            criterion_id  = { type = "string", enum = [for criterion in local.profile.criteria : criterion.criterion_id] }
            value         = { type = "number" }, reason = { type = "string" }
            evidence_refs = { type = "array", items = { type = "string" } }
          }
        }
      }
      metrics = {
        type = "array", minItems = length(local.profile.metrics), maxItems = length(local.profile.metrics)
        items = {
          type = "object", additionalProperties = false, required = ["metric_id", "value"]
          properties = {
            metric_id = { type = "string", enum = [for metric in local.profile.metrics : metric.metric_id] }
            value     = { type = ["string", "number", "boolean", "object", "array", "null"] }
          }
        }
      }
    }
  }

  presets = { for preset in local.manifest.agent_presets : preset.slug => preset }
  entrypoints = {
    "${var.lab_id}_run_evaluation" = {
      file = "${var.lab_id}-run-evaluation.yml"
      yaml = replace(replace(file("${path.module}/workflows/lab-run-evaluation.yml"), "__LAB_ID__", var.lab_id), "__CANDIDATE_WORKFLOW_ALIAS__", "${var.lab_id}_candidate")
      tags = ["run-first"]
    }
    "${var.lab_id}_candidate" = {
      file = "${var.lab_id}-candidate.yml"
      yaml = replace(replace(file("${path.module}/workflows/lab-candidate.yml"), "__LAB_ID__", var.lab_id), "__CANDIDATE_PRESET__", "${var.lab_id}-candidate")
      tags = ["modify"]
    }
    "${var.lab_id}_judge" = {
      file = "${var.lab_id}-judge.yml"
      yaml = replace(file("${path.module}/workflows/lab-judge.yml"), "__LAB_ID__", var.lab_id)
      tags = ["run-second"]
    }
  }
  custom_workflows = {
    for workflow in try(local.manifest.workflows, []) : workflow.alias => merge(workflow, {
      yaml = file("${var.config_dir}/${workflow.file}")
      tags = concat(["internal", replace(workflow.category, "_", "-")], workflow.category == "scorer" ? [local.profile.scorer.kind] : [])
    })
  }
  workflows = merge(local.entrypoints, local.custom_workflows)
  workflow_tags = merge([for alias, workflow in local.workflows : {
    for tag in workflow.tags : "${alias}:${tag}" => { alias = alias, tag = tag }
  }]...)
  integrations = { for integration in try(local.manifest.mcp_integrations, []) : integration.catalog_slug => integration }
  secrets      = { for secret in try(local.manifest.secrets, []) : secret.name => secret }
}

resource "tracecat_table_row" "case_template" {
  for_each        = local.cases
  workspace_id    = var.workspace_id
  table_id        = var.case_template_table_id
  identity_column = "template_key"
  identity_value  = "${var.lab_id}:${each.key}"
  upsert          = true
  data_json = jsonencode({
    template_key    = "${var.lab_id}:${each.key}", schema_version = each.value.schema_version,
    lab_id          = var.lab_id, case_id = each.key, case = each.value.case, oracle = each.value.oracle,
    profile         = local.profile, profile_id = local.profile.profile_id,
    profile_version = local.profile.profile_version, enabled = try(each.value.enabled, true)
  })
}

resource "tracecat_mcp_integration" "integration" {
  for_each               = local.integrations
  workspace_id           = var.workspace_id
  catalog_slug           = each.value.catalog_slug
  connection_option_id   = try(each.value.connection_option_id, null)
  name                   = try(each.value.name, null)
  description            = try(each.value.description, null)
  server_uri             = try(each.value.server_uri, null)
  auth_type              = try(each.value.auth_type, "CUSTOM")
  timeout                = try(each.value.timeout, 120)
  custom_headers_wo_json = contains(keys(var.mcp_credentials), each.key) ? jsonencode(var.mcp_credentials[each.key]) : null
  credentials_wo_version = try(each.value.credentials_version, 0)
}

resource "tracecat_secret" "secret" {
  for_each        = local.secrets
  workspace_id    = var.workspace_id
  name            = each.key
  description     = try(each.value.description, null)
  environment     = try(each.value.environment, "default")
  keys_wo_json    = jsonencode(var.secret_values[each.key])
  keys_wo_version = each.value.version
}

resource "tracecat_agent_preset" "preset" {
  for_each     = local.presets
  workspace_id = var.workspace_id
  config_json = jsonencode({ for key, value in merge(
    { for key, value in each.value : key => value if !contains(["instructions_file", "mcp_catalog_slugs"], key) },
    {
      instructions     = file("${var.config_dir}/${each.value.instructions_file}")
      model_provider   = endswith(each.key, "-candidate") ? var.candidate_model.provider : var.judge_model.provider
      model_name       = endswith(each.key, "-candidate") ? var.candidate_model.name : var.judge_model.name
      catalog_id       = endswith(each.key, "-candidate") ? var.candidate_model.catalog_id : var.judge_model.catalog_id
      mcp_integrations = [for slug in try(each.value.mcp_catalog_slugs, []) : tracecat_mcp_integration.integration[slug].id]
      output_type      = each.key == try(local.profile.scorer.judge_preset, null) ? local.scorer_output_type : null
    }
  ) : key => value if value != null })
}

resource "tracecat_agent_folder_membership" "preset" {
  for_each     = tracecat_agent_preset.preset
  workspace_id = var.workspace_id
  preset_id    = each.value.id
  folder_path  = var.agent_folder_path
}

resource "tracecat_agent_preset_tag" "candidate" {
  for_each     = { for slug, preset in tracecat_agent_preset.preset : slug => preset if endswith(slug, "-candidate") }
  workspace_id = var.workspace_id
  preset_id    = each.value.id
  tag_id       = var.agent_tag_ids["agent-under-test"]
}

resource "tracecat_agent_preset_tag" "judge" {
  for_each     = { for slug, preset in tracecat_agent_preset.preset : slug => preset if endswith(slug, "-judge") }
  workspace_id = var.workspace_id
  preset_id    = each.value.id
  tag_id       = var.agent_tag_ids["judge"]
}

resource "tracecat_workflow" "workflow" {
  for_each     = local.workflows
  workspace_id = var.workspace_id
  filename     = each.value.file
  alias        = each.key
  yaml         = each.value.yaml
  sha256       = sha256(each.value.yaml)
  depends_on   = [tracecat_agent_preset.preset, tracecat_table_row.case_template, tracecat_mcp_integration.integration, tracecat_secret.secret]

  lifecycle {
    create_before_destroy = true
  }
}

resource "tracecat_workflow_folder_membership" "workflow" {
  for_each     = tracecat_workflow.workflow
  workspace_id = var.workspace_id
  workflow_id  = each.value.id
  folder_path  = contains(keys(local.entrypoints), each.key) ? var.workflow_folder_paths.root : var.workflow_folder_paths[local.custom_workflows[each.key].category]
}

resource "tracecat_workflow_tag" "workflow" {
  for_each     = local.workflow_tags
  workspace_id = var.workspace_id
  workflow_id  = tracecat_workflow.workflow[each.value.alias].id
  tag_id       = var.tag_ids[each.value.tag]
}
