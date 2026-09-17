terraform {
  required_version = ">= 1.11.0"
  required_providers {
    tracecat = { source = "tracecathq/tracecat", version = "0.1.0" }
  }
}

locals {
  workflows = {
    run_trial_batch  = { file = "run-trial-batch.yml" }
    run_candidate    = { file = "run-candidate.yml" }
    candidate_trial  = { file = "candidate-trial.yml" }
    run_evaluation   = { file = "run-evaluation.yml" }
    score_with_judge = { file = "score-with-judge.yml" }
    scoring_trial    = { file = "scoring-trial.yml" }
    judge_run        = { file = "judge-run.yml" }
  }
  table_columns = {
    case_templates = [
      { name = "template_key", type = "TEXT", nullable = false, is_index = true },
      { name = "schema_version", type = "INTEGER", nullable = false },
      { name = "lab_id", type = "TEXT", nullable = false },
      { name = "case_id", type = "TEXT", nullable = false },
      { name = "case", type = "JSONB", nullable = false },
      { name = "oracle", type = "JSONB", nullable = false },
      { name = "profile", type = "JSONB", nullable = false },
      { name = "profile_id", type = "TEXT", nullable = false },
      { name = "profile_version", type = "INTEGER", nullable = false },
      { name = "enabled", type = "BOOLEAN", nullable = false }
    ]
    evaluation_runs = [
      { name = "evaluation_run_id", type = "TEXT", nullable = false, is_index = true },
      { name = "schema_version", type = "INTEGER", nullable = false },
      { name = "lab_id", type = "TEXT", nullable = false },
      { name = "profile_id", type = "TEXT", nullable = false },
      { name = "profile_version", type = "INTEGER", nullable = false },
      { name = "profile", type = "JSONB", nullable = false },
      { name = "selected_case_ids", type = "JSONB", nullable = false },
      { name = "template_digest", type = "TEXT", nullable = false },
      { name = "status", type = "TEXT", nullable = false },
      { name = "expected_trial_count", type = "INTEGER", nullable = false },
      { name = "completed_trial_count", type = "INTEGER", nullable = false },
      { name = "batch_size", type = "INTEGER", nullable = false },
    ]
    evaluation_trials = [
      { name = "trial_key", type = "TEXT", nullable = false, is_index = true },
      { name = "schema_version", type = "INTEGER", nullable = false },
      { name = "lab_id", type = "TEXT", nullable = false },
      { name = "evaluation_run_id", type = "TEXT", nullable = false },
      { name = "trial_id", type = "TEXT", nullable = false },
      { name = "case_id", type = "TEXT", nullable = false },
      { name = "trial_number", type = "INTEGER", nullable = false },
      { name = "status", type = "TEXT", nullable = false },
      { name = "trial", type = "JSONB", nullable = false }
    ]
    evaluation_evidence = [
      { name = "evidence_key", type = "TEXT", nullable = false, is_index = true },
      { name = "schema_version", type = "INTEGER", nullable = false },
      { name = "lab_id", type = "TEXT", nullable = false },
      { name = "evaluation_run_id", type = "TEXT", nullable = false },
      { name = "trial_id", type = "TEXT", nullable = false },
      { name = "sequence", type = "INTEGER", nullable = false },
      { name = "evidence_type", type = "TEXT", nullable = false },
      { name = "evidence", type = "JSONB", nullable = false }
    ]
    scoring_attempts = [
      { name = "attempt_key", type = "TEXT", nullable = false, is_index = true },
      { name = "schema_version", type = "INTEGER", nullable = false },
      { name = "lab_id", type = "TEXT", nullable = false },
      { name = "evaluation_run_id", type = "TEXT", nullable = false },
      { name = "trial_id", type = "TEXT", nullable = false },
      { name = "scoring_run_execution_id", type = "TEXT", nullable = false },
      { name = "scoring_attempt_id", type = "TEXT", nullable = false },
      { name = "scorer_kind", type = "TEXT", nullable = false },
      { name = "scorer_workflow_alias", type = "TEXT", nullable = false },
      { name = "judge_session_id", type = "TEXT", nullable = true },
      { name = "status", type = "TEXT", nullable = false },
      { name = "error", type = "TEXT", nullable = true },
      { name = "started_at", type = "TIMESTAMPTZ", nullable = false },
      { name = "completed_at", type = "TIMESTAMPTZ", nullable = true },
      { name = "scorer_latency_seconds", type = "NUMERIC", nullable = true }
    ]
    evaluation_scores = [
      { name = "score_key", type = "TEXT", nullable = false, is_index = true },
      { name = "schema_version", type = "INTEGER", nullable = false },
      { name = "lab_id", type = "TEXT", nullable = false },
      { name = "evaluation_run_id", type = "TEXT", nullable = false },
      { name = "trial_id", type = "TEXT", nullable = false },
      { name = "case_id", type = "TEXT", nullable = false },
      { name = "trial_number", type = "INTEGER", nullable = false },
      { name = "candidate_session_id", type = "TEXT", nullable = false },
      { name = "scoring_run_execution_id", type = "TEXT", nullable = false },
      { name = "scoring_attempt_id", type = "TEXT", nullable = false },
      { name = "judge_session_id", type = "TEXT", nullable = true },
      { name = "profile_id", type = "TEXT", nullable = false },
      { name = "profile_version", type = "INTEGER", nullable = false },
      { name = "scorer_kind", type = "TEXT", nullable = false },
      { name = "scorer_workflow_alias", type = "TEXT", nullable = false },
      { name = "criterion_id", type = "TEXT", nullable = false },
      { name = "criterion_type", type = "TEXT", nullable = false },
      { name = "criterion_weight", type = "NUMERIC", nullable = false },
      { name = "criterion_value", type = "NUMERIC", nullable = false },
      { name = "criterion_points", type = "NUMERIC", nullable = false },
      { name = "criterion_hard_gate", type = "BOOLEAN", nullable = false },
      { name = "criterion_passed", type = "BOOLEAN", nullable = false },
      { name = "trial_hard_failed", type = "BOOLEAN", nullable = false },
      { name = "trial_score", type = "NUMERIC", nullable = false },
      { name = "reason", type = "TEXT", nullable = false },
      { name = "evidence_refs", type = "JSONB", nullable = false },
      { name = "candidate_completed_at", type = "TIMESTAMPTZ", nullable = false },
      { name = "scored_at", type = "TIMESTAMPTZ", nullable = false }
    ]
    evaluation_metrics = [
      { name = "metric_key", type = "TEXT", nullable = false, is_index = true },
      { name = "schema_version", type = "INTEGER", nullable = false },
      { name = "lab_id", type = "TEXT", nullable = false },
      { name = "evaluation_run_id", type = "TEXT", nullable = false },
      { name = "trial_id", type = "TEXT", nullable = false },
      { name = "case_id", type = "TEXT", nullable = false },
      { name = "trial_number", type = "INTEGER", nullable = false },
      { name = "scoring_run_execution_id", type = "TEXT", nullable = false },
      { name = "scoring_attempt_id", type = "TEXT", nullable = false },
      { name = "profile_id", type = "TEXT", nullable = false },
      { name = "profile_version", type = "INTEGER", nullable = false },
      { name = "metric_id", type = "TEXT", nullable = false },
      { name = "metric_type", type = "TEXT", nullable = false },
      { name = "value", type = "JSONB", nullable = false },
      { name = "expected_value", type = "JSONB", nullable = true },
      { name = "scored_at", type = "TIMESTAMPTZ", nullable = false }
    ]
  }
}

resource "tracecat_table" "platform" {
  for_each     = local.table_columns
  workspace_id = var.workspace_id
  name         = each.key
  columns_json = jsonencode(each.value)
}

resource "tracecat_workflow" "shared" {
  for_each     = local.workflows
  workspace_id = var.workspace_id
  filename     = each.value.file
  alias        = each.key
  yaml         = file("${path.module}/workflows/${each.value.file}")
  sha256       = sha256(file("${path.module}/workflows/${each.value.file}"))

  lifecycle {
    create_before_destroy = true
  }
}

resource "tracecat_workflow_folder_membership" "shared" {
  for_each     = tracecat_workflow.shared
  workspace_id = var.workspace_id
  workflow_id  = each.value.id
  folder_path  = var.workflow_folder_path
}

resource "tracecat_workflow_tag" "internal" {
  for_each     = tracecat_workflow.shared
  workspace_id = var.workspace_id
  workflow_id  = each.value.id
  tag_id       = var.internal_tag_id
}
